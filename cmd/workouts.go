package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/quantcli/withings-export-cli/internal/client"
	"github.com/spf13/cobra"
)

// Withings workout category codes. Common ones only — unknowns are rendered as numbers.
var workoutCategoryNames = map[int]string{
	1:  "walk",
	2:  "run",
	3:  "hiking",
	4:  "skating",
	5:  "bmx",
	6:  "bicycling",
	7:  "swimming",
	8:  "surfing",
	9:  "kitesurfing",
	10: "windsurfing",
	11: "bodyboard",
	12: "tennis",
	13: "tabletennis",
	14: "squash",
	15: "badminton",
	16: "lift_weights",
	17: "calisthenics",
	18: "elliptical",
	19: "pilates",
	20: "basketball",
	21: "soccer",
	22: "football",
	23: "rugby",
	24: "volleyball",
	25: "waterpolo",
	26: "horse_riding",
	27: "golf",
	28: "yoga",
	29: "dancing",
	30: "boxing",
	31: "fencing",
	32: "wrestling",
	33: "martial_arts",
	34: "skiing",
	35: "snowboarding",
	36: "other",
	128: "rowing",
	187: "zumba",
	188: "baseball",
	191: "handball",
	192: "hockey",
	193: "ice_hockey",
	194: "climbing",
	195: "ice_skating",
	306: "multi_sport",
	307: "indoor_walk",
	308: "indoor_run",
	309: "indoor_cycle",
}

type workoutSeries struct {
	ID        int64           `json:"id"`
	Category  int             `json:"category"`
	Timezone  string          `json:"timezone"`
	Model     int             `json:"model"`
	Attrib    int             `json:"attrib"`
	StartDate int64           `json:"startdate"`
	EndDate   int64           `json:"enddate"`
	Date      string          `json:"date"`
	DeviceID  string          `json:"deviceid"`
	Data      json.RawMessage `json:"data"`
}

type workoutsResponse struct {
	Series []workoutSeries `json:"series"`
	More   bool            `json:"more"`
	Offset int             `json:"offset"`
}

var (
	workoutsFormatFlag string
	workoutsSinceFlag  string
	workoutsUntilFlag  string
)

