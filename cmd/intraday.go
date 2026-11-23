package cmd

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/quantcli/withings-export-cli/internal/client"
	"github.com/spf13/cobra"
)

// Withings /v2/measure action=getintradayactivity returns at most 24h per call.
const intradayWindow = 24 * time.Hour

// intradayRow is the canonical §4 row for `intraday`, shared by all
// three codecs — see cmd/rowcodec.go.
//
// `hrv_rmssd` / `hrv_sdnn1` are the CSV spellings; the JSON codec used
// to emit the bare upstream `rmssd` / `sdnn1` (#20). `date` is the
// sample instant in RFC3339-with-offset, which the CSV codec used to
// call `datetime` and the JSON codec did not emit at all. The epoch
// `timestamp` is kept alongside it: it is zone-agnostic, which is
// genuinely useful for bucketing, and `prime` already advertised it.
//
// `date` and `timestamp` are the only non-nullable fields, because both
// come from the series *key* — the one thing every sample has.
// Everything else is per-sample and device-dependent, so it is a
// pointer and renders `null` where the sample did not carry it.
// Intraday is where that matters most: it is the sparsest surface in
// the CLI, since which metrics a given minute carries depends on what
// the watch was doing. A sample with no optical reading used to emit
// `"heart_rate": 0` and `"spo2_auto": 0` — values that are not merely
// absent but physiologically impossible, and that a consumer averaging
// a day of samples had no way to exclude (CONTRACT §4, record shape).
type intradayRow struct {
	Date        string   `json:"date"`
	Timestamp   int64    `json:"timestamp"`
	DurationSec *int     `json:"duration_sec"`
	Steps       *int     `json:"steps"`
	DistanceM   *float64 `json:"distance_m"`
	ElevationM  *float64 `json:"elevation_m"`
	Calories    *float64 `json:"calories"`
	HeartRate   *int     `json:"heart_rate"`
	HRVRMSSD    *float64 `json:"hrv_rmssd"`
	HRVSDNN1    *float64 `json:"hrv_sdnn1"`
	HRVQuality  *int     `json:"hrv_quality"`
	SpO2Auto    *float64 `json:"spo2_auto"`
	Model       *string  `json:"model"`
	ModelID     *int     `json:"model_id"`
}

// intradayPayload is one upstream sample: the value side of the
// `series` map. Every field is a pointer for the reason
// cmd/activity.go's activityDay sets out — the API sends the subset of
// `data_fields` this particular sample has.
//
// `model`/`model_id` are the pair worth naming, because they are not in
// the requested `data_fields` at all; the API attaches them to a sample
// on its own, and does not always have them. They are pointers anyway,
// because an unknown device must read as `null` and not as the empty
// string: §4 is explicit that an absent value is "never `""`, never
// `0`".
type intradayPayload struct {
	Duration   *int     `json:"duration"`
	Steps      *int     `json:"steps"`
	Distance   *float64 `json:"distance"`
	Elevation  *float64 `json:"elevation"`
	Calories   *float64 `json:"calories"`
	HeartRate  *int     `json:"heart_rate"`
	HRVQuality *int     `json:"hrv_quality"`
	RMSSD      *float64 `json:"rmssd"`
	SDNN1      *float64 `json:"sdnn1"`
	SpO2       *float64 `json:"spo2_auto"`
	Model      *string  `json:"model"`
	ModelID    *int     `json:"model_id"`
}

type intradayResponse struct {
	Series map[string]intradayPayload `json:"series"`
}

// row converts one upstream sample into the canonical §4 row, applying
// the decimal budgets from #26.
func (p intradayPayload) row(ts int64) intradayRow {
	return intradayRow{
		Date:        time.Unix(ts, 0).Local().Format(time.RFC3339),
		Timestamp:   ts,
		DurationSec: p.Duration,
		Steps:       p.Steps,
		DistanceM:   roundPtr(p.Distance, 2),
		ElevationM:  roundPtr(p.Elevation, 2),
		Calories:    roundPtr(p.Calories, 1),
		HeartRate:   p.HeartRate,
		HRVRMSSD:    roundPtr(p.RMSSD, 2),
		HRVSDNN1:    roundPtr(p.SDNN1, 2),
		HRVQuality:  p.HRVQuality,
		SpO2Auto:    roundPtr(p.SpO2, 1),
		Model:       p.Model,
		ModelID:     p.ModelID,
	}
}

var (
	intradayFormatFlag string
	intradaySinceFlag  string
	intradayUntilFlag  string
)

