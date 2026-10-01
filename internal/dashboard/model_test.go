package dashboard

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"hardener/internal/config"
	"hardener/internal/executor"
)

func TestVerifiedScoreExcludesSkippedAndManualButNotUnverifiedFixes(t *testing.T) {
	suite := suiteState{total: 8, results: map[int]config.CheckResult{
		0: {Passed: true}, 1: {Passed: true}, 2: {}, 3: {Error: "command failed"},
		4: {FixApplied: true, FixVerified: true}, 5: {Skipped: true}, 6: {SkippedMissing: true}, 7: {Passed: true, Manual: true},
	}}
	c := suite.counts()
	if c.percent() != 40 || c.assessed() != 5 || c.complete != 8 || c.fixed != 1 || c.manual != 1 || c.skipped != 2 {
		t.Fatalf("incorrect score accounting: %+v, percent=%d", c, c.percent())
	}
	ineffective := suiteState{total: 1, results: map[int]config.CheckResult{0: {FixApplied: true}}}.counts()
	if ineffective.fixed != 0 || ineffective.unverified != 1 || ineffective.assessed() != 1 {
		t.Fatalf("a fix the re-check did not confirm must not count as fixed: %+v", ineffective)
	}
	if (counts{}).percent() != -1 {
		t.Fatal("unassessed checks must not display 0% or 100%")
	}
}

func testModel() model {
	suites := []config.TestSuite{
		{Title: "Operating System Updates", Checks: make([]config.Check, 3)},
		{Title: "Accounts & Authentication", Checks: make([]config.Check, 2)},
	}
	return newModel(Options{Mode: "audit", System: "linux / amd64", SecurityLevel: "baseline"}, suites, nil, nil)
}

func TestDuplicateIDsAndTitlesRemainIndependent(t *testing.T) {
	m := testModel()
	m.suites[1].title = m.suites[0].title
	for _, event := range []executor.RunEvent{
		{Kind: executor.CheckFinished, SuiteIndex: 0, CheckIndex: 0, Result: config.CheckResult{ID: "same", Passed: true}},
		{Kind: executor.CheckFinished, SuiteIndex: 0, CheckIndex: 1, Result: config.CheckResult{ID: "same"}},
		{Kind: executor.CheckFinished, SuiteIndex: 1, CheckIndex: 0, Result: config.CheckResult{ID: "same", Passed: true}},
	} {
		m.applyEvent(event)
		m.applyEvent(event)
	}
	if c := m.totals(); c.complete != 3 || c.passed != 2 || c.failed != 1 {
		t.Fatalf("duplicate IDs corrupted counters: %+v", c)
	}
}

func TestQuitWaitsForWorkerCompletion(t *testing.T) {
	stopped := false
	m := testModel()
	m.stop = func() { stopped = true }
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if !stopped || cmd != nil {
		t.Fatal("q must request a boundary stop, not quit a running fix")
	}
	m = next.(model)
	next, _ = m.Update(finishedMsg{stopped: true})
	_, cmd = next.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("completed dashboard should allow quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("expected quit after worker completion")
	}
}

func TestViewFitsTerminalSizesAndSanitizesOutput(t *testing.T) {
	for _, size := range [][2]int{{132, 42}, {120, 30}, {96, 24}, {80, 24}, {64, 24}, {40, 12}} {
		m := testModel()
		m.width, m.height = size[0], size[1]
		m.applyEvent(executor.RunEvent{Kind: executor.SuiteStarted, SuiteIndex: 0})
		m.addLog("error", "\x1b[2J\x1b[31m試験 🌍 "+strings.Repeat("long output ", 40))
		view := m.View()
		if lipgloss.Height(view) > m.height {
			t.Fatalf("height %d exceeds %d", lipgloss.Height(view), m.height)
		}
		for _, row := range strings.Split(view, "\n") {
			if lipgloss.Width(row) > m.width {
				t.Fatalf("row exceeds terminal %v: %d", size, lipgloss.Width(row))
			}
		}
		if strings.Contains(view, "\x1b[2J") {
			t.Fatal("command output injected a terminal clear sequence")
		}
		if m.width >= 64 && !strings.Contains(ansi.Strip(view), "HARDENER") {
			t.Fatal("missing app header")
		}
	}
}

func TestLogIsBoundedAndCategoryFollowingCanResume(t *testing.T) {
	m := testModel()
	for i := 0; i < maxLogEntries+20; i++ {
		m.addLog("info", "entry")
	}
	if len(m.logs) != maxLogEntries {
		t.Fatal("activity log is unbounded")
	}
	m.follow = false
	m.selected = 0
	m.applyEvent(executor.RunEvent{Kind: executor.SuiteStarted, SuiteIndex: 1})
	if m.selected != 0 {
		t.Fatal("active suite interrupted manual browsing")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m = next.(model)
	if !m.follow || m.selected != 1 {
		t.Fatal("follow did not resume")
	}
}
