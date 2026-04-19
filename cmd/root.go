package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "withings-export",
	Short: "CLI to export health data from Withings",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(measurementsCmd)
	rootCmd.AddCommand(sleepCmd)
	rootCmd.AddCommand(activityCmd)
	rootCmd.AddCommand(workoutsCmd)
}
