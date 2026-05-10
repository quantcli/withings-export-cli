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

AUTH
  withings-export auth login          OAuth2 in browser; tokens stored locally.
  withings-export auth status         Exit 0 if usable, 1 with reason. No network call.
  withings-export auth refresh|logout

  Optional env: WITHINGS_CLIENT_ID, WITHINGS_CLIENT_SECRET, WITHINGS_CALLBACK_URL.
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
    jq '.[] | {date, category, hr: .data.hr_average}'
  withings-export measurements --since 30d --types 1 --format json |
    jq 'sort_by(.date) | last'

GOTCHAS
  - Times are LOCAL; JSON epoch seconds are zone-agnostic.
  - 'intraday' is a firehose — wide windows take minutes.
  - Withings rate-limits aggressive callers (HTTP 601). 'sleep --derive' throttles itself.
  - Sleep score / apnea fields appear only on supported devices.
  - 'workouts.category' is an integer code in JSON; markdown/CSV map common codes to names.
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