var workoutsCmd = &cobra.Command{
	Use:   "workouts",
	Short: "Export workouts (runs, walks, bikes, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, err := sinceOrDefault(workoutsSinceFlag, 90)
		if err != nil {
			return err
		}
		until, err := untilDayOrToday(workoutsUntilFlag)
		if err != nil {
			return err
		}

		dataFields := "calories,effduration,intensity,manual_distance,manual_calories," +
			"hr_average,hr_min,hr_max,hr_zone_0,hr_zone_1,hr_zone_2,hr_zone_3," +
			"pause_duration,algo_pause_duration,spo2_average," +
			"steps,distance,elevation,pool_laps,strokes,pool_length"

		params := url.Values{}
		params.Set("action", "getworkouts")
		params.Set("startdateymd", since.Format("2006-01-02"))
		params.Set("enddateymd", until.Format("2006-01-02"))
		params.Set("data_fields", dataFields)

		c := client.New()
		var all []workoutSeries
		for {
			var resp workoutsResponse
			if err := c.Call("/v2/measure", params, &resp); err != nil {
				return err
			}
			all = append(all, resp.Series...)
			if !resp.More {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(all, func(i, j int) bool { return all[i].StartDate < all[j].StartDate })

		format, err := validateFormat(workoutsFormatFlag)
		if err != nil {
			return err
		}
		switch format {
		case "json":
			return printJSON(all)
		case "csv":
			return writeWorkoutsCSV(all)
		default:
			return writeWorkoutsMarkdown(all)
		}
	},
}

func writeWorkoutsCSV(series []workoutSeries) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	header := []string{
		"date", "start", "end", "category", "category_code", "timezone",
		"duration_min", "eff_duration_min", "calories", "steps",
		"distance_m", "elevation_m",
		"hr_avg", "hr_min", "hr_max",
		"hr_zone_0", "hr_zone_1", "hr_zone_2", "hr_zone_3",
		"device_id",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, s := range series {
		var d struct {
			Calories    float64 `json:"calories"`
			EffDuration int     `json:"effduration"`
			Distance    float64 `json:"distance"`
			Elevation   float64 `json:"elevation"`
			Steps       int     `json:"steps"`
			HRAvg       float64 `json:"hr_average"`
			HRMin       float64 `json:"hr_min"`
			HRMax       float64 `json:"hr_max"`
			HRZone0     int     `json:"hr_zone_0"`
			HRZone1     int     `json:"hr_zone_1"`
			HRZone2     int     `json:"hr_zone_2"`
			HRZone3     int     `json:"hr_zone_3"`
		}
		_ = json.Unmarshal(s.Data, &d)

		start := time.Unix(s.StartDate, 0).Local()
		end := time.Unix(s.EndDate, 0).Local()
		category := workoutCategoryNames[s.Category]
		if category == "" {
			category = "unknown"
		}

		row := []string{
			s.Date,
			start.Format(time.RFC3339),
			end.Format(time.RFC3339),
			category,
			strconv.Itoa(s.Category),
			s.Timezone,
			strconv.Itoa(int(end.Sub(start).Minutes())),
			strconv.Itoa(d.EffDuration / 60),
			strconv.FormatFloat(d.Calories, 'f', -1, 64),
			strconv.Itoa(d.Steps),
			strconv.FormatFloat(d.Distance, 'f', -1, 64),
			strconv.FormatFloat(d.Elevation, 'f', -1, 64),
			strconv.FormatFloat(d.HRAvg, 'f', -1, 64),
			strconv.FormatFloat(d.HRMin, 'f', -1, 64),
			strconv.FormatFloat(d.HRMax, 'f', -1, 64),
			strconv.Itoa(d.HRZone0),
			strconv.Itoa(d.HRZone1),
			strconv.Itoa(d.HRZone2),
			strconv.Itoa(d.HRZone3),
			s.DeviceID,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

// writeWorkoutsMarkdown emits one fitdown-style block per workout: a
// "Workout DATE" heading, the activity name, then concise stat lines. The
// shape mirrors fitdown's own examples (Workout … / category / details).
func writeWorkoutsMarkdown(series []workoutSeries) error {
	for _, s := range series {
		var d struct {
			Calories    float64 `json:"calories"`
			EffDuration int     `json:"effduration"`
			Distance    float64 `json:"distance"`
			Elevation   float64 `json:"elevation"`
			Steps       int     `json:"steps"`
			HRAvg       float64 `json:"hr_average"`
			HRMin       float64 `json:"hr_min"`
			HRMax       float64 `json:"hr_max"`
			HRZone0     int     `json:"hr_zone_0"`
			HRZone1     int     `json:"hr_zone_1"`
			HRZone2     int     `json:"hr_zone_2"`
			HRZone3     int     `json:"hr_zone_3"`
		}
		_ = json.Unmarshal(s.Data, &d)

		start := time.Unix(s.StartDate, 0).Local()
		end := time.Unix(s.EndDate, 0).Local()
		category := workoutCategoryNames[s.Category]
		if category == "" {
			category = "unknown"
		}

		fmt.Fprintf(os.Stdout, "Workout %s\n\n", s.Date)
		fmt.Fprintf(os.Stdout, "%s\n", category)
		fmt.Fprintf(os.Stdout, "%s → %s (%d min)\n",
			start.Format("15:04"), end.Format("15:04"),
			int(end.Sub(start).Minutes()))
		if d.Calories > 0 {
			fmt.Fprintf(os.Stdout, "%s cal\n", fmtRound(d.Calories))
		}
		if d.HRAvg > 0 {
			fmt.Fprintf(os.Stdout, "HR avg %s, %s-%s\n", fmtRound(d.HRAvg), fmtRound(d.HRMin), fmtRound(d.HRMax))
		}
		if d.Steps > 0 {
			if d.Distance > 0 {
				fmt.Fprintf(os.Stdout, "%d steps, %s m\n", d.Steps, fmtRound(d.Distance))
			} else {
				fmt.Fprintf(os.Stdout, "%d steps\n", d.Steps)
			}
		} else if d.Distance > 0 {
			fmt.Fprintf(os.Stdout, "%s m\n", fmtRound(d.Distance))
		}
		if d.Elevation > 0 {
			fmt.Fprintf(os.Stdout, "%s m elevation\n", fmtRound(d.Elevation))
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func init() {
	workoutsCmd.Flags().StringVar(&workoutsSinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 90d)")
	workoutsCmd.Flags().StringVar(&workoutsUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default today)")
	workoutsCmd.Flags().StringVar(&workoutsFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
}
