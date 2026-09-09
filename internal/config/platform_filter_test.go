package config

import (
	"os"
	"testing"
)

func TestIsSuiteApplicable(t *testing.T) {
	cases := []struct {
		name    string
		docOS   string
		docArch []string
		sys     SystemInfo
		wantOK  bool
	}{
		{"empty os and arch always applies", "", nil, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"exact os match, empty arch", "darwin", nil, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"os mismatch", "darwin", nil, SystemInfo{OS: "linux", Arch: "amd64"}, false},
		{"os match case-insensitive", "Darwin", nil, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"exact arch match", "darwin", []string{"arm64"}, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"arch mismatch", "darwin", []string{"amd64"}, SystemInfo{OS: "darwin", Arch: "arm64"}, false},
		{"arch list, one matches", "darwin", []string{"amd64", "arm64"}, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"arch case-insensitive", "darwin", []string{"ARM64"}, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"amd64 doc matches x86_64 system", "darwin", []string{"amd64"}, SystemInfo{OS: "darwin", Arch: "x86_64"}, true},
		{"x86_64 doc matches amd64 system", "darwin", []string{"x86_64"}, SystemInfo{OS: "darwin", Arch: "amd64"}, true},
		{"linux doc always applies (documented quirk)", "linux", nil, SystemInfo{OS: "darwin", Arch: "arm64"}, true},
		{"linux doc on actual linux system", "linux", nil, SystemInfo{OS: "linux", Arch: "amd64"}, true},
		{
			name: "real-world: Secure Boot (Intel T2) on Apple Silicon must be skipped",
			// This is the exact regression this change exists for: mdmclient's
			// WindowsBootLevel/SecureBootLevel/ExternalBootLevel checks only
			// work on Intel/T2 Macs; on Apple Silicon they report values that
			// never match the expected compliant state, so the check must not
			// run there at all rather than run and always fail.
			docOS: "darwin", docArch: []string{"amd64"}, sys: SystemInfo{OS: "darwin", Arch: "arm64"}, wantOK: false,
		},
		{
			name:  "real-world: Secure Boot (Apple Silicon) on Intel T2 must be skipped",
			docOS: "darwin", docArch: []string{"arm64"}, sys: SystemInfo{OS: "darwin", Arch: "amd64"}, wantOK: false,
		},
		{
			name:  "real-world: Secure Boot (Intel T2) on Intel T2 must apply",
			docOS: "darwin", docArch: []string{"amd64"}, sys: SystemInfo{OS: "darwin", Arch: "amd64"}, wantOK: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := IsSuiteApplicable(tc.docOS, tc.docArch, tc.sys)
			if ok != tc.wantOK {
				t.Fatalf("IsSuiteApplicable(%q, %v, %+v) = (%v, %q), want ok=%v",
					tc.docOS, tc.docArch, tc.sys, ok, reason, tc.wantOK)
			}
			if !ok && reason == "" {
				t.Fatalf("expected a non-empty skip reason when ok=false")
			}
		})
	}
}

func TestFilterSuitesByPlatform(t *testing.T) {
	suites := []TestSuite{
		{Title: "Secure Boot (Intel T2)", OS: "darwin", Arch: []string{"amd64"}},
		{Title: "Secure Boot (Apple Silicon)", OS: "darwin", Arch: []string{"arm64"}},
		{Title: "Gatekeeper", OS: "darwin", Arch: nil}, // universal
	}

	appleSilicon := SystemInfo{OS: "darwin", Arch: "arm64"}
	got := FilterSuitesByPlatform(suites, appleSilicon)

	if len(got) != 2 {
		t.Fatalf("expected 2 applicable suites on Apple Silicon, got %d: %+v", len(got), got)
	}
	titles := map[string]bool{}
	for _, s := range got {
		titles[s.Title] = true
	}
	if !titles["Secure Boot (Apple Silicon)"] || !titles["Gatekeeper"] {
		t.Fatalf("wrong suites returned: %+v", got)
	}
	if titles["Secure Boot (Intel T2)"] {
		t.Fatalf("Intel-only suite should have been filtered out on Apple Silicon: %+v", got)
	}

	intelMac := SystemInfo{OS: "darwin", Arch: "amd64"}
	got = FilterSuitesByPlatform(suites, intelMac)
	if len(got) != 2 {
		t.Fatalf("expected 2 applicable suites on Intel, got %d: %+v", len(got), got)
	}
}

// TestLoadRulesetThenFilter is an end-to-end regression test using an actual
// mixed-architecture ruleset fixture, structured exactly like the real
// Secure Boot split in ernw/hardening's ruleset.yaml, one Intel-only
// document, one Apple-Silicon-only document, one universal document.
func TestLoadRulesetThenFilter(t *testing.T) {
	fixture := `preconditions:
  tools:
  - grep

---

title: Secure Boot (Intel T2)
os: darwin
arch:
- amd64
labels:
- boot
checksuites:
- id: windows-boot-disallowed-check
  description: Intel/T2 only.
  command: /usr/libexec/mdmclient QuerySecurityInfo | grep -c 'WindowsBootLevel = disallowed;'
  expected: 1
  sudo: true
  fix: manual
  fix_sudo: true
  affected_file: N/A
  post_action: none
  security_level: baseline
  risk_level: low
  risk_desc: test fixture

---

title: Secure Boot (Apple Silicon)
os: darwin
arch:
- arm64
labels:
- boot
checksuites:
- id: bputil-smb0-full-security-check
  description: Apple Silicon only.
  command: sudo bputil -d | grep '(smb0)' | grep -c 'Full'
  expected: 1
  sudo: true
  fix: manual
  fix_sudo: true
  affected_file: N/A
  post_action: none
  security_level: high
  risk_level: low
  risk_desc: test fixture

---

title: Gatekeeper
os: darwin
arch:
- arm64
- amd64
labels:
- system
checksuites:
- id: gatekeeper-assessments-enabled
  description: universal.
  command: spctl --status --verbose |grep -c 'assessments enabled'
  expected: 1
  sudo: false
  fix: sudo spctl --master-enable
  fix_sudo: true
  affected_file: N/A
  post_action: none
  security_level: baseline
  risk_level: very low
  risk_desc: test fixture
`

	dir := t.TempDir()
	path := dir + "/ruleset.yaml"
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatalf("failed to write fixture: %v", err)
	}

	suites, err := LoadRuleset(path)
	if err != nil {
		t.Fatalf("LoadRuleset: %v", err)
	}
	if len(suites) != 3 {
		t.Fatalf("expected 3 suites loaded from fixture, got %d", len(suites))
	}

	// This is the actual regression: on Apple Silicon, only 2 of the 3
	// loaded suites should survive filtering.
	appleSilicon := SystemInfo{OS: "darwin", Arch: "arm64"}
	applicable := FilterSuitesByPlatform(suites, appleSilicon)
	if len(applicable) != 2 {
		t.Fatalf("expected 2 applicable suites on Apple Silicon, got %d: %+v", len(applicable), applicable)
	}
	for _, s := range applicable {
		if s.Title == "Secure Boot (Intel T2)" {
			t.Fatalf("Intel-only suite leaked through on Apple Silicon")
		}
	}
}
