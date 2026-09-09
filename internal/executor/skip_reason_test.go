package executor

import (
	"hardener/internal/config"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkipReasonsAreRecorded(t *testing.T) {
	ctx := &config.ExecContext{DistroName: "test-distro", SecurityLevel: "baseline"}
	for _, tc := range []struct {
		check config.Check
		want  string
	}{
		{config.Check{SecurityLevel: "high"}, "selected level is"},
		{config.Check{SecurityLevel: "baseline", Distro: map[string]config.DistroOverride{"other": {}}}, "not supported on distro"},
		{config.Check{SecurityLevel: "baseline", RequiresCommand: "hardener_nonexistent_test_command_91823"}, "required command"},
		{config.Check{SecurityLevel: "baseline", RequiresFile: filepath.Join(t.TempDir(), "missing")}, "required file"},
	} {
		result := runCheck(ctx, ModeAudit, tc.check, "baseline")
		if !(result.Skipped || result.SkippedDistro || result.SkippedMissing) || !strings.Contains(result.SkipReason, tc.want) {
			t.Fatalf("missing skip reason: %+v", result)
		}
	}
}
