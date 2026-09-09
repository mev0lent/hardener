package executor

import (
	"os"
	"path/filepath"
	"testing"

	"hardener/internal/config"
)

func TestObserverStopsBetweenChecksAndPreservesPartialResults(t *testing.T) {
	ctx := &config.ExecContext{BaseDir: t.TempDir(), SecurityLevel: "baseline"}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
		{ID: "same", Command: "printf 1", Expected: "1", SecurityLevel: "baseline"},
		{ID: "same", Command: "printf 0", Expected: "1", SecurityLevel: "baseline"},
	}}}
	var events []RunEvent
	stop := false
	results, _, _, err := RunSuitesObserved(ctx, ModeAudit, "linux", "amd64", suites, func(e RunEvent) {
		events = append(events, e)
		if e.Kind == CheckFinished {
			stop = true
		}
	}, func() bool { return stop })
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].Checks) != 1 || !results[0].Checks[0].Passed {
		t.Fatalf("partial results lost: %+v", results)
	}
	if len(events) != 3 || events[0].Kind != SuiteStarted || events[1].Kind != CheckStarted || events[2].Kind != CheckFinished {
		t.Fatalf("wrong event sequence: %+v", events)
	}
}

func TestObservedFixFinishesBackupBeforeStopping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "setting.conf")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := &config.ExecContext{BaseDir: dir, RunID: "test-run", SecurityLevel: "baseline"}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
		{ID: "fix", Command: "printf 0", Expected: "1", Fix: "printf after > '" + path + "'", AffectedFile: path, SecurityLevel: "baseline"},
		{ID: "later", Command: "printf 1", Expected: "1", SecurityLevel: "baseline"},
	}}}
	stop := false
	results, _, _, err := RunSuitesObserved(ctx, ModeFix, "linux", "amd64", suites, func(e RunEvent) {
		if e.Kind == CheckFinished {
			stop = true
		}
	}, func() bool { return stop })
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].Checks) != 1 || !results[0].Checks[0].FixApplied {
		t.Fatalf("fix result missing: %+v", results)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs.json")); err != nil {
		t.Fatalf("rollback backup missing: %v", err)
	}
}

func TestObservedErrorsAndManualChecksHaveStructuredStatus(t *testing.T) {
	ctx := &config.ExecContext{BaseDir: t.TempDir(), SecurityLevel: "baseline"}
	suites := []config.TestSuite{{Title: "Category", Checks: []config.Check{
		{ID: "error", Command: "exit 2", Expected: "1", SecurityLevel: "baseline"},
		{ID: "manual", Command: "Manual action required: review firmware", SecurityLevel: "baseline"},
	}}}
	results, _, _, err := RunSuitesObserved(ctx, ModeAudit, "linux", "amd64", suites, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Checks[0].Error == "" {
		t.Fatal("command error was lost")
	}
	if !results[0].Checks[1].Manual {
		t.Fatal("manual check was counted as verification")
	}
}

func TestDashboardLabelSelectionMatchesExecution(t *testing.T) {
	suites := []config.TestSuite{{Title: "Network", Labels: []string{"network"}}, {Title: "Auth", Labels: []string{"auth"}}}
	selected := FilterSuites(suites, []string{"NETWORK"})
	if len(selected) != 1 || selected[0].Title != "Network" {
		t.Fatalf("unexpected selection: %+v", selected)
	}
}
