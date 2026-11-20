package cmd

import (
	"fmt"
	"math"
	"net/url"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/quantcli/withings-export-cli/internal/client"
	"github.com/spf13/cobra"
)

// Withings measurement type codes. See https://developer.withings.com/api-reference/#operation/measure-getmeas
var measureTypeNames = map[int]string{
	1:   "weight_kg",
	4:   "height_m",
	5:   "fat_free_mass_kg",
	6:   "fat_ratio_pct",
	8:   "fat_mass_kg",
	9:   "diastolic_bp",
	10:  "systolic_bp",
	11:  "heart_pulse_bpm",
	12:  "temperature_c",
	54:  "spo2_pct",
	71:  "body_temp_c",
	73:  "skin_temp_c",
	76:  "muscle_mass_kg",
	77:  "hydration_kg",
	88:  "bone_mass_kg",
	91:  "pulse_wave_velocity",
	123: "vo2_max",
	135: "qrs_interval_ms",
	136: "pr_interval_ms",
	137: "qt_interval_ms",
	138: "corrected_qt_ms",
}

type measureGroup struct {
	GrpID    int64     `json:"grpid"`
	Date     int64     `json:"date"`
	Category int       `json:"category"`
	DeviceID string    `json:"deviceid"`
	Measures []measure `json:"measures"`
}

type measure struct {
	Value int64 `json:"value"`
	Type  int   `json:"type"`
	Unit  int   `json:"unit"`
}

type getmeasResponse struct {
	Timezone    string         `json:"timezone"`
	Measuregrps []measureGroup `json:"measuregrps"`
	More        int            `json:"more"`
	Offset      int            `json:"offset"`
}

// measurementRow is the canonical §4 row for `measurements`, shared by
// all three codecs — see cmd/rowcodec.go.
//
// `date` is a pre-formatted RFC3339 string rather than a time.Time so
// that the one representation §2 requires is stated here instead of
// delegated to Go's default marshaler (which emits RFC3339Nano, a
// different layout from the other four subcommands when the instant
// has sub-second precision).
//
// `device_id` is a pointer: it was `omitempty`, so the JSON key
// vanished on rows with no device while the CSV column stayed, which
// made the two codecs disagree about the key set for exactly those
// rows. §4 wants the key present and the value `null`.
type measurementRow struct {
	Date     string  `json:"date"`
	Type     string  `json:"type"`
	TypeCode int     `json:"type_code"`
	Value    float64 `json:"value"`
	DeviceID *string `json:"device_id"`
	GrpID    int64   `json:"grp_id"`

	// ts is the row's instant, kept for sorting and for the markdown
	// group heading. Unexported, so neither codec sees it: RFC3339
	// strings do not sort correctly across a UTC-offset change, which
	// a window spanning a DST boundary contains.
	ts time.Time
}

var (
	measurementsFormatFlag string
	measurementsSinceFlag  string
	measurementsUntilFlag  string
	measurementsTypesFlag  string
)

var measurementsCmd = &cobra.Command{
	Use:   "measurements",
	Short: "Export body measurements (weight, body fat, BP, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Parse-level flag validation runs before any network call so an
		// invalid --format exits non-zero hermetically (CONTRACT §4).
		format, err := validateFormat(measurementsFormatFlag)
		if err != nil {
			return err
		}
		since, err := sinceOrDefault(measurementsSinceFlag, 30)
		if err != nil {
			return err
		}
		until, err := untilOrNow(measurementsUntilFlag)
		if err != nil {
			return err
		}
		if printed, err := printResolvedWindow(cmd, since, until); printed || err != nil {
			return err
		}

		params := url.Values{}
		params.Set("action", "getmeas")
		params.Set("category", "1")
		params.Set("startdate", strconv.FormatInt(since.Unix(), 10))
		params.Set("enddate", strconv.FormatInt(until.Unix(), 10))
		if measurementsTypesFlag != "" {
			params.Set("meastypes", measurementsTypesFlag)
		}

		c := client.New()
		var rows []measurementRow
		for {
			var resp getmeasResponse
			if err := c.Call("/measure", params, &resp); err != nil {
				return err
			}
			for _, g := range resp.Measuregrps {
				t := time.Unix(g.Date, 0).Local()
				var deviceID *string
				if g.DeviceID != "" {
					id := g.DeviceID
					deviceID = &id
				}
				for _, m := range g.Measures {
					// value × 10^unit is where #26's float noise comes
					// from: 72844 × 10⁻³ is not exactly representable,
					// so it surfaces as 72.84400000000001. Withings
					// stores at most 3 decimal places, so rounding to 3
					// is lossless for real data and removes the tail.
					value := round(float64(m.Value)*math.Pow10(m.Unit), 3)
					name, ok := measureTypeNames[m.Type]
					if !ok {
						name = fmt.Sprintf("type_%d", m.Type)
					}
					rows = append(rows, measurementRow{
						Date:     t.Format(time.RFC3339),
						Type:     name,
						TypeCode: m.Type,
						Value:    value,
						DeviceID: deviceID,
						GrpID:    g.GrpID,
						ts:       t,
					})
				}
			}
			if resp.More == 0 {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(rows, func(i, j int) bool { return rows[i].ts.Before(rows[j].ts) })

		switch format {
		case "json":
			return printJSON(rows)
		case "csv":
			return writeCSV(rows)
		default:
			return writeMeasurementsMarkdown(rows)
		}
	},
}

// fmtMeasurement renders a measurement value with up to 3 decimal places,
// stripping float-precision noise (e.g. 107.35000000000001 → 107.35).
func fmtMeasurement(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	// trim trailing zeros and trailing dot
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}

// writeMeasurementsMarkdown emits one fitdown-style block per measurement
// group (a single weigh-in event), with one line per measure type.
func writeMeasurementsMarkdown(rows []measurementRow) error {
	type group struct {
		date time.Time
		rows []measurementRow
	}
	groups := map[int64]*group{}
	var order []int64
	for _, r := range rows {
		g, ok := groups[r.GrpID]
		if !ok {
			g = &group{date: r.ts}
			groups[r.GrpID] = g
			order = append(order, r.GrpID)
		}
		g.rows = append(g.rows, r)
	}
	sort.Slice(order, func(i, j int) bool {
		return groups[order[i]].date.Before(groups[order[j]].date)
	})
	for _, id := range order {
		g := groups[id]
		fmt.Fprintf(os.Stdout, "Measurements %s\n\n", g.date.Format("2006-01-02 15:04"))
		for _, r := range g.rows {
			fmt.Fprintf(os.Stdout, "%s %s\n", r.Type, fmtMeasurement(r.Value))
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

func init() {
	measurementsCmd.Flags().StringVar(&measurementsSinceFlag, "since", "",
		"Filter on or after date (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default 30d)")
	measurementsCmd.Flags().StringVar(&measurementsUntilFlag, "until", "",
		"Filter through date, inclusive (today, yesterday, YYYY-MM-DD, or Nd/Nw/Nm/Ny; default now)")
	measurementsCmd.Flags().StringVar(&measurementsFormatFlag, "format", "markdown",
		"Output format: markdown (default, fitdown-style), json, or csv")
	measurementsCmd.Flags().StringVar(&measurementsTypesFlag, "types", "",
		"Comma-separated measure type codes to include (e.g. 1,6,76); default is all")
}
