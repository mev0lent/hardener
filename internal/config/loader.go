package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hardener/internal/ui"

	"github.com/adrg/frontmatter"
)

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// LoadChecks scans a directory for Markdown files, parses their frontmatter,
// and assembles a list of TestSuites compatible with the host system.
func LoadChecks(dir string, sys SystemInfo) ([]TestSuite, error) {
	var suites []TestSuite
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		// Filter for Markdown files, ignoring directories and hidden files
		if f.IsDir() || strings.HasPrefix(f.Name(), ".") || filepath.Ext(f.Name()) != ".md" {
			continue
		}

		ui.PrintInfo(fmt.Sprintf("Processing file: %s", f.Name()))

		fullFilePath := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(fullFilePath)
		if err != nil {
			ui.Error(fmt.Errorf("failed to read %s: %w", f.Name(), err))
			continue
		}

		// Structure for unmarshaling the Markdown YAML header
		type FileFrontMatter struct {
			Title  string   `yaml:"title"`
			OS     string   `yaml:"os"`
			Arch   []string `yaml:"arch"`
			Checks []Check  `yaml:"checksuites"`
		}

		var fm FileFrontMatter
		if _, err := frontmatter.Parse(bytes.NewReader(data), &fm); err != nil {
			ui.Error(fmt.Errorf("metadata parsing error in %s: %w", f.Name(), err))
			continue
		}

		// --- Platform Validation Logic ---

		// 1. OS normalization and case-insensitive matching
		// "linux" acts as a wildcard for specific distributions
		docOS := strings.ToLower(fm.OS)
		sysOS := strings.ToLower(sys.OS)

		if docOS != "" && docOS != sysOS {
			if docOS != "linux" {
				ui.PrintInfo(fmt.Sprintf("Skipping %s: OS mismatch (%s vs %s)", f.Name(), docOS, sysOS))
				continue
			}
		}

		// 2. Architecture normalization
		// Reconciles naming differences between Go (amd64) and standard benchmarks (x86_64)
		sysArch := strings.ToLower(sys.Arch)
		isArchSupported := len(fm.Arch) == 0 // Default to universal if empty

		for _, a := range fm.Arch {
			normalizedA := strings.ToLower(a)
			if normalizedA == sysArch ||
				(normalizedA == "x86_64" && sysArch == "amd64") ||
				(normalizedA == "amd64" && sysArch == "x86_64") {
				isArchSupported = true
				break
			}
		}

		if !isArchSupported {
			ui.PrintInfo(fmt.Sprintf("Skipping %s: Architecture unsupported (%v)", f.Name(), fm.Arch))
			continue
		}

		// --- Suite Assembly ---

		if len(fm.Checks) > 0 {
			ui.PrintInfo(fmt.Sprintf("Adding Suite: %s (%d checks)", fm.Title, len(fm.Checks)))

			suites = append(suites, TestSuite{
				Title:  fm.Title,
				Labels: []string{strings.ToLower(fm.Title)},
				OS:     fm.OS,
				Arch:   fm.Arch,
				Checks: fm.Checks, // Checks inherit the document-level architecture validity
			})
		}
	}

	return suites, nil
}