package dashboard

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"hardener/internal/config"
	"hardener/internal/executor"
	"hardener/internal/ui"
)

const maxLogEntries = 800

type suiteState struct {
	title   string
	total   int
	results map[int]config.CheckResult
	done    bool
}

type counts struct {
	total, complete, passed, failed, errors, fixed, skipped, manual int
}

func (c counts) assessed() int { return c.passed + c.failed + c.errors + c.fixed }
func (c counts) percent() int {
	if c.assessed() == 0 {
		return -1
	}
	return 100 * c.passed / c.assessed()
}

func (s suiteState) counts() counts {
	c := counts{total: s.total, complete: len(s.results)}
	for _, result := range s.results {
		switch {
		case result.Skipped || result.SkippedDistro || result.SkippedMissing:
			c.skipped++
		case result.Manual:
			c.manual++
		case result.FixApplied:
			c.fixed++
		case result.Error != "":
			c.errors++
		case result.Passed:
			c.passed++
		default:
			c.failed++
		}
	}
	return c
}

func (m model) totals() counts {
	var total counts
	for _, suite := range m.suites {
		c := suite.counts()
		total.total += c.total
		total.complete += c.complete
		total.passed += c.passed
		total.failed += c.failed
		total.errors += c.errors
		total.fixed += c.fixed
		total.skipped += c.skipped
		total.manual += c.manual
	}
	return total
}

type logEntry struct{ at, level, text string }
type readyMsg struct{}
type tickMsg time.Time
type finishedMsg struct {
	err     error
	stopped bool
}

type model struct {
	viewing, opening                      bool
	pathInput, openError, source, savedAt string
	incomplete                            bool
	options                               Options
	suites                                []suiteState
	logs                                  []logEntry
	width, height                         int
	active, selected, focus, logOffset    int
	follow, done, stopping                bool
	runErr                                error
	started                               time.Time
	finished                              time.Time
	frame                                 int
	ready                                 chan struct{}
	stop                                  func()
}

func newModel(options Options, suites []config.TestSuite, ready chan struct{}, stop func()) model {
	m := model{options: options, active: -1, follow: true, started: time.Now(), ready: ready, stop: stop}
	for _, suite := range suites {
		m.suites = append(m.suites, suiteState{title: cleanText(suite.Title), total: len(suite.Checks), results: make(map[int]config.CheckResult)})
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(func() tea.Msg { return readyMsg{} }, tick())
}
func tick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case readyMsg:
		if m.ready != nil {
			close(m.ready)
			m.ready = nil
		}
	case tea.WindowSizeMsg:
		initialSize := m.width == 0
		m.width, m.height = msg.Width, msg.Height
		if m.viewing && initialSize {
			m.logOffset = m.reportTopOffset()
		}
		m.logOffset = min(m.logOffset, max(0, len(m.logRows(m.logWidth()))-1))
	case tickMsg:
		m.frame++
		if !m.done {
			return m, tick()
		}
	case executor.RunEvent:
		m.applyEvent(msg)
	case ui.LogEntry:
		m.addLog(msg.Level, msg.Message)
	case finishedMsg:
		m.done, m.stopping, m.runErr, m.finished = true, msg.stopped, msg.err, time.Now()
		if msg.err != nil {
			m.addLog("error", msg.err.Error())
		}
		if msg.stopped {
			m.addLog("stop", "Stopped between checks. Completed results remain visible here.")
		} else {
			m.addLog("done", "Run finished. Review the results; press q to return to your shell.")
		}
		if m.totals().fixed > 0 {
			m.addLog("info", "Fix commands are not verification. Run an audit again to measure the new hardening level.")
		}
	case tea.KeyMsg:
		if m.opening {
			return m.updateOpen(msg)
		}
		previous := m.selected
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			if m.done {
				return m, tea.Quit
			}
			if !m.stopping {
				m.stopping = true
				if m.stop != nil {
					m.stop()
				}
				m.addLog("stop", "Stop requested. Waiting for the current check and any rollback backup to finish…")
			}
		case "o":
			if m.done {
				m.opening = true
				m.pathInput = ""
				m.openError = ""
			} else {
				m.addLog("info", "Open a saved report after this run finishes.")
			}
		case "tab":
			m.focus = 1 - m.focus
		case "left", "h":
			m.focus = 0
		case "right", "l":
			m.focus = 1
		case "f":
			m.follow = true
			m.logOffset = 0
			if m.active >= 0 {
				m.selected = m.active
			}
		case "up", "k":
			m.scroll(-1)
		case "down", "j":
			m.scroll(1)
		case "pgup":
			m.scroll(-5)
		case "pgdown":
			m.scroll(5)
		case "g", "home":
			m.follow = false
			if m.focus == 0 {
				m.selected = 0
			} else {
				m.logOffset = max(0, len(m.logRows(m.logWidth()))-1)
			}
		case "G", "end":
			if m.focus == 0 {
				m.selected = max(0, len(m.suites)-1)
				m.follow = false
			} else {
				m.logOffset = 0
			}
		}
		if m.viewing && previous != m.selected {
			m.logOffset = m.reportTopOffset()
		}
	}
	return m, nil
}

func (m *model) scroll(delta int) {
	if m.focus == 0 {
		m.follow = false
		m.selected = min(max(0, m.selected+delta), max(0, len(m.suites)-1))
	} else {
		m.logOffset = min(max(0, m.logOffset-delta), max(0, len(m.logRows(m.logWidth()))-1))
	}
}

func (m *model) applyEvent(event executor.RunEvent) {
	if event.SuiteIndex < 0 || event.SuiteIndex >= len(m.suites) {
		return
	}
	suite := &m.suites[event.SuiteIndex]
	switch event.Kind {
	case executor.SuiteStarted:
		m.active = event.SuiteIndex
		if m.follow {
			m.selected = m.active
		}
		m.addLog("suite", suite.title)
	case executor.CheckStarted:
		m.addLog("run", event.Check.ID+" · "+event.Check.Description)
	case executor.CheckFinished:
		if event.CheckIndex < 0 || event.CheckIndex >= suite.total {
			return
		}
		if _, exists := suite.results[event.CheckIndex]; exists {
			return
		}
		suite.results[event.CheckIndex] = event.Result
		level, text := resultMessage(event.Result)
		m.addLog(level, text)
	case executor.SuiteFinished:
		suite.done = true
	}
}

func (m *model) addLog(level, text string) {
	oldRows := 0
	if m.logOffset > 0 {
		oldRows = len(m.logRows(m.logWidth()))
	}
	text = cleanText(text)
	if len(text) > 4096 {
		text = string([]rune(text)[:min(1024, len([]rune(text)))]) + "…"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		lines = append(lines[:10], "… output truncated in dashboard; see report")
	}
	m.logs = append(m.logs, logEntry{time.Now().Format("15:04:05"), level, strings.Join(lines, "\n")})
	if len(m.logs) > maxLogEntries {
		m.logs = m.logs[len(m.logs)-maxLogEntries:]
	}
	if m.logOffset > 0 {
		m.logOffset = max(0, m.logOffset+len(m.logRows(m.logWidth()))-oldRows)
	}
}

// Remove terminal escapes and control characters from command/ruleset text.
func cleanText(text string) string {
	text = ansi.Strip(text)
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
}

func percentage(n int) string {
	if n < 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", n)
}
