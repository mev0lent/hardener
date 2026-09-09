package dashboard

import (
	tea "github.com/charmbracelet/bubbletea"
	"hardener/internal/config"
	"hardener/internal/executor"
	"hardener/internal/ui"
	"strings"
	"testing"
)

func TestCategoryLogsFollowSelectionAndResume(t *testing.T) {
	m := testModel()
	m.width, m.height = 120, 36
	// Duplicate titles must not combine different suites.
	m.suites[1].title = m.suites[0].title
	for i, output := range []string{"alpha-output", "beta-output"} {
		m.applyEvent(executor.RunEvent{Kind: executor.SuiteStarted, SuiteIndex: i})
		m.applyEvent(executor.RunEvent{Kind: executor.CheckFinished, SuiteIndex: i, CheckIndex: 0, Result: config.CheckResult{Output: output}})
	}
	if got := strings.Join(m.logRows(80), "\n"); !strings.Contains(got, "beta-output") || strings.Contains(got, "alpha-output") {
		t.Fatal(got)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(model)
	if got := strings.Join(m.logRows(80), "\n"); !strings.Contains(got, "alpha-output") || strings.Contains(got, "beta-output") {
		t.Fatal(got)
	}
	before := strings.Join(m.logRows(80), "\n")
	next, _ = m.Update(ui.LogEntry{Level: "info", Message: "raw-beta-command"})
	m = next.(model)
	if m.follow || strings.Join(m.logRows(80), "\n") != before {
		t.Fatal("background output changed browsed category")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	m = next.(model)
	if !m.follow || m.selected != 1 || m.logOffset != 0 || !strings.Contains(strings.Join(m.logRows(80), "\n"), "raw-beta-command") {
		t.Fatal("follow did not restore category and latest output")
	}
	m.focus = 1
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(model)
	m.applyEvent(executor.RunEvent{Kind: executor.SuiteStarted, SuiteIndex: 0})
	if m.follow || m.selected != 1 {
		t.Fatal("scrolling should pause category following")
	}
}

func TestCategoryBuffersAndGlobalNotices(t *testing.T) {
	m := testModel()
	m.addSuiteLog(0, "info", "keep-first-category")
	for i := 0; i < maxLogEntries+10; i++ {
		m.addSuiteLog(1, "info", "second-category")
	}
	if len(m.suites[1].logs) != maxLogEntries || len(m.suites[0].logs) != 1 {
		t.Fatal("buffers must be independent and bounded")
	}
	m.addLog("report", "saved-report")
	for i := range m.suites {
		m.selected = i
		if !strings.Contains(strings.Join(m.logRows(80), "\n"), "[RUN] saved-report") {
			t.Fatal("global report notice hidden")
		}
	}
}
