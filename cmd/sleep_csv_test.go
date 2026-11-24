package cmd

import (
	"encoding/json"
	"testing"
)

func TestSleepCSVRowMissingValuesAreEmpty(t *testing.T) {
	cols := map[string]int{"total": 4, "light": 5, "deep": 6, "rem": 7, "score": 11, "hr_avg": 12, "rr_avg": 15, "snore": 18, "source": 20}
	cases := []struct {
		name string
		s    sleepSeries
		want map[string]string
	}{
		{"full summary",
			sleepSeries{Source: "summary", Data: json.RawMessage(`{"lightsleepduration":3600,"deepsleepduration":1800,"remsleepduration":1830,"sleep_score":80,"hr_average":55.5,"snoringepisodecount":0}`)},
			map[string]string{"total": "120", "light": "60", "deep": "30", "rem": "30", "score": "80", "hr_avg": "55.5", "snore": "0", "rr_avg": ""}},
		{"device reports no stages or score",
			sleepSeries{Source: "summary", Data: json.RawMessage(`{"hr_average":60}`)},
			map[string]string{"total": "", "light": "", "deep": "", "rem": "", "score": "", "hr_avg": "60", "snore": ""}},
		{"derived night has a total but no stages",
			sleepSeries{Source: "derived", Data: json.RawMessage(`{"lightsleepduration":27000,"hr_average":52}`)},
			map[string]string{"total": "450", "light": "", "deep": "", "rem": "", "hr_avg": "52", "source": "derived"}},
	}
	for _, c := range cases {
		row := sleepCSVRow(c.s)
		if len(row) != 21 {
			t.Fatalf("%s: %d columns, want 21", c.name, len(row))
		}
		for k, want := range c.want {
			if got := row[cols[k]]; got != want {
				t.Errorf("%s: %s = %q, want %q", c.name, k, got, want)
			}
		}
	}
}
