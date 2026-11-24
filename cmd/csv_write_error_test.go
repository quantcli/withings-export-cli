package cmd

import (
	"os"
	"testing"
)

// A CSV export whose output could not be written must fail, so the CLI
// exits non-zero instead of reporting success with nothing delivered.
func TestCSVWritersReportWriteErrors(t *testing.T) {
	writers := map[string]func() error{
		"activity":     func() error { return writeActivityCSV(nil) },
		"intraday":     func() error { return writeIntradayCSV(nil) },
		"measurements": func() error { return writeMeasurementsCSV(nil) },
		"sleep":        func() error { return writeSleepCSV(nil) },
		"workouts":     func() error { return writeWorkoutsCSV(nil) },
	}
	for name, write := range writers {
		_, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.Close() // every write to it now fails
		saved := os.Stdout
		os.Stdout = w
		err = write()
		os.Stdout = saved
		if err == nil {
			t.Errorf("%s: write to closed stdout returned nil", name)
		}
	}
}
