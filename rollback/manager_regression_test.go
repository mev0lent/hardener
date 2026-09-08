package rollback

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"hardener/internal/config"
)

func TestInitializeRunsHandlesNullAndReadErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runs.json")
	if err := os.WriteFile(path, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	runs, err := initializeRuns(path)
	if err != nil || runs == nil {
		t.Fatalf("null history: got %v, %v", runs, err)
	}
	if _, err := initializeRuns(dir); err == nil {
		t.Fatal("directory read error must not be treated as missing history")
	}
}

func TestPostDeltaPreservesCorruptedHistory(t *testing.T) {
	dir := t.TempDir()
	ctx := &config.ExecContext{RunID: "test-run", BaseDir: dir}
	path := filepath.Join(dir, "config")
	history := filepath.Join(dir, "runs.json")
	const corrupted = "{broken history"
	if err := os.WriteFile(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history, []byte(corrupted), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PostDelta(ctx, path, []byte("before"), 0600, config.Check{}); err == nil {
		t.Fatal("expected refusal to replace corrupted history")
	}
	got, err := os.ReadFile(history)
	if err != nil || string(got) != corrupted {
		t.Fatalf("history changed: %q, %v", got, err)
	}
}

func TestRestoreEntryRestoresUnchangedFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	delta, checksum := ComputeDelta("unchanged", "unchanged")
	entry := config.DeltaEntry{FilePath: path, Delta: delta, Checksum: checksum, Perm: 0600}
	if err := restoreEntry(entry); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %04o, want 0600", got)
	}
}

func TestApplyDeltaStopsFailedFileChainWithoutReordering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	// The older entry could apply to current bytes. It must still be skipped
	// once the newer entry for the same file has failed.
	delta, checksum := ComputeDelta("current", "older")
	entries := []config.DeltaEntry{
		{FilePath: path, Delta: delta, Checksum: checksum, Perm: 0600},
		{FilePath: path, Delta: "invalid patch", Perm: 0600},
	}
	original := append([]config.DeltaEntry(nil), entries...)
	if err := applyDelta(&config.ExecContext{}, entries); err == nil {
		t.Fatal("expected invalid patch error")
	}
	if !reflect.DeepEqual(entries, original) {
		t.Fatal("caller's entries were reordered")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "current" {
		t.Fatalf("failed chain changed file: %q, %v", got, err)
	}
}

func TestAtomicWriteFailurePreservesTargetAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "existing-directory")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("replacement"), 0600); err == nil {
		t.Fatal("expected rename failure when target is a directory")
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("target directory was lost: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".hardener-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files remain: %v, %v", leftovers, err)
	}
}
