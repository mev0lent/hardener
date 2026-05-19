package config

import (
	"hardener/internal/ui"
	"os"
	"os/exec"
)

type preconditions struct {
	Tools []string `yaml:"tools"`
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func FileExists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

func CheckPreconditions(tools []string) bool {
	conditionsMet := true
	for _, tool := range tools {
		if !commandExists(tool) {
			ui.PrintErrorMessage("Missing required tool: " + tool)
			conditionsMet = false
		}
	}
	return conditionsMet
}

func DetermineSecurityFit(requestedLevel, checkLevel string) bool {
    // If requested is "high", we want both "high" AND "baseline"
    if requestedLevel == "high" {
        return checkLevel == "high" || checkLevel == "baseline"
    }
    // If requested is "baseline", we ONLY want "baseline"
    return checkLevel == "baseline"
}