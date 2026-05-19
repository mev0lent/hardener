package executor

import (
	"bytes"
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"hardener/rollback"
	"os/exec"
	"strings"
)

// RunSuites orchestrates the execution of all test suites.
func RunSuites(ctx *config.ExecContext, mode RunMode, osName, archName string, suites []config.TestSuite) (
	[]config.SuiteResult, map[string]bool, map[string]bool, error) {

	if len(suites) == 0 {
		// All return statements must now include a nil error.
		return nil, nil, nil, fmt.Errorf("error: No hardening guides (.md files with checks) found at the defined position: %s", ctx.BaseDir)
	}

	checksPassed := make(map[string]bool)
	fixesApplied := make(map[string]bool)
	var suiteResults []config.SuiteResult

	for _, suite := range suites {
		msg := fmt.Sprintf("=== Running suite: %s ===",
			suite.Title)
		ui.PrintHeader(msg)

		suiteResult, runs := runSuite(ctx, mode, suite, ctx.SecurityLevel)
		suiteResults = append(suiteResults, suiteResult)

		for id, passed := range runs {
			checksPassed[id] = passed
		}

		// Collect fixApplied status from suiteResult
		for _, check := range suiteResult.Checks {
			if !check.Passed {
				fixesApplied[check.ID] = check.FixApplied
			}
		}
	}

	return suiteResults, checksPassed, fixesApplied, nil
}

func runSuite(ctx *config.ExecContext, mode RunMode, suite config.TestSuite, security_level string) (config.SuiteResult, map[string]bool) {
	runs := make(map[string]bool)
	var checkResults []config.CheckResult

	fixedCount, skippedCount, passedCount, failedCount, errorCount := 0, 0, 0, 0, 0
	for _, check := range suite.Checks {
		result := runCheck(ctx, mode, check, security_level)
		runs[check.ID] = result.Passed
		checkResults = append(checkResults, result)

		// Print status immediately

		if result.Skipped {
			ui.PrintSkipped(check.ID)
			skippedCount++
		} else if result.FixApplied {
			fixedCount++
		} else {
			manualActionPrefix := "manual action required"

			// 1. Check if the check PASSED AND its command starts with the Manual Action prefix
			if result.Passed && strings.HasPrefix(strings.ToLower(check.Command), manualActionPrefix) {
				// This is a documented audit point, print the command/output as INFO
				ui.PrintInfo(fmt.Sprintf("Check %s: MANUAL ACTION REQUIRED: %s", check.ID, result.Output))
				passedCount++
			} else if strings.Contains(result.Output, "error") {
				// Original error handling for non-manual checks
				msg := fmt.Sprintf(" Check %s: %s | error: %s", check.ID, check.Description, result.Output)
				ui.PrintErrorMessage(msg)
				errorCount++
			} else if result.Passed {
				// Generic Passed state for real checks that executed commands
				ui.PrintPassed(check.ID)
				passedCount++
			} else {
				// Failed state
				ui.PrintFailed(check.ID, check.Expected, result.Output, check.Command)
				failedCount++
			}
		}

	}
	summary := fmt.Sprintf("")
	if mode == ModeAudit {
		summary = fmt.Sprintf("Summary for '%s': %d total, %d passed, %d failed, %d errors, %d skipped",
			suite.Title, len(suite.Checks), passedCount, failedCount, errorCount, skippedCount)
	} else if mode == ModeFix {
		summary = fmt.Sprintf("Summary for '%s': %d total, %d fixed, %d passed, %d failed, %d errors, %d skipped",
			suite.Title, len(suite.Checks), fixedCount, passedCount, failedCount, errorCount, skippedCount)
	}
	ui.PrintSummary(summary)

	return config.SuiteResult{
		Title:  suite.Title,
		Checks: checkResults,
	}, runs
}

func RunCheck(check config.Check) (bool, string, error) {
	// --- Check for Manual Action Command (Case-Insensitive) ---
	manualActionPrefix := "manual action required"

	// Check if the command starts with the required phrase (case-insensitive comparison)
	if strings.HasPrefix(strings.ToLower(check.Command), manualActionPrefix) {
		// If it's a documentation command, we "pass" it and print the command itself.
		// The check is deemed successful as it verified the documentation requirement.
		// We set output to the command text for display purposes (optional, but informative).

		// Note: The caller (runSuite) expects the CheckResult object to show the command,
		// but here we return a direct pass with the command text as the output.

		// The check ID and description are handled by runSuite when it sees result.Passed=true.
		return true, check.Command, nil
	}
	// --- End Manual Action Check ---

	/* taken out for now, up for discussion
	if !config.FileExists(check.AffectedFile) {
		if !(check.AffectedFile == "N/A") {
			return false, "", fmt.Errorf("%s does not exist", check.AffectedFile)
		}
	}
	*/
	var cmd *exec.Cmd
	if check.Sudo {
		cmd = exec.Command("sudo", "sh", "-c", check.Command)
	} else {
		cmd = exec.Command("sh", "-c", check.Command)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := strings.TrimSpace(out.String())
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// non-exit errors, e.g., command not found
			return false, output, fmt.Errorf("command failed to start: %w", err)
		}
	}

	// Determine if check passed
	passed := output == check.Expected

	// Special handling for grep -c: exit 1 with 0 matches is not a true command error
	if strings.HasPrefix(check.Command, "grep -c") && exitCode == 1 && output == "0" {
		return false, output, nil // check failed, but command worked
	}

	// Any other non-zero exit code = real error
	if exitCode != 0 {
		return false, output, fmt.Errorf("command exited with code %d", exitCode)
	}

	return passed, output, nil
}

// RunFix executes the fix for a check and safely handles errors.
func RunFix(ctx *config.ExecContext, check config.Check) (applied bool, output string, err error) {
	manualActionPrefix := "manual action required"

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(check.Fix)), manualActionPrefix) {
		ui.PrintInfo(fmt.Sprintf("Check %s: %s", check.ID, check.Fix))
		return false, "Manual intervention required", nil
	}

	if strings.TrimSpace(check.Fix) == "" {
		return false, "", fmt.Errorf("no fix defined for check %s", check.ID)
	}

	cmdStr := check.Fix
	useSudo := check.Sudo

	ui.PrintInfo(fmt.Sprintf("Fixing %s with: %s", check.ID, check.Fix))

	if check.FixSudo != nil {
		useSudo = *check.FixSudo
	}

	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "sh", "-c", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Backup file before applying fix
	oldContent, origPerm, backupErr := rollback.PreBackup(check.AffectedFile)
	if backupErr != nil {
		ui.PrintErrorMessage(fmt.Sprintf("Could not backup file %s: %v", check.AffectedFile, backupErr))
	}

	// Run the fix command
	runErr := cmd.Run()
	output = strings.TrimSpace(out.String())

	// Backup already saved? Good, continue even if command had issues
	if err := rollback.PostDelta(ctx, check.AffectedFile, oldContent, origPerm, check); err != nil {
		return false, "", fmt.Errorf("failed to save delta: %w", err)
	}
	ui.PrintInfo("Backed-up file: " + check.AffectedFile)

	ui.PrintInfo(fmt.Sprintf("Post Action required: %s", check.PostAction))

	// Handle command exit
	if runErr != nil {
		// Non-fatal: mark as applied but warn user
		ui.PrintErrorMessage(fmt.Sprintf("Fix command for check %s exited with error: %v\nOutput:\n%s", check.ID, runErr, output))
	} else {
		ui.PrintFixed(fmt.Sprintf("Fix command for check %s applied successfully.", check.ID))
	}

	return true, output, nil
}
