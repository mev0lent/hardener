package scoring

func CalcCheckScore(checksPassed map[string]bool) float32 {
	total := len(checksPassed)
	if total == 0 {
		return 0
	}
	counter := 0
	for _, passed := range checksPassed {
		if passed {
			counter++
		}
	}
	return float32(counter) / float32(total) * 100
}

func CalcFixScore(fixesApplied map[string]bool) float32 {
	total := len(fixesApplied)
	if total == 0 {
		return 0
	}
	counter := 0
	for _, applied := range fixesApplied {
		if applied {
			counter++
		}
	}
	return float32(counter) / float32(total) * 100
}
