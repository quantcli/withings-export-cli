// Package stubapi stands up an in-process stub of the Withings API so
// tests can exercise the CLI's real data path — and therefore its real
// serializers — without credentials or network access.
//
// The binary under test is unmodified: internal/client honours
// WITHINGS_API_BASE, so pointing that at Server.URL is the whole
// integration. Tests also need a token file; see WriteAuthToken.
//
// Fixtures are deliberately *not* clean. They carry the float-precision
// noise Withings actually sends (unit-scaled integers like 72844×10⁻³,
// HealthKit-derived calories such as 550.09729003906), because rounding
// is part of the §4 surface under test. A fixture of tidy round numbers
// would let a precision regression pass.
//
// See CONTRACT.md §8 "Running the data path without credentials".
package stubapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Fixed epoch instants the fixtures are keyed to. Tests that assert on
// rendered timestamps convert these with time.Unix(...).Local() so the
// expectation follows TZ rather than hard-coding an offset.
const (
	// WorkoutStart is 2026-04-15T10:00:00Z.
	WorkoutStart int64 = 1776247200
	// WorkoutEnd is 45m01s after WorkoutStart — deliberately not a
	// whole number of minutes, so a duration that is truncated to
	// minutes is distinguishable from one carried in seconds.
	WorkoutEnd int64 = WorkoutStart + 2701

	// SleepStart is 2026-04-15T02:00:00Z, SleepEnd 7h30m later.
	SleepStart int64 = 1776218400
	SleepEnd   int64 = SleepStart + 27000

	// MeasureAt is the instant of the single measurement group.
	MeasureAt int64 = 1776247500

	// IntradaySampleAt is the instant of the single intraday sample.
	IntradaySampleAt int64 = 1776247260

	// DeriveSampleBase is the first sample of the synthetic quiet run
	// used by the sleep --derive path.
	DeriveSampleBase int64 = 1776132000

	// The second-day instants, exactly 24h after the first. They key the
	// sparse second record each series carries — see the comment on
	// ActivityDay2.
	WorkoutStart2 int64 = WorkoutStart + 86400
	WorkoutEnd2   int64 = WorkoutStart2 + 1517
	SleepStart2   int64 = SleepStart + 86400
	SleepEnd2     int64 = SleepStart2 + 21600
)

// ActivityDay is the calendar day the activity fixture reports. Sleep
// and workout fixtures use the same day so a test can assert that the
// `date` field agrees across subcommands.
const ActivityDay = "2026-04-15"

// ActivityDay2 is a second calendar day, present in every series so the
// fixtures carry more than one record.
//
// Two reasons, and the second is the load-bearing one:
//
//   - common's compat/parity bundle skips its JSONKeySetIsUniform
//     subtest below two records, because one record cannot disagree with
//     itself. A single-record fixture earns a skip, and a skip is a gap
//     in attestation rather than a pass.
//   - The second record is deliberately *sparse*: it omits the fields a
//     cheaper device or an unworn night would not report (the hr_* and
//     hr_zone_* set, elevation, the respiratory and snoring block, the
//     pool fields). That is the shape that produced the §4 record-shape
//     defect in the first place — JSON that passed the upstream `data`
//     blob through emitted whatever keys the device happened to send, so
//     no consumer could rely on a field existing. With cmd/rowcodec.go's
//     declared row structs the absent values render as `null` in JSON
//     and an empty cell in CSV, and the key set stays fixed. A
//     regression to envelope passthrough fails the bundle on this
//     record.
const ActivityDay2 = "2026-04-16"

// Server is a running stub. Close it via t.Cleanup.
type Server struct {
	*httptest.Server

	// DeriveIntraday, when true, makes getintradayactivity return the
	// synthetic quiet run that `sleep --derive` turns into a derived
	// row. The default (false) returns the single-sample fixture the
	// `intraday` subcommand uses. The two cannot share one response:
	// deriveSleep needs ≥10 low-HR samples spanning ≥3h, which would
	// make `intraday`'s own fixture unreadably large.
	DeriveIntraday bool

	// SleepSummaries, when false, makes getsummary return zero series
	// so `sleep --derive` has a night to polyfill. Default true.
	SleepSummaries bool
}

