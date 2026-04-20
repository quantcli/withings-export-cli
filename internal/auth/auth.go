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
	UserID       string    `json:"user_id"`
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
//
// If callbackURL is empty, a random local port is used with a /callback path.
// Otherwise callbackURL is used verbatim as the redirect_uri sent to Withings,
// and the local server binds to the embedded http://localhost:PORT/PATH it
// contains. This supports the `https://redirectmeto.com/http://localhost:PORT/PATH`
// workaround for Withings apps that require an HTTPS callback.
func Login(ctx context.Context, clientID, clientSecret, callbackURL string) error {
	if clientID == "" || clientSecret == "" {
		return errors.New("client id and secret are required")
	}

	bindAddr, callbackPath, redirectURI, err := resolveCallback(callbackURL)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to bind local callback (%s): %w", bindAddr, err)
	}
	if callbackURL == "" {
		// Replace the 0 port with the actual port the kernel picked.
		port := listener.Addr().(*net.TCPAddr).Port
		redirectURI = fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)
	}

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
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
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
		UserID:       resp.UserID.String(),
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
	if resp.UserID.String() != "" {
		store.UserID = resp.UserID.String()
	}
	return save(store)
}

type tokenResponse struct {
	UserID       json.Number `json:"userid"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresIn    int         `json:"expires_in"`
	Scope        string      `json:"scope"`
	TokenType    string      `json:"token_type"`
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

// CallbackURLFromEnv returns the WITHINGS_CALLBACK_URL env var or "".
func CallbackURLFromEnv() string {
	return strings.TrimSpace(os.Getenv("WITHINGS_CALLBACK_URL"))
}

// resolveCallback decides where the local HTTP server binds, which path it
// listens on, and what redirect_uri to send to Withings.
//
// When callbackURL is empty: bind 127.0.0.1:0, path /callback, redirectURI
// filled in by the caller once the random port is known.
//
// When callbackURL is non-empty: search for an embedded http://localhost or
// http://127.0.0.1 URL (as used by the redirectmeto.com workaround). If found,
// the local server binds to its host:port and serves its path; the full
// callbackURL is passed verbatim as redirectURI. If the callbackURL itself
// is an http://localhost URL, it's used directly.
func resolveCallback(callbackURL string) (bindAddr, path, redirectURI string, err error) {
	if callbackURL == "" {
		return "127.0.0.1:0", "/callback", "", nil
	}

	local := extractLocalURL(callbackURL)
	if local == nil {
		return "", "", "", fmt.Errorf(
			"WITHINGS_CALLBACK_URL %q has no embedded http://localhost:PORT/PATH "+
				"— either use a localhost URL directly or wrap one via redirectmeto.com",
			callbackURL)
	}

	host := local.Host
	if !strings.Contains(host, ":") {
		return "", "", "", fmt.Errorf("callback URL %q must include an explicit port", local.String())
	}
	p := local.Path
	if p == "" {
		p = "/"
	}
	return host, p, callbackURL, nil
}

// extractLocalURL returns the last http://localhost or http://127.0.0.1 URL
// embedded in s, or nil if none found.
func extractLocalURL(s string) *url.URL {
	for _, marker := range []string{"http://localhost:", "http://127.0.0.1:"} {
		if idx := strings.LastIndex(s, marker); idx >= 0 {
			u, err := url.Parse(s[idx:])
			if err == nil && u.Host != "" {
				return u
			}
		}
	}
	return nil
}
