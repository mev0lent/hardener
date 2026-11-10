package config

import (
	"bufio"
	"fmt"
	"hardener/internal/ui"
	"os"
	"regexp"
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
	Labels []string `yaml:"labels"`
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
			Labels: rd.Labels,
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
//
// A "---" line separates documents at any indentation, as it always has here,
// so rulesets with indented separators keep loading. The exception is the
// content of a block scalar (command: |, fix: >-, ...): a fix that writes a
// YAML file from a heredoc can contain a "---" line of its own.
func splitYAMLDocuments(data []byte) []string {
	var docs []string
	var current strings.Builder
	blockIndent := -1 // column of the key whose block scalar is open, or -1

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if blockIndent >= 0 && (trimmed == "" || leadingSpace(line) > blockIndent) {
			current.WriteString(line)
			current.WriteByte('\n')
			continue
		}
		blockIndent = -1
		if isDocumentMarker(trimmed) {
			if s := strings.TrimSpace(current.String()); s != "" {
				docs = append(docs, s)
			}
			current.Reset()
			continue
		}
		current.WriteString(line)
		current.WriteByte('\n')
		if blockScalarStart.MatchString(line) {
			blockIndent = keyColumn(line)
		}
	}
	// flush the last document
	if s := strings.TrimSpace(current.String()); s != "" {
		docs = append(docs, s)
	}
	return docs
}

// blockScalarStart matches a line whose value is a block scalar indicator,
// such as "  command: |", "fix: >-" or "- |2".
var blockScalarStart = regexp.MustCompile(`(?:^|[:-])\s+[|>][-+0-9]*\s*(?:#.*)?$`)

// isDocumentMarker matches a trimmed "---" line, optionally with a comment.
func isDocumentMarker(trimmed string) bool {
	rest, ok := strings.CutPrefix(trimmed, "---")
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest == "" || strings.HasPrefix(rest, "#")
}

func leadingSpace(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// keyColumn is the column of the first key on a line, after any "- " of a
// sequence entry. Block scalar content is indented further than that.
func keyColumn(line string) int {
	i := 0
	for i < len(line) {
		switch {
		case line[i] == ' ' || line[i] == '\t':
			i++
		case line[i] == '-' && i+1 < len(line) && (line[i+1] == ' ' || line[i+1] == '\t'):
			i += 2
		default:
			return i
		}
	}
	return i
}
