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
	fixCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Audit and list the fix each failing check would get, without applying any")
	fixCmd.Flags().StringVarP(&securityLevel, "security-level", "s", "baseline", "Highest security level to run: baseline, medium, high, or optional (runs every check)")
	// --arch was never read; keep accepting it so existing scripts still run.
	_ = fixCmd.Flags().MarkDeprecated("arch", "it never had an effect; suites are matched against the running architecture")
	fixCmd.Flags().BoolVarP(&allSuites, "all", "A", false, "Run all applicable suites without interactive selection")
}

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Fix encountered problems",
	Long:  "Fix encountered problems with specified fix command",
	RunE:  fixRun,
}
