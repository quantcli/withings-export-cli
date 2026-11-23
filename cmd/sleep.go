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

type sleepSeries struct {
	ID        int64     `json:"id"`
	Timezone  string    `json:"timezone"`
	Model     int       `json:"model"`
	StartDate int64     `json:"startdate"`
	EndDate   int64     `json:"enddate"`
	Date      string    `json:"date"`
	Data      sleepData `json:"data"`
}

// sleepData is the upstream `data` sub-object.
//
// Every field is a pointer because Withings only sends the ones the
// recording device supports — sleep score and the apnea index appear
// only on some hardware. Decoding into value types made "my device does
// not measure this" indistinguishable from "this was zero", which is
// what CONTRACT §4 forbids: an absent value is `null`, never 0, "so a
// missing measurement can't be read as a real one".
type sleepData struct {
	WakeupDuration        *int     `json:"wakeupduration"`
	LightSleepDuration    *int     `json:"lightsleepduration"`
	DeepSleepDuration     *int     `json:"deepsleepduration"`
	REMSleepDuration      *int     `json:"remsleepduration"`
	DurationToSleep       *int     `json:"durationtosleep"`
	DurationToWakeup      *int     `json:"durationtowakeup"`
	WakeupCount           *int     `json:"wakeupcount"`
	SleepScore            *int     `json:"sleep_score"`
	HRAvg                 *float64 `json:"hr_average"`
	HRMin                 *float64 `json:"hr_min"`
	HRMax                 *float64 `json:"hr_max"`
	RRAvg                 *float64 `json:"rr_average"`
	RRMin                 *float64 `json:"rr_min"`
	RRMax                 *float64 `json:"rr_max"`
	BreathingDisturbances *int     `json:"breathing_disturbances_intensity"`
	Snoring               *int     `json:"snoring"`
	SnoringEpisodeCount   *int     `json:"snoringepisodecount"`
	ApneaHypopneaIndex    *float64 `json:"apnea_hypopnea_index"`
}

// sleepRow is the canonical §4 row for `sleep`, shared by all three
// codecs — see cmd/rowcodec.go.
//
// Durations are seconds. Before this change `--format csv` emitted
// `total_sleep_min` / `light_min` / `deep_min` / `rem_min` while
// `--format json` passed the upstream blob through with the same
// quantities nested and in seconds — issue #42, "not two encodings of
// one dataset; two different datasets". Seconds win because they are
// upstream's unit and `/60` is lossy; markdown still renders "8h04",
// which is where human formatting belongs.
//
// Everything the `--derive` heuristic cannot measure is a pointer, so a
// derived row reports `null` rather than claiming a measured zero
// (#21). `source` distinguishes the two origins.
type sleepRow struct {
	Date                  string   `json:"date"`
	CalendarDate          string   `json:"calendar_date"`
	Start                 string   `json:"start"`
	End                   string   `json:"end"`
	StartEpoch            int64    `json:"start_epoch"`
	EndEpoch              int64    `json:"end_epoch"`
	ID                    *int64   `json:"id"`
	Timezone              string   `json:"timezone"`
	Model                 *int     `json:"model"`
	TotalSleepSec         *int     `json:"total_sleep_sec"`
	LightSec              *int     `json:"light_sec"`
	DeepSec               *int     `json:"deep_sec"`
	REMSec                *int     `json:"rem_sec"`
	AwakeSec              *int     `json:"awake_sec"`
	TimeToSleepSec        *int     `json:"time_to_sleep_sec"`
	TimeToWakeupSec       *int     `json:"time_to_wakeup_sec"`
	WakeupCount           *int     `json:"wakeup_count"`
	SleepScore            *int     `json:"sleep_score"`
	HRAvg                 *float64 `json:"hr_avg"`
	HRMin                 *float64 `json:"hr_min"`
	HRMax                 *float64 `json:"hr_max"`
	RRAvg                 *float64 `json:"rr_avg"`
	RRMin                 *float64 `json:"rr_min"`
	RRMax                 *float64 `json:"rr_max"`
	BreathingDisturbances *int     `json:"breathing_disturbances_intensity"`
	SnoringSec            *int     `json:"snoring_sec"`
	SnoreEpisodes         *int     `json:"snore_episodes"`
	ApneaHypopneaIndex    *float64 `json:"apnea_hypopnea_index"`
	Source                string   `json:"source"`
}

type sleepResponse struct {
	Series []sleepSeries `json:"series"`
	More   bool          `json:"more"`
	Offset int           `json:"offset"`
}

