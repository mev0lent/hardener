package ui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors (true color)
	Green  = lipgloss.Color("#04B575") // Passed / Success
	Red    = lipgloss.Color("#FF4C4C") // Failed / Error
	Blue   = lipgloss.Color("#18ABCC") // Info / Plan
	Amber  = lipgloss.Color("#FBBF24") // Highlight / Warning
	Cream  = lipgloss.Color("#FDF6E3") // Background
	Orange = lipgloss.Color("#FFA500")
)

// ------------------------
// Common box options
// ------------------------
var (
	boxPadding = 1
	boxWidth   = 60
)

var paddedLabelBase = lipgloss.NewStyle().
	Bold(true).
	PaddingLeft(3).
	Width(boxWidth)

// ------------------------
// Header
// ------------------------
var Header = lipgloss.NewStyle().
	Bold(true).
	Foreground(Green).
	PaddingBottom(1).
	Align(lipgloss.Center)

// --- Status labels ---
func Info(msg string) string {
	return paddedLabelBase.Copy().
		Foreground(Cream).
		Render(msg)
}

func FinalInfo(msg string) string {
	// This style doesn't have the 3-space left padding, so it is separate.
	return lipgloss.NewStyle().
		Foreground(Cream).
		Bold(true).
		Render(msg)
}

// --- Status labels ---
func Passed(msg string) string {
	return paddedLabelBase.Copy().
		Foreground(Blue).
		Render(msg)
}

func Failed(msg string) string {
	return paddedLabelBase.Copy().
		Foreground(Orange).
		Render(msg)
}

func Report(msg string) string {
	// This style doesn't have the 3-space left padding.
	return lipgloss.NewStyle().
		Foreground(Cream).
		Bold(true).
		Render(msg)
}

// ------------------------
// Command block
// ------------------------
var CommandBlock = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(Red).
	Foreground(Red).
	Padding(boxPadding, 2).
	Width(boxWidth).
	MarginBottom(2).
	Padding(boxPadding, 2)

// ------------------------
// Summary box
// ------------------------
var SummaryBox = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(Green).
	Foreground(Green).
	Padding(boxPadding, 2).
	MarginLeft(3).
	MarginBottom(2).
	Width(boxWidth)

// ------------------------
// Info box
// ------------------------
var InfoBox = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(Amber).
	Background(Cream).
	Foreground(Blue).
	Padding(boxPadding, 2).
	Width(boxWidth)

// ------------------------
// Log box
// ------------------------
var LogBox = lipgloss.NewStyle().
	Border(lipgloss.ThickBorder()).
	BorderForeground(Green).
	Foreground(Green).
	Padding(boxPadding, 2).
	Width(boxWidth)

// ------------------------
// Error box
// ------------------------
var ErrorBox = lipgloss.NewStyle().
	Foreground(Red).
	Bold(true).
	Padding(boxPadding, 3).
	Width(boxWidth)
