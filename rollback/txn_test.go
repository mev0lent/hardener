package rollback

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"hardener/internal/config"
)

func newTestCtx(t *testing.T) *config.ExecContext {
	t.Helper()
	return &config.ExecContext{RunID: "test-run", BaseDir: t.TempDir()}
}

func readRuns(t *testing.T, ctx *config.ExecContext) []config.DeltaEntry {
	t.Helper()
	runs, err := initializeRuns(filepath.Join(ctx.BaseDir, "runs.json"))
	if err != nil {
		t.Fatal(err)
	}
	return runs[ctx.RunID]
}

// Issue #14: a file the fix created must be removed, not left empty.
func TestRollbackRemovesFileCreatedByFix(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "blacklist-usb-storage.conf")
	recordFix(t, ctx, config.Check{AffectedFile: file}, func() {
		if err := os.WriteFile(file, []byte("install usb-storage /bin/true\n"), 0644); err != nil {
			t.Fatal(err)
		}
	})
	if entries := readRuns(t, ctx); len(entries) != 1 || !entries[0].Created || entries[0].Pending {
		t.Fatalf("expected one completed created entry, got %+v", entries)
	}
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("created file still present after rollback: %v", err)
	}
	// A second rollback finds the file already absent and succeeds.
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatalf("repeated rollback: %v", err)
	}
}

func TestRollbackKeepsCreatedFileChangedAfterFix(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "created.conf")
	recordFix(t, ctx, config.Check{AffectedFile: file}, func() {
		os.WriteFile(file, []byte("from fix\n"), 0644)
	})
	os.WriteFile(file, []byte("edited by admin\n"), 0644)
	if err := ApplyRun(ctx, nil); err == nil {
		t.Fatal("expected refusal to remove a file changed after the fix")
	}
	if got, _ := os.ReadFile(file); string(got) != "edited by admin\n" {
		t.Fatalf("file changed: %q", got)
	}
}

func TestRollbackRecreatesFileRemovedByFix(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "cron.allow")
	os.WriteFile(file, []byte("root\n"), 0640)
	recordFix(t, ctx, config.Check{AffectedFile: file}, func() { os.Remove(file) })
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "root\n" || info.Mode().Perm() != 0640 {
		t.Fatalf("got %q mode %04o", got, info.Mode().Perm())
	}
}

func TestFixThatTouchesNothingLeavesNoRecord(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "never-created.conf")
	recordFix(t, ctx, config.Check{AffectedFile: file}, func() {})
	if entries := readRuns(t, ctx); len(entries) != 0 {
		t.Fatalf("expected no entries, got %+v", entries)
	}
}

// Issue #15: an unusable backup must be reported before the fix runs.
func TestBeginFailsForUnbackupablePath(t *testing.T) {
	ctx := newTestCtx(t)
	dir := t.TempDir()
	if _, err := Begin(ctx, config.Check{AffectedFile: dir}); err == nil {
		t.Fatal("expected Begin to refuse a directory")
	}
	if _, err := os.Stat(filepath.Join(ctx.BaseDir, "runs.json")); !os.IsNotExist(err) {
		t.Fatalf("no record may be written for a failed backup: %v", err)
	}
}

// Issue #16: the pre-fix state is on disk before the fix runs, and a record
// that was never completed can still be rolled back.
func TestPendingRecordSurvivesInterruptedFix(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "permit-root.conf")
	os.WriteFile(file, []byte("PermitRootLogin yes\n"), 0600)
	if _, err := Begin(ctx, config.Check{ID: "ssh-root", AffectedFile: file}); err != nil {
		t.Fatal(err)
	}
	entries := readRuns(t, ctx)
	if len(entries) != 1 || !entries[0].Pending || entries[0].Before == "" || entries[0].CheckID != "ssh-root" {
		t.Fatalf("expected a pending entry with the pre-fix copy, got %+v", entries)
	}
	// The process ends during the fix: content changed, Commit never runs.
	os.WriteFile(file, []byte("PermitRootLogin no\n"), 0600)
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "PermitRootLogin yes\n" {
		t.Fatalf("got %q", got)
	}
}

