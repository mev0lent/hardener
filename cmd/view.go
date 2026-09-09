package cmd

import (
	"github.com/spf13/cobra"
	"hardener/internal/dashboard"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "view [report.json]",
		Short: "Browse saved audit/fix results in a read-only dashboard",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) > 0 {
				path = args[0]
			}
			return dashboard.ViewReport(path)
		},
	})
}
