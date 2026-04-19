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

type sleepSeries struct {
	ID        int64           `json:"id"`
	Timezone  string          `json:"timezone"`
	Model     int             `json:"model"`
	StartDate int64           `json:"startdate"`
	EndDate   int64           `json:"enddate"`
	Date      string          `json:"date"`
	Data      json.RawMessage `json:"data"`
}

type sleepResponse struct {
	Series []sleepSeries `json:"series"`
	More   int           `json:"more"`
	Offset int           `json:"offset"`
}

var (
	sleepJSONFlag  bool
	sleepSinceFlag string
)

var sleepCmd = &cobra.Command{
	Use:   "sleep",
	Short: "Export nightly sleep summaries",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, err := sinceOrDefault(sleepSinceFlag, 30)
		if err != nil {
			return err
		}

		// All sleep summary data fields we care about. Must be explicitly requested.
		dataFields := "wakeupduration,lightsleepduration,deepsleepduration,remsleepduration," +
			"durationtosleep,durationtowakeup,wakeupcount,sleep_score," +
			"hr_average,hr_min,hr_max,rr_average,rr_min,rr_max,breathing_disturbances_intensity," +
			"snoring,snoringepisodecount,apnea_hypopnea_index"

		params := url.Values{}
		params.Set("action", "getsummary")
		params.Set("startdateymd", since.Format("2006-01-02"))
		params.Set("enddateymd", time.Now().Format("2006-01-02"))
		params.Set("data_fields", dataFields)

		c := client.New()
		var all []sleepSeries
		for {
			var resp sleepResponse
			if err := c.Call("/v2/sleep", params, &resp); err != nil {
				return err
			}
			all = append(all, resp.Series...)
			if resp.More == 0 {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(all, func(i, j int) bool { return all[i].StartDate < all[j].StartDate })

		if sleepJSONFlag {
			return printJSON(all)
		}
		return writeSleepCSV(all)
	},
}

func writeSleepCSV(series []sleepSeries) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	header := []string{
		"date", "start", "end", "timezone",
		"total_sleep_min", "light_min", "deep_min", "rem_min",
		"time_to_sleep_sec", "time_to_wakeup_sec", "wakeup_count",
		"sleep_score", "hr_avg", "hr_min", "hr_max",
		"rr_avg", "rr_min", "rr_max", "snore_episodes", "apnea_hypopnea_index",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, s := range series {
		var d struct {
			Wake           int     `json:"wakeupduration"`
			Light          int     `json:"lightsleepduration"`
			Deep           int     `json:"deepsleepduration"`
			REM            int     `json:"remsleepduration"`
			ToSleep        int     `json:"durationtosleep"`
			ToWakeup       int     `json:"durationtowakeup"`
			WakeupCount    int     `json:"wakeupcount"`
			SleepScore     int     `json:"sleep_score"`
			HRAvg          float64 `json:"hr_average"`
			HRMin          float64 `json:"hr_min"`
			HRMax          float64 `json:"hr_max"`
			RRAvg          float64 `json:"rr_average"`
			RRMin          float64 `json:"rr_min"`
			RRMax          float64 `json:"rr_max"`
			SnoreEpisodes  int     `json:"snoringepisodecount"`
			ApneaHypopnea  float64 `json:"apnea_hypopnea_index"`
		}
		_ = json.Unmarshal(s.Data, &d)

		start := time.Unix(s.StartDate, 0).UTC()
		end := time.Unix(s.EndDate, 0).UTC()
		totalSleepMin := (d.Light + d.Deep + d.REM) / 60

		row := []string{
			s.Date,
			start.Format(time.RFC3339),
			end.Format(time.RFC3339),
			s.Timezone,
			strconv.Itoa(totalSleepMin),
			strconv.Itoa(d.Light / 60),
			strconv.Itoa(d.Deep / 60),
			strconv.Itoa(d.REM / 60),
			strconv.Itoa(d.ToSleep),
			strconv.Itoa(d.ToWakeup),
			strconv.Itoa(d.WakeupCount),
			strconv.Itoa(d.SleepScore),
			strconv.FormatFloat(d.HRAvg, 'f', -1, 64),
			strconv.FormatFloat(d.HRMin, 'f', -1, 64),
			strconv.FormatFloat(d.HRMax, 'f', -1, 64),
			strconv.FormatFloat(d.RRAvg, 'f', -1, 64),
			strconv.FormatFloat(d.RRMin, 'f', -1, 64),
			strconv.FormatFloat(d.RRMax, 'f', -1, 64),
			strconv.Itoa(d.SnoreEpisodes),
			strconv.FormatFloat(d.ApneaHypopnea, 'f', -1, 64),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	sleepCmd.Flags().StringVar(&sleepSinceFlag, "since", "",
		"Filter on or after date (e.g. 2026-01-01, 30d, 4w, 6m, 1y; default 30d)")
	sleepCmd.Flags().BoolVar(&sleepJSONFlag, "json", false,
		"Output as JSON instead of CSV")
}
