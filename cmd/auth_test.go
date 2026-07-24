package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// With WITHINGS_REFRESH_TOKEN set, 'auth status' reports the headless source
// and exits 0 without consulting the saved token file — that file may not
// exist at all in a container. Precedence is CONTRACT.md §5.
func TestAuthStatus_HeadlessEnvWins(t *testing.T) {
	t.Setenv("WITHINGS_REFRESH_TOKEN", "rt-from-env")
	t.Setenv("WITHINGS_CLIENT_ID", "cid")
	t.Setenv("WITHINGS_CLIENT_SECRET", "csecret")
	t.Setenv("HOME", t.TempDir()) // no auth.json anywhere

	var out bytes.Buffer
	statusCmd.SetOut(&out)
	t.Cleanup(func() { statusCmd.SetOut(nil) })

	if err := statusCmd.RunE(statusCmd, nil); err != nil {
		t.Fatalf("headless status should succeed with no saved token, got: %v", err)
	}
	if !strings.Contains(out.String(), "WITHINGS_REFRESH_TOKEN") {
		t.Errorf("status should name the token source, got: %q", out.String())
	}
}

// Withings needs client credentials to redeem a refresh token, so a refresh
// token alone is not a usable headless setup — say so rather than exiting 0
// and failing later at the first API call.
func TestAuthStatus_HeadlessMissingClientCreds(t *testing.T) {
	t.Setenv("WITHINGS_REFRESH_TOKEN", "rt-from-env")
	t.Setenv("WITHINGS_CLIENT_ID", "")
	t.Setenv("WITHINGS_CLIENT_SECRET", "")
	t.Setenv("HOME", t.TempDir())

	err := statusCmd.RunE(statusCmd, nil)
	if err == nil {
		t.Fatal("expected an error when client credentials are missing")
	}
	if !strings.Contains(err.Error(), "WITHINGS_CLIENT_ID") {
		t.Errorf("error should name the missing vars, got: %v", err)
	}
}

// Without the env var and without a saved token, status still fails and
// points at both recovery paths.
func TestAuthStatus_NoTokenFails(t *testing.T) {
	t.Setenv("WITHINGS_REFRESH_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	err := statusCmd.RunE(statusCmd, nil)
	if err == nil {
		t.Fatal("expected an error when no token is available")
	}
	if !strings.Contains(err.Error(), "WITHINGS_REFRESH_TOKEN") {
		t.Errorf("error should mention the headless option, got: %v", err)
	}
}
