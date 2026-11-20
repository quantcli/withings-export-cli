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

// Withings workout category codes. Common ones only — unknowns are rendered as numbers.
var workoutCategoryNames = map[int]string{
	1:   "walk",
	2:   "run",
	3:   "hiking",
	4:   "skating",
	5:   "bmx",
	6:   "bicycling",
	7:   "swimming",
	8:   "surfing",
	9:   "kitesurfing",
	10:  "windsurfing",
	11:  "bodyboard",
	12:  "tennis",
	13:  "tabletennis",
	14:  "squash",
	15:  "badminton",
	16:  "lift_weights",
	17:  "calisthenics",
	18:  "elliptical",
	19:  "pilates",
	20:  "basketball",
	21:  "soccer",
	22:  "football",
	23:  "rugby",
	24:  "volleyball",
	25:  "waterpolo",
	26:  "horse_riding",
	27:  "golf",
	28:  "yoga",
	29:  "dancing",
	30:  "boxing",
	31:  "fencing",
	32:  "wrestling",
	33:  "martial_arts",
	34:  "skiing",
	35:  "snowboarding",
	36:  "other",
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
	ID        int64       `json:"id"`
	Category  int         `json:"category"`
	Timezone  string      `json:"timezone"`
	Model     int         `json:"model"`
	Attrib    int         `json:"attrib"`
	StartDate int64       `json:"startdate"`
	EndDate   int64       `json:"enddate"`
	Date      string      `json:"date"`
	DeviceID  string      `json:"deviceid"`
	Data      workoutData `json:"data"`
}

// workoutData is the upstream `data` sub-object. Every field the
// subcommand asks for in data_fields is decoded here.
//
// It used to be a json.RawMessage that `--format json` passed straight
// through (#27), which meant the JSON key set depended on which fields
// the user's device happened to report — so no consumer could rely on a
// field existing — while `--format csv` surfaced a hand-picked subset
// under different names. Decoding into a declared struct makes the row
// schema fixed and the same in both codecs.
type workoutData struct {
	Calories          float64 `json:"calories"`
	EffDuration       int     `json:"effduration"`
	Intensity         int     `json:"intensity"`
	ManualDistance    float64 `json:"manual_distance"`
	ManualCalories    float64 `json:"manual_calories"`
	HRAvg             float64 `json:"hr_average"`
	HRMin             float64 `json:"hr_min"`
	HRMax             float64 `json:"hr_max"`
	HRZone0           int     `json:"hr_zone_0"`
	HRZone1           int     `json:"hr_zone_1"`
	HRZone2           int     `json:"hr_zone_2"`
	HRZone3           int     `json:"hr_zone_3"`
	PauseDuration     int     `json:"pause_duration"`
	AlgoPauseDuration int     `json:"algo_pause_duration"`
	SpO2Average       float64 `json:"spo2_average"`
	Steps             int     `json:"steps"`
	Distance          float64 `json:"distance"`
	Elevation         float64 `json:"elevation"`
	PoolLaps          int     `json:"pool_laps"`
	Strokes           int     `json:"strokes"`
	PoolLength        float64 `json:"pool_length"`
}

// workoutRow is the canonical §4 row for `workouts`, shared by all
// three codecs — see cmd/rowcodec.go.
//
// Durations are seconds, not minutes. CSV used to emit `duration_min`
// and `eff_duration_min` while JSON carried the upstream seconds, so
// the two codecs disagreed on the *unit* of the same quantity (#42 for
// the sleep equivalent). Seconds win because they are upstream's unit
// and `/60` is lossy — the 45m01s fixture in internal/stubapi exists to
// keep that honest. Human-friendly durations stay in markdown, which is
// where display formatting belongs.
type workoutRow struct {
	Date                 string  `json:"date"`
	CalendarDate         string  `json:"calendar_date"`
	Start                string  `json:"start"`
	End                  string  `json:"end"`
	StartEpoch           int64   `json:"start_epoch"`
	EndEpoch             int64   `json:"end_epoch"`
	ID                   int64   `json:"id"`
	Timezone             string  `json:"timezone"`
	Model                int     `json:"model"`
	Attrib               int     `json:"attrib"`
	Category             string  `json:"category"`
	CategoryCode         int     `json:"category_code"`
	DurationSec          int     `json:"duration_sec"`
	EffDurationSec       int     `json:"eff_duration_sec"`
	PauseDurationSec     int     `json:"pause_duration_sec"`
	AlgoPauseDurationSec int     `json:"algo_pause_duration_sec"`
	Calories             float64 `json:"calories"`
	ManualCalories       float64 `json:"manual_calories"`
	Intensity            int     `json:"intensity"`
	Steps                int     `json:"steps"`
	DistanceM            float64 `json:"distance_m"`
	ManualDistanceM      float64 `json:"manual_distance_m"`
	ElevationM           float64 `json:"elevation_m"`
	HRAvg                float64 `json:"hr_avg"`
	HRMin                float64 `json:"hr_min"`
	HRMax                float64 `json:"hr_max"`
	HRZone0              int     `json:"hr_zone_0"`
	HRZone1              int     `json:"hr_zone_1"`
	HRZone2              int     `json:"hr_zone_2"`
	HRZone3              int     `json:"hr_zone_3"`
	SpO2AvgPct           float64 `json:"spo2_avg_pct"`
	PoolLaps             int     `json:"pool_laps"`
	PoolLengthM          float64 `json:"pool_length_m"`
	Strokes              int     `json:"strokes"`
	DeviceID             *string `json:"device_id"`
}

