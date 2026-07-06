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
			Output:      fmt.Sprintf("not the security level of %s", check.SecurityLevel),
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
				SkippedMissing: true,
			}
		}
	}

	passed, output, err := RunCheck(check)
	result := config.CheckResult{
		ID:          check.ID,
		Description: check.Description,
		Passed:      passed,
		Output:      output,
	}

	if err != nil {
		result.Output = err.Error()
		passed = false
	}

	if !passed && mode == ModeFix && strings.TrimSpace(check.Fix) != "" {
		ok, out, fixErr := RunFix(ctx, check)
		result.FixApplied = ok
		if fixErr != nil {
			result.Output = fmt.Sprintf("Fix failed: %v", fixErr)
		} else {
			result.Output = out
		}
	}

	return result
}
