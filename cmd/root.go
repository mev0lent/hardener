package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hardener",
	Short: "Hardener is a cross-platform security auditing & hardening tool.",
	Long:  "Hardener is a cross-platform security auditing & hardening tool, able to identify & remediate security issues and audit your whole system.",
	Run: func(cmd *cobra.Command, args []string) {

	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Oops. An error while executing hardener '%s'\n", err)
		if err != nil {
			return
		}
		os.Exit(1)
	}
}
