package dashboard

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	ink   = lipgloss.AdaptiveColor{Light: "#192536", Dark: "#E6EDF7"}
	muted = lipgloss.AdaptiveColor{Light: "#58677C", Dark: "#8191AA"}
	line  = lipgloss.AdaptiveColor{Light: "#B6C1CE", Dark: "#34435A"}
	blue  = lipgloss.AdaptiveColor{Light: "#315FC9", Dark: "#7AA8FF"}
	green = lipgloss.AdaptiveColor{Light: "#14795B", Dark: "#6DDDB5"}
	amber = lipgloss.AdaptiveColor{Light: "#A14F09", Dark: "#F5A65B"}
	red   = lipgloss.AdaptiveColor{Light: "#B6344B", Dark: "#F58B98"}
)

func paint(text string, color lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Foreground(color).Render(text)
}
func bold(text string, color lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Bold(true).Foreground(color).Render(text)
}
func scoreColor(percent int) lipgloss.TerminalColor {
	if percent < 0 {
		return muted
	}
	if percent < 35 {
		return red
	}
	if percent < 65 {
		return amber
	}
	return green
}
func statusColor(level string) lipgloss.TerminalColor {
	switch level {
	case "pass", "done":
		return green
	case "fail", "error", "stop":
		return red
	case "fixed", "manual":
		return amber
	case "suite", "run", "report":
		return blue
	default:
		return muted
	}
}

func fit(text string, width int) string {
	width = max(0, width)
	text = ansi.Truncate(text, width, "…")
	return text + strings.Repeat(" ", max(0, width-lipgloss.Width(text)))
}
func fitRows(text string, width, height int) string {
	rows := strings.Split(text, "\n")
	if len(rows) > height {
		rows = rows[:max(0, height)]
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = fit(rows[i], width)
	}
	return strings.Join(rows, "\n")
}
func panel(text string, width, height int, border lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).
		Padding(0, 1).Render(fitRows(text, width-4, height-2))
}
func bar(percent, width int, color lipgloss.TerminalColor) string {
	filled := 0
	if percent >= 0 {
		filled = min(width, max(0, percent*width/100))
	}
	return paint(strings.Repeat("━", filled), color) + paint(strings.Repeat("─", width-filled), line)
}
func pair(left, right string, width int) string {
	rightWidth := lipgloss.Width(right)
	if rightWidth >= width {
		return fit(right, width)
	}
	return fit(left, width-rightWidth) + right
}

func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Preparing dashboard…"
	}
	if m.opening {
		return m.openView()
	}
	if m.width < 64 || m.height < 24 {
		return fitRows("HARDENER\n\nResize to at least 64 × 24 to see the dashboard.\nExecution continues; q requests a stop after this check.", m.width, m.height)
	}
	c := m.totals()
	status := []string{"◐", "◓", "◑", "◒"}[m.frame%4] + " RUNNING"
	statusInk := blue
	if m.stopping {
		status, statusInk = "STOPPING AFTER CURRENT CHECK", amber
	}
	if m.done {
		status, statusInk = "COMPLETE · q TO CLOSE", green
		if m.stopping {
			status, statusInk = "STOPPED · q TO CLOSE", amber
		}
		if m.runErr != nil {
			status, statusInk = "RUN ERROR · q TO CLOSE", red
		}
	}
	if m.viewing {
		status, statusInk = "SAVED REPORT · READ ONLY", blue
		if m.incomplete {
			status, statusInk = "PARTIAL REPORT · READ ONLY", amber
		}
	}
	end := time.Now()
	if !m.finished.IsZero() {
		end = m.finished
	}
	elapsed := end.Sub(m.started).Round(time.Second)
	header := pair(bold("HARDENER", ink)+paint("  /  "+strings.ToUpper(m.options.Mode), blue), bold(status, statusInk), m.width)
	meta := pair(paint(cleanText(m.options.System)+"  ·  "+cleanText(m.options.SecurityLevel), muted), paint(elapsed.String(), muted), m.width)
	if m.viewing {
		meta = fit(paint(cleanText(m.options.System)+" · "+m.savedAt+" · "+cleanText(m.source), muted), m.width)
	}
	w1, w2 := (m.width-2)/3, (m.width-2)/3
	w3 := m.width - w1 - w2 - 2
	score := panel(paint("VERIFIED HARDENING", muted)+"\n"+bold(percentage(c.percent()), scoreColor(c.percent()))+paint(fmt.Sprintf("  %d/%d assessed", c.passed, c.assessed()), muted), w1, 4, line)
	progress := panel(paint("RUN PROGRESS", muted)+"\n"+bold(fmt.Sprintf("%d / %d", c.complete, c.total), ink)+paint(" checks", muted), w2, 4, line)
	if m.viewing {
		progress = panel(paint("RECORDED CHECKS", muted)+"\n"+bold(fmt.Sprint(c.complete), ink)+paint(" · planned total not recorded", muted), w2, 4, line)
	}
	findings := panel(paint("FINDINGS / FIX COMMANDS", muted)+"\n"+paint(fmt.Sprintf("%d fail · %d error", c.failed, c.errors), red)+paint(fmt.Sprintf(" · %d fixed", c.fixed), amber), w3, 4, line)
	metrics := lipgloss.JoinHorizontal(lipgloss.Top, score, " ", progress, " ", findings)

	bodyHeight := m.height - 10
	var body string
	if m.width >= 96 {
		leftWidth := min(48, max(34, m.width*36/100))
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.categoryView(leftWidth, bodyHeight), " ", m.activityView(m.width-leftWidth-1, bodyHeight))
	} else {
		// Compact terminals show one category at a time above the activity feed.
		body = m.categoryView(m.width, 7) + "\n" + m.activityView(m.width, bodyHeight-8)
	}
	footer := paint("tab focus  ↑/↓ scroll  f follow live  o open JSON  q stop/close", muted)
	if m.viewing {
		footer = paint("tab focus  ↑/↓ browse  home/end scroll  o open JSON  q close", muted)
	}
	legend := paint("Score = passed / assessed. Skipped & manual excluded; fixes need re-audit.", muted)
	return fitRows(header+"\n"+meta+"\n\n"+metrics+"\n\n"+body+"\n"+footer+"\n"+legend, m.width, m.height)
}

