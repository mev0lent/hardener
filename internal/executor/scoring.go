package executor

import (
	"fmt"
	"hardener/internal/scoring"
	"hardener/internal/ui"
)

func MakeScorings(checksPassed, fixesApplied map[string]bool) {
	checkScore := scoring.CalcCheckScore(checksPassed)
	fixScore := scoring.CalcFixScore(fixesApplied)

	ui.PrintFinalInfo(fmt.Sprintf("Check pass rate: %.2f%%", checkScore))
	ui.PrintFinalInfo(fmt.Sprintf("Fix applied rate: %.2f%%", fixScore))
}
