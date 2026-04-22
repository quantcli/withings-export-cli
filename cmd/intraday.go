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

// Withings /v2/measure action=getintradayactivity returns at most 24h per call.
const intradayWindow = 24 * time.Hour

type intradaySample struct {
	Timestamp  int64   `json:"timestamp"`
	Duration   int     `json:"duration"`
	Steps      int     `json:"steps"`
	Distance   float64 `json:"distance"`
	Elevation  float64 `json:"elevation"`
	Calories   float64 `json:"calories"`
	HeartRate  int     `json:"heart_rate"`
	HRVQuality int     `json:"hrv_quality"`
	RMSSD      float64 `json:"rmssd"`
	SDNN1      float64 `json:"sdnn1"`
	SpO2       float64 `json:"spo2_auto"`
	Model      string  `json:"model"`
	ModelID    int     `json:"model_id"`
}

type intradayPayload struct {
	Duration   int     `json:"duration"`
	Steps      int     `json:"steps"`
	Distance   float64 `json:"distance"`
	Elevation  float64 `json:"elevation"`
	Calories   float64 `json:"calories"`
	HeartRate  int     `json:"heart_rate"`
	HRVQuality int     `json:"hrv_quality"`
	RMSSD      float64 `json:"rmssd"`
	SDNN1      float64 `json:"sdnn1"`
	SpO2       float64 `json:"spo2_auto"`
	Model      string  `json:"model"`
	ModelID    int     `json:"model_id"`
}

type intradayResponse struct {
	Series map[string]intradayPayload `json:"series"`
}

var (
	intradayJSONFlag  bool
	intradaySinceFlag string
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
		since, err := sinceOrDefault(intradaySinceFlag, 1)
		if err != nil {
			return err
		}

		dataFields := "steps,elevation,calories,distance,duration,heart_rate," +
			"hrv_quality,rmssd,sdnn1,spo2_auto"

		c := client.New()
		var all []intradaySample
		for chunkStart := since; chunkStart.Before(time.Now()); chunkStart = chunkStart.Add(intradayWindow) {
			chunkEnd := chunkStart.Add(intradayWindow)
			if chunkEnd.After(time.Now()) {
				chunkEnd = time.Now()
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
				all = append(all, intradaySample{
					Timestamp:  ts,
					Duration:   p.Duration,
					Steps:      p.Steps,
					Distance:   p.Distance,
					Elevation:  p.Elevation,
					Calories:   p.Calories,
					HeartRate:  p.HeartRate,
					HRVQuality: p.HRVQuality,
					RMSSD:      p.RMSSD,
					SDNN1:      p.SDNN1,
					SpO2:       p.SpO2,
					Model:      p.Model,
					ModelID:    p.ModelID,
				})
			}
		}

		sort.Slice(all, func(i, j int) bool { return all[i].Timestamp < all[j].Timestamp })

		if intradayJSONFlag {
			return printJSON(all)
		}
		return writeIntradayCSV(all)
	},
}

func writeIntradayCSV(samples []intradaySample) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	header := []string{
		"timestamp", "datetime", "duration_sec",
		"steps", "distance_m", "elevation_m", "calories",
		"heart_rate", "hrv_rmssd", "hrv_sdnn1", "hrv_quality", "spo2_auto",
		"model", "model_id",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, s := range samples {
		row := []string{
			strconv.FormatInt(s.Timestamp, 10),
			time.Unix(s.Timestamp, 0).Local().Format(time.RFC3339),
			strconv.Itoa(s.Duration),
			strconv.Itoa(s.Steps),
			strconv.FormatFloat(s.Distance, 'f', -1, 64),
			strconv.FormatFloat(s.Elevation, 'f', -1, 64),
			strconv.FormatFloat(s.Calories, 'f', -1, 64),
			strconv.Itoa(s.HeartRate),
			strconv.FormatFloat(s.RMSSD, 'f', -1, 64),
			strconv.FormatFloat(s.SDNN1, 'f', -1, 64),
			strconv.Itoa(s.HRVQuality),
			strconv.FormatFloat(s.SpO2, 'f', -1, 64),
			s.Model,
			strconv.Itoa(s.ModelID),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	intradayCmd.Flags().StringVar(&intradaySinceFlag, "since", "",
		"Filter on or after date (e.g. 2026-04-15, 1d, 4w, 6m; default 1d)")
	intradayCmd.Flags().BoolVar(&intradayJSONFlag, "json", false,
		"Output as JSON instead of CSV")
}