// New returns a started stub server registered for cleanup on t.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{SleepSummaries: true}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var body any
	switch r.Form.Get("action") {
	case "getactivity":
		body = activityBody()
	case "getworkouts":
		body = workoutsBody()
	case "getmeas":
		body = measurementsBody()
	case "getsummary":
		if !s.SleepSummaries {
			body = map[string]any{"series": []any{}, "more": false, "offset": 0}
			break
		}
		body = sleepBody()
	case "getintradayactivity":
		if s.DeriveIntraday {
			body = deriveIntradayBody()
			break
		}
		body = intradayBody()
	default:
		// Unknown action: the empty-but-successful envelope. Keeps the
		// stub honest about what it does and does not model.
		body = map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": 0, "body": body})
}

// activityBody mirrors /v2/measure?action=getactivity for two days.
// Values on the first day are taken from the repro in
// quantcli/withings-export-cli#26: `calories` and `distance` carry the
// decimals the API really sends. The second day is the sparse record
// described on ActivityDay2 — a tracker with no optical heart rate, so
// the whole hr_* and hr_zone_* block and `elevation` are absent.
func activityBody() map[string]any {
	return map[string]any{
		"activities": []map[string]any{{
			"date":          ActivityDay,
			"timezone":      "America/New_York",
			"steps":         11482,
			"distance":      11395.42,
			"elevation":     42.5,
			"soft":          7260,
			"moderate":      1980,
			"intense":       600,
			"active":        9840,
			"calories":      806.75,
			"totalcalories": 1099,
			"hr_average":    72.4,
			"hr_min":        51,
			"hr_max":        163,
			"hr_zone_0":     5400,
			"hr_zone_1":     3000,
			"hr_zone_2":     1140,
			"hr_zone_3":     300,
		}, {
			"date":          ActivityDay2,
			"timezone":      "America/New_York",
			"steps":         4310,
			"distance":      3281.07,
			"soft":          3600,
			"moderate":      420,
			"intense":       0,
			"active":        4020,
			"calories":      211.5,
			"totalcalories": 912,
		}},
		"more":   false,
		"offset": 0,
	}
}

// workoutsBody mirrors /v2/measure?action=getworkouts for one session.
// The `data` sub-object carries every field the subcommand requests via
// data_fields, including the float noise from #26, so the flattening
// and rounding paths are both exercised.
func workoutsBody() map[string]any {
	return map[string]any{
		"series": []map[string]any{{
			"id":        1234567890,
			"category":  2, // run
			"timezone":  "America/New_York",
			"model":     16,
			"attrib":    0,
			"startdate": WorkoutStart,
			"enddate":   WorkoutEnd,
			"date":      ActivityDay,
			"deviceid":  "aabbccddeeff00112233445566778899",
			"data": map[string]any{
				"calories":            550.09729003906,
				"effduration":         2680,
				"intensity":           50,
				"manual_distance":     4575.8657226562,
				"manual_calories":     315.9172668457,
				"hr_average":          142.66666666666666,
				"hr_min":              98,
				"hr_max":              176,
				"hr_zone_0":           300,
				"hr_zone_1":           900,
				"hr_zone_2":           1200,
				"hr_zone_3":           280,
				"pause_duration":      21,
				"algo_pause_duration": 0,
				"spo2_average":        96.5,
				"steps":               6207,
				"distance":            157.71188354492,
				"elevation":           12.25,
				"pool_laps":           0,
				"strokes":             0,
				"pool_length":         0,
			},
		}, {
			// The sparse session: a manually logged walk from a scale-only
			// account, so there is no device id, no heart-rate block and
			// none of the pool fields.
			"id":        1234567891,
			"category":  1, // walk
			"timezone":  "America/New_York",
			"model":     1,
			"attrib":    2,
			"startdate": WorkoutStart2,
			"enddate":   WorkoutEnd2,
			"date":      ActivityDay2,
			"data": map[string]any{
				"calories":    88.25,
				"effduration": 1517,
				"intensity":   20,
				"steps":       2140,
				"distance":    1604.5,
			},
		}},
		"more":   false,
		"offset": 0,
	}
}

// measurementsBody mirrors /measure?action=getmeas. The unit-scaled
// integers are the real source of the `72.84400000000001` noise in #26:
// 72844 × 10⁻³ is not exactly representable in binary floating point.
func measurementsBody() map[string]any {
	return map[string]any{
		"timezone": "America/New_York",
		"measuregrps": []map[string]any{{
			"grpid":    987654321,
			"date":     MeasureAt,
			"category": 1,
			"deviceid": "aabbccddeeff00112233445566778899",
			"measures": []map[string]any{
				{"value": 107350, "type": 1, "unit": -3}, // weight_kg 107.35
				{"value": 72844, "type": 5, "unit": -3},  // fat_free_mass_kg 72.844
				{"value": 32143, "type": 6, "unit": -3},  // fat_ratio_pct 32.143
				{"value": 975, "type": 54, "unit": -1},   // spo2_pct 97.5
			},
		}},
		"more":   0,
		"offset": 0,
	}
}

