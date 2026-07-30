package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overwritten at release time via
// -ldflags "-X github.com/quantcli/withings-export-cli/cmd.version=v1.2.0".
// Setting rootCmd.Version below makes cobra register --version for free.
var version = "dev"

var rootCmd = &cobra.Command{
	Use:          "withings-export",
	Short:        "CLI to export health data from Withings",
	Version:      version,
	SilenceUsage: true,
	Long: `withings-export reads your personal Withings health data — activity,
sleep, workouts, body measurements, intraday samples — and prints it on
stdout. Default output is narrow, fitdown-style markdown; pass
--format json or --format csv for structured output.

LLM agents: run 'withings-export prime' for a one-screen orientation
(I/O contract, subcommands, date flags, jq recipes).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SetOut(cmd.ErrOrStderr())
		_ = cmd.Help()
		return fmt.Errorf("subcommand required")
	},
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
	rootCmd.AddCommand(intradayCmd)
}
