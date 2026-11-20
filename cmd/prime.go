package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const primeText = `withings-export — primer for LLM agents
=======================================

WHAT IT IS
  CLI for personal Withings data: activity, sleep, workouts, body
  measurements, minute-level intraday samples (HR/HRV/SpO2/steps).

I/O
  stdout: data in --format markdown (default), json, or csv.
  stderr: errors. Exit 0 on success including empty results.

ROW SCHEMA  (json and csv)
  json and csv are two encodings of ONE dataset: same rows, same field names,
  same units. Prototype with jq, then hand the same names to a spreadsheet.
    - Field names carry their unit: distance_m, duration_sec, soft_sec.
    - Durations are seconds. Human durations ("8h04") are markdown only.
    - 'date' is RFC3339 with offset in every subcommand. Day-keyed rows
      (activity, sleep, workouts) put local midnight there and the plain
      day string in 'calendar_date'; sleep and workouts also carry precise
      'start'/'end' plus zone-agnostic 'start_epoch'/'end_epoch'.
    - Not measured is null in json, an empty cell in csv — never 0. So a
      device that does not report sleep stages is distinguishable from a
      night with no deep sleep.
    - Rows are flat. There is no nested 'data' object.

AUTH
  withings-export auth login          OAuth2 in browser; tokens stored locally.
  withings-export auth status         Exit 0 if usable, 1 with reason. No network call.
  withings-export auth logout         Remove stored tokens.

  Optional env: WITHINGS_CLIENT_ID, WITHINGS_CLIENT_SECRET, WITHINGS_CALLBACK_URL.
  Headless (CI): set WITHINGS_REFRESH_TOKEN (+ WITHINGS_CLIENT_ID/SECRET) to skip auth
  login. It wins over any saved token. Withings rotates it on each use — the rotated
  value lands in ~/.config/withings-export/auth.json; re-inject it on the next run.
  HTTPS-callback workaround: register https://redirectmeto.com/http://localhost:8128/oauth/authorize
  (verbatim) and set WITHINGS_CALLBACK_URL to the same string.

DATE FLAGS  (every subcommand)
  --since VALUE / --until VALUE
  VALUE: today | yesterday | YYYY-MM-DD | Nd/Nw/Nm/Ny
  See https://github.com/quantcli/common/blob/main/CONTRACT.md#3-date-flags

SUBCOMMANDS  (defaults in parens)
  activity      (30d) daily steps/distance/calories/HR zones
  sleep         (30d) stages, score, HR/RR; --derive polyfills missing nights
  workouts      (90d) runs/walks/bikes/lifts with calories/HR/distance
  measurements  (30d) weight/fat/BP/SpO2/temp; --types LIST filters
  intraday      (1d)  minute-level HR/HRV/SpO2/steps; dense — keep windows narrow

  Inspect any subcommand's row schema with: <subcommand> --since 1d --format json

EXAMPLES
  withings-export sleep --since 7d
  withings-export workouts --since 30d --format json |
    jq '.[] | {date, category, hr: .hr_avg}'
  withings-export measurements --since 30d --types 1 --format json |
    jq 'sort_by(.date) | last'
  withings-export sleep --since 30d --format json |
    jq '[.[] | select(.deep_sec != null) | .deep_sec] | add / 3600'

GOTCHAS
  - Times are LOCAL. 'date'/'start'/'end' carry the offset; the *_epoch
    and intraday 'timestamp' fields are zone-agnostic Unix seconds.
  - 'intraday' is a firehose — wide windows take minutes.
  - Withings rate-limits aggressive callers (HTTP 601). 'sleep --derive' throttles itself.
  - Sleep score / apnea fields appear only on supported devices; they are
    null (not 0) on a device that does not report them.
  - 'sleep --derive' rows carry source="derived" and know only the session
    span and mean heart rate. Every stage, latency and count is null.
  - 'workouts.category' is the name string; 'category_code' is the integer.
`

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Print an LLM-targeted primer (one screen)",
	Long: `Print a one-screen primer aimed at LLM agents calling this CLI as a tool.
Covers I/O, auth, the shared date flags, the subcommand menu, and a few jq
recipes. Per the quantcli contract, prime is short — anything that wants
to grow into a man page belongs in --help on the relevant subcommand or
in https://github.com/quantcli/common/blob/main/CONTRACT.md.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), primeText)
		return err
	},
}

func init() {
	rootCmd.AddCommand(primeCmd)
}
