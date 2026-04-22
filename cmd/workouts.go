package cmd

import (
	"encoding/csv"
	"encoding/json"
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
	workoutsJSONFlag  bool
	workoutsSinceFlag string
)

var workoutsCmd = &cobra.Command{
	Use:   "workouts",
	Short: "Export workouts (runs, walks, bikes, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, err := sinceOrDefault(workoutsSinceFlag, 90)
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
		params.Set("enddateymd", time.Now().Format("2006-01-02"))
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

		if workoutsJSONFlag {
			return printJSON(all)
		}
		return writeWorkoutsCSV(all)
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

func init() {
	workoutsCmd.Flags().StringVar(&workoutsSinceFlag, "since", "",
		"Filter on or after date (e.g. 2026-01-01, 30d, 4w, 6m, 1y; default 90d)")
	workoutsCmd.Flags().BoolVar(&workoutsJSONFlag, "json", false,
		"Output as JSON instead of CSV")
}
