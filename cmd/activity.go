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

// activityDay is the upstream day record.
//
// `date` and `timezone` are envelope fields — the API sends them on
// every day it returns. Everything below them is requested through
// `data_fields`, which makes it optional *by construction*: Withings
// answers with the subset the user's devices actually recorded, so a
// tracker with no optical sensor and no barometer sends no `hr_*` block
// and no `elevation` at all.
//
// The pointers are therefore a rule rather than a per-field judgement
// call: envelope fields are values, `data_fields` fields are pointers.
// Decoding an omitted field into a value type makes "my device does not
// measure this" indistinguishable from "this was zero" — the mistake
// cmd/sleep.go's sleepData already documents, and the one CONTRACT §4's
// record-shape clause forbids ("A field with no value is present and
// `null` ... not omitted").
//
// The one thing this cannot separate is an upstream that sends an
// explicit `0` for a metric its device never measured: that arrives
// non-nil and renders as `0`, because nothing in the response
// distinguishes it from a real zero. Recorded here rather than guessed
// at — inventing an absence is as wrong as inventing a measurement.
type activityDay struct {
	Date          string   `json:"date"`
	Timezone      string   `json:"timezone"`
	Steps         *int     `json:"steps"`
	Distance      *float64 `json:"distance"`
	Elevation     *float64 `json:"elevation"`
	Soft          *int     `json:"soft"`
	Moderate      *int     `json:"moderate"`
	Intense       *int     `json:"intense"`
	Active        *int     `json:"active"`
	Calories      *float64 `json:"calories"`
	TotalCalories *float64 `json:"totalcalories"`
	HRAvg         *float64 `json:"hr_average"`
	HRMin         *float64 `json:"hr_min"`
	HRMax         *float64 `json:"hr_max"`
	HRZone0       *int     `json:"hr_zone_0"`
	HRZone1       *int     `json:"hr_zone_1"`
	HRZone2       *int     `json:"hr_zone_2"`
	HRZone3       *int     `json:"hr_zone_3"`
}

// activityRow is the canonical §4 row for `activity`, shared by all
// three codecs. Names carry their unit (`distance_m`, `soft_sec`) and
// match the CSV header exactly — see cmd/rowcodec.go.
//
// `date` is the row's instant in RFC3339-with-offset, which for a
// day-keyed row is local midnight; `calendar_date` is the day label
// itself. Splitting them is what lets `date` mean one thing across all
// five subcommands (§2, §4) without losing the plain day string.
//
// Every measured quantity is a pointer, carrying activityDay's
// nil-means-absent straight through to the wire: `null` in JSON, an
// empty cell in CSV. `date`, `calendar_date` and `timezone` are the
// only non-nullable fields, because a day record that lacks them is not
// a day record. `hr_avg` being nullable here is also what makes it one
// JSON type across `activity` and `sleep` — the §4 scalar-typing clause
// #27 was filed about.
type activityRow struct {
	Date          string   `json:"date"`
	CalendarDate  string   `json:"calendar_date"`
	Timezone      string   `json:"timezone"`
	Steps         *int     `json:"steps"`
	DistanceM     *float64 `json:"distance_m"`
	ElevationM    *float64 `json:"elevation_m"`
	SoftSec       *int     `json:"soft_sec"`
	ModerateSec   *int     `json:"moderate_sec"`
	IntenseSec    *int     `json:"intense_sec"`
	ActiveSec     *int     `json:"active_sec"`
	Calories      *float64 `json:"calories"`
	TotalCalories *float64 `json:"total_calories"`
	HRAvg         *float64 `json:"hr_avg"`
	HRMin         *float64 `json:"hr_min"`
	HRMax         *float64 `json:"hr_max"`
	HRZone0       *int     `json:"hr_zone_0"`
	HRZone1       *int     `json:"hr_zone_1"`
	HRZone2       *int     `json:"hr_zone_2"`
	HRZone3       *int     `json:"hr_zone_3"`
}

type activityResponse struct {
	Activities []activityDay `json:"activities"`
	More       bool          `json:"more"`
	Offset     int           `json:"offset"`
}

var (
	activityFormatFlag string
	activitySinceFlag  string
	activityUntilFlag  string
)

var activityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Export daily activity (steps, distance, calories, HR zones)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse-level flag validation runs before any network call so an
		// invalid --format exits non-zero hermetically (CONTRACT §4).
		format, err := validateFormat(activityFormatFlag)
		if err != nil {
			return err
		}
		since, err := sinceOrDefault(activitySinceFlag, 30)
		if err != nil {
			return err
		}
		until, err := untilDayOrToday(activityUntilFlag)
		if err != nil {
			return err
		}
		if printed, err := printResolvedWindow(cmd, since, until.AddDate(0, 0, 1)); printed || err != nil {
			return err
		}

		dataFields := "steps,distance,elevation,soft,moderate,intense,active," +
			"calories,totalcalories,hr_average,hr_min,hr_max,hr_zone_0,hr_zone_1,hr_zone_2,hr_zone_3"

		params := url.Values{}
		params.Set("action", "getactivity")
		params.Set("startdateymd", since.Format("2006-01-02"))
		params.Set("enddateymd", until.Format("2006-01-02"))
		params.Set("data_fields", dataFields)

		c := client.New()
		var all []activityDay
		for {
			var resp activityResponse
			if err := c.Call("/v2/measure", params, &resp); err != nil {
				return err
			}
			all = append(all, resp.Activities...)
			if !resp.More {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(all, func(i, j int) bool { return all[i].Date < all[j].Date })

		rows := make([]activityRow, 0, len(all))
		for _, d := range all {
			rows = append(rows, d.row())
		}

		switch format {
		case "json":
			return printJSON(rows)
		case "csv":
			return writeCSV(rows)
		default:
			return writeActivityMarkdown(rows)
		}
	},
}

// row converts one upstream activity day into the canonical §4 row,
// applying the decimal budgets from #26. The rounding lives here rather
// than in either serializer so JSON and CSV cannot disagree about it.
func (d activityDay) row() activityRow {
	return activityRow{
		Date:          localMidnightRFC3339(d.Date),
		CalendarDate:  d.Date,
		Timezone:      d.Timezone,
		Steps:         d.Steps,
		DistanceM:     roundPtr(d.Distance, 2),
		ElevationM:    roundPtr(d.Elevation, 2),
		SoftSec:       d.Soft,
		ModerateSec:   d.Moderate,
		IntenseSec:    d.Intense,
		ActiveSec:     d.Active,
		Calories:      roundPtr(d.Calories, 1),
		TotalCalories: roundPtr(d.TotalCalories, 1),
		HRAvg:         roundPtr(d.HRAvg, 1),
		HRMin:         roundPtr(d.HRMin, 0),
		HRMax:         roundPtr(d.HRMax, 0),
		HRZone0:       d.HRZone0,
		HRZone1:       d.HRZone1,
		HRZone2:       d.HRZone2,
		HRZone3:       d.HRZone3,
	}
}

// writeActivityMarkdown emits one fitdown-style block per day. Empty/zero
// fields are dropped to keep output tight.
//
// deref collapses an absent value to zero, which is exactly the right
// reading here: markdown already suppresses zero-valued lines, so an
// unreported metric and a zero one are both simply not printed. The
// distinction §4 cares about lives in the structured codecs, which
// cannot omit a field and so say `null` instead.
func writeActivityMarkdown(days []activityRow) error {
	for _, d := range days {
		fmt.Fprintf(os.Stdout, "Activity %s\n\n", d.CalendarDate)
		steps, distance := deref(d.Steps), deref(d.DistanceM)
		if steps > 0 || distance > 0 {
			fmt.Fprintf(os.Stdout, "%d steps, %.2f km\n", steps, distance/1000)
		}
		calories, totalCalories := deref(d.Calories), deref(d.TotalCalories)
		if calories > 0 || totalCalories > 0 {
			fmt.Fprintf(os.Stdout, "%s cal active, %s total\n", fmtRound(calories), fmtRound(totalCalories))
		}
		if hrAvg := deref(d.HRAvg); hrAvg > 0 {
			fmt.Fprintf(os.Stdout, "HR avg %s, %s-%s\n",
				fmtRound(hrAvg), fmtRound(deref(d.HRMin)), fmtRound(deref(d.HRMax)))
		}
		if activeSec := deref(d.ActiveSec); activeSec > 0 {
			fmt.Fprintf(os.Stdout, "Active %s — %s soft, %s moderate, %s intense\n",
				fmtDur(activeSec), fmtDur(deref(d.SoftSec)), fmtDur(deref(d.ModerateSec)), fmtDur(deref(d.IntenseSec)))
		}
		if elevation := deref(d.ElevationM); elevation > 0 {
			fmt.Fprintf(os.Stdout, "%s m elevation\n", fmtRound(elevation))
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// localMidnightRFC3339 renders a YYYY-MM-DD day label as local midnight
// of that day in RFC3339 with offset. Returns the input unchanged if it
// is not a parseable day, so a surprising upstream value is visible
// rather than replaced by a wrong timestamp.
func localMidnightRFC3339(day string) string {
	t, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return day
	}
	return t.Format(time.RFC3339)
}

func init() {
	activityCmd.Flags().StringVar(&activitySinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 30d)")
	activityCmd.Flags().StringVar(&activityUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default today)")
	activityCmd.Flags().StringVar(&activityFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
}
