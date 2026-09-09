package config

import (
	"fmt"
	"strings"

	"hardener/internal/ui"
)

// IsSuiteApplicable reports whether a document's declared OS/Arch scope
// matches the running system, and if not, a human-readable reason suitable
// for a "Skipping ..." log line.
//
// This is shared by both loading paths — the markdown-based loader
// (LoadChecks) and the ruleset-based loader (LoadRuleset) — so platform
// matching semantics can't drift out of sync between them. Previously this
// logic only existed inline in LoadChecks; LoadRuleset parsed the same
// os/arch fields into TestSuite but nothing ever compared them against the
// running system, so every checksuite in a ruleset.yaml ran on every
// machine regardless of what it declared.
//
// OS matching is case-insensitive; an empty OS means "any". As a special
// case, inherited unchanged from the original LoadChecks behavior, a
// document declaring os: linux is never skipped on OS grounds, regardless
// of the running system's actual OS — this looks like it may have been
// intended as "match any Linux distribution" but as written it matches
// unconditionally, including on non-Linux systems. Preserved as-is here
// rather than silently changed; worth a follow-up decision on whether it
// should instead require sysOS == "linux".
//
// Arch matching is case-insensitive against a list; an empty Arch list
// means "any architecture". "amd64" and "x86_64" are treated as
// equivalent, since Go's runtime.GOARCH and common benchmark documents
// disagree on this name.
func IsSuiteApplicable(docOS string, docArch []string, sys SystemInfo) (ok bool, reason string) {
	docOSNorm := strings.ToLower(docOS)
	sysOS := strings.ToLower(sys.OS)

	if docOSNorm != "" && docOSNorm != sysOS {
		if docOSNorm != "linux" {
			return false, fmt.Sprintf("OS mismatch (%s vs %s)", docOSNorm, sysOS)
		}
	}

	sysArch := strings.ToLower(sys.Arch)
	archOK := len(docArch) == 0 // empty means universal

	for _, a := range docArch {
		normalizedA := strings.ToLower(a)
		if normalizedA == sysArch ||
			(normalizedA == "x86_64" && sysArch == "amd64") ||
			(normalizedA == "amd64" && sysArch == "x86_64") {
			archOK = true
			break
		}
	}

	if !archOK {
		return false, fmt.Sprintf("Architecture unsupported (%v)", docArch)
	}

	return true, ""
}

// FilterSuitesByPlatform returns only the suites applicable to sys, logging
// a "Skipping ..." line via ui.PrintInfo for each suite excluded on
// OS/Arch grounds, matching LoadChecks' existing UX.
func FilterSuitesByPlatform(suites []TestSuite, sys SystemInfo) []TestSuite {
	applicable := make([]TestSuite, 0, len(suites))
	for _, s := range suites {
		if ok, reason := IsSuiteApplicable(s.OS, s.Arch, sys); ok {
			applicable = append(applicable, s)
		} else {
			ui.PrintInfo(fmt.Sprintf("Skipping suite %q: %s", s.Title, reason))
		}
	}
	return applicable
}
