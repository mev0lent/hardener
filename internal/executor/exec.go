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

	return RunSuitesObserved(ctx, mode, osName, archName, suites, nil, nil)
}

// RunSuitesObserved lets an optional UI observe results and request a stop
// between checks. A running fix always gets to complete its rollback record.
func RunSuitesObserved(ctx *config.ExecContext, mode RunMode, osName, archName string,
	suites []config.TestSuite, observer Observer, stop func() bool) (
	[]config.SuiteResult, map[string]bool, map[string]bool, error) {

	if len(suites) == 0 {
		// All return statements must now include a nil error.
		return nil, nil, nil, fmt.Errorf("error: No hardening guides (.md files with checks) found at the defined position: %s", ctx.BaseDir)
	}

	checksPassed := make(map[string]bool)
	fixesApplied := make(map[string]bool)
	var suiteResults []config.SuiteResult

	for suiteIndex, suite := range suites {
		if !suiteMatchesLabels(suite, ctx.Labels) {
			continue
		}
		if stopRequested(stop) {
			break
		}
		notify(observer, RunEvent{Kind: SuiteStarted, SuiteIndex: suiteIndex, Suite: suite})
		msg := fmt.Sprintf("=== Running suite: %s ===",
			suite.Title)
		ui.PrintHeader(msg)

		suiteResult := runSuite(ctx, mode, suite, ctx.SecurityLevel, suiteIndex, observer, stop)
		suiteResults = append(suiteResults, suiteResult)
		if len(suiteResult.Checks) == len(suite.Checks) {
			notify(observer, RunEvent{Kind: SuiteFinished, SuiteIndex: suiteIndex, Suite: suite})
		}

		for _, check := range suiteResult.Checks {
			// Manual checks pass without verifying anything, so they stay out
			// of the pass rate, as they do on the dashboard.
			if check.Skipped || check.SkippedDistro || check.SkippedMissing || check.Manual {
				continue
			}
			checksPassed[check.ID] = check.Passed
			if !check.Passed && mode == ModeFix {
				// Only a fix the re-run check confirmed counts as applied.
				fixesApplied[check.ID] = check.FixVerified
			}
		}
	}

	return suiteResults, checksPassed, fixesApplied, nil
}

func runSuite(ctx *config.ExecContext, mode RunMode, suite config.TestSuite, security_level string, suiteIndex int, observer Observer, stop func() bool) config.SuiteResult {
	var checkResults []config.CheckResult

	fixedCount, notEffectiveCount, skippedCount, distroSkippedCount, missingCount, passedCount, failedCount, errorCount, manualCount := 0, 0, 0, 0, 0, 0, 0, 0, 0
	for checkIndex, check := range suite.Checks {
		if stopRequested(stop) {
			break
		}
		notify(observer, RunEvent{Kind: CheckStarted, SuiteIndex: suiteIndex, CheckIndex: checkIndex, Check: check})
		result, ran := runCheck(ctx, mode, check, security_level)
		checkResults = append(checkResults, result)
		notify(observer, RunEvent{Kind: CheckFinished, SuiteIndex: suiteIndex, CheckIndex: checkIndex, Check: check, Result: result})

		// Print status immediately. The order matches the dashboard's
		// classification, and errors come from the structured Error field:
		// searching the output for "error" counted real errors whose message
		// lacks the word as failures, and passing checks whose output
		// contains it as errors.
		switch {
		case result.SkippedMissing:
			missingCount++
		case result.SkippedDistro:
			ui.PrintSkipped(check.ID, result.SkipReason)
			distroSkippedCount++
		case result.Skipped:
			ui.PrintSkipped(check.ID, result.SkipReason)
			skippedCount++
		case result.Manual:
			// A documented audit point: show the instruction as INFO.
			ui.PrintInfo(fmt.Sprintf("Check %s: MANUAL ACTION REQUIRED: %s", check.ID, result.Output))
			manualCount++
		case result.FixVerified:
			fixedCount++
		case result.FixApplied:
			notEffectiveCount++
		case result.Error != "":
			ui.PrintErrorMessage(fmt.Sprintf(" Check %s: %s | error: %s", check.ID, check.Description, result.Output))
			errorCount++
		case result.Passed:
			ui.PrintPassed(check.ID)
			passedCount++
		default:
			// Show the command and expected value the distro overrides selected.
			expected := ran.Expected
			if ran.ExpectedOp != "" {
				expected = ran.ExpectedOp + " " + expected
			}
			ui.PrintFailed(check.ID, expected, result.Output, ran.Command)
			failedCount++
		}

	}
	summary := fmt.Sprintf("")
	if mode == ModeAudit {
		summary = fmt.Sprintf("Summary for '%s': %d total, %d passed, %d failed, %d errors, %d manual, %d skipped, %d distro-skipped, %d missing-command",
			suite.Title, len(suite.Checks), passedCount, failedCount, errorCount, manualCount, skippedCount, distroSkippedCount, missingCount)
	} else if mode == ModeFix {
		summary = fmt.Sprintf("Summary for '%s': %d total, %d fixed, %d fix not effective, %d passed, %d failed, %d errors, %d manual, %d skipped, %d distro-skipped, %d missing-command",
			suite.Title, len(suite.Checks), fixedCount, notEffectiveCount, passedCount, failedCount, errorCount, manualCount, skippedCount, distroSkippedCount, missingCount)
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
	// A "Manual action required: ..." command documents an audit point that
	// cannot be automated. It "passes" with its own text as output; callers
	// mark the result as manual so it is not counted as a verified pass.
	if isManualAction(check.Command) {
		return true, check.Command, nil
	}

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

	// stdout and stderr are captured separately. Only stdout is compared
	// against `expected`: a diagnostic line such as
	// "grep: /etc/modprobe.d/x.conf: Permission denied" is not part of the
	// value the check produces, and merging it into the same buffer made any
	// check that touched an unreadable path fail regardless of system state.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	errOutput := strings.TrimSpace(stderr.String())
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// non-exit errors, e.g., command not found
			return false, output, fmt.Errorf("command failed to start: %w%s", err, stderrSuffix(errOutput))
		}
	}

	// A counting grep exits 1 when it matches nothing, but "0" is a valid count
	// and the check then fails or passes on it like any other value.
	if exitCode == 1 && output == "0" && countsGrepMatches(check.Command) {
		passed := compareExpected(output, check.Expected, check.ExpectedOp)
		return passed, output, nil
	}

	// Any other non-zero exit code = real error
	if exitCode != 0 {
		return false, output, fmt.Errorf("command exited with code %d%s", exitCode, stderrSuffix(errOutput))
	}

	passed := compareExpected(output, check.Expected, check.ExpectedOp)
	return passed, output, nil
}

