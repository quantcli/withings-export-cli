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
//
// These are the black-box checks: they exec the binary CI ships. The
// codec-parity assertions in cmd/parity_test.go are in-process and run
// under the plain `go test ./...` job; the two are complementary, and
// both drive the same internal/stubapi fixtures.
package main_test

import (
	"os"
	"testing"

	"github.com/quantcli/common/compat"
	"github.com/quantcli/common/compat/dates"
	"github.com/quantcli/common/compat/formats"
	"github.com/quantcli/withings-export-cli/internal/stubapi"
)

// withingsSubcommands is every data-producing subcommand. The compat
// suite dispatches per subcommand under a `subcommand=NAME/...` subtree
// so a regression in any single subcommand surfaces as a named subtest
// failure rather than masking the rest.
var withingsSubcommands = []string{
	"activity",
	"intraday",
	"measurements",
	"sleep",
	"workouts",
}

func TestContractFormats(t *testing.T) {
	bin := os.Getenv("WITHINGS_EXPORT_BIN")
	if bin == "" {
		t.Skip("WITHINGS_EXPORT_BIN not set; skipping compat suite")
	}

	// withings's data path requires a usable OAuth token plus a
	// reachable Withings API origin, neither of which a clean CI runner
	// has. internal/stubapi supplies both: a stub upstream the binary
	// reaches via WITHINGS_API_BASE, and a never-expired token in a
	// scratch HOME so auth.GetToken returns without touching the
	// network.
	//
	// The stub returns *populated* fixtures rather than the empty
	// Withings envelope this file used to serve, which changes what the
	// bundle actually attests. Against an empty response every codec
	// renders the empty-result form, so JSONIsArray only ever saw `[]`,
	// CSVHasHeader only ever saw a header, and DefaultIsMarkdown
	// compared two empty strings. With rows present all three assert
	// against real output.
	stub := stubapi.New(t)
	home := stubapi.WriteAuthToken(t)

	formats.RunContract(t, compat.Runner{
		Binary: bin,
		// withings is cobra-based: --format lives on each
		// data-producing subcommand rather than the root binary.
		Subcommands: withingsSubcommands,
		Env: []string{
			"HOME=" + home,
			"PATH=/usr/bin:/bin",
			"TZ=UTC",
			"WITHINGS_API_BASE=" + stub.URL,
		},
	})
}

// TestResolvedWindow verifies local dates independently of upstream credentials.
func TestResolvedWindow(t *testing.T) {
	bin := os.Getenv("WITHINGS_EXPORT_BIN")
	if bin == "" {
		t.Skip("WITHINGS_EXPORT_BIN not set")
	}
	dates.RunResolvedWindow(t, compat.Runner{Binary: bin, Subcommands: withingsSubcommands})
}
