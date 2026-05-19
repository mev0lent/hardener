package rollback

import (
	"encoding/json"
	"hardener/internal/config"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPostDeltaCreatesEntry(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &config.ExecContext{RunID: "test-run", BaseDir: tmpDir}

	file := filepath.Join(tmpDir, "test.conf")
	os.WriteFile(file, []byte("old config line"), 0644)

	oldContent, perm, _ := PreBackup(file)
	os.WriteFile(file, []byte("new config line"), 0644)

	check := config.Check{ID: "C001", AffectedFile: file}
	if err := PostDelta(ctx, file, oldContent, perm, check); err != nil {
		t.Fatalf("PostDelta failed: %v", err)
	}

	runsFile := filepath.Join(tmpDir, "runs.json")
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(runsFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	data, err := os.ReadFile(runsFile)
	if err != nil {
		t.Fatalf("runs.json not found: %v", err)
	}

	var runs map[string][]config.DeltaEntry
	if err := json.Unmarshal(data, &runs); err != nil {
		t.Fatalf("invalid runs.json: %v", err)
	}
	if len(runs) == 0 {
		t.Fatalf("expected at least one run entry, got none")
	}

	found := false
	for ts, entries := range runs {
		if len(entries) > 0 {
			found = true
			t.Logf("found run at %s with %d entries", ts, len(entries))
			break
		}
	}
	if !found {
		t.Fatalf("expected 1 entry, got none")
	}
}

func TestApplyRunRestoresFile(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := &config.ExecContext{RunID: "test-run", BaseDir: tmpDir}

	file := filepath.Join(tmpDir, "test.conf")
	orig := []byte("old config line")
	os.WriteFile(file, orig, 0644)

	oldContent, perm, _ := PreBackup(file)
	os.WriteFile(file, []byte("new config line"), 0644)

	check := config.Check{ID: "C001", AffectedFile: file}
	PostDelta(ctx, file, oldContent, perm, check)

	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatalf("ApplyRun failed: %v", err)
	}

	restored, _ := os.ReadFile(file)
	if string(restored) != string(orig) {
		t.Errorf("rollback failed: got %q, want %q", restored, orig)
	}
}

func TestFilterRollbackFiles(t *testing.T) {
	entries := []config.DeltaEntry{
		{FilePath: "/tmp/a.conf"},
		{FilePath: "/tmp/b.conf"},
	}
	files := []string{"/tmp/b.conf"}

	filtered := filterRollbackFiles(entries, files)
	if len(filtered) != 1 || filtered[0].FilePath != "/tmp/b.conf" {
		t.Errorf("expected 1 matching entry, got %+v", filtered)
	}
}

func TestInitializeRunsWithCorruptedJSON(t *testing.T) {
	tmpDir := t.TempDir()
	badFile := filepath.Join(tmpDir, "runs.json")
	os.WriteFile(badFile, []byte("{invalid-json"), 0644)

	runs, err := initializeRuns(badFile)
	if err == nil {
		t.Errorf("expected error for corrupted JSON, got nil")
	}
	if len(runs) != 0 {
		t.Errorf("expected empty runs map on error, got %d entries", len(runs))
	}
}
