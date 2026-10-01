package config

import (
	"time"
)

// DistroOverride holds the fields that differ on a specific Linux distribution.
// Only non-zero fields override the top-level Check values.
type DistroOverride struct {
	Command  string `yaml:"command,omitempty"`
	Expected string `yaml:"expected,omitempty"`
	Fix      string `yaml:"fix,omitempty"`
	Sudo     *bool  `yaml:"sudo,omitempty"`
	FixSudo  *bool  `yaml:"fix_sudo,omitempty"`
}

type Check struct {
	ID              string                    `yaml:"id"`
	Description     string                    `yaml:"description"`
	Command         string                    `yaml:"command,omitempty"`
	Expected        string                    `yaml:"expected"`
	ExpectedOp      string                    `yaml:"expected_op,omitempty"`
	Sudo            bool                      `yaml:"sudo"`
	Fix             string                    `yaml:"fix,omitempty"`
	FixSudo         *bool                     `yaml:"fix_sudo,omitempty"`
	OS              []string                  `yaml:"os,omitempty"`
	Arch            []string                  `yaml:"arch,omitempty"`
	Distro          map[string]DistroOverride `yaml:"distro,omitempty"`
	RequiresCommand string                    `yaml:"requires_command,omitempty"`
	// RequiresFile skips the check with a missing-command state when the
	// given path does not exist. Useful for checks that inspect files that
	// are absent on some distros (e.g. /etc/hosts.allow, /etc/login.defs).
	RequiresFile  string `yaml:"requires_file,omitempty"`
	AffectedFile  string `yaml:"affected_file,omitempty"`
	PostAction    string `yaml:"post_action,omitempty"`
	SecurityLevel string `yaml:"security_level,omitempty"`
	RiskClass     string `yaml:"risk_class,omitempty"`
	RiskLevel     string `yaml:"risk_level,omitempty"`
	RiskDesc      string `yaml:"risk_desc,omitempty"`
}

// ResolveForDistro returns a copy of the check with distro-specific overrides applied.
//
// Lookup order (most specific → least specific):
//  1. "<distro>-<profile>" (e.g. "debian-server")
//  2. "<family>-<profile>" for each family in the chain
//  3. "<distro>"
//  4. "<family>" for each family in the chain (e.g. "debian" for Ubuntu,
//     "rhel" for Rocky, "suse" for openSUSE, "arch" for Manjaro)
//
// The distroChain must start with the concrete distro ID; subsequent entries
// are broader families supplied by DetectDistroFamily(). Returns false when
// the check has a distro map but no key in the chain matches, so the caller
// can skip it with a distro_skipped state.
func (c Check) ResolveForDistro(distroChain []string, profile string) (Check, bool) {
	if len(c.Distro) == 0 {
		return c, true // universal check — no distro map
	}
	if len(distroChain) == 0 {
		return c, false
	}
	if profile != "" {
		for _, d := range distroChain {
			if ov, ok := c.Distro[d+"-"+profile]; ok {
				return applyDistroOverride(c, ov), true
			}
		}
	}
	for _, d := range distroChain {
		if ov, ok := c.Distro[d]; ok {
			return applyDistroOverride(c, ov), true
		}
	}
	return c, false
}

func applyDistroOverride(c Check, ov DistroOverride) Check {
	if ov.Command != "" {
		c.Command = ov.Command
	}
	if ov.Expected != "" {
		c.Expected = ov.Expected
	}
	if ov.Fix != "" {
		c.Fix = ov.Fix
	}
	if ov.Sudo != nil {
		c.Sudo = *ov.Sudo
	}
	if ov.FixSudo != nil {
		c.FixSudo = ov.FixSudo
	}
	return c
}

type TestSuite struct {
	Title  string   `yaml:"title"`
	Labels []string `yaml:"labels"`
	OS     string   `yaml:"os,omitempty"`
	Arch   []string `yaml:"arch,omitempty"`
	Checks []Check  `yaml:"checksuites"`
}

// Individual check result
type CheckResult struct {
	// Structured status for reports and dashboards; never infer errors from output text.
	SkipReason  string `json:"skip_reason,omitempty"`
	Error       string `json:"error,omitempty"`
	Manual      bool   `json:"manual,omitempty"`
	ID          string `json:"id"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	// FixApplied means the fix command exited 0. FixVerified means the check
	// was re-run afterwards and passed; only that counts as fixed.
	FixApplied     bool   `json:"fix_applied"`
	FixVerified    bool   `json:"fix_verified,omitempty"`
	Output         string `json:"output"`
	Skipped        bool   `json:"skipped"`
	SkippedDistro  bool   `json:"skipped_distro"`
	SkippedMissing bool   `json:"skipped_missing"`
}

// Suite result
type SuiteResult struct {
	Title  string        `json:"title"`
	Checks []CheckResult `json:"checks"`
}

// Full audit report
type AuditReport struct {
	Incomplete   bool          `json:"incomplete,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
	OS           string        `json:"os"`
	Arch         string        `json:"arch"`
	Distro       string        `json:"distro"`
	SuiteResults []SuiteResult `json:"suite_results"`
	ReportType   string        `json:"report_type"`
}

type SystemInfo struct {
	OS     string
	Arch   string
	Distro string
}

// === ROLLBACK TYPES ===

type DeltaEntry struct {
	RunID     string `json:"run_id"`
	Timestamp string `json:"timestamp"`
	FilePath  string `json:"file_path"`
	Checksum  string `json:"checksum"`
	Delta     string `json:"delta"`
	Perm      uint32 `json:"perm"`

	// The fields below were added after v1.2; entries without them are
	// complete records of a file that existed before the fix.

	// ID links a write-ahead record to its completion.
	ID      string `json:"id,omitempty"`
	CheckID string `json:"check_id,omitempty"`
	// Kind is "" for a file and EntryKindSysctl for a live kernel value.
	Kind string `json:"kind,omitempty"`
	// Pending marks a record written before the fix ran that was never
	// completed, e.g. because the process ended while the fix was running.
	Pending bool `json:"pending,omitempty"`
	// Created means the file did not exist before the fix; Removed means
	// the fix deleted a file that existed.
	Created bool `json:"created,omitempty"`
	Removed bool `json:"removed,omitempty"`
	// PostChecksum is the SHA-256 of what the fix left behind. Rollback
	// refuses to touch a file whose content no longer matches it.
	PostChecksum string `json:"post_checksum,omitempty"`
	// Before is the base64 pre-fix content: the whole file while a record is
	// pending, or the previous value of a sysctl entry.
	Before string `json:"before,omitempty"`
}

const EntryKindSysctl = "sysctl"
