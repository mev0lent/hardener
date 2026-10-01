package executor

import (
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"os"
	"os/exec"
	"strings"
)

func runCheck(ctx *config.ExecContext, mode RunMode, check config.Check, security_level string) config.CheckResult {
	if !config.DetermineSecurityFit(security_level, check.SecurityLevel) {
		return config.CheckResult{
			ID:          check.ID,
			Description: check.Description,
			Output:      fmt.Sprintf("requires security level %q; selected level is %q", check.SecurityLevel, security_level),
			SkipReason:  fmt.Sprintf("requires security level %q; selected level is %q", check.SecurityLevel, security_level),
			Skipped:     true,
		}
	}

	// Build the distro lookup chain: [DistroName, ...DistroFamily].
	// DistroFamily already starts with the ID when populated by run.go,
	// but we defensively include DistroName first in case a caller has only
	// set DistroName without family detection.
	chain := ctx.DistroFamily
	if len(chain) == 0 || (len(chain) > 0 && chain[0] != ctx.DistroName) {
		chain = append([]string{ctx.DistroName}, chain...)
	}

	resolved, supported := check.ResolveForDistro(chain, ctx.Profile)
	if !supported {
		return config.CheckResult{
			ID:            check.ID,
			Description:   check.Description,
			Output:        fmt.Sprintf("not supported on distro %q", ctx.DistroName),
			SkipReason:    fmt.Sprintf("not supported on distro %q", ctx.DistroName),
			SkippedDistro: true,
		}
	}
	check = resolved

	if check.RequiresCommand != "" {
		probe := exec.Command("sh", "-c", "command -v "+check.RequiresCommand+" >/dev/null 2>&1")
		probe.Env = HardenerCmdEnv()
		if err := probe.Run(); err != nil {
			ui.PrintSkippedMissing(check.ID, check.RequiresCommand)
			return config.CheckResult{
				ID:             check.ID,
				Description:    check.Description,
				Output:         fmt.Sprintf("required command %q not found", check.RequiresCommand),
				SkipReason:     fmt.Sprintf("required command %q not found", check.RequiresCommand),
				SkippedMissing: true,
			}
		}
	}

	if check.RequiresFile != "" {
		// Only skip on true "not found" — a permission error means the file
		// probably exists but the audit process can't stat it without sudo.
		if _, err := os.Lstat(check.RequiresFile); err != nil && os.IsNotExist(err) {
			ui.PrintSkippedMissing(check.ID, check.RequiresFile)
			return config.CheckResult{
				ID:             check.ID,
				Description:    check.Description,
				Output:         fmt.Sprintf("required file %q not present", check.RequiresFile),
				SkipReason:     fmt.Sprintf("required file %q not present", check.RequiresFile),
				SkippedMissing: true,
			}
		}
	}

	passed, output, err := RunCheck(check)

	if err != nil {
		// Keep whatever the command actually produced and attach the error as
		// context. Replacing the output with the error string made every
		// failure render as the same "command exited with code 1" line, which
		// hid the difference between a genuine finding, a broken command, a
		// permission problem and a failed sudo.
		if strings.TrimSpace(output) != "" {
			output = fmt.Sprintf("%s (%v)", output, err)
		} else {
			output = err.Error()
		}
		passed = false
	}

	// Built after the error branch, so Passed and Output are each assigned
	// exactly once and cannot drift apart.
	result := config.CheckResult{
		ID:          check.ID,
		Description: check.Description,
		Passed:      passed,
		Output:      output,
		Manual:      strings.HasPrefix(strings.ToLower(strings.TrimSpace(check.Command)), "manual action required"),
	}
	if err != nil {
		result.Error = err.Error()
	}

	if !passed && mode == ModeFix && strings.TrimSpace(check.Fix) != "" {
		ok, out, fixErr := RunFix(ctx, check)
		result.FixApplied = ok
		if fixErr != nil {
			result.Output = fmt.Sprintf("Fix failed: %v", fixErr)
			result.Error = fixErr.Error()
		} else {
			result.Output = out
		}
		if ok {
			verifyFix(check, &result)
		}
	}

	return result
}

// verifyFix re-runs the check after a fix whose command exited 0. Exit code 0
// alone says nothing about the resulting state: a fix can succeed and still
// leave the check failing, e.g. until its post_action (a reboot, a service
// restart) has happened.
func verifyFix(check config.Check, result *config.CheckResult) {
	passed, output, err := RunCheck(check)
	result.FixVerified = err == nil && passed
	if result.FixVerified {
		ui.PrintFixed(fmt.Sprintf("Fix for check %s verified: the check now passes.", check.ID))
		return
	}
	got := output
	if err != nil {
		got = strings.TrimSpace(fmt.Sprintf("%s (%v)", output, err))
	}
	msg := fmt.Sprintf("Fix for check %s ran, but the check still fails (expected %q, got %q).", check.ID, check.Expected, got)
	if pa := strings.TrimSpace(check.PostAction); pa != "" && !strings.EqualFold(pa, "none") {
		msg += " It may take effect after the post action: " + pa
	}
	ui.PrintErrorMessage(msg)
	if strings.TrimSpace(result.Output) != "" {
		msg += "\nFix output: " + result.Output
	}
	result.Output = msg
}
