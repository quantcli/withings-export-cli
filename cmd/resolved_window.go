package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"time"
)

func init() {
	for _, cmd := range []*cobra.Command{measurementsCmd, intradayCmd, sleepCmd, activityCmd, workoutsCmd} {
		cmd.Flags().Bool("print-resolved-window", false, "Print resolved dates without contacting Withings")
	}
}

func printResolvedWindow(cmd *cobra.Command, since, until time.Time) (bool, error) {
	enabled, err := cmd.Flags().GetBool("print-resolved-window")
	if err != nil || !enabled {
		return false, err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "since=%s until_exclusive=%s\n", since.Format(time.RFC3339), until.Format(time.RFC3339))
	return true, err
}