var (
	sleepFormatFlag string
	sleepSinceFlag  string
	sleepUntilFlag  string
	sleepDeriveFlag bool
)

var sleepCmd = &cobra.Command{
	Use:   "sleep",
	Short: "Export nightly sleep summaries",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse-level flag validation runs before any network call so an
		// invalid --format exits non-zero hermetically (CONTRACT §4).
		format, err := validateFormat(sleepFormatFlag)
		if err != nil {
			return err
		}
		since, err := sinceOrDefault(sleepSinceFlag, 30)
		if err != nil {
			return err
		}
		until, err := untilDayOrToday(sleepUntilFlag)
		if err != nil {
			return err
		}
		if printed, err := printResolvedWindow(cmd, since, until.AddDate(0, 0, 1)); printed || err != nil {
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
		params.Set("enddateymd", until.Format("2006-01-02"))
		params.Set("data_fields", dataFields)

		c := client.New()
		var all []sleepRow
		haveDate := make(map[string]bool)
		for {
			var resp sleepResponse
			if err := c.Call("/v2/sleep", params, &resp); err != nil {
				return err
			}
			for _, s := range resp.Series {
				all = append(all, s.row())
				haveDate[s.Date] = true
			}
			if !resp.More {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		if sleepDeriveFlag {
			first := true
			for d := since; !d.After(until); d = d.AddDate(0, 0, 1) {
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

		sort.Slice(all, func(i, j int) bool { return all[i].StartEpoch < all[j].StartEpoch })

		switch format {
		case "json":
			return printJSON(all)
		case "csv":
			return writeCSV(all)
		default:
			return writeSleepMarkdown(all)
		}
	},
}

// row converts one upstream sleep summary into the canonical §4 row.
func (s sleepSeries) row() sleepRow {
	d := s.Data
	id := s.ID
	model := s.Model
	return sleepRow{
		Date:                  localMidnightRFC3339(s.Date),
		CalendarDate:          s.Date,
		Start:                 time.Unix(s.StartDate, 0).Local().Format(time.RFC3339),
		End:                   time.Unix(s.EndDate, 0).Local().Format(time.RFC3339),
		StartEpoch:            s.StartDate,
		EndEpoch:              s.EndDate,
		ID:                    &id,
		Timezone:              s.Timezone,
		Model:                 &model,
		TotalSleepSec:         totalSleepSec(d),
		LightSec:              d.LightSleepDuration,
		DeepSec:               d.DeepSleepDuration,
		REMSec:                d.REMSleepDuration,
		AwakeSec:              d.WakeupDuration,
		TimeToSleepSec:        d.DurationToSleep,
		TimeToWakeupSec:       d.DurationToWakeup,
		WakeupCount:           d.WakeupCount,
		SleepScore:            d.SleepScore,
		HRAvg:                 roundPtr(d.HRAvg, 1),
		HRMin:                 roundPtr(d.HRMin, 0),
		HRMax:                 roundPtr(d.HRMax, 0),
		RRAvg:                 roundPtr(d.RRAvg, 1),
		RRMin:                 roundPtr(d.RRMin, 0),
		RRMax:                 roundPtr(d.RRMax, 0),
		BreathingDisturbances: d.BreathingDisturbances,
		SnoringSec:            d.Snoring,
		SnoreEpisodes:         d.SnoringEpisodeCount,
		ApneaHypopneaIndex:    roundPtr(d.ApneaHypopneaIndex, 1),
		Source:                "summary",
	}
}

// totalSleepSec sums the sleep stages Withings reported, excluding
// time awake. It returns nil when the device reported no stage at all,
// rather than 0 — a watch that does not do stage detection has not
// measured "zero sleep".
//
// Summing only the stages that are present is deliberate: a device that
// reports light and deep but not REM still has a meaningful total, and
// treating the missing stage as zero is the closest honest reading.
func totalSleepSec(d sleepData) *int {
	total := 0
	any := false
	for _, stage := range []*int{d.LightSleepDuration, d.DeepSleepDuration, d.REMSleepDuration} {
		if stage == nil {
			continue
		}
		total += *stage
		any = true
	}
	if !any {
		return nil
	}
	return &total
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
//
// The heuristic establishes exactly two things: a session span and a mean
// heart rate. It cannot see sleep stages, wakeups, or latencies, so the
// returned row leaves those `null` (#21). An earlier version assigned the
// whole span to `lightsleepduration` so the CSV writer's
// `(light+deep+rem)/60` total would add up, which made every derived row
// report a measured-looking "deep 0, REM 0" and a total equal to light
// sleep alone. The total is now its own field and the stages stay unknown.
func deriveSleep(c *client.Client, date time.Time) (*sleepRow, error) {
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
		// deref is right here: the run detection below already reads a
		// zero heart rate as "this sample carries no reading", which is
		// what an absent one means too.
		samples = append(samples, sample{ts: ts, hr: deref(p.HeartRate), steps: deref(p.Steps)})
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].ts < samples[j].ts })
	if len(samples) == 0 {
		return nil, nil
	}

	// A sample "breaks" a sleep run when it shows real wakefulness: elevated
	// heart rate, or a meaningful burst of steps. Small step samples (bathroom
	// trips, rolling over) do not break the run — they're just HealthKit's way
	// of reporting brief motion without an accompanying HR reading.
	const hrBreak = 90 // bpm above this = awake/active
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

	dur := int(best.end - best.start)
	return &sleepRow{
		Date:          localMidnightRFC3339(date.Format("2006-01-02")),
		CalendarDate:  date.Format("2006-01-02"),
		Start:         time.Unix(best.start, 0).Local().Format(time.RFC3339),
		End:           time.Unix(best.end, 0).Local().Format(time.RFC3339),
		StartEpoch:    best.start,
		EndEpoch:      best.end,
		Timezone:      loc.String(),
		TotalSleepSec: &dur,
		HRAvg:         numPtr(float64(best.hrSum)/float64(best.hrCount), 1),
		Source:        "derived",
	}, nil
}

// writeSleepMarkdown emits one fitdown-style block per night. Stage breakdown,
// score, RR, and snore lines only render when populated. Derived rows tag the
// heading so polyfilled nights are visually distinct from real summaries.
func writeSleepMarkdown(rows []sleepRow) error {
	for _, s := range rows {
		heading := fmt.Sprintf("Sleep %s", s.CalendarDate)
		if s.Source == "derived" {
			heading += " (derived)"
		}
		fmt.Fprintf(os.Stdout, "%s\n\n", heading)
		fmt.Fprintf(os.Stdout, "%s → %s (%s)\n",
			time.Unix(s.StartEpoch, 0).Local().Format("15:04"),
			time.Unix(s.EndEpoch, 0).Local().Format("15:04"),
			fmtDur(deref(s.TotalSleepSec)))

		if hrAvg := deref(s.HRAvg); hrAvg > 0 {
			if deref(s.HRMin) > 0 || deref(s.HRMax) > 0 {
				fmt.Fprintf(os.Stdout, "HR avg %s, %s-%s\n",
					fmtFloat1(hrAvg), fmtRound(deref(s.HRMin)), fmtRound(deref(s.HRMax)))
			} else {
				fmt.Fprintf(os.Stdout, "HR avg %s\n", fmtFloat1(hrAvg))
			}
		}
		if deref(s.DeepSec) > 0 || deref(s.REMSec) > 0 {
			fmt.Fprintf(os.Stdout, "Light %s, deep %s, REM %s\n",
				fmtDur(deref(s.LightSec)), fmtDur(deref(s.DeepSec)), fmtDur(deref(s.REMSec)))
		}
		if score := deref(s.SleepScore); score > 0 {
			fmt.Fprintf(os.Stdout, "Score %d\n", score)
		}
		if rrAvg := deref(s.RRAvg); rrAvg > 0 {
			fmt.Fprintf(os.Stdout, "RR avg %s, %s-%s\n",
				fmtFloat1(rrAvg), fmtRound(deref(s.RRMin)), fmtRound(deref(s.RRMax)))
		}
		if wakeups := deref(s.WakeupCount); wakeups > 0 {
			fmt.Fprintf(os.Stdout, "%d wakeups\n", wakeups)
		}
		if ahi := deref(s.ApneaHypopneaIndex); ahi > 0 {
			fmt.Fprintf(os.Stdout, "AHI %s\n", fmtNum(ahi))
		}
		if episodes := deref(s.SnoreEpisodes); episodes > 0 {
			fmt.Fprintf(os.Stdout, "Snoring: %d episodes\n", episodes)
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func init() {
	sleepCmd.Flags().StringVar(&sleepSinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 30d)")
	sleepCmd.Flags().StringVar(&sleepUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default today)")
	sleepCmd.Flags().StringVar(&sleepFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
	sleepCmd.Flags().BoolVar(&sleepDeriveFlag, "derive", false,
		"For nights with no Withings sleep summary, polyfill start/end from intraday heart-rate samples")
}
