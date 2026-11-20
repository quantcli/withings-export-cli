package cmd

// §4 codec-parity attestation.
//
// CONTRACT.md §4 says a logical field has one JSON type across every
// subcommand that emits it, that numeric quantities are JSON numbers,
// and that `--format json` and `--format csv` are two encodings of one
// dataset rather than two datasets. The shared `compat/formats` bundle
// checks §4's *structure* — that json is an array, that csv has a
// header row, that the default is byte-identical to markdown — and it
// runs withings against an empty stub, so it never sees a row. None of
// the key-name, unit, or precision rules are reachable from there.
//
// These tests close that gap. They run the real serializers against
// non-empty fixtures from internal/stubapi on every PR under the plain
// `go test ./...` job, so a regression fails CI rather than review.
//
// Deliberately repo-local for now. Promoting this to a reusable
// `compat/parity` bundle in quantcli/common is the follow-up (see the
// PR body); the comparison engine below is exporter-agnostic, but it
// needs each exporter to supply non-empty fixtures first, and withings
// is the only one with a stub today.

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/quantcli/withings-export-cli/internal/stubapi"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// dataSubcommands is every subcommand that emits rows. Kept as a
// package-level list so a newly added exporter subcommand that forgets
// its parity coverage shows up as a missing entry here rather than as
// silent absence.
var dataSubcommands = []string{"activity", "intraday", "measurements", "sleep", "workouts"}

// fixedWindow pins --since/--until on every invocation.
//
// Without it the tests inherit each subcommand's default lookback,
// which makes them neither deterministic nor fast: `intraday` chunks
// its window into 24h requests and would collect the stub's single
// sample once per chunk, and `sleep --derive` walks the window one
// throttled request per day. A one-day window gives exactly one chunk
// and one derive probe.
//
// The day is the one internal/stubapi's fixtures are keyed to. The stub
// ignores the requested range and always answers with its fixture, so
// this controls the CLI's request loop rather than the response.
var fixedWindow = []string{"--since", stubapi.ActivityDay, "--until", stubapi.ActivityDay}

// canonicalKeys is the declared §4 row schema per subcommand: the exact
// set of keys `--format json` emits and `--format csv` writes as its
// header, in CSV column order.
//
// This exists because the pre-fix JSON passed Withings' `data` blob
// through verbatim, which made the JSON key set depend on what the
// user's *device* happened to report — so no consumer could rely on a
// field being present. Pinning the set here makes the schema a decision
// rather than an accident: adding or renaming a key requires editing
// this map, which puts it in the diff and in review.
var canonicalKeys = map[string][]string{
	"activity": {
		"date", "calendar_date", "timezone", "steps", "distance_m", "elevation_m",
		"soft_sec", "moderate_sec", "intense_sec", "active_sec",
		"calories", "total_calories",
		"hr_avg", "hr_min", "hr_max",
		"hr_zone_0", "hr_zone_1", "hr_zone_2", "hr_zone_3",
	},
	"intraday": {
		"date", "timestamp", "duration_sec",
		"steps", "distance_m", "elevation_m", "calories",
		"heart_rate", "hrv_rmssd", "hrv_sdnn1", "hrv_quality", "spo2_auto",
		"model", "model_id",
	},
	"measurements": {
		"date", "type", "type_code", "value", "device_id", "grp_id",
	},
	"sleep": {
		"date", "calendar_date", "start", "end", "start_epoch", "end_epoch",
		"id", "timezone", "model",
		"total_sleep_sec", "light_sec", "deep_sec", "rem_sec", "awake_sec",
		"time_to_sleep_sec", "time_to_wakeup_sec", "wakeup_count", "sleep_score",
		"hr_avg", "hr_min", "hr_max", "rr_avg", "rr_min", "rr_max",
		"breathing_disturbances_intensity", "snoring_sec", "snore_episodes",
		"apnea_hypopnea_index", "source",
	},
	"workouts": {
		"date", "calendar_date", "start", "end", "start_epoch", "end_epoch",
		"id", "timezone", "model", "attrib", "category", "category_code",
		"duration_sec", "eff_duration_sec", "pause_duration_sec", "algo_pause_duration_sec",
		"calories", "manual_calories", "intensity",
		"steps", "distance_m", "manual_distance_m", "elevation_m",
		"hr_avg", "hr_min", "hr_max",
		"hr_zone_0", "hr_zone_1", "hr_zone_2", "hr_zone_3",
		"spo2_avg_pct", "pool_laps", "pool_length_m", "strokes", "device_id",
	},
}

