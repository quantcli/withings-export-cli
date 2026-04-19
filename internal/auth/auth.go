package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	authURL  = "https://account.withings.com/oauth2_user/authorize2"
	tokenURL = "https://wbsapi.withings.net/v2/oauth2"
	// Scopes covered by the subcommands: measurements, activity, sleep, workouts.
	scope = "user.metrics,user.activity,user.sleepevents"
)

// TokenStore is persisted to ~/.config/withings-export/auth.json.
type TokenStore struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	UserID       int       `json:"user_id"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "withings-export", "auth.json")
}

// GetToken returns a valid access token, refreshing if needed.
func GetToken() (string, error) {
	store, err := load()
	if err != nil {
		return "", fmt.Errorf("not logged in — run: withings-export auth login")
	}
	if time.Now().After(store.ExpiresAt) {
		if err := refresh(store); err != nil {
			return "", fmt.Errorf("token refresh failed: %w", err)
		}
	}
	return store.AccessToken, nil
}

// Login runs the OAuth2 authorization-code flow against a local callback server.
// clientID and clientSecret are the developer credentials from dev.withings.com.
func Login(ctx context.Context, clientID, clientSecret string) error {
	if clientID == "" || clientSecret == "" {
		return errors.New("client id and secret are required")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to bind local callback port: %w", err)
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return err
	}
	state := hex.EncodeToString(stateBytes)

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			resultCh <- result{err: errors.New("state mismatch in callback")}
			return
		}
		if errMsg := q.Get("error"); errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			resultCh <- result{err: fmt.Errorf("authorization denied: %s", errMsg)}
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			resultCh <- result{err: errors.New("no code in callback")}
			return
		}
		fmt.Fprintln(w, "Withings authorization complete. You can close this tab.")
		resultCh <- result{code: code}
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	params := url.Values{
		"response_type": {"code"},
		"client_id":     {clientID},
		"scope":         {scope},
		"redirect_uri":  {redirectURI},
		"state":         {state},
	}
	authorizeURL := authURL + "?" + params.Encode()
	fmt.Println("Opening browser to authorize withings-export...")
	fmt.Println("If it doesn't open, visit this URL:")
	fmt.Println(authorizeURL)
	_ = openBrowser(authorizeURL)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return res.err
		}
		return exchangeCode(res.code, redirectURI, clientID, clientSecret)
	case <-time.After(5 * time.Minute):
		return errors.New("login timed out waiting for browser callback")
	}
}

// Logout removes stored tokens.
func Logout() error {
	err := os.Remove(configPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// exchangeCode trades an authorization code for access/refresh tokens.
func exchangeCode(code, redirectURI, clientID, clientSecret string) error {
	body := url.Values{
		"action":        {"requesttoken"},
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	var resp tokenResponse
	if err := postToken(body, &resp); err != nil {
		return err
	}
	store := &TokenStore{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).Add(-5 * time.Minute),
		UserID:       resp.UserID,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}
	return save(store)
}

// refresh exchanges the refresh token for a fresh access token. Updates store in place.
func refresh(store *TokenStore) error {
	body := url.Values{
		"action":        {"requesttoken"},
		"grant_type":    {"refresh_token"},
		"client_id":     {store.ClientID},
		"client_secret": {store.ClientSecret},
		"refresh_token": {store.RefreshToken},
	}
	var resp tokenResponse
	if err := postToken(body, &resp); err != nil {
		return err
	}
	store.AccessToken = resp.AccessToken
	store.RefreshToken = resp.RefreshToken
	store.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).Add(-5 * time.Minute)
	if resp.UserID != 0 {
		store.UserID = resp.UserID
	}
	return save(store)
}

type tokenResponse struct {
	UserID       int    `json:"userid"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

func postToken(form url.Values, out *tokenResponse) error {
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
		Error  string          `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}
	if envelope.Status != 0 {
		msg := envelope.Error
		if msg == "" {
			msg = string(envelope.Body)
		}
		return fmt.Errorf("token endpoint returned status %d: %s", envelope.Status, msg)
	}
	if err := json.Unmarshal(envelope.Body, out); err != nil {
		return fmt.Errorf("failed to parse token body: %w", err)
	}
	return nil
}

func save(store *TokenStore) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(store, "", "  ")
	return os.WriteFile(path, data, 0600)
}

func load() (*TokenStore, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil, err
	}
	var store TokenStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return &store, nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	return cmd.Start()
}

// CredentialsFromEnv reads client id/secret from WITHINGS_CLIENT_ID/WITHINGS_CLIENT_SECRET.
// Returns empty strings if unset.
func CredentialsFromEnv() (string, string) {
	return strings.TrimSpace(os.Getenv("WITHINGS_CLIENT_ID")),
		strings.TrimSpace(os.Getenv("WITHINGS_CLIENT_SECRET"))
}
