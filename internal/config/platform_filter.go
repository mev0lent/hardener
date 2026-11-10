package config

import (
	"fmt"
	"slices"
	"strings"

	"hardener/internal/ui"
)

// IsSuiteApplicable reports whether a document's declared OS/Arch scope
// matches the running system, and if not, a human-readable reason suitable
// for a "Skipping ..." log line.
//
// This is shared by both loading paths — the markdown-based loader
// (LoadChecks) and the ruleset-based loader (LoadRuleset) — so platform
// matching semantics can't drift out of sync between them.
//
// OS matching is case-insensitive; an empty OS means "any". The original
// LoadChecks let "linux" match any system, as a wildcard for distro names
// back when the system OS could be one. The system OS is runtime.GOOS now,
// so that exception only made Linux suites, and their fixes, run on macOS.
//
// Arch matching is case-insensitive against a list; an empty Arch list
// means "any architecture". "amd64"/"x86_64" and "arm64"/"aarch64" are
// treated as equivalent, since Go's runtime.GOARCH and uname disagree on
// these names.
func IsSuiteApplicable(docOS string, docArch []string, sys SystemInfo) (ok bool, reason string) {
	docOSNorm := strings.ToLower(docOS)
	sysOS := strings.ToLower(sys.OS)

	if docOSNorm != "" && docOSNorm != sysOS {
		return false, fmt.Sprintf("OS mismatch (%s vs %s)", docOSNorm, sysOS)
	}

	if !ArchMatches(docArch, sys.Arch) {
		return false, fmt.Sprintf("Architecture unsupported (%v)", docArch)
	}

	return true, ""
}

// ArchMatches reports whether sysArch is in docArch, ignoring case and
// treating the uname and Go names of an architecture as the same (x86_64 is
// amd64, aarch64 is arm64). An empty list matches any arch.
func ArchMatches(docArch []string, sysArch string) bool {
	if len(docArch) == 0 {
		return true
	}
	sysArch = canonicalArch(sysArch)
	for _, a := range docArch {
		if canonicalArch(a) == sysArch {
			return true
		}
	}
	return false
}

func canonicalArch(arch string) string {
	switch arch = strings.ToLower(strings.TrimSpace(arch)); arch {
	case "x86_64":
		return "amd64"
	case "aarch64":
		return "arm64"
	}
	return arch
}

// IsCheckApplicable applies a check's own os/arch lists, which narrow the
// scope of its suite. An empty list means any.
func IsCheckApplicable(check Check, sys SystemInfo) (ok bool, reason string) {
	matchesOS := func(os string) bool { return strings.EqualFold(os, sys.OS) }
	if len(check.OS) > 0 && !slices.ContainsFunc(check.OS, matchesOS) {
		return false, fmt.Sprintf("check applies to OS %v, not %s", check.OS, sys.OS)
	}
	if !ArchMatches(check.Arch, sys.Arch) {
		return false, fmt.Sprintf("check applies to arch %v, not %s", check.Arch, sys.Arch)
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
