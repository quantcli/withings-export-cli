package cmd

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWorkoutsCSVRowMissingValuesAreEmpty(t *testing.T) {
	cols := map[string]int{
		"date": 0, "start": 1, "end": 2, "category": 3, "category_code": 4, "timezone": 5,
		"duration_min": 6, "eff_duration_min": 7, "calories": 8, "steps": 9,
		"distance_m": 10, "elevation_m": 11, "hr_avg": 12, "hr_min": 13, "hr_max": 14,
		"hr_zone_0": 15, "hr_zone_1": 16, "hr_zone_2": 17, "hr_zone_3": 18, "device_id": 19,
	}
	cases := []struct {
		name     string
		data     string
		deviceID string
		want     map[string]string
	}{
		{"fully populated workout",
			`{"calories":456.75,"effduration":1831,"distance":8765.25,"elevation":42.5,"steps":12345,"hr_average":65.5,"hr_min":45,"hr_max":145.25,"hr_zone_0":0,"hr_zone_1":200,"hr_zone_2":300,"hr_zone_3":400}`, "tracker-123",
			map[string]string{"eff_duration_min": "30", "calories": "456.75", "steps": "12345", "distance_m": "8765.25", "elevation_m": "42.5", "hr_avg": "65.5", "hr_min": "45", "hr_max": "145.25", "hr_zone_0": "0", "hr_zone_1": "200", "hr_zone_2": "300", "hr_zone_3": "400"}},
		{"tracker without optical HR or altimeter",
			`{"calories":456.75,"effduration":1831,"distance":8765.25,"steps":12345,"hr_zone_0":0}`, "",
			map[string]string{"eff_duration_min": "30", "calories": "456.75", "steps": "12345", "distance_m": "8765.25", "elevation_m": "", "hr_avg": "", "hr_min": "", "hr_max": "", "hr_zone_0": "0", "hr_zone_1": "", "hr_zone_2": "", "hr_zone_3": ""}},
		{"all measurements absent", `{}`, "", map[string]string{}},
		{"explicit zero measurements",
			`{"calories":0,"effduration":0,"distance":0,"elevation":0,"steps":0,"hr_average":0,"hr_min":0,"hr_max":0,"hr_zone_0":0,"hr_zone_1":0,"hr_zone_2":0,"hr_zone_3":0}`, "",
			map[string]string{"eff_duration_min": "0", "calories": "0", "steps": "0", "distance_m": "0", "elevation_m": "0", "hr_avg": "0", "hr_min": "0", "hr_max": "0", "hr_zone_0": "0", "hr_zone_1": "0", "hr_zone_2": "0", "hr_zone_3": "0"}},
	}
	for _, c := range cases {
		s := workoutSeries{Date: "2026-11-24", Category: 2, Timezone: "America/New_York", StartDate: 1795536000, EndDate: 1795539631, DeviceID: c.deviceID, Data: json.RawMessage(c.data)}
		row := workoutsCSVRow(s)
		if len(row) != 20 {
			t.Fatalf("%s: %d columns, want 20", c.name, len(row))
		}
		want := map[string]string{
			"date":     s.Date,
			"start":    time.Unix(s.StartDate, 0).Local().Format(time.RFC3339),
			"end":      time.Unix(s.EndDate, 0).Local().Format(time.RFC3339),
			"category": "run", "category_code": "2", "timezone": s.Timezone,
			"duration_min": "60", "device_id": c.deviceID,
		}
		for k, v := range c.want {
			want[k] = v
		}
		for k, col := range cols {
			if got := row[col]; got != want[k] {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got, want[k])
			}
		}
	}
}
