package config

import (
	"time"
)

type Check struct {
	ID            string   `yaml:"id"`
	Description   string   `yaml:"description"`
	Command       string   `yaml:"command"`
	Expected      string   `yaml:"expected"`
	Sudo          bool     `yaml:"sudo"`
	Fix           string   `yaml:"fix,omitempty"`
	FixSudo       *bool    `yaml:"fix_sudo,omitempty"`
	OS            []string `yaml:"os,omitempty"`
	Arch          []string `yaml:"arch,omitempty"`
	AffectedFile  string   `yaml:"affected_file,omitempty"`
	PostAction    string   `yaml:"post_action,omitempty"`
	SecurityLevel string   `yaml:"security_level,omitempty"`
	RiskClass     string   `yaml:"risk_class,omitempty"`
	RiskLevel     string   `yaml:"risk_level,omitempty"`
	RiskDesc      string   `yaml:"risk_desc,omitempty"`
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
	ID          string `json:"id"`
	Description string `json:"description"`
	Passed      bool   `json:"passed"`
	FixApplied  bool   `json:"fix_applied"`
	Output      string `json:"output"`
	Skipped     bool   `json:"skipped"`
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