// sleepBody mirrors /v2/sleep?action=getsummary for one night. Stage
// durations are seconds, as the API sends them.
func sleepBody() map[string]any {
	return map[string]any{
		"series": []map[string]any{{
			"id":        2233445566,
			"timezone":  "America/New_York",
			"model":     32,
			"startdate": SleepStart,
			"enddate":   SleepEnd,
			"date":      ActivityDay,
			"data": map[string]any{
				"wakeupduration":                   1800,
				"lightsleepduration":               14400,
				"deepsleepduration":                7200,
				"remsleepduration":                 3600,
				"durationtosleep":                  780,
				"durationtowakeup":                 420,
				"wakeupcount":                      3,
				"sleep_score":                      81,
				"hr_average":                       58.333333333333336,
				"hr_min":                           49,
				"hr_max":                           77,
				"rr_average":                       14.666666666666666,
				"rr_min":                           11,
				"rr_max":                           19,
				"breathing_disturbances_intensity": 7,
				"snoring":                          960,
				"snoringepisodecount":              4,
				"apnea_hypopnea_index":             3.7000000000000002,
			},
		}, {
			// The sparse night: a sleep mat reports stage durations but no
			// respiratory, snoring or heart-rate analysis, and scores the
			// night only when it has those inputs.
			"id":        2233445567,
			"timezone":  "America/New_York",
			"model":     16,
			"startdate": SleepStart2,
			"enddate":   SleepEnd2,
			"date":      ActivityDay2,
			"data": map[string]any{
				"wakeupduration":     900,
				"lightsleepduration": 12600,
				"deepsleepduration":  5400,
				"remsleepduration":   2700,
				"durationtosleep":    1020,
				"durationtowakeup":   360,
				"wakeupcount":        1,
			},
		}},
		"more":   false,
		"offset": 0,
	}
}

// intradayBody mirrors /v2/measure?action=getintradayactivity with a
// single sample. The series is keyed by epoch-second *strings*, which
// is what the real API does.
func intradayBody() map[string]any {
	return map[string]any{
		"series": map[string]any{
			itoa(IntradaySampleAt): map[string]any{
				"duration":    60,
				"steps":       37,
				"distance":    12.345678901234,
				"elevation":   0.5,
				"calories":    1.3999999999999999,
				"heart_rate":  71,
				"hrv_quality": 2,
				"rmssd":       42.5,
				"sdnn1":       31.25,
				"spo2_auto":   97.5,
				"model":       "ScanWatch 2",
				"model_id":    62,
			},
		},
	}
}

// deriveIntradayBody returns a quiet run long enough for deriveSleep to
// accept it: samples every 5 minutes for 6h at a sleeping heart rate
// with no steps. The mean HR is deliberately fractional so the derived
// `hr_avg` exercises the rounding path from #21.
func deriveIntradayBody() map[string]any {
	series := map[string]any{}
	for i := 0; i < 72; i++ {
		ts := DeriveSampleBase + int64(i)*300
		series[itoa(ts)] = map[string]any{
			// 43..50 bpm, so the mean is not a whole number.
			"heart_rate": 43 + i%8,
			"steps":      0,
			"duration":   300,
		}
	}
	return map[string]any{"series": series}
}

// itoa renders an epoch second as the decimal string the API uses for
// intraday series keys.
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// WriteAuthToken seeds a never-expiring token store under a fresh HOME
// so auth.GetToken short-circuits to the not-expired branch and the
// binary proceeds to the HTTP call. Returns the HOME to export.
//
// The values are placeholders — the stub does not check Authorization.
func WriteAuthToken(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "withings-export")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("stubapi: mkdir auth dir: %v", err)
	}
	token := map[string]any{
		"access_token":  "stub-access-token",
		"refresh_token": "stub-refresh-token",
		"expires_at":    time.Now().AddDate(10, 0, 0).Format(time.RFC3339),
		"user_id":       "0",
		"client_id":     "stub-client-id",
		"client_secret": "stub-client-secret",
	}
	body, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		t.Fatalf("stubapi: marshal token: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), body, 0o600); err != nil {
		t.Fatalf("stubapi: write auth.json: %v", err)
	}
	return home
}
