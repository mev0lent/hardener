package executor

import (
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"hardener/report"
	"time"
)

func MakeReport(sys config.SystemInfo, suiteResults []config.SuiteResult, reportType, path string) {
	auditReport := config.AuditReport{
		Timestamp:    time.Now(),
		OS:           sys.OS,
		Arch:         sys.Arch,
		Distro:       sys.Distro,
		SuiteResults: suiteResults,
		ReportType:   reportType,
	}
	if err := report.Save(auditReport, ""); err != nil {
		msg := fmt.Sprintf("Failed to save report: %s\n", err)
		ui.PrintErrorMessage(msg)
	} else {
		msg := "\nSuccessfully saved audit report to " + path + "/reports\n"
		ui.PrintReport(msg)
	}
}
