package config

import (
	"bufio"
	"fmt"
	"hardener/internal/ui"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// rulesetHeader is the optional first document in a ruleset.yaml.
// It has no title and no checksuites — only guide-level metadata.
type rulesetHeader struct {
	Preconditions preconditions `yaml:"preconditions"`
}

// rulesetDoc mirrors one suite document in a ruleset.yaml file.
// Both "checksuites" and "checks" are accepted as the list key so that
// existing ruleset files that accidentally used "checks" still work.
type rulesetDoc struct {
	Title  string   `yaml:"title"`
	OS     string   `yaml:"os"`
	Arch   []string `yaml:"arch"`
	Suites []Check  `yaml:"checksuites"`
	Checks []Check  `yaml:"checks"` // fallback key used by some entries in the wild
}

// LoadRuleset parses a multi-document YAML file (documents separated by "---")
// and returns one TestSuite per document.  It intentionally skips OS / arch
// filtering so the caller (executeRun) can decide whether to apply it.
//
// The optional first document may be a header-only block (no title, no
// checksuites) that declares guide-level preconditions:
//
//	---
//	preconditions:
//	  tools: [brew, csrutil]
//	---
//	title: Secure Boot
//	checksuites: ...
func LoadRuleset(path string) ([]TestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read ruleset file %q: %w", path, err)
	}

	docs := splitYAMLDocuments(data)

	// Check whether the first non-empty document is a header (no title).
	suiteStartIdx := 0
	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		var probe rulesetDoc
		if err := yaml.Unmarshal([]byte(doc), &probe); err != nil {
			return nil, fmt.Errorf("ruleset document %d: YAML parse error: %w", i+1, err)
		}

		if probe.Title == "" {
			// Title-less first document → treat as guide header.
			var hdr rulesetHeader
			if err := yaml.Unmarshal([]byte(doc), &hdr); err != nil {
				return nil, fmt.Errorf("ruleset header: YAML parse error: %w", err)
			}
			if len(hdr.Preconditions.Tools) > 0 {
				ui.PrintInfo(fmt.Sprintf("Checking guide preconditions: %v", hdr.Preconditions.Tools))
				if !CheckPreconditions(hdr.Preconditions.Tools) {
					return nil, fmt.Errorf("ruleset %q cannot run: required tools are missing", path)
				}
			}
			suiteStartIdx = i + 1
		}
		break
	}

	var suites []TestSuite

	for i, doc := range docs[suiteStartIdx:] {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		var rd rulesetDoc
		if err := yaml.Unmarshal([]byte(doc), &rd); err != nil {
			return nil, fmt.Errorf("ruleset document %d: YAML parse error: %w", suiteStartIdx+i+1, err)
		}

		if rd.Title == "" {
			continue
		}

		// Prefer "checksuites", fall back to "checks"
		checks := rd.Suites
		if len(checks) == 0 {
			checks = rd.Checks
		}

		if len(checks) == 0 {
			continue
		}

		suites = append(suites, TestSuite{
			Title:  rd.Title,
			Labels: []string{strings.ToLower(rd.Title)},
			OS:     rd.OS,
			Arch:   rd.Arch,
			Checks: checks,
		})
	}

	if len(suites) == 0 {
		return nil, fmt.Errorf("no valid suites found in ruleset file %q", path)
	}

	return suites, nil
}

// splitYAMLDocuments splits a multi-document YAML byte slice on "---" lines.
// It preserves each document as a raw string for individual unmarshalling.
func splitYAMLDocuments(data []byte) []string {
	var docs []string
	var current strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			if s := strings.TrimSpace(current.String()); s != "" {
				docs = append(docs, s)
			}
			current.Reset()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
	}
	// flush the last document
	if s := strings.TrimSpace(current.String()); s != "" {
		docs = append(docs, s)
	}
	return docs
}
