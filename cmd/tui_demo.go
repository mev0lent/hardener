package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"hardener/internal/config"
	"hardener/internal/dashboard"
	"hardener/internal/executor"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use: "tui-demo", Short: "Preview the live dashboard with simulated checks (no system changes)",
		RunE: func(_ *cobra.Command, _ []string) error {
			titles := []string{"Operating System Updates", "Accounts & Authentication", "Network & Firewall", "SSH Configuration", "Filesystem Permissions", "Audit & Logging"}
			var suites []config.TestSuite
			for _, title := range titles {
				suite := config.TestSuite{Title: title}
				for i := 0; i < 6; i++ {
					suite.Checks = append(suite.Checks, config.Check{ID: fmt.Sprintf("check-%02d", i+1), Description: title + " · simulated control"})
				}
				suites = append(suites, suite)
			}
			return closeDashboard(dashboard.Run(dashboard.Options{Mode: "DEMO", System: "SIMULATED · no commands executed", SecurityLevel: "baseline"}, suites,
				func(observer executor.Observer, stop func() bool) error {
					for si, suite := range suites {
						if stop() {
							break
						}
						observer(executor.RunEvent{Kind: executor.SuiteStarted, SuiteIndex: si, Suite: suite})
						for ci, check := range suite.Checks {
							if stop() {
								return nil
							}
							observer(executor.RunEvent{Kind: executor.CheckStarted, SuiteIndex: si, CheckIndex: ci, Check: check})
							time.Sleep(220 * time.Millisecond)
							result := config.CheckResult{ID: check.ID, Passed: true, Output: "Simulated configuration meets the expected value"}
							switch {
							case ci == 5:
								result.Passed = false
								result.Skipped = true
								result.Output = "Simulated high-security check excluded at baseline"
							case ci == 4 && si%2 == 1:
								result.Passed = false
								result.Output = "Simulated setting requires attention"
							case ci == 3 && si == 2:
								result.Passed = false
								result.Error = "Simulated command error"
								result.Output = result.Error
							case ci == 3 && si == 1:
								result.Passed = false
								result.FixApplied = true
								result.FixVerified = true
								result.Output = "Simulated fix applied and verified by re-check"
							}
							observer(executor.RunEvent{Kind: executor.CheckFinished, SuiteIndex: si, CheckIndex: ci, Check: check, Result: result})
						}
						observer(executor.RunEvent{Kind: executor.SuiteFinished, SuiteIndex: si, Suite: suite})
					}
					return nil
				}))
		},
	})
}
