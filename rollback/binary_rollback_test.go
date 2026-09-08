package rollback

import (
	"bytes"
	"hardener/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryPlistRollback(t *testing.T) {
	before, err := os.ReadFile("testdata/software-update-before.plist")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("testdata/software-update-after.plist")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"restore", "drift", "corrupt snapshot"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx := &config.ExecContext{RunID: "test-run", BaseDir: dir}
			file := filepath.Join(dir, "SoftwareUpdate.plist")
			write := func(data []byte) {
				t.Helper()
				if err := os.WriteFile(file, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(before)
			old, perm, err := PreBackup(file)
			if err != nil {
				t.Fatal(err)
			}
			write(after)
			if err := PostDelta(ctx, file, old, perm, config.Check{}); err != nil {
				t.Fatal(err)
			}
			want := before
			var rollbackErr error
			switch mode {
			case "restore":
				// A second change to the same file must unwind in reverse order.
				intermediate := append(append([]byte{}, after...), 0xff)
				write(intermediate)
				if err := PostDelta(ctx, file, after, perm, config.Check{}); err != nil {
					t.Fatal(err)
				}
				rollbackErr = ApplyRun(ctx, nil)
			case "drift":
				want = append(append([]byte{}, after...), 0xfe)
				write(want)
				rollbackErr = ApplyRun(ctx, nil)
			case "corrupt snapshot":
				runs, err := initializeRuns(filepath.Join(dir, "runs.json"))
				if err != nil {
					t.Fatal(err)
				}
				entries := runs[ctx.RunID]
				// Valid encoding but wrong target bytes: the recorded target
				// SHA-256 must still prevent a write.
				entries[0].Delta, _ = ComputeDelta(string(after), "\xffwrong target")
				want = after
				rollbackErr = applyDelta(ctx, entries)
			}
			if mode == "restore" && rollbackErr != nil {
				t.Fatal(rollbackErr)
			}
			if mode != "restore" && rollbackErr == nil {
				t.Fatal("expected rollback refusal")
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("file differs: got %x, want %x", got, want)
			}
		})
	}
}
