package dashboard

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"hardener/internal/config"
)

// resultMessage is shared by live events and saved reports, including legacy
// reports where the reason was stored only in output.
func resultMessage(r config.CheckResult) (string, string) {
	level, detail := "fail", r.Output
	switch {
	case r.Skipped || r.SkippedDistro || r.SkippedMissing:
		level = "skip"
		if r.SkipReason != "" {
			detail = r.SkipReason
		}
		if strings.TrimSpace(detail) == "" {
			switch {
			case r.SkippedDistro:
				detail = "unsupported distribution (details not recorded)"
			case r.SkippedMissing:
				detail = "missing prerequisite (details not recorded)"
			default:
				detail = "security level excluded this check (level not recorded)"
			}
		}
		detail = "Reason: " + detail
	case r.Manual:
		level = "manual"
	case r.FixApplied:
		level = "fixed"
	case r.Error != "":
		level = "error"
	case r.Passed:
		level = "pass"
	}
	if r.Error != "" && !strings.Contains(detail, r.Error) {
		detail += "\nError: " + r.Error
	}
	return level, r.ID + " · " + detail
}

func loadSavedReport(path string) (config.AuditReport, error) {
	var saved config.AuditReport
	f, err := os.Open(path)
	if err != nil {
		return saved, err
	}
	defer f.Close()
	const limit = 32 << 20
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return saved, err
	}
	if len(data) > limit {
		return saved, fmt.Errorf("report exceeds 32 MiB")
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return saved, fmt.Errorf("invalid report JSON: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return saved, err
	}
	if _, exists := fields["suite_results"]; !exists || (saved.ReportType != "audit" && saved.ReportType != "fix") {
		return saved, fmt.Errorf("expected an audit/fix report with suite_results; rollback runs.json is not a results report")
	}
	return saved, nil
}

func (m *model) setReport(saved config.AuditReport, path string) {
	m.viewing, m.done, m.opening, m.follow = true, true, false, false
	m.stopping, m.incomplete, m.runErr = false, saved.Incomplete, nil
	m.source, m.selected, m.active, m.focus = path, 0, -1, 0
	m.options = Options{Mode: saved.ReportType, System: saved.OS + " / " + saved.Arch + " · " + saved.Distro}
	m.savedAt = "timestamp not recorded"
	if !saved.Timestamp.IsZero() {
		m.savedAt = saved.Timestamp.Format(time.RFC3339)
	}
	m.suites, m.logs = nil, nil
	for _, s := range saved.SuiteResults {
		state := suiteState{title: cleanText(s.Title), total: len(s.Checks), results: make(map[int]config.CheckResult), done: true}
		for i, r := range s.Checks {
			state.results[i] = r
		}
		m.suites = append(m.suites, state)
	}
	m.logOffset = m.reportTopOffset()
}

// Saved results are not constrained by the live log ring buffer. Category
// navigation exposes every recorded check, without inventing per-check times.
func (m model) reportLogs() []logEntry {
	if m.selected < 0 || m.selected >= len(m.suites) {
		return nil
	}
	s := m.suites[m.selected]
	entries := make([]logEntry, 0, len(s.results))
	for i := 0; i < s.total; i++ {
		r := s.results[i]
		level, text := resultMessage(r)
		if r.Description != "" {
			text += "\n" + r.Description
		}
		entries = append(entries, logEntry{"        ", level, cleanText(text)})
	}
	return entries
}

func (m model) updateOpen(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.opening = false
	case "enter":
		path := strings.TrimSpace(m.pathInput)
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				m.openError = err.Error()
				return m, nil
			}
			path = filepath.Join(home, path[2:])
		}
		saved, err := loadSavedReport(path)
		if err != nil {
			m.openError = cleanText(err.Error())
		} else {
			m.setReport(saved, path)
		}
	case "backspace", "ctrl+h":
		r := []rune(m.pathInput)
		if len(r) > 0 {
			m.pathInput = string(r[:len(r)-1])
		}
	case "ctrl+u":
		m.pathInput = ""
	default:
		if key.Type == tea.KeyRunes {
			text := strings.ReplaceAll(cleanText(string(key.Runes)), "\n", "")
			// Preserve spaces typed individually in paths.
			if string(key.Runes) == " " {
				text = " "
			}
			if len(m.pathInput)+len(text) <= 4096 {
				m.pathInput += text
			}
		}
	}
	return m, nil
}

func (m model) openView() string {
	text := bold("OPEN SAVED REPORT", blue) + "\n\nEnter a JSON report path (relative to the working directory).\nExample: reports/audit-2026-09-08_120000.json\n\n" +
		cleanText(m.pathInput) + "▏\n\n" + paint(cleanText(m.openError), red) +
		"\n\nEnter open · Esc cancel · Ctrl+U clear · Ctrl+C close\n\nSaved results are read only. No checks or fixes are executed."
	return fitRows(text, m.width, m.height)
}

// ViewReport opens a standalone read-only dashboard without an executor worker.
func ViewReport(path string) error {
	if !Available() {
		return fmt.Errorf("report viewer requires an interactive terminal")
	}
	m := newModel(Options{Mode: "report"}, nil, nil, nil)
	m.done = true
	if path == "" {
		m.opening = true
	} else {
		saved, err := loadSavedReport(path)
		if err != nil {
			return err
		}
		m.setReport(saved, path)
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithFPS(20)).Run()
	return err
}

// Start saved categories at their first result with a full viewport.
func (m model) reportTopOffset() int {
	height := m.height - 10
	if m.width < 96 {
		height -= 8
	}
	return max(0, len(m.logRows(m.logWidth()))-max(1, height-4))
}