// maxDecimals is the decimal budget per field: the most fractional
// digits a value of that field may render with, in JSON and CSV alike.
//
// Issues #21 and #26 are both this rule being unenforced — a float64
// like 72844×10⁻³ or a mean of 29 heart-rate samples reaches stdout as
// `72.84400000000001` / `45.86206896551724`. Fields absent from this
// map must render as integers (budget 0) because every remaining
// numeric field in the schema is a count, a code, or a whole second.
var maxDecimals = map[string]map[string]int{
	"activity": {
		"distance_m": 2, "elevation_m": 2, "calories": 1, "total_calories": 1,
		"hr_avg": 1,
	},
	"intraday": {
		"distance_m": 2, "elevation_m": 2, "calories": 1, "spo2_auto": 1,
		"hrv_rmssd": 2, "hrv_sdnn1": 2,
	},
	"measurements": {"value": 3},
	"sleep": {
		"hr_avg": 1, "rr_avg": 1, "apnea_hypopnea_index": 1,
	},
	"workouts": {
		"calories": 1, "manual_calories": 1,
		"distance_m": 2, "manual_distance_m": 2, "elevation_m": 2,
		"hr_avg": 1, "spo2_avg_pct": 1, "pool_length_m": 2,
	},
}

// TestJSONAndCSVAgree is the core §4 parity assertion: for every
// data-producing subcommand, `--format json` and `--format csv` must
// carry the same rows, the same field names, and the same values in the
// same units.
//
// This is the test that fails on main for all five of the bugs this
// change closes (#20, #21, #26, #27, #42).
func TestJSONAndCSVAgree(t *testing.T) {
	for _, sub := range dataSubcommands {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			env := stubEnv(t, false)
			jsonRows := runJSON(t, env, sub)
			header, csvRows := runCSV(t, env, sub)

			if len(jsonRows) == 0 {
				t.Fatalf("fixture produced no JSON rows; the parity check would be vacuous")
			}
			if len(jsonRows) != len(csvRows) {
				t.Fatalf("row count differs: json=%d csv=%d", len(jsonRows), len(csvRows))
			}

			want := canonicalKeys[sub]
			if want == nil {
				t.Fatalf("no canonical key set declared for subcommand %q", sub)
			}
			if diff := diffStrings(want, header); diff != "" {
				t.Errorf("csv header does not match the declared §4 schema:\n%s", diff)
			} else if !slices.Equal(want, header) {
				// Same names, different order. Not a §4 violation —
				// CSV column order is a presentation choice — but the
				// declared list is documented as being in column
				// order, so keep the two honest about each other.
				t.Errorf("csv column order differs from the declared schema:\n  declared: %s\n  actual:   %s",
					strings.Join(want, ", "), strings.Join(header, ", "))
			}
			if diff := diffStrings(want, sortedKeys(jsonRows[0])); diff != "" {
				t.Errorf("json keys do not match the declared §4 schema:\n%s", diff)
			}

			for i, obj := range jsonRows {
				for col, name := range header {
					if col >= len(csvRows[i]) {
						t.Errorf("row %d: csv has %d cells, header has %d", i, len(csvRows[i]), len(header))
						break
					}
					raw, ok := obj[name]
					if !ok {
						// Key-set mismatch is already reported above;
						// don't bury it under a per-row cascade.
						continue
					}
					got, err := renderJSONValue(raw)
					if err != nil {
						t.Errorf("row %d field %q: %v", i, name, err)
						continue
					}
					if want := csvRows[i][col]; got != want {
						t.Errorf("row %d field %q disagrees across codecs: json=%q csv=%q",
							i, name, got, want)
					}
				}
			}
		})
	}
}

