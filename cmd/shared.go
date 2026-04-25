package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// parseDateValue parses --since / --until argument values per the quantcli
// shared contract: "today", "yesterday", absolute YYYY-MM-DD, or relative
// Nd/Nw/Nm/Ny. Returns local midnight for the target day; empty string
// yields the zero time. See https://github.com/quantcli/common/blob/main/CONTRACT.md#3-date-flags.
func parseDateValue(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	switch strings.ToLower(s) {
	case "today":
		return today, nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD, today, yesterday, or Nd/Nw/Nm/Ny)", s)
	}
	n := 0
	if _, err := fmt.Sscanf(s[:len(s)-1], "%d", &n); err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD, today, yesterday, or Nd/Nw/Nm/Ny)", s)
	}
	switch s[len(s)-1] {
	case 'd':
		return today.AddDate(0, 0, -n), nil
	case 'w':
		return today.AddDate(0, 0, -n*7), nil
	case 'm':
		return today.AddDate(0, -n, 0), nil
	case 'y':
		return today.AddDate(-n, 0, 0), nil
	default:
		return time.Time{}, fmt.Errorf("invalid date unit %q: use d, w, m, or y", string(s[len(s)-1]))
	}
}

// parseUntilValue resolves --until to the exclusive upper bound of a
// half-open window. The user-supplied date names a calendar day they expect
// to be included, so we add 24h to the parsed start-of-day. Empty string
// yields the zero time, which callers treat as "no upper bound (i.e. now)".
func parseUntilValue(s string) (time.Time, error) {
	t, err := parseDateValue(s)
	if err != nil || t.IsZero() {
		return t, err
	}
	return t.AddDate(0, 0, 1), nil
}

// sinceOrDefault returns the --since value, defaulting to local midnight
// daysBack days ago when empty.
func sinceOrDefault(s string, daysBack int) (time.Time, error) {
	t, err := parseDateValue(s)
	if err != nil {
		return time.Time{}, err
	}
	if t.IsZero() {
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
		return today.AddDate(0, 0, -daysBack), nil
	}
	return t, nil
}

// untilOrNow returns the --until value as the exclusive upper bound of
// the window, defaulting to the current instant when empty. Use for epoch
// API params (Unix seconds) and client-side filtering.
func untilOrNow(s string) (time.Time, error) {
	t, err := parseUntilValue(s)
	if err != nil {
		return time.Time{}, err
	}
	if t.IsZero() {
		return time.Now(), nil
	}
	return t, nil
}

// untilDayOrToday returns the --until value as the inclusive end calendar
// day at local midnight, defaulting to today's midnight when empty. Use
// for ymd-format API params where the API treats the date string as the
// last day to include.
func untilDayOrToday(s string) (time.Time, error) {
	t, err := parseDateValue(s)
	if err != nil {
		return time.Time{}, err
	}
	if t.IsZero() {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local), nil
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