type workoutsResponse struct {
	Series []workoutSeries `json:"series"`
	More   bool            `json:"more"`
	Offset int             `json:"offset"`
}

// row converts one upstream workout into the canonical §4 row, applying
// the decimal budgets from #26.
func (s workoutSeries) row() workoutRow {
	start := time.Unix(s.StartDate, 0).Local()
	end := time.Unix(s.EndDate, 0).Local()
	category := workoutCategoryNames[s.Category]
	if category == "" {
		category = "unknown"
	}
	var deviceID *string
	if s.DeviceID != "" {
		id := s.DeviceID
		deviceID = &id
	}
	d := s.Data
	return workoutRow{
		Date:                 localMidnightRFC3339(s.Date),
		CalendarDate:         s.Date,
		Start:                start.Format(time.RFC3339),
		End:                  end.Format(time.RFC3339),
		StartEpoch:           s.StartDate,
		EndEpoch:             s.EndDate,
		ID:                   s.ID,
		Timezone:             s.Timezone,
		Model:                s.Model,
		Attrib:               s.Attrib,
		Category:             category,
		CategoryCode:         s.Category,
		DurationSec:          int(s.EndDate - s.StartDate),
		EffDurationSec:       d.EffDuration,
		PauseDurationSec:     d.PauseDuration,
		AlgoPauseDurationSec: d.AlgoPauseDuration,
		Calories:             round(d.Calories, 1),
		ManualCalories:       round(d.ManualCalories, 1),
		Intensity:            d.Intensity,
		Steps:                d.Steps,
		DistanceM:            round(d.Distance, 2),
		ManualDistanceM:      round(d.ManualDistance, 2),
		ElevationM:           round(d.Elevation, 2),
		HRAvg:                round(d.HRAvg, 1),
		HRMin:                round(d.HRMin, 0),
		HRMax:                round(d.HRMax, 0),
		HRZone0:              d.HRZone0,
		HRZone1:              d.HRZone1,
		HRZone2:              d.HRZone2,
		HRZone3:              d.HRZone3,
		SpO2AvgPct:           round(d.SpO2Average, 1),
		PoolLaps:             d.PoolLaps,
		PoolLengthM:          round(d.PoolLength, 2),
		Strokes:              d.Strokes,
		DeviceID:             deviceID,
	}
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
		// Parse-level flag validation runs before any network call so an
		// invalid --format exits non-zero hermetically (CONTRACT §4).
		format, err := validateFormat(workoutsFormatFlag)
		if err != nil {
			return err
		}
		since, err := sinceOrDefault(workoutsSinceFlag, 90)
		if err != nil {
			return err
		}
		until, err := untilDayOrToday(workoutsUntilFlag)
		if err != nil {
			return err
		}
		if printed, err := printResolvedWindow(cmd, since, until.AddDate(0, 0, 1)); printed || err != nil {
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

		rows := make([]workoutRow, 0, len(all))
		for _, s := range all {
			rows = append(rows, s.row())
		}

		switch format {
		case "json":
			return printJSON(rows)
		case "csv":
			return writeCSV(rows)
		default:
			return writeWorkoutsMarkdown(rows)
		}
	},
}

// writeWorkoutsMarkdown emits one fitdown-style block per workout: a
// "Workout DATE" heading, the activity name, then concise stat lines. The
// shape mirrors fitdown's own examples (Workout … / category / details).
func writeWorkoutsMarkdown(rows []workoutRow) error {
	for _, s := range rows {
		fmt.Fprintf(os.Stdout, "Workout %s\n\n", s.CalendarDate)
		fmt.Fprintf(os.Stdout, "%s\n", s.Category)
		fmt.Fprintf(os.Stdout, "%s → %s (%d min)\n",
			time.Unix(s.StartEpoch, 0).Local().Format("15:04"),
			time.Unix(s.EndEpoch, 0).Local().Format("15:04"),
			s.DurationSec/60)
		if s.Calories > 0 {
			fmt.Fprintf(os.Stdout, "%s cal\n", fmtRound(s.Calories))
		}
		if s.HRAvg > 0 {
			fmt.Fprintf(os.Stdout, "HR avg %s, %s-%s\n", fmtRound(s.HRAvg), fmtRound(s.HRMin), fmtRound(s.HRMax))
		}
		if s.Steps > 0 {
			if s.DistanceM > 0 {
				fmt.Fprintf(os.Stdout, "%d steps, %s m\n", s.Steps, fmtRound(s.DistanceM))
			} else {
				fmt.Fprintf(os.Stdout, "%d steps\n", s.Steps)
			}
		} else if s.DistanceM > 0 {
			fmt.Fprintf(os.Stdout, "%s m\n", fmtRound(s.DistanceM))
		}
		if s.ElevationM > 0 {
			fmt.Fprintf(os.Stdout, "%s m elevation\n", fmtRound(s.ElevationM))
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