func TestCommitReplacesPendingRecord(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "login.defs")
	os.WriteFile(file, []byte("PASS_MAX_DAYS 99999\n"), 0644)
	recordFix(t, ctx, config.Check{AffectedFile: file}, func() {
		os.WriteFile(file, []byte("PASS_MAX_DAYS 365\n"), 0644)
	})
	entries := readRuns(t, ctx)
	if len(entries) != 1 || entries[0].Pending || entries[0].Before != "" || entries[0].PostChecksum == "" {
		t.Fatalf("expected one completed entry without a full copy, got %+v", entries)
	}
}

// Issue #17: the file is re-checked immediately before it is replaced.
func TestRollbackWriteRefusesConcurrentChange(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config")
	os.WriteFile(file, []byte("post-fix"), 0600)
	expected, err := captureFile(file)
	if err != nil {
		t.Fatal(err)
	}
	// Another writer changes the file after rollback has read and verified it.
	os.WriteFile(file, []byte("concurrent edit"), 0600)
	entry := config.DeltaEntry{FilePath: file, Perm: 0600}
	if err := writeRollbackTarget(entry, []byte("pre-fix"), expected); err == nil {
		t.Fatal("expected refusal")
	}
	if got, _ := os.ReadFile(file); string(got) != "concurrent edit" {
		t.Fatalf("concurrent edit lost: %q", got)
	}
}

// Issue #18: live kernel values set by a fix are restored.
func TestRollbackRestoresLiveKernelValue(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("kernel values are only tracked on Linux")
	}
	root := t.TempDir()
	old := procSys
	procSys = root
	t.Cleanup(func() { procSys = old })
	key := filepath.Join(root, "kernel", "dmesg_restrict")
	os.MkdirAll(filepath.Dir(key), 0755)
	os.WriteFile(key, []byte("0\n"), 0644)

	ctx := newTestCtx(t)
	check := config.Check{
		Command: "cat /proc/sys/kernel/dmesg_restrict",
		Fix:     "sysctl -w kernel.dmesg_restrict=1",
	}
	recordFix(t, ctx, check, func() { os.WriteFile(key, []byte("1\n"), 0644) })
	entries := readRuns(t, ctx)
	if len(entries) != 1 || entries[0].Kind != config.EntryKindSysctl {
		t.Fatalf("expected one sysctl entry, got %+v", entries)
	}
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(key); string(got) != "0\n" {
		t.Fatalf("kernel value = %q", got)
	}
}

func TestKernelPathsFromCommands(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("kernel values are only tracked on Linux")
	}
	root := t.TempDir()
	old := procSys
	procSys = root
	t.Cleanup(func() { procSys = old })
	for _, rel := range []string{"kernel/kptr_restrict", "net/ipv4/conf/all/rp_filter"} {
		path := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(path), 0755)
		os.WriteFile(path, []byte("0\n"), 0644)
	}
	check := config.Check{
		Command: "sysctl -n kernel.kptr_restrict",
		Fix:     "sysctl -q -w net.ipv4.conf.all.rp_filter=1 && sysctl --system && cat /proc/sys/kernel/missing",
	}
	got := kernelPaths(check)
	want := []string{filepath.Join(root, "kernel/kptr_restrict"), filepath.Join(root, "net/ipv4/conf/all/rp_filter")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("kernelPaths = %v, want %v", got, want)
	}
}

func TestUnixModeToFileMode(t *testing.T) {
	if got := unixModeToFileMode(0104755); got != 0755|os.ModeSetuid {
		t.Fatalf("got %v", got)
	}
}

// runs.json written by v1.2 has none of the new fields and must still restore.
func TestLegacyV12RecordStillRollsBack(t *testing.T) {
	ctx := newTestCtx(t)
	file := filepath.Join(t.TempDir(), "legacy.conf")
	os.WriteFile(file, []byte("after\n"), 0644)
	delta, checksum := ComputeDelta("after\n", "before\n")
	legacy := `{"test-run":[{"run_id":"test-run","timestamp":"2026-09-01T00:00:00Z","file_path":"` + file +
		`","checksum":"` + checksum + `","delta":` + strconv.Quote(delta) + `,"perm":420}]}`
	if err := os.WriteFile(filepath.Join(ctx.BaseDir, "runs.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(file); string(got) != "before\n" {
		t.Fatalf("got %q", got)
	}
}
