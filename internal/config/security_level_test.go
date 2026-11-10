package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDetermineSecurityFit(t *testing.T) {
	for _, tc := range []struct {
		requested, check string
		want             bool
	}{
		{"baseline", "baseline", true},
		{"baseline", "medium", false},
		{"baseline", "high", false},
		{"baseline", "optional", false},
		{"medium", "medium", true},
		{"medium", "high", false},
		{"high", "baseline", true},
		{"high", "high", true},
		// Optional controls may cost functionality, so high does not include
		// them; they run only when asked for.
		{"high", "optional", false},
		{"optional", "optional", true},
		{"optional", "high", true},
		{"optional", "baseline", true},
		{"High", "HIGH ", true},
		{"typo", "baseline", true}, // an unknown request behaves as baseline
		{"typo", "high", false},
		{"optional", "", false}, // a missing level never runs
		{"optional", "critical", false},
	} {
		if got := DetermineSecurityFit(tc.requested, tc.check); got != tc.want {
			t.Errorf("DetermineSecurityFit(%q, %q) = %v, want %v", tc.requested, tc.check, got, tc.want)
		}
	}
}

func TestUnknownSecurityLevels(t *testing.T) {
	suites := []TestSuite{{Checks: []Check{
		{ID: "ok", SecurityLevel: "baseline"},
		{ID: "opt", SecurityLevel: "optional"},
		{ID: "case", SecurityLevel: "High"},
		{ID: "typo", SecurityLevel: "hihg"},
		{ID: "missing"},
	}}}
	got := strings.Join(UnknownSecurityLevels(suites), ", ")
	if got != `typo ("hihg"), missing ("")` {
		t.Fatalf("UnknownSecurityLevels = %s", got)
	}
}

func TestIsCheckApplicable(t *testing.T) {
	intel := SystemInfo{OS: "darwin", Arch: "amd64"}
	for _, tc := range []struct {
		name  string
		check Check
		want  bool
	}{
		{"no scope", Check{}, true},
		{"arch matches", Check{Arch: []string{"amd64", "arm64"}}, true},
		{"x86_64 alias", Check{Arch: []string{"x86_64"}}, true},
		{"arch mismatch", Check{Arch: []string{"arm64"}}, false},
		{"os matches", Check{OS: []string{"Darwin"}}, true},
		{"os mismatch", Check{OS: []string{"linux"}}, false},
	} {
		if ok, reason := IsCheckApplicable(tc.check, intel); ok != tc.want {
			t.Errorf("%s: got %v (%s), want %v", tc.name, ok, reason, tc.want)
		}
	}
}

// A "---" inside a block scalar is content, e.g. in a fix that writes a YAML
// file. Elsewhere "---" still separates documents at any indentation, as it
// always has, so rulesets with indented separators keep loading.
func TestSplitYAMLDocuments(t *testing.T) {
	data := "title: A\nchecksuites:\n- id: a\n  fix: |\n    cat > /etc/x.yaml <<EOF\n    ---\n    key: v\n    EOF\n  expected: 1\n--- # next suite\ntitle: B\nchecksuites:\n- command: >-\n    printf x\n  ---\ntitle: C\n---\r\ntitle: D\n"
	docs := splitYAMLDocuments([]byte(data))
	if len(docs) != 4 || !strings.Contains(docs[0], "    ---\n    key: v") || !strings.HasPrefix(docs[2], "title: C") {
		t.Fatalf("got %d documents: %q", len(docs), docs)
	}
	for _, doc := range docs {
		var probe rulesetDoc
		if err := yaml.Unmarshal([]byte(doc), &probe); err != nil {
			t.Fatalf("document does not parse: %v\n%s", err, doc)
		}
	}
}