// TestDateIsOneRepresentationEverywhere pins down §2 + §4's timestamp
// clause: `date` may be a formatted string, but it is RFC3339 with an
// offset and it is *the same* representation in every subcommand.
//
// On main this fails for four of the five: activity/sleep/workouts emit
// a bare "2026-04-15" and intraday emits no `date` at all, only an
// epoch `timestamp`. (DT's 2026-07-21 comment on #27.)
func TestDateIsOneRepresentationEverywhere(t *testing.T) {
	for _, sub := range dataSubcommands {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			rows := runJSON(t, stubEnv(t, false), sub)
			if len(rows) == 0 {
				t.Fatalf("fixture produced no rows")
			}
			raw, ok := rows[0]["date"]
			if !ok {
				t.Fatalf("no `date` field; §4 requires one timestamp representation across every subcommand")
			}
			s, ok := raw.(string)
			if !ok {
				t.Fatalf("`date` is %T (%v), want an RFC3339 string", raw, raw)
			}
			ts, err := time.Parse(time.RFC3339, s)
			if err != nil {
				t.Fatalf("`date`=%q does not parse as RFC3339: %v", s, err)
			}
			if _, offset := ts.Zone(); offset == 0 && !strings.HasSuffix(s, "Z") {
				t.Errorf("`date`=%q carries no UTC offset; §2 requires RFC3339 *with offset*", s)
			}
			// A bare calendar day would have failed the parse above;
			// this catches a midnight-truncated value that still
			// parses, which is legitimate only because some rows are
			// day-keyed. Assert the shape, not the precision.
			if len(s) != len(time.Now().Format(time.RFC3339)) {
				t.Errorf("`date`=%q is not the same RFC3339 layout the other subcommands emit", s)
			}
		})
	}
}

// TestNumericQuantitiesAreJSONNumbers asserts §4's first scalar-typing
// bullet: a numeric quantity is a JSON number, never a quoted string,
// even when the upstream API sends it quoted.
func TestNumericQuantitiesAreJSONNumbers(t *testing.T) {
	// Fields whose values are genuinely text, so a numeric-looking
	// value must not be reinterpreted. `model` is a device name
	// ("ScanWatch 2"); `device_id` is a hex string that would lose
	// leading zeros as a number; `date`/`start`/`end` are RFC3339 per
	// the §2 carve-out.
	textFields := map[string]bool{
		"date": true, "calendar_date": true, "start": true, "end": true,
		"timezone": true, "type": true, "category": true, "source": true,
		"model": true, "device_id": true,
	}
	for _, sub := range dataSubcommands {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			rows := runJSON(t, stubEnv(t, false), sub)
			if len(rows) == 0 {
				t.Fatalf("fixture produced no rows")
			}
			for i, obj := range rows {
				for _, k := range sortedKeys(obj) {
					if textFields[k] {
						continue
					}
					switch v := obj[k].(type) {
					case float64, nil:
						// number, or explicitly-absent per §4.
					case string:
						t.Errorf("row %d field %q is the string %q; §4 requires a JSON number", i, k, v)
					case map[string]any, []any:
						t.Errorf("row %d field %q is a nested %T; the §4 row schema is flat", i, k, v)
					default:
						t.Errorf("row %d field %q is %T, want a JSON number", i, k, v)
					}
				}
			}
		})
	}
}

