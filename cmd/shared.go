package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

// parseSince accepts absolute dates (YYYY-MM-DD) or relative values like 30d, 4w, 6m, 1y.
// An empty string yields the zero time.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid --since value: %q", s)
	}
	n := 0
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil {
		return time.Time{}, fmt.Errorf("invalid --since value: %q", s)
	}
	now := time.Now()
	switch s[len(s)-1] {
	case 'd':
		return now.AddDate(0, 0, -n), nil
	case 'w':
		return now.AddDate(0, 0, -n*7), nil
	case 'm':
		return now.AddDate(0, -n, 0), nil
	case 'y':
		return now.AddDate(-n, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("invalid --since unit %q: use d, w, m, or y", string(s[len(s)-1]))
	}
}

// sinceOrDefault returns the --since value, defaulting to daysBack days ago when empty.
func sinceOrDefault(s string, daysBack int) (time.Time, error) {
	t, err := parseSince(s)
	if err != nil {
		return time.Time{}, err
	}
	if t.IsZero() {
		return time.Now().AddDate(0, 0, -daysBack), nil
	}
	return t, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	return nil
}

// validateFormat checks that --format is one of the supported choices and
// normalizes it to a canonical lowercase value.
func validateFormat(format string) (string, error) {
	switch format {
	case "", "markdown", "md":
		return "markdown", nil
	case "json":
		return "json", nil
	case "csv":
		return "csv", nil
	default:
		return "", fmt.Errorf("unknown --format %q (use markdown, json, or csv)", format)
	}
}

// fmtDur renders seconds as a fitdown-friendly duration: "8h04", "47 min",
// or "0 min" for zero. Negative values render as empty.
func fmtDur(seconds int) string {
	if seconds < 0 {
		return ""
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh%02d", h, m)
	}
	return fmt.Sprintf("%d min", m)
}

// fmtNum trims trailing zeros from a float for human-readable output.
func fmtNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// fmtRound rounds a float to the nearest integer. Used in markdown output for
// values where the API returns trailing-precision noise (HR, calories).
func fmtRound(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// fmtFloat1 renders a float with one decimal place — useful for HR averages
// where ~10ths of a bpm carry a little signal but full precision is noise.
func fmtFloat1(v float64) string {
	return fmt.Sprintf("%.1f", v)
}
