package rollback

import (
	"os"
	"path/filepath"
	"testing"

	"hardener/internal/config"
)

// `systemctl mask` creates a link to /dev/null. Commit and rollback used to
// fail on it as "not a regular file", which also skipped the live-state hooks
// for every other restored file.
func TestRollbackRemovesMaskLinkCreatedByFix(t *testing.T) {
	ctx := newTestCtx(t)
	unit := filepath.Join(t.TempDir(), "avahi-daemon.service")
	recordFix(t, ctx, config.Check{AffectedFile: unit}, func() {
		if err := os.Symlink(os.DevNull, unit); err != nil {
			t.Fatal(err)
		}
	})
	if entries := readRuns(t, ctx); len(entries) != 1 || !entries[0].Created || entries[0].Pending {
		t.Fatalf("expected one completed created entry, got %+v", entries)
	}
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(unit); !os.IsNotExist(err) {
		t.Fatalf("mask link still present after rollback: %v", err)
	}
	if info, err := os.Stat(os.DevNull); err != nil || info.Mode()&os.ModeDevice == 0 {
		t.Fatalf("%s was touched: %v", os.DevNull, err)
	}
}

func TestRollbackKeepsMaskLinkRetargetedAfterFix(t *testing.T) {
	ctx := newTestCtx(t)
	dir := t.TempDir()
	unit := filepath.Join(dir, "bluetooth.service")
	recordFix(t, ctx, config.Check{AffectedFile: unit}, func() { os.Symlink(os.DevNull, unit) })
	os.Remove(unit)
	os.Symlink(filepath.Join(dir, "elsewhere"), unit)
	if err := ApplyRun(ctx, nil); err == nil {
		t.Fatal("expected refusal to remove a link changed after the fix")
	}
	if target, _ := os.Readlink(unit); target != filepath.Join(dir, "elsewhere") {
		t.Fatalf("link changed: %q", target)
	}
}

// A fix that replaces a file with a special link is undone by writing the file
// back in place of the link, never through it.
func TestRollbackReplacesLinkWithOriginalFile(t *testing.T) {
	ctx := newTestCtx(t)
	dir := t.TempDir()
	unit := filepath.Join(dir, "ctrl-alt-del.target")
	os.WriteFile(unit, []byte("[Unit]\nDescription=Reboot\n"), 0644)
	target := filepath.Join(dir, "must-not-be-written") // a dangling link target
	recordFix(t, ctx, config.Check{AffectedFile: unit}, func() {
		os.Remove(unit)
		os.Symlink(target, unit)
	})
	if err := ApplyRun(ctx, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(unit)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
		t.Fatalf("original file not restored: %v %v", info, err)
	}
	if got, _ := os.ReadFile(unit); string(got) != "[Unit]\nDescription=Reboot\n" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("rollback wrote through the link")
	}
}

// A path that already is a special link is not covered, like a directory.
func TestBeginLeavesExistingSpecialLinkUncovered(t *testing.T) {
	ctx := newTestCtx(t)
	unit := filepath.Join(t.TempDir(), "cups-browsed.service")
	os.Symlink(os.DevNull, unit)
	txn, err := Begin(ctx, config.Check{AffectedFile: unit})
	if err != nil {
		t.Fatalf("Begin refused an existing link: %v", err)
	}
	if txn.File() != "" || len(readRuns(t, ctx)) != 0 {
		t.Fatalf("special link recorded: %q %+v", txn.File(), readRuns(t, ctx))
	}
}