func (m model) categoryView(width, height int) string {
	if len(m.suites) == 0 {
		return fitRows("No matching categories", width, height)
	}
	slots := max(1, (height-2)/5)
	selected := min(max(0, m.selected), len(m.suites)-1)
	start := (selected / slots) * slots
	end := min(len(m.suites), start+slots)
	focusInk := muted
	if m.focus == 0 {
		focusInk = blue
	}
	header := pair(bold("CATEGORIES", focusInk), paint(fmt.Sprintf("%d–%d / %d", start+1, end, len(m.suites)), muted), width)
	rows := []string{header}
	for i := start; i < end; i++ {
		suite := m.suites[i]
		c := suite.counts()
		border := line
		if i == selected {
			border = blue
		}
		state := "QUEUED"
		if i == m.active && !suite.done {
			state = "RUNNING"
		}
		if suite.done {
			state = "DONE"
		}
		if m.done && !suite.done {
			state = "PARTIAL"
			if c.complete == 0 {
				state = "NOT RUN"
			}
		}
		if m.viewing {
			state = "RECORDED"
		}
		title := pair(bold(fmt.Sprintf("%02d ", i+1), muted)+bold(suite.title, ink), paint(" "+state, muted), width-4)
		pct := c.percent()
		gauge := bold(percentage(pct), scoreColor(pct)) + " " + bar(pct, max(4, width-20), scoreColor(pct)) + paint(" verified", muted)
		counts := fmt.Sprintf("%d/%d run · %d pass · %d fail", c.complete, c.total, c.passed, c.failed)
		if m.viewing {
			counts = fmt.Sprintf("%d recorded · %d pass · %d fail", c.complete, c.passed, c.failed)
		}
		if c.errors > 0 {
			counts += fmt.Sprintf(" · %d err", c.errors)
		}
		if c.fixed > 0 {
			counts += fmt.Sprintf(" · %d fix", c.fixed)
		}
		if c.skipped > 0 {
			counts += fmt.Sprintf(" · %d skip", c.skipped)
		}
		rows = append(rows, panel(title+"\n"+gauge+"\n"+paint(counts, muted), width, 5, border))
	}
	mode := "following active suite"
	if !m.follow {
		mode = "browsing · f to follow"
	}
	if m.viewing {
		mode = "select category to inspect saved results"
	}
	rows = append(rows, paint(mode, muted))
	return fitRows(strings.Join(rows, "\n"), width, height)
}

func (m model) logWidth() int {
	if m.width >= 96 {
		return m.width - min(48, max(34, m.width*36/100)) - 5
	}
	return max(20, m.width-4)
}
func (m model) logRows(width int) []string {
	var rows []string
	entries := m.logs
	if m.viewing {
		entries = m.reportLogs()
	}
	for _, entry := range entries {
		prefix := paint(entry.at, muted) + " " + bold(fmt.Sprintf("%-6s", strings.ToUpper(entry.level)), statusColor(entry.level)) + " "
		prefixWidth := 16
		wrapped := strings.Split(ansi.Wrap(entry.text, max(1, width-prefixWidth), ""), "\n")
		for i, text := range wrapped {
			if i == 0 {
				rows = append(rows, prefix+paint(text, ink))
			} else {
				rows = append(rows, strings.Repeat(" ", prefixWidth)+paint(text, muted))
			}
		}
	}
	return rows
}
func (m model) activityView(width, height int) string {
	border := line
	if m.focus == 1 {
		border = blue
	}
	mode := "LIVE ↓"
	if m.logOffset > 0 {
		mode = "SCROLLED · f TO FOLLOW"
	}
	label := "ACTIVITY"
	if m.viewing {
		label, mode = "SAVED RESULTS", "↑/↓ SCROLL"
	}
	title := pair(bold(label, ink), paint(mode, blue), width-4)
	rows := m.logRows(width - 4)
	visible := max(1, height-4)
	end := max(0, len(rows)-m.logOffset)
	start := max(0, end-visible)
	body := strings.Join(rows[start:end], "\n")
	if len(rows) == 0 {
		body = paint("Waiting for the first check…", muted)
		if m.viewing {
			body = paint("No checks recorded in this category.", muted)
		}
	}
	return panel(title+"\n\n"+body, width, height, border)
}
