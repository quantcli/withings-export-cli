package auth

import (
	"encoding/json"
	"testing"
	"time"
)

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
