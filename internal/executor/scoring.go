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
	// fixesApplied is empty for an audit, and for a fix run with nothing to
	// fix; a 0% fix rate would be meaningless there.
	if len(fixesApplied) > 0 {
		ui.PrintFinalInfo(fmt.Sprintf("Fix success rate (verified by re-check): %.2f%%", fixScore))
	}
}
