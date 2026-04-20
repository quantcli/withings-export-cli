package auth

import (
	"encoding/json"
	"testing"
)

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
