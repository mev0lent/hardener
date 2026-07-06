package executor

import (
	"bytes"
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"hardener/rollback"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// hardenerSbinDirs are the sbin locations we prepend to PATH. Distros like
// openSUSE do not include /usr/sbin in the login PATH for regular users, which
// makes commands such as `sysctl`, `firewall-cmd`, `iptables`, and `auditctl`
// return exit 127 in non-privileged checks. sudo also resets PATH via
// secure_path from sudoers, and some distros ship a secure_path that omits
// /usr/sbin, so we ALSO inject an `export PATH=...` prefix into the shell
// command itself — that prefix runs inside the sudo'd shell after sudo has
// reset the environment, guaranteeing our sbin dirs are visible.
var hardenerSbinDirs = []string{"/usr/local/sbin", "/usr/sbin", "/sbin"}

// pathPrefix is the shell snippet prepended to every check/fix command so
// that sysctl/firewall-cmd/auditctl/iptables are always findable regardless
// of sudo's secure_path or the user's login PATH.
const pathPrefix = `export PATH="/usr/local/sbin:/usr/sbin:/sbin:${PATH:-/usr/local/bin:/usr/bin:/bin}"; `

// wrapWithPath prepends pathPrefix to the shell command.
func wrapWithPath(cmd string) string {
	return pathPrefix + cmd
}

// HardenerCmdEnv returns the environment that every check/fix command should
// run with: the parent process env plus /usr/local/sbin, /usr/sbin, /sbin
// prepended to PATH if they are not already present. This helps non-sudo
// commands; sudo'd commands additionally get the pathPrefix wrapped into the
// command string above.
func HardenerCmdEnv() []string {
	env := os.Environ()
	currentPath := ""
	pathIdx := -1
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			currentPath = strings.TrimPrefix(kv, "PATH=")
			pathIdx = i
			break
		}
	}
	parts := strings.Split(currentPath, ":")
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		seen[p] = true
	}
	var prepend []string
	for _, d := range hardenerSbinDirs {
		if !seen[d] {
			prepend = append(prepend, d)
			seen[d] = true
		}
	}
	if len(prepend) == 0 {
		return env
	}
	newPath := strings.Join(prepend, ":")
	if currentPath != "" {
		newPath += ":" + currentPath
	}
	if pathIdx == -1 {
		return append(env, "PATH="+newPath)
	}
	env[pathIdx] = "PATH=" + newPath
	return env
}

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
		if !suiteMatchesLabels(suite, ctx.Labels) {
			continue
		}
		msg := fmt.Sprintf("=== Running suite: %s ===",
			suite.Title)
		ui.PrintHeader(msg)

		suiteResult := runSuite(ctx, mode, suite, ctx.SecurityLevel)
		suiteResults = append(suiteResults, suiteResult)

		for _, check := range suiteResult.Checks {
			if check.Skipped || check.SkippedDistro || check.SkippedMissing {
				continue
			}
			checksPassed[check.ID] = check.Passed
			if !check.Passed {
				fixesApplied[check.ID] = check.FixApplied
			}
		}
	}

	return suiteResults, checksPassed, fixesApplied, nil
}

func runSuite(ctx *config.ExecContext, mode RunMode, suite config.TestSuite, security_level string) config.SuiteResult {
	var checkResults []config.CheckResult

	fixedCount, skippedCount, distroSkippedCount, missingCount, passedCount, failedCount, errorCount := 0, 0, 0, 0, 0, 0, 0
	for _, check := range suite.Checks {
		result := runCheck(ctx, mode, check, security_level)
		checkResults = append(checkResults, result)

		// Print status immediately

		if result.SkippedMissing {
			missingCount++
		} else if result.SkippedDistro {
			ui.PrintSkipped(fmt.Sprintf("%s (distro)", check.ID))
			distroSkippedCount++
		} else if result.Skipped {
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
		summary = fmt.Sprintf("Summary for '%s': %d total, %d passed, %d failed, %d errors, %d skipped, %d distro-skipped, %d missing-command",
			suite.Title, len(suite.Checks), passedCount, failedCount, errorCount, skippedCount, distroSkippedCount, missingCount)
	} else if mode == ModeFix {
		summary = fmt.Sprintf("Summary for '%s': %d total, %d fixed, %d passed, %d failed, %d errors, %d skipped, %d distro-skipped, %d missing-command",
			suite.Title, len(suite.Checks), fixedCount, passedCount, failedCount, errorCount, skippedCount, distroSkippedCount, missingCount)
	}
	ui.PrintSummary(summary)

	return config.SuiteResult{
		Title:  suite.Title,
		Checks: checkResults,
	}
}

func suiteMatchesLabels(suite config.TestSuite, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, req := range requested {
		for _, label := range suite.Labels {
			if strings.EqualFold(label, req) {
				return true
			}
		}
	}
	return false
}

// compareExpected evaluates whether output satisfies the expected value under the given op.
// Supported ops: ">=", ">", "<=", "<" (numeric); anything else falls back to string equality.
func compareExpected(output, expected, op string) bool {
	switch op {
	case ">=", ">", "<=", "<":
		outVal, err1 := strconv.Atoi(strings.TrimSpace(output))
		expVal, err2 := strconv.Atoi(strings.TrimSpace(expected))
		if err1 != nil || err2 != nil {
			return false
		}
		switch op {
		case ">=":
			return outVal >= expVal
		case ">":
			return outVal > expVal
		case "<=":
			return outVal <= expVal
		case "<":
			return outVal < expVal
		}
	}
	return output == expected
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
	// Wrap the command so PATH is always augmented — this survives sudo's
	// env_reset / secure_path (openSUSE's secure_path omits /usr/sbin, which
	// otherwise makes `sysctl` and friends exit 127 under sudo).
	wrapped := wrapWithPath(check.Command)
	var cmd *exec.Cmd
	if check.Sudo {
		cmd = exec.Command("sudo", "sh", "-c", wrapped)
	} else {
		cmd = exec.Command("sh", "-c", wrapped)
	}
	cmd.Env = HardenerCmdEnv()

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

	// Special handling for grep -c: exit 1 with 0 matches is not a true command error.
	// Matches both standalone "grep -c ..." and piped "... | grep -c ..."
	if strings.Contains(check.Command, "grep -c") && exitCode == 1 && output == "0" {
		passed := compareExpected(output, check.Expected, check.ExpectedOp)
		return passed, output, nil
	}

	// Any other non-zero exit code = real error
	if exitCode != 0 {
		return false, output, fmt.Errorf("command exited with code %d", exitCode)
	}

	passed := compareExpected(output, check.Expected, check.ExpectedOp)
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

	wrapped := wrapWithPath(cmdStr)
	var cmd *exec.Cmd
	if useSudo {
		cmd = exec.Command("sudo", "sh", "-c", wrapped)
	} else {
		cmd = exec.Command("sh", "-c", wrapped)
	}
	cmd.Env = HardenerCmdEnv()

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

	if runErr != nil {
		ui.PrintErrorMessage(fmt.Sprintf("Fix command for check %s exited with error: %v\nOutput:\n%s", check.ID, runErr, output))
		return false, output, nil
	}

	ui.PrintFixed(fmt.Sprintf("Fix command for check %s applied successfully.", check.ID))
	return true, output, nil
}
