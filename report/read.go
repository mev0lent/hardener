package report

import (
	"encoding/json"
	config "hardener/internal/config"
	"os"
)

func Load(filePath string) (config.AuditReport, error) {
	var report config.AuditReport
	data, err := os.ReadFile(filePath)
	if err != nil {
		return report, err
	}
	err = json.Unmarshal(data, &report)
	return report, err
}
