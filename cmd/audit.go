package cmd

import (
	"github.com/spf13/cobra"
)

// used for audit & fix:
var labelFlags []string
var profileFlag string
var archFlag string
var securityLevel string
var pathFlag string    // path to a markdown-based hardening guide directory
var rulesetFlag string // path to a standalone ruleset.yaml file
var allSuites bool     // skip interactive selection and run all suites

func init() {
	rootCmd.AddCommand(auditCmd)

	auditCmd.Flags().StringSliceVarP(&labelFlags, "label", "l", nil,
		"Run only suites matching the given label(s), comma-separated (e.g. kernel,network,auth)")
	auditCmd.Flags().StringVarP(&profileFlag, "profile", "P", "", "System role profile for distro overrides (e.g. server, client)")
	auditCmd.Flags().StringVarP(&archFlag, "arch", "a", "", "Filter by architecture (defaults to current arch)")
	auditCmd.Flags().StringVarP(&pathFlag, "path", "p", "", "Path to the directory containing the hardening guide (markdown mode)")
	auditCmd.Flags().StringVarP(&rulesetFlag, "ruleset", "r", "", "Path to a standalone ruleset.yaml file (alternative to --path)")
	auditCmd.Flags().StringVarP(&securityLevel, "security-level", "s", "baseline", "Filter by security level")
	auditCmd.Flags().BoolVarP(&allSuites, "all", "A", false, "Run all applicable suites without interactive selection")
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Run audits",
	Long:  "Run audit checks for current system, or for specific module if passed.",
	RunE:  auditRun,
}
