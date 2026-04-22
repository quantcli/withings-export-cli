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
	measurementsJSONFlag  bool
	measurementsSinceFlag string
	measurementsTypesFlag string
)

var measurementsCmd = &cobra.Command{
	Use:   "measurements",
	Short: "Export body measurements (weight, body fat, BP, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		since, err := sinceOrDefault(measurementsSinceFlag, 30)
		if err != nil {
			return err
		}

		params := url.Values{}
		params.Set("action", "getmeas")
		params.Set("category", "1")
		params.Set("startdate", strconv.FormatInt(since.Unix(), 10))
		params.Set("enddate", strconv.FormatInt(time.Now().Unix(), 10))
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

		if measurementsJSONFlag {
			return printJSON(rows)
		}
		return writeMeasurementsCSV(rows)
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

func init() {
	measurementsCmd.Flags().StringVar(&measurementsSinceFlag, "since", "",
		"Filter on or after date (e.g. 2026-01-01, 30d, 4w, 6m, 1y; default 30d)")
	measurementsCmd.Flags().BoolVar(&measurementsJSONFlag, "json", false,
		"Output as JSON instead of CSV")
	measurementsCmd.Flags().StringVar(&measurementsTypesFlag, "types", "",
		"Comma-separated measure type codes to include (e.g. 1,6,76); default is all")
}
