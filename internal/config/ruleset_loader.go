package config

import (
	"bufio"
	"fmt"
	"hardener/internal/ui"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// rulesetDoc mirrors one YAML document in a ruleset.yaml file.
// Both "checksuites" and "checks" are accepted as the list key so that
// existing ruleset files that accidentally used "checks" still work.
type rulesetDoc struct {
	Title         string        `yaml:"title"`
	OS            string        `yaml:"os"`
	Arch          []string      `yaml:"arch"`
	Preconditions preconditions `yaml:"preconditions"`
	Suites        []Check       `yaml:"checksuites"`
	Checks        []Check       `yaml:"checks"` // fallback key used by some entries in the wild
}

// LoadRuleset parses a multi-document YAML file (documents separated by "---")
// and returns one TestSuite per document.  It intentionally skips OS / arch
// filtering so the caller (executeRun) can decide whether to apply it.
func LoadRuleset(path string) ([]TestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read ruleset file %q: %w", path, err)
	}

	docs := splitYAMLDocuments(data)
	var suites []TestSuite

	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" || doc == "---" {
			continue
		}

		var rd rulesetDoc
		if err := yaml.Unmarshal([]byte(doc), &rd); err != nil {
			return nil, fmt.Errorf("ruleset document %d: YAML parse error: %w", i+1, err)
		}

		if rd.Title == "" {
			// skip blank / comment-only documents
			continue
		}

		if len(rd.Preconditions.Tools) > 0 {
			if !CheckPreconditions(rd.Preconditions.Tools) {
				ui.PrintInfo(fmt.Sprintf("Skipping suite %q: preconditions not met", rd.Title))
				continue
			}
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
