package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"hardener/internal/config"
)

func TestScoreThresholds(t *testing.T) {
	for _, n := range []int{0, 34} {
		if scoreColor(n) != red {
			t.Errorf("%d should be red", n)
		}
	}
	for _, n := range []int{35, 64} {
		if scoreColor(n) != amber {
			t.Errorf("%d should be orange", n)
		}
	}
	for _, n := range []int{65, 100} {
		if scoreColor(n) != green {
			t.Errorf("%d should be green", n)
		}
	}
	if scoreColor(-1) != muted {
		t.Fatal("unassessed score should be neutral")
	}
}

func TestResultReasons(t *testing.T) {
	for _, tc := range []struct {
		r           config.CheckResult
		level, text string
	}{
		{config.CheckResult{Skipped: true, SkipReason: "requires strict; selected baseline", Output: "old"}, "skip", "Reason: requires strict"},
		{config.CheckResult{SkippedMissing: true, Output: "required file missing"}, "skip", "Reason: required file missing"},
		{config.CheckResult{SkippedDistro: true}, "skip", "unsupported distribution"},
		{config.CheckResult{Skipped: true}, "skip", "level not recorded"},
		{config.CheckResult{Error: "permission denied", Output: "stderr"}, "error", "Error: permission denied"},
	} {
		level, text := resultMessage(tc.r)
		if level != tc.level || !strings.Contains(text, tc.text) {
			t.Fatalf("%s: %q", level, text)
		}
	}
}

func TestLoadReports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	for _, tc := range []struct {
		json  string
		valid bool
	}{
		{`{"report_type":"audit","suite_results":[]}`, true},
		{`{"report_type":"fix","incomplete":true,"suite_results":[{"title":"Updates","checks":[{"id":"one","skipped":true,"output":"requires strict"}]}]}`, true},
		{`{}`, false}, {`null`, false}, {`{"run-id":[]}`, false},
		{`{"report_type":"audit","suite_results":null}`, true},
		{`{"report_type":"audit","suite_results":[]} trailing`, false},
	} {
		if err := os.WriteFile(path, []byte(tc.json), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadSavedReport(path)
		if (err == nil) != tc.valid {
			t.Errorf("valid=%v: %s: %v", tc.valid, tc.json, err)
		}
	}
	if _, err := loadSavedReport(path + ".missing"); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestSavedReportNavigationAndOpen(t *testing.T) {
	saved := config.AuditReport{ReportType: "audit", Incomplete: true, SuiteResults: []config.SuiteResult{
		{Title: "First", Checks: []config.CheckResult{{ID: "first", Passed: true}}},
		{Title: "Second", Checks: make([]config.CheckResult, maxLogEntries+5)},
	}}
	saved.SuiteResults[1].Checks[maxLogEntries+4] = config.CheckResult{ID: "last", Skipped: true, Output: "higher security level"}
	path := filepath.Join(t.TempDir(), "saved report.json")
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	m := testModel()
	m.done, m.width, m.height = true, 120, 36
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = next.(model)
	m.pathInput = path
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if !m.viewing || !m.incomplete || m.opening || m.totals().passed != 1 {
		t.Fatalf("incorrect import: %+v", m)
	}
	if view := m.View(); !strings.Contains(view, "PARTIAL REPORT") || strings.Contains(view, "H  HARDENER") || !strings.Contains(view, "RECORDED CHECKS") {
		t.Fatal("incorrect saved report presentation")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(model)
	logs := m.reportLogs()
	if len(logs) != maxLogEntries+5 || !strings.Contains(logs[len(logs)-1].text, "last") {
		t.Fatal("report results truncated by live log limit")
	}
	m.opening, m.pathInput = true, path+".missing"
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(model)
	if m.openError == "" || !m.opening || m.selected != 1 {
		t.Fatal("failed open should retain previous report")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(model).opening {
		t.Fatal("escape must close path prompt")
	}
}

func TestOpenDoesNotInterruptActiveRun(t *testing.T) {
	m := testModel()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if next.(model).opening || next.(model).stopping {
		t.Fatal("open must wait until execution finishes")
	}
}
