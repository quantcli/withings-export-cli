package cmd

import (
	"encoding/csv"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/quantcli/withings-export-cli/internal/client"
	"github.com/spf13/cobra"
)

type activityDay struct {
	Date          string  `json:"date"`
	Timezone      string  `json:"timezone"`
	Steps         int     `json:"steps"`
	Distance      float64 `json:"distance"`
	Elevation     float64 `json:"elevation"`
	Soft          int     `json:"soft"`
	Moderate      int     `json:"moderate"`
	Intense       int     `json:"intense"`
	Active        int     `json:"active"`
	Calories      float64 `json:"calories"`
	TotalCalories float64 `json:"totalcalories"`
	HRAvg         float64 `json:"hr_average"`
	HRMin         float64 `json:"hr_min"`
	HRMax         float64 `json:"hr_max"`
	HRZone0       int     `json:"hr_zone_0"`
	HRZone1       int     `json:"hr_zone_1"`
	HRZone2       int     `json:"hr_zone_2"`
	HRZone3       int     `json:"hr_zone_3"`
}

type activityResponse struct {
	Activities []activityDay `json:"activities"`
	More       int           `json:"more"`
	Offset     int           `json:"offset"`
}

var (
	activityJSONFlag  bool
	activitySinceFlag string
)

var activityCmd = &cobra.Command{
	Use:   "activity",
	Short: "Export daily activity (steps, distance, calories, HR zones)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, err := sinceOrDefault(activitySinceFlag, 30)
		if err != nil {
			return err
		}

		dataFields := "steps,distance,elevation,soft,moderate,intense,active," +
			"calories,totalcalories,hr_average,hr_min,hr_max,hr_zone_0,hr_zone_1,hr_zone_2,hr_zone_3"

		params := url.Values{}
		params.Set("action", "getactivity")
		params.Set("startdateymd", since.Format("2006-01-02"))
		params.Set("enddateymd", time.Now().Format("2006-01-02"))
		params.Set("data_fields", dataFields)

		c := client.New()
		var all []activityDay
		for {
			var resp activityResponse
			if err := c.Call("/v2/measure", params, &resp); err != nil {
				return err
			}
			all = append(all, resp.Activities...)
			if resp.More == 0 {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(all, func(i, j int) bool { return all[i].Date < all[j].Date })

		if activityJSONFlag {
			return printJSON(all)
		}
		return writeActivityCSV(all)
	},
}

func writeActivityCSV(days []activityDay) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
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
		row := []string{
			d.Date,
			d.Timezone,
			strconv.Itoa(d.Steps),
			strconv.FormatFloat(d.Distance, 'f', -1, 64),
			strconv.FormatFloat(d.Elevation, 'f', -1, 64),
			strconv.Itoa(d.Soft),
			strconv.Itoa(d.Moderate),
			strconv.Itoa(d.Intense),
			strconv.Itoa(d.Active),
			strconv.FormatFloat(d.Calories, 'f', -1, 64),
			strconv.FormatFloat(d.TotalCalories, 'f', -1, 64),
			strconv.FormatFloat(d.HRAvg, 'f', -1, 64),
			strconv.FormatFloat(d.HRMin, 'f', -1, 64),
			strconv.FormatFloat(d.HRMax, 'f', -1, 64),
			strconv.Itoa(d.HRZone0),
			strconv.Itoa(d.HRZone1),
			strconv.Itoa(d.HRZone2),
			strconv.Itoa(d.HRZone3),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	activityCmd.Flags().StringVar(&activitySinceFlag, "since", "",
		"Filter on or after date (e.g. 2026-01-01, 30d, 4w, 6m, 1y; default 30d)")
	activityCmd.Flags().BoolVar(&activityJSONFlag, "json", false,
		"Output as JSON instead of CSV")
}
