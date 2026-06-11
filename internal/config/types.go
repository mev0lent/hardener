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
	AffectedFile    string                    `yaml:"affected_file,omitempty"`
	PostAction      string                    `yaml:"post_action,omitempty"`
	SecurityLevel   string                    `yaml:"security_level,omitempty"`
	RiskClass       string                    `yaml:"risk_class,omitempty"`
	RiskLevel       string                    `yaml:"risk_level,omitempty"`
	RiskDesc        string                    `yaml:"risk_desc,omitempty"`
}

// ResolveForDistro returns a copy of the check with distro-specific overrides applied.
// It first tries the combined "distro-profile" key (e.g. "debian-server"), then falls
// back to the distro-only key. Returns false when the check has a distro map but neither
// key matches — the caller should skip the check in that case.
func (c Check) ResolveForDistro(distro, profile string) (Check, bool) {
	if len(c.Distro) == 0 {
		return c, true // universal check — no distro map
	}
	if profile != "" {
		if ov, ok := c.Distro[distro+"-"+profile]; ok {
			return applyDistroOverride(c, ov), true
		}
	}
	if ov, ok := c.Distro[distro]; ok {
		return applyDistroOverride(c, ov), true
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
	ID             string `json:"id"`
	Description    string `json:"description"`
	Passed         bool   `json:"passed"`
	FixApplied     bool   `json:"fix_applied"`
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
}
