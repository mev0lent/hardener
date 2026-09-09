package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"hardener/internal/config"
	"hardener/internal/dashboard"
	"hardener/internal/executor"
	"hardener/internal/ui"
)

func init() {
	auditCmd.Flags().Bool("tui", false, "Show a live dashboard with category scores and scrolling activity")
	fixCmd.Flags().Bool("tui", false, "Show a live dashboard with category scores and scrolling activity")
}

// Authenticate before Bubble Tea puts the terminal into raw mode. The running
// dashboard refreshes the cache noninteractively; arbitrary interactive rules
// should use the existing plain mode.
func prepareDashboardPrivileges(ctx *config.ExecContext, mode executor.RunMode, suites []config.TestSuite) (bool, error) {
	if os.Geteuid() == 0 {
		return false, nil
	}
	needed := false
	for _, suite := range suites {
		for _, check := range suite.Checks {
			if !config.DetermineSecurityFit(ctx.SecurityLevel, check.SecurityLevel) {
				continue
			}
			chain := ctx.DistroFamily
			if len(chain) == 0 {
				chain = []string{ctx.DistroName}
			}
			resolved, supported := check.ResolveForDistro(chain, ctx.Profile)
			if !supported {
				continue
			}
			needed = needed || resolved.Sudo || strings.Contains(resolved.Command, "sudo ")
			if mode == executor.ModeFix {
				fixSudo := resolved.Sudo
				if resolved.FixSudo != nil {
					fixSudo = *resolved.FixSudo
				}
				needed = needed || fixSudo || strings.Contains(resolved.Fix, "sudo ") || strings.HasPrefix(resolved.AffectedFile, "/")
			}
		}
	}
	if !needed {
		return false, nil
	}
	ui.PrintInfo("Validating sudo credentials before opening the dashboard…")
	command := exec.Command("sudo", "-v")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return false, fmt.Errorf("dashboard sudo validation failed: %w", err)
	}
	return true, nil
}

func runDashboard(ctx *config.ExecContext, mode executor.RunMode, suites []config.TestSuite, work dashboard.Work) error {
	keepAlive, err := prepareDashboardPrivileges(ctx, mode, suites)
	if err != nil {
		return err
	}
	return closeDashboard(dashboard.Run(dashboard.Options{
		Mode: string(mode), System: ctx.OSName + " / " + ctx.ArchName + " · " + ctx.DistroName,
		SecurityLevel: ctx.SecurityLevel, KeepSudoAlive: keepAlive,
	}, suites, work))
}

// A requested stop is a normal interactive action, not a Cobra usage error.
func closeDashboard(err error) error {
	if errors.Is(err, dashboard.ErrStopped) {
		ui.PrintInfo("Run stopped between checks.")
		return nil
	}
	return err
}
