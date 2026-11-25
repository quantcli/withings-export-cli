package cmd

import (
	"encoding/json"
	"testing"
)

func TestActivityCSVRowMissingValuesAreEmpty(t *testing.T) {
	cols := map[string]int{
		"date": 0, "timezone": 1, "steps": 2, "distance_m": 3, "elevation_m": 4,
		"soft_sec": 5, "moderate_sec": 6, "intense_sec": 7, "active_sec": 8,
		"calories": 9, "total_calories": 10, "hr_avg": 11, "hr_min": 12, "hr_max": 13,
		"hr_zone_0": 14, "hr_zone_1": 15, "hr_zone_2": 16, "hr_zone_3": 17,
	}
	cases := []struct {
		name string
		data string
		want map[string]string
	}{
		{"fully populated day",
			`{"date":"2026-11-24","timezone":"America/New_York","steps":12345,"distance":8765.25,"elevation":42.5,"soft":3601,"moderate":1202,"intense":603,"active":5406,"calories":456.75,"totalcalories":2345.5,"hr_average":65.5,"hr_min":45,"hr_max":145.25,"hr_zone_0":100,"hr_zone_1":200,"hr_zone_2":300,"hr_zone_3":400}`,
			map[string]string{"date": "2026-11-24", "timezone": "America/New_York", "steps": "12345", "distance_m": "8765.25", "elevation_m": "42.5", "soft_sec": "3601", "moderate_sec": "1202", "intense_sec": "603", "active_sec": "5406", "calories": "456.75", "total_calories": "2345.5", "hr_avg": "65.5", "hr_min": "45", "hr_max": "145.25", "hr_zone_0": "100", "hr_zone_1": "200", "hr_zone_2": "300", "hr_zone_3": "400"}},
		{"tracker without optical HR or altimeter",
			`{"date":"2026-11-24","timezone":"America/New_York","steps":12345,"distance":8765.25,"soft":3601,"moderate":1202,"intense":0,"active":4803,"calories":456.75,"totalcalories":2345.5}`,
			map[string]string{"date": "2026-11-24", "timezone": "America/New_York", "steps": "12345", "distance_m": "8765.25", "elevation_m": "", "soft_sec": "3601", "moderate_sec": "1202", "intense_sec": "0", "active_sec": "4803", "calories": "456.75", "total_calories": "2345.5", "hr_avg": "", "hr_min": "", "hr_max": "", "hr_zone_0": "", "hr_zone_1": "", "hr_zone_2": "", "hr_zone_3": ""}},
		{"all measurements absent", `{}`, map[string]string{}},
		{"explicit zero measurements",
			`{"steps":0,"distance":0,"elevation":0,"soft":0,"moderate":0,"intense":0,"active":0,"calories":0,"totalcalories":0,"hr_average":0,"hr_min":0,"hr_max":0,"hr_zone_0":0,"hr_zone_1":0,"hr_zone_2":0,"hr_zone_3":0}`,
			map[string]string{"steps": "0", "distance_m": "0", "elevation_m": "0", "soft_sec": "0", "moderate_sec": "0", "intense_sec": "0", "active_sec": "0", "calories": "0", "total_calories": "0", "hr_avg": "0", "hr_min": "0", "hr_max": "0", "hr_zone_0": "0", "hr_zone_1": "0", "hr_zone_2": "0", "hr_zone_3": "0"}},
	}
	for _, c := range cases {
		var d activityDay
		if err := json.Unmarshal([]byte(c.data), &d); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		row := activityCSVRow(d)
		if len(row) != 18 {
			t.Fatalf("%s: %d columns, want 18", c.name, len(row))
		}
		for k, col := range cols {
			if got, want := row[col], c.want[k]; got != want {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got, want)
			}
		}
	}
}
