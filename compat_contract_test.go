//go:build compat

// Compat-test entry point for withings-export-cli.
//
// This file is only compiled under the `compat` build tag, so it does
// not affect the default `go test ./...` run. CI invokes it as
// `go test -tags=compat ./...` after building the export binary and
// exposing its path through WITHINGS_EXPORT_BIN.
//
// The actual assertions live in github.com/quantcli/common/compat.
// Drift between this CLI and CONTRACT.md surfaces as a failure here.
package main_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantcli/common/compat"
	"github.com/quantcli/common/compat/formats"
)

func TestContractFormats(t *testing.T) {
	bin := os.Getenv("WITHINGS_EXPORT_BIN")
	if bin == "" {
		t.Skip("WITHINGS_EXPORT_BIN not set; skipping compat suite")
	}

	// withings's data path requires a usable OAuth token plus a
	// reachable Withings API origin, neither of which a clean CI
	// runner has. We stand up a stub that returns the empty Withings
	// envelope to every POST and write a never-expired token into a
	// scratch HOME so auth.GetToken returns without touching the
	// network. The binary picks up the stub origin via
	// WITHINGS_API_BASE (added in internal/client) and the token via
	// the existing HOME-rooted config path.
	stub := newWithingsStub()
	t.Cleanup(stub.Close)
	home := writeFakeAuthToken(t)

	formats.RunContract(t, compat.Runner{
		Binary: bin,
		// withings is cobra-based: --format lives on each
		// data-producing subcommand. The compat suite dispatches per
		// subcommand under a `subcommand=NAME/...` subtree so any
		// regression surfaces as a named subtest failure rather than
		// masking the rest.
		Subcommands: []string{
			"activity",
			"intraday",
			"measurements",
			"sleep",
			"workouts",
		},
		Env: []string{
			"HOME=" + home,
			"PATH=/usr/bin:/bin",
			"TZ=UTC",
			"WITHINGS_API_BASE=" + stub.URL,
		},
	})
}

// newWithingsStub returns an httptest.Server that answers every POST
// with the Withings success envelope and an empty body. The Withings
// shape is `{"status":0,"body":{…}}`; an empty body satisfies each
// subcommand's response struct (slice fields stay nil, `more` stays
// false, the pagination loop exits on the first call), so every codec
// renders the empty-result form: `--format json` → `[]`, `--format csv`
// → a header line, default and `--format markdown` → byte-identical
// (empty) stdout. The stub does not validate the Authorization header —
// the fake token below is just a placeholder so the auth layer reaches
// the HTTP call rather than short-circuiting at `not logged in`.
func newWithingsStub() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":0,"body":{}}`))
	}))
}

// writeFakeAuthToken seeds a synthetic, never-expired token store at
// HOME/.config/withings-export/auth.json so auth.GetToken returns a
// usable string and the binary proceeds to the HTTP path. Returns the
// HOME directory to set in Runner.Env.
func writeFakeAuthToken(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "withings-export")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	token := map[string]any{
		"access_token":  "compat-fake-access-token",
		"refresh_token": "compat-fake-refresh-token",
		// Far enough in the future that auth.GetToken takes the
		// not-expired branch on every invocation.
		"expires_at":    time.Now().AddDate(10, 0, 0).Format(time.RFC3339),
		"user_id":       "0",
		"client_id":     "compat-fake-client-id",
		"client_secret": "compat-fake-client-secret",
	}
	body, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		t.Fatalf("marshal fake token: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), body, 0o600); err != nil {
		t.Fatalf("write auth.json: %v", err)
	}
	return home
}
