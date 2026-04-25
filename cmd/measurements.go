package cmd

import (
	"encoding/csv"
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
	Timezone     string         `json:"timezone"`
	Measuregrps  []measureGroup `json:"measuregrps"`
	More         int            `json:"more"`
	Offset       int            `json:"offset"`
}

type measurementRow struct {
	Date     time.Time `json:"date"`
	Type     string    `json:"type"`
	TypeCode int       `json:"type_code"`
	Value    float64   `json:"value"`
	DeviceID string    `json:"device_id,omitempty"`
	GrpID    int64     `json:"grp_id"`
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
		since, err := sinceOrDefault(measurementsSinceFlag, 30)
		if err != nil {
			return err
		}
		until, err := untilOrNow(measurementsUntilFlag)
		if err != nil {
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
				for _, m := range g.Measures {
					value := float64(m.Value) * math.Pow10(m.Unit)
					name, ok := measureTypeNames[m.Type]
					if !ok {
						name = fmt.Sprintf("type_%d", m.Type)
					}
					rows = append(rows, measurementRow{
						Date:     t,
						Type:     name,
						TypeCode: m.Type,
						Value:    value,
						DeviceID: g.DeviceID,
						GrpID:    g.GrpID,
					})
				}
			}
			if resp.More == 0 {
				break
			}
			params.Set("offset", strconv.Itoa(resp.Offset))
		}

		sort.Slice(rows, func(i, j int) bool { return rows[i].Date.Before(rows[j].Date) })

		format, err := validateFormat(measurementsFormatFlag)
		if err != nil {
			return err
		}
		switch format {
		case "json":
			return printJSON(rows)
		case "csv":
			return writeMeasurementsCSV(rows)
		default:
			return writeMeasurementsMarkdown(rows)
		}
	},
}

func writeMeasurementsCSV(rows []measurementRow) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()
	if err := w.Write([]string{"date", "type", "type_code", "value", "device_id", "grp_id"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write([]string{
			r.Date.Format(time.RFC3339),
			r.Type,
			strconv.Itoa(r.TypeCode),
			strconv.FormatFloat(r.Value, 'f', -1, 64),
			r.DeviceID,
			strconv.FormatInt(r.GrpID, 10),
		}); err != nil {
			return err
		}
	}
	return nil
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
			g = &group{date: r.Date}
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