var intradayCmd = &cobra.Command{
	Use:   "intraday",
	Short: "Export minute-level samples (HR, HRV, SpO2, steps, distance) from Apple Watch/Withings trackers",
	Long: `Export Withings intraday activity samples.

Withings caps each API request at a 24h window, so the CLI chunks requests
automatically. Each sample carries the data fields the source device reports:
Apple Watch via HealthKit typically provides heart_rate, hrv_rmssd/sdnn1,
spo2_auto, steps and distance; native Withings trackers report steps and HR.

Default window is the last 24h — intraday is dense; wider ranges are slow.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse-level flag validation runs before any network call so an
		// invalid --format exits non-zero hermetically (CONTRACT §4).
		format, err := validateFormat(intradayFormatFlag)
		if err != nil {
			return err
		}
		since, err := sinceOrDefault(intradaySinceFlag, 1)
		if err != nil {
			return err
		}
		until, err := untilOrNow(intradayUntilFlag)
		if err != nil {
			return err
		}
		if printed, err := printResolvedWindow(cmd, since, until); printed || err != nil {
			return err
		}

		dataFields := "steps,elevation,calories,distance,duration,heart_rate," +
			"hrv_quality,rmssd,sdnn1,spo2_auto"

		c := client.New()
		var all []intradayRow
		for chunkStart := since; chunkStart.Before(until); chunkStart = chunkStart.Add(intradayWindow) {
			chunkEnd := chunkStart.Add(intradayWindow)
			if chunkEnd.After(until) {
				chunkEnd = until
			}

			params := url.Values{}
			params.Set("action", "getintradayactivity")
			params.Set("startdate", strconv.FormatInt(chunkStart.Unix(), 10))
			params.Set("enddate", strconv.FormatInt(chunkEnd.Unix(), 10))
			params.Set("data_fields", dataFields)

			var resp intradayResponse
			if err := c.Call("/v2/measure", params, &resp); err != nil {
				return err
			}
			for tsStr, p := range resp.Series {
				ts, err := strconv.ParseInt(tsStr, 10, 64)
				if err != nil {
					continue
				}
				all = append(all, p.row(ts))
			}
		}

		sort.Slice(all, func(i, j int) bool { return all[i].Timestamp < all[j].Timestamp })

		switch format {
		case "json":
			return printJSON(all)
		case "csv":
			return writeCSV(all)
		default:
			return writeIntradayMarkdown(all)
		}
	},
}

// writeIntradayMarkdown emits one fitdown-style "Intraday DATE" block per
// local day, with one line per sample carrying only the fields that have
// values. Suppressing zero fields keeps the firehose readable.
func writeIntradayMarkdown(samples []intradayRow) error {
	var lastDate string
	for _, s := range samples {
		ts := time.Unix(s.Timestamp, 0).Local()
		date := ts.Format("2006-01-02")
		if date != lastDate {
			if lastDate != "" {
				fmt.Fprintln(os.Stdout)
			}
			fmt.Fprintf(os.Stdout, "Intraday %s\n\n", date)
			lastDate = date
		}
		fmt.Fprint(os.Stdout, ts.Format("15:04:05"))
		if hr := deref(s.HeartRate); hr > 0 {
			fmt.Fprintf(os.Stdout, " HR %d", hr)
		}
		if rmssd := deref(s.HRVRMSSD); rmssd > 0 {
			fmt.Fprintf(os.Stdout, " rmssd %s", fmtNum(rmssd))
		}
		if sdnn1 := deref(s.HRVSDNN1); sdnn1 > 0 {
			fmt.Fprintf(os.Stdout, " sdnn1 %s", fmtNum(sdnn1))
		}
		if spo2 := deref(s.SpO2Auto); spo2 > 0 {
			fmt.Fprintf(os.Stdout, " spo2 %s", fmtNum(spo2))
		}
		if steps := deref(s.Steps); steps > 0 {
			fmt.Fprintf(os.Stdout, " %d steps", steps)
		}
		if distance := deref(s.DistanceM); distance > 0 {
			fmt.Fprintf(os.Stdout, " %sm", fmtNum(distance))
		}
		if calories := deref(s.Calories); calories > 0 {
			fmt.Fprintf(os.Stdout, " %s cal", fmtNum(calories))
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func init() {
	intradayCmd.Flags().StringVar(&intradaySinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 1d)")
	intradayCmd.Flags().StringVar(&intradayUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default now)")
	intradayCmd.Flags().StringVar(&intradayFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
}
