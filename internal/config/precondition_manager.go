package config

import (
	"fmt"
	"hardener/internal/ui"
	"os"
	"os/exec"
	"slices"
	"strings"
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

// SecurityLevels lists the security levels from least to most demanding.
// Hardening guides mark some controls "optional": recommended, but they may
// cost functionality the system needs. Those run only when requested
// explicitly with --security-level optional, which selects every check.
var SecurityLevels = []string{"baseline", "medium", "high", "optional"}

// NormalizeSecurityLevel lowercases and trims a level so that "High" in a
// ruleset or on the command line means the same as "high".
func NormalizeSecurityLevel(level string) string {
	return strings.ToLower(strings.TrimSpace(level))
}

func securityRank(level string) int {
	return slices.Index(SecurityLevels, NormalizeSecurityLevel(level))
}

// IsSecurityLevel reports whether level is one of SecurityLevels.
func IsSecurityLevel(level string) bool {
	return securityRank(level) >= 0
}

// DetermineSecurityFit reports whether a check at checkLevel runs when
// requestedLevel is selected. An unknown requested level behaves as baseline;
// a check with an unknown or missing level never runs (see
// UnknownSecurityLevels).
func DetermineSecurityFit(requestedLevel, checkLevel string) bool {
	requested := max(securityRank(requestedLevel), 0)
	check := securityRank(checkLevel)
	return check >= 0 && check <= requested
}

// UnknownSecurityLevels describes every check whose security_level is not one
// of SecurityLevels. Such a check is skipped at every level, so a typo would
// otherwise hide it for good.
func UnknownSecurityLevels(suites []TestSuite) []string {
	var unknown []string
	for _, suite := range suites {
		for _, check := range suite.Checks {
			if !IsSecurityLevel(check.SecurityLevel) {
				unknown = append(unknown, fmt.Sprintf("%s (%q)", check.ID, check.SecurityLevel))
			}
		}
	}
	return unknown
}
