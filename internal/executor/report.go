package executor

import (
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"hardener/report"
	"time"
)

func MakeReport(sys config.SystemInfo, suiteResults []config.SuiteResult, reportType string) {
	MakeReportWithStatus(sys, suiteResults, reportType, false)
}

// MakeReportWithStatus explicitly marks reports from a stopped run as incomplete.
func MakeReportWithStatus(sys config.SystemInfo, suiteResults []config.SuiteResult, reportType string, incomplete bool) {
	auditReport := config.AuditReport{
		Incomplete:   incomplete,
		Timestamp:    time.Now(),
		OS:           sys.OS,
		Arch:         sys.Arch,
		Distro:       sys.Distro,
		SuiteResults: suiteResults,
		ReportType:   reportType,
	}
	// Report the file Save wrote. The message used to name the binary's
	// directory, while reports go to the working directory.
	if path, err := report.Save(auditReport, ""); err != nil {
		msg := fmt.Sprintf("Failed to save report: %s\n", err)
		ui.PrintErrorMessage(msg)
	} else {
		msg := "\nSuccessfully saved " + reportType + " report to " + path + "\n"
		ui.PrintReport(msg)
	}
}
