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
	switch requestedLevel {
	case "high":
		return checkLevel == "high" || checkLevel == "medium" || checkLevel == "baseline"
	case "medium":
		return checkLevel == "medium" || checkLevel == "baseline"
	default: // "baseline" and any unknown level
		return checkLevel == "baseline"
	}
}