package executor

import (
	"os"
	"path/filepath"
	"testing"

	"hardener/internal/config"
)

// Issue #13: FIXED must mean the check passes afterwards, not exit code 0.
func TestFixIsVerifiedByRerunningTheCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setting.conf")
	if err := os.WriteFile(path, []byte("0"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &config.ExecContext{BaseDir: dir, RunID: "test-run", SecurityLevel: "baseline"}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
		// Fix works: the check reads the file the fix writes.
		{ID: "effective", Command: "cat '" + path + "'", Expected: "1", Fix: "printf 1 > '" + path + "'", AffectedFile: path, SecurityLevel: "baseline"},
		// Fix exits 0 but changes nothing the check sees.
		{ID: "ineffective", Command: "printf 0", Expected: "1", Fix: "true", PostAction: "Reboot required", SecurityLevel: "baseline"},
		// Fix command fails.
		{ID: "failing", Command: "printf 0", Expected: "1", Fix: "exit 3", SecurityLevel: "baseline"},
	}}}
	results, _, fixes, err := RunSuitesObserved(ctx, ModeFix, "linux", "amd64", suites, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	checks := results[0].Checks
	if !checks[0].FixApplied || !checks[0].FixVerified {
		t.Fatalf("effective fix not verified: %+v", checks[0])
	}
	if !checks[1].FixApplied || checks[1].FixVerified {
		t.Fatalf("ineffective fix reported as verified: %+v", checks[1])
	}
	if checks[2].FixApplied || checks[2].FixVerified {
		t.Fatalf("failing fix reported as applied: %+v", checks[2])
	}
	if !fixes["effective"] || fixes["ineffective"] || fixes["failing"] {
		t.Fatalf("fix score counts unverified fixes: %v", fixes)
	}
}

// Issue #15: no fix may run without a usable rollback record.
func TestFixDoesNotRunWhenBackupFails(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "fix-ran")
	ctx := &config.ExecContext{BaseDir: dir, RunID: "test-run", SecurityLevel: "baseline"}
	check := config.Check{
		ID: "unbackupable", Command: "printf 0", Expected: "1",
		Fix:          "touch '" + marker + "'",
		AffectedFile: dir, // a directory cannot be backed up
	}
	applied, _, err := RunFix(ctx, check)
	if applied || err == nil {
		t.Fatalf("applied=%v err=%v, want refusal", applied, err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("fix ran although the backup failed")
	}
}
