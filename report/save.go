package report

import (
	"encoding/json"
	"fmt"
	"hardener/internal/config"
	"os"
	"path/filepath"
)

// Save writes the report as JSON into dir, or into ./reports when dir is
// empty, and returns the path of the file it wrote.
func Save(report config.AuditReport, dir string) (string, error) {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("could not determine current directory: %w", err)
		}
		dir = filepath.Join(cwd, "reports")
	}

	// Make sure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	// Filename with timestamp
	filename := filepath.Join(dir, fmt.Sprintf("%s-%s.json", report.ReportType, report.Timestamp.Format("2006-01-02_150405")))

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}

	return filename, os.WriteFile(filename, data, 0644)
}
