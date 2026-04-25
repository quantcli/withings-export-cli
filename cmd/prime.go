package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const primeText = `withings-export — primer for LLM agents
=======================================

WHAT IT IS
  A CLI that reads your personal Withings health data — activity, sleep,
  workouts, body measurements (weight/fat/BP/etc), and minute-level
  intraday samples (HR/HRV/SpO2/steps) — and prints it on stdout.

OUTPUT FORMATS
  Default: narrow, fitdown-style markdown — date-grouped headings, tight
  one-line stat blocks per row, easy to skim and easy for an LLM to
  consume inline.

  --format json   Pretty-printed JSON ARRAY of full rows.  Use this when
                  you want the complete row, when piping to jq, or when
                  round-tripping into other tools.

  --format csv    Spreadsheet-friendly columnar output.  Useful for
                  pandas, Excel, or quick correlations across rows.

  Errors go to stderr.  You do NOT need '2>&1'.  Exit code is 0 on
  success and non-zero on auth or network failure.  An empty result is
  success — markdown is empty, JSON is '[]', CSV has just the header.

AUTH
  Withings auth is OAuth2.  You need a Withings developer app — create
  one at https://developer.withings.com/.  Withings requires an HTTPS
  callback URL; the recommended workaround is to register
  https://redirectmeto.com/http://localhost:8128/oauth/authorize and set
  WITHINGS_CALLBACK_URL to that exact value (the CLI binds locally and
  catches the redirect).

  'withings-export auth login' opens a browser to authorize and writes
  ~/.config/withings-export/auth.json (access/refresh tokens, user id,
  client id/secret).  Subsequent calls auto-refresh ~5 min before expiry.

  'withings-export auth status' is a fast local check that exits 0 when a
  saved token is present and not yet expired, 1 with a clear "not logged
  in" or "token expired" message otherwise.  No network call.

  'withings-export auth refresh' forces a refresh now.
  'withings-export auth logout' deletes the stored tokens.

  Optional env vars (read by 'auth login' so the prompts can be skipped):
    WITHINGS_CLIENT_ID       developer app client id
    WITHINGS_CLIENT_SECRET   developer app client secret
    WITHINGS_CALLBACK_URL    redirect URI registered with Withings

DATE FLAGS  (every export subcommand accepts these)
  --since VALUE   inclusive lower bound
  --until VALUE   inclusive upper bound; defaults to now
  VALUE: today | yesterday | YYYY-MM-DD | Nd/Nw/Nm/Ny

  See https://github.com/quantcli/common/blob/main/CONTRACT.md#3-date-flags
  for the cross-CLI specification.

SUBCOMMANDS

  activity     — daily activity rollups (steps, distance, calories,
                 active-time bands, HR average/min/max, HR zones).
                 Default window: 30d.
    JSON keys: date, timezone, steps, distance, elevation, soft, moderate,
               intense, active, calories, totalcalories, hr_average,
               hr_min, hr_max, hr_zone_0..3.

  sleep        — nightly sleep summaries (light/deep/REM minutes,
                 latency, score, HR & RR ranges, snore/apnea).  Default
                 window: 30d.
                 --derive  for nights with no Withings summary, polyfill
                           start/end from intraday HR samples.  Adds a
                           'source' column distinguishing summary vs
                           derived rows.
    JSON keys: id, timezone, startdate, enddate, date, data{...}, source.

  workouts     — workouts (runs/walks/bikes/swims/lifts/etc) with
                 calories, effective duration, distance, HR/zones,
                 device id, category code.  Default window: 90d.
    JSON keys: id, category, timezone, startdate, enddate, date,
               deviceid, data{...}.

  measurements — body measurements: weight, body fat %, lean mass,
                 BP, heart pulse, SpO2, body temp, hydration, bone mass,
                 PWV, VO2 max.  Default window: 30d.
                 --types LIST  comma-separated measure type codes (e.g.
                               '1,6,76' = weight,fat_ratio,muscle_mass).
    JSON keys: date, type, type_code, value, device_id, grp_id.

  intraday     — minute-level samples (HR, HRV rmssd/sdnn1, SpO2, steps,
                 distance) from Apple Watch via HealthKit or native
                 Withings trackers.  Default window: 1d.
                 Withings caps requests at 24h windows; the CLI auto-
                 chunks wider ranges.

EXAMPLES

  # Last week's sleep, scannable
  withings-export sleep --since 7d

  # Workout HR distribution as JSON
  withings-export workouts --since 30d --format json | jq '
    .[] | { date: .date, category: .category, hr_avg: .data.hr_average }'

  # Latest weight reading
  withings-export measurements --since 30d --types 1 --format json |
    jq 'sort_by(.date) | last'

  # Resting HR across last 30 days (intraday is dense — narrow window)
  withings-export intraday --since 1d --format json |
    jq '[.[] | select(.heart_rate > 0) | .heart_rate] | min'

  # Sleep efficiency derived from light+deep+rem vs in-bed time
  withings-export sleep --since 30d --format json | jq '.[] | {
    date,
    sleep_min:  ((.data.lightsleepduration + .data.deepsleepduration +
                  .data.remsleepduration) / 60),
    inbed_min:  ((.enddate - .startdate) / 60)
  }'

GOTCHAS
  - Times are LOCAL.  RFC3339 fields in CSV/markdown carry the user's
    offset; epoch seconds are timezone-agnostic in JSON.
  - 'intraday' is a firehose.  A 7d query returns ~10K rows and takes
    minutes (auto-chunked into 24h slices, each rate-limited).  Stick
    to 1-2d unless you need the full history.
  - Withings rate-limits aggressive callers (HTTP 601 "Too many
    requests").  'sleep --derive' on a wide window throttles itself
    (250ms between calls); ad-hoc loops should do the same.
  - Sleep score and apnea fields are populated only on supported devices.
    'data.sleep_score' is null on unsupported wakeup-light models.
  - 'workouts' category is a Withings integer code in JSON (1=walk,
    2=run, 6=bicycling, 16=lift_weights, ...).  Markdown and CSV map
    common codes to string names ('lift_weights', 'walk', ...) and
    unknown codes render as 'unknown'; CSV also keeps the raw integer
    in a 'category_code' column.
`

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Print an LLM-targeted primer (output formats, subcommands, jq recipes)",
	Long: `Print a one-screen primer aimed at LLM agents calling this CLI as a tool.
Covers the output formats (markdown by default, --format json/csv for structured),
auth subcommands and OAuth setup, the subcommands and what their rows look like,
the shared date flags, and a few jq recipes for common questions.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), primeText)
		return err
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
}
