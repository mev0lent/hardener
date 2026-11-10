package report

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hardener/internal/config"
)

// The success message used to name the binary's directory, while the report
// went to another; Save now returns the file it wrote.
func TestSaveReturnsWrittenPath(t *testing.T) {
	dir := t.TempDir()
	path, err := Save(config.AuditReport{ReportType: "audit", Timestamp: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "audit-2026-09-08_120000.json") {
		t.Fatalf("unexpected path %q", path)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("returned path is not the saved report: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
