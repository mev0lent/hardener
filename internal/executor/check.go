package executor

import (
	"fmt"
	"hardener/internal/config"
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