// stderrSuffix renders captured stderr as trailing diagnostic context for an
// error message. Returns "" when the command wrote nothing to stderr, so error
// strings are unchanged in the common case. Multi-line stderr is collapsed to
// its first three lines, so that one unreadable directory cannot turn a single
// finding into a forty-line report entry.
func stderrSuffix(stderr string) string {
	if stderr == "" {
		return ""
	}
	lines := strings.Split(stderr, "\n")
	if len(lines) > 3 {
		return fmt.Sprintf(" (stderr: %s; ... +%d more)",
			strings.Join(lines[:3], "; "), len(lines)-3)
	}
	return fmt.Sprintf(" (stderr: %s)", strings.Join(lines, "; "))
}

// RunFix executes the fix for a check and safely handles errors. applied only
// means the fix command exited 0; runCheck re-runs the check to verify it.
func RunFix(ctx *config.ExecContext, check config.Check) (applied bool, output string, err error) {
	if isManualAction(check.Fix) {
		ui.PrintInfo(fmt.Sprintf("Check %s: %s", check.ID, check.Fix))
		return false, "Manual intervention required", nil
	}

	if strings.TrimSpace(check.Fix) == "" {
		return false, "", fmt.Errorf("no fix defined for check %s", check.ID)
	}

	ui.PrintInfo(fmt.Sprintf("Fixing %s with: %s", check.ID, check.Fix))

	wrapped := wrapWithPath(check.Fix)
	var cmd *exec.Cmd
	if fixUsesSudo(check) {
		cmd = exec.Command("sudo", "sh", "-c", wrapped)
	} else {
		cmd = exec.Command("sh", "-c", wrapped)
	}
	cmd.Env = HardenerCmdEnv()

	// A fix's output is only ever displayed, never compared against `expected`
	// (compareExpected is called from RunCheck alone), so both streams share one
	// buffer here on purpose: that preserves the chronological interleaving of
	// normal output and diagnostics, which is what makes a failed fix readable.
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Record the pre-fix state before anything changes. Without a usable
	// record, rollback could later empty the file or strip its permissions,
	// so the fix does not run at all.
	txn, backupErr := rollback.Begin(ctx, check)
	if backupErr != nil {
		ui.PrintErrorMessage(fmt.Sprintf("Fix for check %s not applied: %v", check.ID, backupErr))
		return false, "", fmt.Errorf("rollback backup failed, fix not applied: %w", backupErr)
	}

	// Run the fix command
	runErr := cmd.Run()
	output = strings.TrimSpace(out.String())

	// Complete the record even when the fix failed: it may have changed
	// something before exiting. On failure the pending record is kept and
	// still restores the pre-fix state.
	if err := txn.Commit(); err != nil {
		ui.PrintErrorMessage(fmt.Sprintf("Rollback record for check %s left pending: %v", check.ID, err))
	} else if txn.File() != "" {
		ui.PrintInfo("Backed-up file: " + txn.File())
	}

	ui.PrintInfo(fmt.Sprintf("Post Action required: %s", check.PostAction))

	if runErr != nil {
		ui.PrintErrorMessage(fmt.Sprintf("Fix command for check %s exited with error: %v\nOutput:\n%s", check.ID, runErr, output))
		return false, output, nil
	}

	return true, output, nil
}

// fixUsesSudo reports whether a check's fix runs under sudo: fix_sudo when
// the ruleset sets it, otherwise the check's own sudo setting.
func fixUsesSudo(check config.Check) bool {
	if check.FixSudo != nil {
		return *check.FixSudo
	}
	return check.Sudo
}

// isManualAction reports whether a command or fix is a "Manual action
// required: ..." instruction instead of something to execute.
func isManualAction(command string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(command)), "manual action required")
}
