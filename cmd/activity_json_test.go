package cmd

import (
	"encoding/json"
	"testing"
)

func TestActivityJSONMissingValuesAreNull(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"tracker without optical HR or altimeter",
			`{"date":"2026-11-24","timezone":"America/New_York","steps":12345,"distance":8765.25,"soft":3601,"moderate":1202,"intense":0,"active":4803,"calories":456.75,"totalcalories":2345.5}`,
			`{"date":"2026-11-24","timezone":"America/New_York","steps":12345,"distance":8765.25,"elevation":null,"soft":3601,"moderate":1202,"intense":0,"active":4803,"calories":456.75,"totalcalories":2345.5,"hr_average":null,"hr_min":null,"hr_max":null,"hr_zone_0":null,"hr_zone_1":null,"hr_zone_2":null,"hr_zone_3":null}`},
		{"all measurements absent", `{}`,
			`{"date":"","timezone":"","steps":null,"distance":null,"elevation":null,"soft":null,"moderate":null,"intense":null,"active":null,"calories":null,"totalcalories":null,"hr_average":null,"hr_min":null,"hr_max":null,"hr_zone_0":null,"hr_zone_1":null,"hr_zone_2":null,"hr_zone_3":null}`},
		{"explicit zero measurements",
			`{"steps":0,"distance":0,"elevation":0,"soft":0,"moderate":0,"intense":0,"active":0,"calories":0,"totalcalories":0,"hr_average":0,"hr_min":0,"hr_max":0,"hr_zone_0":0,"hr_zone_1":0,"hr_zone_2":0,"hr_zone_3":0}`,
			`{"date":"","timezone":"","steps":0,"distance":0,"elevation":0,"soft":0,"moderate":0,"intense":0,"active":0,"calories":0,"totalcalories":0,"hr_average":0,"hr_min":0,"hr_max":0,"hr_zone_0":0,"hr_zone_1":0,"hr_zone_2":0,"hr_zone_3":0}`},
	}
	for _, c := range cases {
		var d activityDay
		if err := json.Unmarshal([]byte(c.data), &d); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		// Compare bytes as well as values to preserve every key and its order.
		if string(got) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}