// TestNumbersRespectTheirDecimalBudget is #26 and the first half of
// #21: the markdown renderer already rounds these fields, so the
// helpers exist — the structured codecs just never called them.
func TestNumbersRespectTheirDecimalBudget(t *testing.T) {
	for _, sub := range dataSubcommands {
		sub := sub
		t.Run(sub, func(t *testing.T) {
			budgets := maxDecimals[sub]
			rows := runJSON(t, stubEnv(t, false), sub)
			if len(rows) == 0 {
				t.Fatalf("fixture produced no rows")
			}
			for i, obj := range rows {
				for _, k := range sortedKeys(obj) {
					f, ok := obj[k].(float64)
					if !ok {
						continue
					}
					got := decimalPlaces(strconv.FormatFloat(f, 'f', -1, 64))
					if want := budgets[k]; got > want {
						t.Errorf("row %d field %q renders %s with %d decimals, budget is %d",
							i, k, strconv.FormatFloat(f, 'f', -1, 64), got, want)
					}
				}
			}
		})
	}
}

// TestDerivedSleepRowsNullUnknownStages is the second half of #21.
//
// `sleep --derive` polyfills a night from intraday heart rate, which
// can establish a total sleep duration but nothing about stages. Before
// this change the whole duration was stuffed into `lightsleepduration`
// so the existing CSV total would add up — which made every derived row
// claim "deep_min=0, rem_min=0, wakeup_count=0" as if those were
// measurements. §4 is explicit: an absent value is `null`, never 0, "so
// a missing measurement can't be read as a real one".
//
// Note this is not the arithmetic bug #21's title suggests. The summary
// path's total really is light+deep+rem; on a derived row total ==
// light because the stage fields were being faked, not mis-added.
func TestDerivedSleepRowsNullUnknownStages(t *testing.T) {
	rows := runJSON(t, stubEnv(t, true), "sleep", "--derive")
	var derived map[string]any
	for _, r := range rows {
		if r["source"] == "derived" {
			derived = r
			break
		}
	}
	if derived == nil {
		t.Fatalf("fixture produced no derived sleep row; got %d row(s): %v", len(rows), rows)
	}

	// The heuristic can measure a total and a mean heart rate.
	for _, k := range []string{"total_sleep_sec", "hr_avg"} {
		if derived[k] == nil {
			t.Errorf("derived row field %q is null, but --derive can establish it", k)
		}
	}
	// It cannot measure any of these, so they must be null, not zero.
	for _, k := range []string{
		"light_sec", "deep_sec", "rem_sec", "awake_sec",
		"time_to_sleep_sec", "time_to_wakeup_sec", "wakeup_count", "sleep_score",
		"hr_min", "hr_max", "rr_avg", "rr_min", "rr_max",
		"snore_episodes", "snoring_sec", "apnea_hypopnea_index",
		"breathing_disturbances_intensity",
	} {
		if got := derived[k]; got != nil {
			t.Errorf("derived row field %q is %v; --derive cannot measure it, so §4 requires null", k, got)
		}
	}
	// And the total must not be laundered through a stage field.
	if derived["total_sleep_sec"] == derived["light_sec"] {
		t.Errorf("derived total_sleep_sec still equals light_sec (%v); the stage breakdown is being faked",
			derived["total_sleep_sec"])
	}
}

// --- harness ---------------------------------------------------------

// stubEnv starts a stub API and returns the environment that points the
// CLI at it. derive selects the fixture pair the `sleep --derive` path
// needs: no sleep summaries (so there is a night to polyfill) and an
// intraday response long enough to qualify as a sleep run.
func stubEnv(t *testing.T, derive bool) []string {
	t.Helper()
	s := stubapi.New(t)
	s.DeriveIntraday = derive
	s.SleepSummaries = !derive
	// No TZ here on purpose. These tests run in-process, and Go resolves
	// time.Local once per process from the TZ present at first use, so
	// setting it now would not take effect and would read as if it had.
	// The assertions are offset-agnostic by design: both codecs render
	// from the same process in the same zone, and the §2 check asserts
	// that `date` carries *an* offset in one consistent layout rather
	// than a particular one. The compat-tagged suite execs a real
	// binary and does pin TZ.
	return []string{
		"HOME=" + stubapi.WriteAuthToken(t),
		"WITHINGS_API_BASE=" + s.URL,
	}
}

