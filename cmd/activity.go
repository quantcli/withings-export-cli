package cmd

import (
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"

	"github.com/quantcli/withings-export-cli/internal/client"
	"github.com/spf13/cobra"
)

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

		switch format {
		case "json":
			return printJSON(all)
		case "csv":
			return writeActivityCSV(all)
		default:
			return writeActivityMarkdown(all)
		}
	},
}

func writeActivityCSV(days []activityDay) error {
	w := csv.NewWriter(os.Stdout)
	header := []string{
		"date", "timezone", "steps", "distance_m", "elevation_m",
		"soft_sec", "moderate_sec", "intense_sec", "active_sec",
		"calories", "total_calories",
		"hr_avg", "hr_min", "hr_max",
		"hr_zone_0", "hr_zone_1", "hr_zone_2", "hr_zone_3",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, d := range days {
		row := activityCSVRow(d)
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error() // a failed write must exit non-zero
}

// activityCSVRow leaves unreported measurements empty instead of writing 0.
func activityCSVRow(d activityDay) []string {
	return []string{
		d.Date,
		d.Timezone,
		csvInt(d.Steps, 1),
		csvFloat(d.Distance),
		csvFloat(d.Elevation),
		csvInt(d.Soft, 1),
		csvInt(d.Moderate, 1),
		csvInt(d.Intense, 1),
		csvInt(d.Active, 1),
		csvFloat(d.Calories),
		csvFloat(d.TotalCalories),
		csvFloat(d.HRAvg),
		csvFloat(d.HRMin),
		csvFloat(d.HRMax),
		csvInt(d.HRZone0, 1),
		csvInt(d.HRZone1, 1),
		csvInt(d.HRZone2, 1),
		csvInt(d.HRZone3, 1),
	}
}

// writeActivityMarkdown emits one fitdown-style block per day. Empty/zero
// fields are dropped to keep output tight.
func writeActivityMarkdown(days []activityDay) error {
	for _, d := range days {
		steps := activityValue(d.Steps)
		distance := activityValue(d.Distance)
		calories := activityValue(d.Calories)
		totalCalories := activityValue(d.TotalCalories)
		hrAvg := activityValue(d.HRAvg)
		hrMin := activityValue(d.HRMin)
		hrMax := activityValue(d.HRMax)
		active := activityValue(d.Active)
		soft := activityValue(d.Soft)
		moderate := activityValue(d.Moderate)
		intense := activityValue(d.Intense)
		elevation := activityValue(d.Elevation)
		fmt.Fprintf(os.Stdout, "Activity %s\n\n", d.Date)
		if steps > 0 || distance > 0 {
			fmt.Fprintf(os.Stdout, "%d steps, %.2f km\n", steps, distance/1000)
		}
		if calories > 0 || totalCalories > 0 {
			fmt.Fprintf(os.Stdout, "%s cal active, %s total\n", fmtRound(calories), fmtRound(totalCalories))
		}
		if hrAvg > 0 {
			fmt.Fprintf(os.Stdout, "HR avg %s, %s-%s\n", fmtRound(hrAvg), fmtRound(hrMin), fmtRound(hrMax))
		}
		if active > 0 {
			fmt.Fprintf(os.Stdout, "Active %s — %s soft, %s moderate, %s intense\n",
				fmtDur(active), fmtDur(soft), fmtDur(moderate), fmtDur(intense))
		}
		if elevation > 0 {
			fmt.Fprintf(os.Stdout, "%s m elevation\n", fmtRound(elevation))
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func activityValue[T int | float64](v *T) T {
	if v == nil {
		return 0
	}
	return *v
}

func init() {
	activityCmd.Flags().StringVar(&activitySinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 30d)")
	activityCmd.Flags().StringVar(&activityUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default today)")
	activityCmd.Flags().StringVar(&activityFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
}
