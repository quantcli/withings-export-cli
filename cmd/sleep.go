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
	// Source is synthetic (not from API): "summary" for getsummary rows,
	// "derived" for rows polyfilled from intraday samples via --derive.
	Source string `json:"source"`
}

type sleepResponse struct {
	Series []sleepSeries `json:"series"`
	More   bool          `json:"more"`
	Offset int           `json:"offset"`
}

var (
	sleepJSONFlag   bool
	sleepSinceFlag  string
	sleepDeriveFlag bool
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
			if !resp.More {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		haveDate := make(map[string]bool, len(all))
		for i := range all {
			all[i].Source = "summary"
			haveDate[all[i].Date] = true
		}

		if sleepDeriveFlag {
			today := time.Now()
			first := true
			for d := since; !d.After(today); d = d.AddDate(0, 0, 1) {
				dateStr := d.Format("2006-01-02")
				if haveDate[dateStr] {
					continue
				}
				// Throttle — Withings rate-limits aggressive callers (status 601).
				// 250ms between calls keeps a wide window-derive under the cap.
				if !first {
					time.Sleep(250 * time.Millisecond)
				}
				first = false
				derived, err := deriveSleep(c, d)
				if err != nil {
					return err
				}
				if derived != nil {
					all = append(all, *derived)
				}
			}
		}

		sort.Slice(all, func(i, j int) bool { return all[i].StartDate < all[j].StartDate })

		if sleepJSONFlag {
			return printJSON(all)
		}
		return writeSleepCSV(all)
	},
}

// deriveSleep polyfills a sleep start/end for a night that has no getsummary
// record, by finding the longest contiguous "quiet" run of intraday samples.
//
// Window: prior-day 18:00 local → current-day 12:00 local (18h — fits in one
// 24h getintradayactivity call). A sample is "quiet" when heart_rate is
// present, ≤ 80 bpm, and steps == 0. Runs tolerate gaps ≤ 60 min between
// consecutive quiet samples (watch off for charging). The longest qualifying
// run ≥ 3h becomes the derived sleep session. Returns (nil, nil) when no
// window qualifies or intraday has no samples.
func deriveSleep(c *client.Client, date time.Time) (*sleepSeries, error) {
	loc := time.Local
	day := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	winStart := day.AddDate(0, 0, -1).Add(18 * time.Hour)
	winEnd := day.Add(12 * time.Hour)

	params := url.Values{}
	params.Set("action", "getintradayactivity")
	params.Set("startdate", strconv.FormatInt(winStart.Unix(), 10))
	params.Set("enddate", strconv.FormatInt(winEnd.Unix(), 10))
	params.Set("data_fields", "steps,heart_rate,duration")

	var resp intradayResponse
	if err := c.Call("/v2/measure", params, &resp); err != nil {
		return nil, err
	}

	type sample struct {
		ts    int64
		hr    int
		steps int
	}
	samples := make([]sample, 0, len(resp.Series))
	for tsStr, p := range resp.Series {
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			continue
		}
		samples = append(samples, sample{ts: ts, hr: p.HeartRate, steps: p.Steps})
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].ts < samples[j].ts })
	if len(samples) == 0 {
		return nil, nil
	}

	// A sample "breaks" a sleep run when it shows real wakefulness: elevated
	// heart rate, or a meaningful burst of steps. Small step samples (bathroom
	// trips, rolling over) do not break the run — they're just HealthKit's way
	// of reporting brief motion without an accompanying HR reading.
	const hrBreak = 90  // bpm above this = awake/active
	const stepsBreak = 30

	const maxGap int64 = 90 * 60  // 90 min — tolerate watch-off-for-charging
	const minDur int64 = 3 * 3600 // 3h — ignore naps

	isBreak := func(s sample) bool { return s.hr > hrBreak || s.steps > stepsBreak }

	type run struct {
		start, end int64
		hrSum      int
		hrCount    int
	}
	var runs []run
	var cur run
	inRun := false
	var lastTS int64

	startRun := func(s sample) {
		cur = run{start: s.ts, end: s.ts}
		if s.hr > 0 {
			cur.hrSum = s.hr
			cur.hrCount = 1
		}
		inRun = true
	}
	extendRun := func(s sample) {
		cur.end = s.ts
		if s.hr > 0 {
			cur.hrSum += s.hr
			cur.hrCount++
		}
	}
	flushRun := func() {
		runs = append(runs, cur)
		inRun = false
	}

	for _, s := range samples {
		if isBreak(s) {
			if inRun {
				flushRun()
			}
			continue
		}
		switch {
		case !inRun:
			startRun(s)
		case s.ts-lastTS > maxGap:
			flushRun()
			startRun(s)
		default:
			extendRun(s)
		}
		lastTS = s.ts
	}
	if inRun {
		flushRun()
	}

	// Accept the longest run that looks like sleep: ≥ 3h, with enough HR data
	// to confirm it's not just "watch not worn," and a mean HR in the sleep range.
	var best *run
	for i := range runs {
		r := &runs[i]
		if r.end-r.start < minDur {
			continue
		}
		if r.hrCount < 10 {
			continue
		}
		if float64(r.hrSum)/float64(r.hrCount) > 80 {
			continue
		}
		if best == nil || r.end-r.start > best.end-best.start {
			best = r
		}
	}
	if best == nil {
		return nil, nil
	}

	bestStart := best.start
	bestEnd := best.end
	bestDur := best.end - best.start
	hrAvg := float64(best.hrSum) / float64(best.hrCount)

	// Stuff total duration into lightsleepduration so the existing CSV writer's
	// total_sleep_min = (light+deep+rem)/60 renders correctly. Derived rows
	// have no stage breakdown, so all sleep time shows as "light".
	dataObj := map[string]any{
		"lightsleepduration": bestDur,
		"hr_average":         hrAvg,
	}
	data, _ := json.Marshal(dataObj)

	return &sleepSeries{
		Timezone:  loc.String(),
		StartDate: bestStart,
		EndDate:   bestEnd,
		Date:      date.Format("2006-01-02"),
		Data:      json.RawMessage(data),
		Source:    "derived",
	}, nil
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
		"source",
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

		start := time.Unix(s.StartDate, 0).Local()
		end := time.Unix(s.EndDate, 0).Local()
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
			s.Source,
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
	sleepCmd.Flags().BoolVar(&sleepDeriveFlag, "derive", false,
		"For nights with no Withings sleep summary, polyfill start/end from intraday heart-rate samples")
}