// runJSON runs `<sub> --format json` against the stub and unmarshals
// stdout as the §4 array of row objects.
func runJSON(t *testing.T, env []string, sub string, extra ...string) []map[string]any {
	t.Helper()
	out := runCLI(t, env, sub, append(append([]string{"--format", "json"}, fixedWindow...), extra...)...)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("%s --format json: stdout is not an array of objects: %v\nstdout=%s", sub, err, out)
	}
	return rows
}

// runCSV runs `<sub> --format csv` against the stub and returns the
// header row and the data rows.
func runCSV(t *testing.T, env []string, sub string, extra ...string) ([]string, [][]string) {
	t.Helper()
	out := runCLI(t, env, sub, append(append([]string{"--format", "csv"}, fixedWindow...), extra...)...)
	recs, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil {
		t.Fatalf("%s --format csv: %v\nstdout=%s", sub, err, out)
	}
	if len(recs) == 0 {
		t.Fatalf("%s --format csv: no header row", sub)
	}
	return recs[0], recs[1:]
}

// runCLI invokes a subcommand in-process with os.Stdout captured, and
// returns stdout. In-process rather than exec-ing a built binary so the
// assertions run under the plain `go test ./...` CI job with no build
// step — the compat-tagged suite is where the black-box, built-binary
// checks live.
func runCLI(t *testing.T, env []string, sub string, args ...string) string {
	t.Helper()
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed env entry %q", kv)
		}
		t.Setenv(k, v)
	}

	// Flag values are package-level vars bound once in init(), so they
	// persist across invocations within a test binary. Reset the
	// subcommand's flags to their declared defaults or a previous run's
	// --format leaks into this one.
	target, _, err := rootCmd.Find([]string{sub})
	if err != nil {
		t.Fatalf("no such subcommand %q: %v", sub, err)
	}
	resetFlags(target)
	t.Cleanup(func() { resetFlags(target) })

	stdout := os.Stdout
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("capture stdout: %v", err)
	}
	os.Stdout = f
	defer func() { os.Stdout = stdout }()

	rootCmd.SetArgs(append([]string{sub}, args...))
	rootCmd.SetOut(f)
	runErr := rootCmd.Execute()
	rootCmd.SetArgs(nil)

	os.Stdout = stdout
	if cerr := f.Close(); cerr != nil {
		t.Fatalf("close capture: %v", cerr)
	}
	if runErr != nil {
		t.Fatalf("%s %v: %v", sub, args, runErr)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return string(b)
}

func resetFlags(c *cobra.Command) {
	c.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
}

// renderJSONValue converts a decoded JSON value to the text a CSV cell
// carrying the same value must contain. This is the definition of
// "the two codecs agree": CSV has no types and no null, so a number
// renders as its shortest round-tripping decimal and an absent value
// renders as an empty cell.
func renderJSONValue(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("value is a nested %T; the §4 row schema is flat", v)
	}
}

// decimalPlaces counts the fractional digits in a 'f'-formatted number.
func decimalPlaces(s string) int {
	_, frac, ok := strings.Cut(s, ".")
	if !ok {
		return 0
	}
	return len(frac)
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// diffStrings compares two field-name lists as sets and returns a
// human-readable report, or "" when they hold the same names. Order is
// not compared: CSV column order is a presentation choice, while JSON
// object key order is not meaningful at all.
func diffStrings(want, got []string) string {
	inWant := map[string]bool{}
	for _, s := range want {
		inWant[s] = true
	}
	inGot := map[string]bool{}
	for _, s := range got {
		inGot[s] = true
	}
	var missing, extra []string
	for _, s := range want {
		if !inGot[s] {
			missing = append(missing, s)
		}
	}
	for _, s := range got {
		if !inWant[s] {
			extra = append(extra, s)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	var b strings.Builder
	if len(missing) > 0 {
		fmt.Fprintf(&b, "  missing: %s\n", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "  unexpected: %s\n", strings.Join(extra, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
