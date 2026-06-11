package cmd

import (
	"github.com/spf13/cobra"
)

var dryRun = false

func init() {
	rootCmd.AddCommand(fixCmd)

	fixCmd.Flags().StringSliceVarP(&labelFlags, "label", "l", nil,
		"Run only suites matching the given label(s), comma-separated (e.g. kernel,network,auth)")
	fixCmd.Flags().StringVarP(&profileFlag, "profile", "P", "", "System role profile for distro overrides (e.g. server, client)")
	fixCmd.Flags().StringVarP(&archFlag, "arch", "a", "", "Filter by architecture (defaults to current arch)")
	fixCmd.Flags().StringVarP(&pathFlag, "path", "p", "", "Path to the directory containing the hardening guide (markdown mode)")
	fixCmd.Flags().StringVarP(&rulesetFlag, "ruleset", "r", "", "Path to a standalone ruleset.yaml file (alternative to --path)")
	fixCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Show planned fixes but do not apply")
	fixCmd.Flags().StringVarP(&securityLevel, "security-level", "s", "baseline", "Filter by security level")
	fixCmd.Flags().BoolVarP(&allSuites, "all", "A", false, "Run all applicable suites without interactive selection")
}

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Fix encountered problems",
	Long:  "Fix encountered problems with specified fix command",
	RunE:  fixRun,
}
