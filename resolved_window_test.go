package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvedWindowOffline(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "export-cli")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	cases := []struct{ zone, day, want string }{
		{"Pacific/Honolulu", "2026-04-15", "since=2026-04-15T00:00:00-10:00 until_exclusive=2026-04-16T00:00:00-10:00\n"},
		{"Pacific/Kiritimati", "2026-04-15", "since=2026-04-15T00:00:00+14:00 until_exclusive=2026-04-16T00:00:00+14:00\n"},
		{"America/New_York", "2026-03-08", "since=2026-03-08T00:00:00-05:00 until_exclusive=2026-03-09T00:00:00-04:00\n"},
		{"America/New_York", "2026-11-01", "since=2026-11-01T00:00:00-04:00 until_exclusive=2026-11-02T00:00:00-05:00\n"},
	}
	for _, sub := range []string{"activity", "intraday", "measurements", "sleep", "workouts"} {
		for _, tc := range cases {
			t.Run(sub+"/"+tc.zone+"/"+tc.day, func(t *testing.T) {
				args := append(strings.Fields(sub), "--since", tc.day, "--until", tc.day, "--print-resolved-window")
				cmd := exec.Command(bin, args...)
				cmd.Env = []string{"TZ=" + tc.zone, "HOME=" + t.TempDir(), "HTTPS_PROXY=http://127.0.0.1:1", "PATH=" + os.Getenv("PATH")}
				out, err := cmd.CombinedOutput()
				if err != nil || string(out) != tc.want {
					t.Fatalf("output=%q error=%v, want %q", out, err, tc.want)
				}
			})
		}
	}
}
