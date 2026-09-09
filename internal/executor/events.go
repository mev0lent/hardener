package executor

import "hardener/internal/config"

// RunEvent carries execution state independently of terminal rendering.
// Indices, rather than titles/IDs, keep duplicate ruleset labels distinct.
type RunEvent struct {
	Kind       EventKind
	SuiteIndex int
	CheckIndex int
	Suite      config.TestSuite
	Check      config.Check
	Result     config.CheckResult
}

type EventKind uint8

const (
	SuiteStarted EventKind = iota
	CheckStarted
	CheckFinished
	SuiteFinished
)

type Observer func(RunEvent)

func notify(observer Observer, event RunEvent) {
	if observer != nil {
		observer(event)
	}
}

func stopRequested(stop func() bool) bool {
	return stop != nil && stop()
}

// FilterSuites applies the same label matching used by execution. The dashboard
// calls it before rendering so hidden suites cannot inflate its progress total.
func FilterSuites(suites []config.TestSuite, labels []string) []config.TestSuite {
	var selected []config.TestSuite
	for _, suite := range suites {
		if suiteMatchesLabels(suite, labels) {
			selected = append(selected, suite)
		}
	}
	return selected
}
