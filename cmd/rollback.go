package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(rollbackCmd)
	rollbackCmd.Flags().BoolP("latest", "l", false, "Rollback to the latest run (default if no timestamp is provided)")
	rollbackCmd.Flags().StringP("timestamp", "t", "", "Rollback to a specific run, by its timestamp in runs.json (UTC RFC 3339, e.g. 2026-05-18T14:32:00Z)")
	rollbackCmd.Flags().StringSlice("files", nil, "List of files to rollback")
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback configuration files to a previous state",
	Long: `Rollback configuration files to a previous state. 
			You can rollback the entire system to the latest run, 
			or specify a timestamp to rollback to a specific run.
			Optionally, provide a list of files to rollback only those files.`,
	RunE: rollbackRun,
}
