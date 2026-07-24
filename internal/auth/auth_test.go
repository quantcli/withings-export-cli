package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubTokenEndpoint points the token exchange at a local server returning a
// well-formed Withings envelope, so refresh paths are testable without
// rotating (and thereby invalidating) a real refresh token.
func stubTokenEndpoint(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":0,"body":{"userid":"12345",
			"access_token":"at-new","refresh_token":"rt-rotated","expires_in":10800}}`))
	}))
	t.Cleanup(srv.Close)

	orig := tokenURL
	tokenURL = srv.URL
	t.Cleanup(func() { tokenURL = orig })
}

// A minted access token is usable whether or not the cache write lands, so an
// unwritable token file must not fail the export — read-only rootfs and a
// container with no writable HOME are the environments the headless path
// exists to serve. CONTRACT.md §5; the warning goes to stderr per §4.
func TestGetToken_HeadlessSurvivesUnwritableTokenFile(t *testing.T) {
	stubTokenEndpoint(t)

	home := t.TempDir()
	// Create the config dir, then drop write permission: save()'s MkdirAll then
	// succeeds on the existing dir and WriteFile is what fails — the same shape
	// as a read-only rootfs.
	cfg := filepath.Join(home, ".config", "withings-export")
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cfg, 0o700) }) // let TempDir clean up
	if err := os.WriteFile(filepath.Join(cfg, "probe"), nil, 0o600); err == nil {
		t.Skip("filesystem permissions not enforced (running as root?)")
	}
	t.Setenv("HOME", home)

	t.Setenv("WITHINGS_REFRESH_TOKEN", "rt-injected")
	t.Setenv("WITHINGS_CLIENT_ID", "cid")
	t.Setenv("WITHINGS_CLIENT_SECRET", "csecret")

	tok, err := GetToken()
	if err != nil {
		t.Fatalf("headless GetToken must succeed when the token file is unwritable, got: %v", err)
	}
	if tok != "at-new" {
		t.Errorf("GetToken = %q, want the freshly minted %q", tok, "at-new")
	}
}

// The env var wins over a saved token file (CONTRACT.md §5): a container with
// a stale mounted config and a freshly injected secret must use the secret.
func TestGetToken_EnvWinsOverSavedToken(t *testing.T) {
	stubTokenEndpoint(t)

	t.Setenv("HOME", t.TempDir())
	if err := save(&TokenStore{
		AccessToken:  "at-from-file",
		RefreshToken: "rt-from-file",
		ExpiresAt:    time.Now().Add(time.Hour), // valid: would be used if the file won
		ClientID:     "cid",
		ClientSecret: "csecret",
	}); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WITHINGS_REFRESH_TOKEN", "rt-injected")
	t.Setenv("WITHINGS_CLIENT_ID", "cid")
	t.Setenv("WITHINGS_CLIENT_SECRET", "csecret")

	tok, err := GetToken()
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if tok == "at-from-file" {
		t.Fatal("saved token was used; WITHINGS_REFRESH_TOKEN must take precedence")
	}
	if tok != "at-new" {
		t.Errorf("GetToken = %q, want the env-minted %q", tok, "at-new")
	}

	// Withings rotates on every refresh, so the rotated value must reach disk
	// for a repeat caller to re-inject it.
	saved, err := load()
	if err != nil {
		t.Fatalf("load after refresh: %v", err)
	}
	if saved.RefreshToken != "rt-rotated" {
		t.Errorf("persisted refresh token = %q, want the rotated %q", saved.RefreshToken, "rt-rotated")
	}
}

// envStore is the headless auth path (CONTRACT.md §5): build a TokenStore from
// WITHINGS_REFRESH_TOKEN + client creds, with an already-elapsed ExpiresAt so
// GetToken mints an access token before first use.
func TestEnvStore(t *testing.T) {
	t.Setenv("WITHINGS_REFRESH_TOKEN", "  rt-abc  ")
	t.Setenv("WITHINGS_CLIENT_ID", "cid")
	t.Setenv("WITHINGS_CLIENT_SECRET", "csecret")

	if got := EnvRefreshToken(); got != "rt-abc" {
		t.Fatalf("EnvRefreshToken = %q, want %q (trimmed)", got, "rt-abc")
	}
	s, err := envStore()
	if err != nil {
		t.Fatalf("envStore: %v", err)
	}
	if s.RefreshToken != "rt-abc" || s.ClientID != "cid" || s.ClientSecret != "csecret" {
		t.Fatalf("envStore = %+v, want rt/cid/csecret", s)
	}
	if !time.Now().After(s.ExpiresAt) {
		t.Fatal("ExpiresAt should be already-elapsed so GetToken refreshes on first use")
	}
}

func TestEnvStore_Errors(t *testing.T) {
	t.Run("no refresh token", func(t *testing.T) {
		t.Setenv("WITHINGS_REFRESH_TOKEN", "")
		if _, err := envStore(); err == nil {
			t.Fatal("want error when WITHINGS_REFRESH_TOKEN unset")
		}
	})
	t.Run("refresh token but no client creds", func(t *testing.T) {
		t.Setenv("WITHINGS_REFRESH_TOKEN", "rt")
		t.Setenv("WITHINGS_CLIENT_ID", "")
		t.Setenv("WITHINGS_CLIENT_SECRET", "")
		if _, err := envStore(); err == nil {
			t.Fatal("want error when client creds missing")
		}
	})
}

// Withings returns userid as a JSON string on the initial authorization_code
// grant and as a JSON number on the refresh_token grant. tokenResponse.UserID
// must unmarshal both without error — json.Number accepts either form.
func TestTokenResponse_UserIDUnmarshalsStringAndNumber(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"string form (initial login)", `{"userid":"12345","access_token":"a","refresh_token":"r","expires_in":10800,"scope":"s","token_type":"Bearer"}`},
		{"number form (refresh)", `{"userid":12345,"access_token":"a","refresh_token":"r","expires_in":10800,"scope":"s","token_type":"Bearer"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp tokenResponse
			if err := json.Unmarshal([]byte(tc.body), &resp); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if resp.UserID.String() != "12345" {
				t.Fatalf("UserID = %q, want %q", resp.UserID.String(), "12345")
			}
		})
	}
}
