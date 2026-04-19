package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// parseSince accepts absolute dates (YYYY-MM-DD) or relative values like 30d, 4w, 6m, 1y.
// An empty string yields the zero time.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
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
