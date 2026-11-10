package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hardener/internal/config"
	"hardener/internal/ui"
)

// captureLog collects plain-mode log lines by level while fn runs.
func captureLog(t *testing.T, fn func()) map[string][]string {
	t.Helper()
	logs := make(map[string][]string)
	restore := ui.SetLogSink(func(e ui.LogEntry) { logs[e.Level] = append(logs[e.Level], e.Message) })
	defer restore()
	fn()
	return logs
}

// Errors are counted from the structured Error field. Searching the output
// for "error" counted a passing check that prints the word as an error and a
// failing command whose message lacks it as a plain failure.
func TestSummaryCountsErrorsFromStructuredStatus(t *testing.T) {
	ctx := &config.ExecContext{BaseDir: t.TempDir(), SecurityLevel: "baseline"}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
		{ID: "passes", Command: "printf error", Expected: "error", SecurityLevel: "baseline"},
		{ID: "broken", Command: "exit 2", Expected: "1", SecurityLevel: "baseline"},
		{ID: "fails", Command: "printf 0", Expected: "1", SecurityLevel: "baseline"},
		{ID: "manual", Command: "Manual action required: review firmware", SecurityLevel: "baseline"},
	}}}
	var passed map[string]bool
	var fixes map[string]bool
	logs := captureLog(t, func() {
		var err error
		_, passed, fixes, err = RunSuitesObserved(ctx, ModeAudit, "linux", "amd64", suites, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	})
	summary := strings.Join(logs["summary"], "\n")
	if !strings.Contains(summary, "4 total, 1 passed, 1 failed, 1 errors, 1 manual") {
		t.Fatalf("wrong summary: %s", summary)
	}
	// Manual checks verify nothing and stay out of the pass rate.
	if _, counted := passed["manual"]; counted || len(passed) != 3 {
		t.Fatalf("pass rate inputs: %v", passed)
	}
	// An audit applies no fixes, so it has no fix rate.
	if len(fixes) != 0 {
		t.Fatalf("audit recorded fix results: %v", fixes)
	}
}

// A failure shows the command and expected value of the distro override that
// ran, not the universal ones.
func TestFailureShowsResolvedDistroCommand(t *testing.T) {
	ctx := &config.ExecContext{BaseDir: t.TempDir(), SecurityLevel: "baseline", DistroName: "ubuntu", DistroFamily: []string{"ubuntu", "debian"}}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{{
		ID: "c", Command: "printf universal", Expected: "u", SecurityLevel: "baseline",
		Distro:     map[string]config.DistroOverride{"debian": {Command: "printf 2", Expected: "3"}},
		ExpectedOp: ">=",
	}}}}
	logs := captureLog(t, func() {
		RunSuitesObserved(ctx, ModeAudit, "linux", "amd64", suites, nil, nil)
	})
	fail := strings.Join(logs["fail"], "\n")
	if !strings.Contains(fail, "expected >= 3") || !strings.Contains(fail, "printf 2") {
		t.Fatalf("failure does not show the override that ran: %s", fail)
	}
}

// --dry-run used to be ignored, so fix --dry-run applied every fix. The CLI
// runs a dry run as an audit; a fix-mode run with DryRun set must not fix
// either.
func TestDryRunListsFixesWithoutRunningThem(t *testing.T) {
	for _, mode := range []RunMode{ModeAudit, ModeFix} {
		dir := t.TempDir()
		marker := filepath.Join(dir, "fix-ran")
		affected := filepath.Join(dir, "setting.conf")
		os.WriteFile(affected, []byte("0"), 0600)
		ctx := &config.ExecContext{BaseDir: dir, RunID: "test-run", SecurityLevel: "baseline", DryRun: true}
		suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
			{ID: "c", Command: "printf 0", Expected: "1", Fix: "touch '" + marker + "'", AffectedFile: affected, SecurityLevel: "baseline"},
		}}}
		var results []config.SuiteResult
		logs := captureLog(t, func() {
			results, _, _, _ = RunSuitesObserved(ctx, mode, "linux", "amd64", suites, nil, nil)
		})
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("%s: dry run executed the fix", mode)
		}
		if _, err := os.Stat(filepath.Join(dir, "runs.json")); !os.IsNotExist(err) {
			t.Fatalf("%s: dry run wrote a rollback record", mode)
		}
		if r := results[0].Checks[0]; r.Passed || r.FixApplied {
			t.Fatalf("%s: dry run changed the result: %+v", mode, r)
		}
		if info := strings.Join(logs["info"], "\n"); !strings.Contains(info, "Dry run: c would be fixed with: touch") {
			t.Fatalf("%s: planned fix not listed: %s", mode, info)
		}
	}
}

// A check's own os/arch lists were parsed but never applied.
func TestCheckLevelArchIsApplied(t *testing.T) {
	ctx := &config.ExecContext{SecurityLevel: "baseline", OSName: "darwin", ArchName: "amd64"}
	result, _ := runCheck(ctx, ModeAudit, config.Check{ID: "silicon", Command: "printf 1", Expected: "1", SecurityLevel: "baseline", Arch: []string{"arm64"}}, "baseline")
	if !result.SkippedDistro || !strings.Contains(result.SkipReason, "arm64") {
		t.Fatalf("arm64-only check ran on amd64: %+v", result)
	}
	result, _ = runCheck(ctx, ModeAudit, config.Check{ID: "any", Command: "printf 1", Expected: "1", SecurityLevel: "baseline", Arch: []string{"x86_64", "arm64"}}, "baseline")
	if !result.Passed {
		t.Fatalf("check for both archs did not run: %+v", result)
	}
}

func TestOptionalChecksRunOnlyWhenRequested(t *testing.T) {
	check := config.Check{ID: "opt", Command: "printf 1", Expected: "1", SecurityLevel: "optional"}
	ctx := &config.ExecContext{}
	if result, _ := runCheck(ctx, ModeAudit, check, "high"); !result.Skipped {
		t.Fatalf("optional check ran at high: %+v", result)
	}
	if result, _ := runCheck(ctx, ModeAudit, check, "optional"); !result.Passed {
		t.Fatalf("optional check did not run at optional: %+v", result)
	}
	if result, _ := runCheck(ctx, ModeAudit, config.Check{SecurityLevel: "hihg"}, "optional"); !strings.Contains(result.SkipReason, "unknown security level") {
		t.Fatalf("unknown level not explained: %+v", result)
	}
}
