package cmd

import (
	"bytes"
	"fmt"
	"hardener/internal/config"
	"hardener/internal/executor"
	"hardener/internal/ui"
	"hardener/rollback"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"github.com/adrg/frontmatter"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func auditRun(cmd *cobra.Command, args []string) error {
	return executeRun(
		cmd,
		executor.ModeAudit,
		"AUDIT",
		"In this mode, your system will be audited with a series of tests concerning security hardening.",
	)
}

func fixRun(cmd *cobra.Command, args []string) error {
	return executeRun(
		cmd,
		executor.ModeFix,
		"FIX",
		"In this mode, according to your audit results, automated fixes will be applied.",
	)
}

func rollbackRun(cmd *cobra.Command, args []string) error {
	ui.PrintWelcome("ROLLBACK", "In this mode, your system will be rolled back to an older version, either partially or complete.")

	timestamp, _ := cmd.Flags().GetString("timestamp")
	latest, _ := cmd.Flags().GetBool("latest")
	files, _ := cmd.Flags().GetStringSlice("files")

	ex, _ := os.Executable()
	currentPath := filepath.Dir(ex)
	ctx := config.NewExecContext(uuid.New().String(), currentPath)

	var target string
	if timestamp != "" {
		target = timestamp
	} else {
		target = "latest"
	}

	if latest && timestamp != "" {
		return ui.Error(fmt.Errorf("--latest and --timestamp cannot be used together"))
	}

	if len(files) > 0 {
		ui.PrintInfo(fmt.Sprintf("Rolling back files from %s | Files: %s", target, files))
	} else {
		ui.PrintInfo(fmt.Sprintf("Rolling back entire system to '%s'", target))
	}
	ctx.Timestamp = timestamp

	err := rollback.ApplyRun(ctx, files)
	if err != nil {
		return err
	}

	return nil
}

func runHardwareInfo() {
	InfoCmd := exec.Command("system_profiler", "SPHardwareDataType")

	var out bytes.Buffer
	InfoCmd.Stdout = &out
	InfoCmd.Stderr = &out

	err := InfoCmd.Run()
	if err != nil {
		fmt.Println("Error running command:", err)
		return
	}

	output := strings.TrimSpace(out.String())
	ui.PrintInfo(output)
}

func executeRun(cmd *cobra.Command, mode executor.RunMode, title string, description string) error {
	ui.PrintWelcome(title, description)

	// Detect which input mode the user requested.
	rulesetPath, _ := cmd.Flags().GetString("ruleset")
	mdPath, _ := cmd.Flags().GetString("path")

	// Validate: exactly one of --ruleset or --path must be provided.
	if rulesetPath != "" && mdPath != "" {
		return ui.ReturnError("Invalid flags", fmt.Errorf("--ruleset and --path are mutually exclusive; please provide only one"))
	}
	if rulesetPath == "" && mdPath == "" {
		return ui.ReturnError("Missing input", fmt.Errorf("you must provide either --path <guide-dir> or --ruleset <ruleset.yaml>"))
	}

	// Build a shared execution context.
	ex, _ := os.Executable()
	currentPath := filepath.Dir(ex)
	ctx := config.NewExecContext(uuid.New().String(), currentPath)
	ctx.RunID = time.Now().UTC().Format(time.RFC3339)
	ctx.OSName = runtime.GOOS
	ctx.ArchName = runtime.GOARCH

	level, _ := cmd.Flags().GetString("security-level")
	ctx.SecurityLevel = level

	profile, _ := cmd.Flags().GetString("profile")
	ctx.Profile = profile

	labels, _ := cmd.Flags().GetStringSlice("label")
	ctx.Labels = labels

	ui.PrintInfo(fmt.Sprintf("System: %s | Arch: %s", ctx.OSName, ctx.ArchName))
	if ctx.OSName == "darwin" {
		runHardwareInfo()
	}

	var (
		suites []config.TestSuite
		sys    config.SystemInfo
		err    error
	)

	if rulesetPath != "" {
		// ── Ruleset mode ──────────────────────────────────────────────────────
		suites, sys, err = setupAndValidateRuleset(ctx, rulesetPath)
		if err != nil {
			return ui.ReturnError("Ruleset loading failed", err)
		}
	} else {
		// ── Markdown / guide-directory mode ───────────────────────────────────
		ctx, sys, mdPath, err = setupAndValidate(cmd, title, description)
		if err != nil {
			return ui.ReturnError("System validation failed", err)
		}
		// Re-stamp RunID / OS info that setupAndValidate doesn't set.
		ctx.RunID = time.Now().UTC().Format(time.RFC3339)
		ctx.OSName = runtime.GOOS
		ctx.ArchName = runtime.GOARCH
		ctx.SecurityLevel = level

		ui.PrintInfo(fmt.Sprintf("Starting to collect Suites from %s", mdPath))
		suites, err = config.LoadChecks(mdPath, sys)
		if err != nil {
			return ui.ReturnError("Failed to load checks", err)
		}
	}

	if len(suites) == 0 {
		ui.PrintInfo("No compatible hardening checks found for your system.")
		return nil
	}

	// Distro detection happens after both loading paths so ctx is always set.
	ctx.DistroName = config.DetectDistro()
	ctx.DistroFamily = config.DetectDistroFamily()
	distroInfo := fmt.Sprintf("Distro: %s", ctx.DistroName)
	if len(ctx.DistroFamily) > 1 {
		distroInfo += fmt.Sprintf(" (family: %s)", strings.Join(ctx.DistroFamily[1:], ","))
	}
	if ctx.Profile != "" {
		distroInfo += fmt.Sprintf(" | Profile: %s", ctx.Profile)
	}
	ui.PrintInfo(distroInfo)
	if len(ctx.Labels) > 0 {
		ui.PrintInfo(fmt.Sprintf("Label filter: %s", strings.Join(ctx.Labels, ", ")))
	}

	// ── Suite selection prompt ─────────────────────────────────────────────
	runAll, _ := cmd.Flags().GetBool("all")
	selectedSuites, err := promptForSuites(suites, runAll)
	if err != nil {
		ui.PrintFailedAsBox(fmt.Sprintf("Suite selection cancelled: %v", err))
		return nil
	}
	if len(selectedSuites) == 0 {
		ui.PrintInfo("No suites selected for execution. Exiting.")
		return nil
	}

	// ── Execution & reporting ──────────────────────────────────────────────
	suiteResults, checksPassed, fixesApplied, _ := executor.RunSuites(
		ctx, mode, sys.OS, sys.Arch, selectedSuites,
	)

	reportType := "audit"
	if mode == executor.ModeFix {
		reportType = "fix"
	}
	executor.MakeReport(sys, suiteResults, reportType, currentPath)
	executor.MakeScorings(checksPassed, fixesApplied)

	return nil
}

// setupAndValidateRuleset loads suites from a ruleset.yaml and derives a
// SystemInfo from the current runtime, no 00_README.md is required.
func setupAndValidateRuleset(ctx *config.ExecContext, rulesetPath string) ([]config.TestSuite, config.SystemInfo, error) {
	sys := config.SystemInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Distro: config.DetectDistro(),
	}

	ui.PrintInfo(fmt.Sprintf("Loading ruleset from %s", rulesetPath))

	suites, err := config.LoadRuleset(rulesetPath)
	if err != nil {
		return nil, sys, err
	}

	ui.PrintInfo(fmt.Sprintf("Loaded %d suite(s) from ruleset", len(suites)))
	return suites, sys, nil
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

func compareTarget(path string, sys config.SystemInfo) (bool, config.SystemInfo) {
	readmePath := filepath.Join(path, "00_README.md")

	if !config.FileExists(readmePath) {
		ui.PrintErrorMessage(fmt.Sprintf("Required file '00_README.md' not found in path: %s", path))
		return false, config.SystemInfo{}
	}

	files, err := os.ReadDir(path)
	if err != nil {
		ui.PrintErrorMessage(err.Error())
		return false, config.SystemInfo{}
	}
	type Preconditions struct {
		Tools []string `yaml:"tools"`
	}
	type ReportInfo struct {
		OS            string        `yaml:"os"`
		Arch          []string      `yaml:"arch"`
		Preconditions Preconditions `yaml:"preconditions"`
	}
	for _, file := range files {
		fileName := file.Name()
		if fileName == "00_README.md" {
			fullFilePath := filepath.Join(path, fileName)

			data, err := os.ReadFile(fullFilePath)
			if err != nil {
				ui.PrintErrorMessage(fmt.Sprintf("could not read file %s: %v", fileName, err))
				return false, config.SystemInfo{}
			}

			var frontMatter ReportInfo
			reader := bytes.NewReader(data)
			if _, err := frontmatter.Parse(reader, &frontMatter); err != nil {
				ui.PrintErrorMessage(fmt.Sprintf("could not parse YAML from %s: %v", fileName, err))
				return false, config.SystemInfo{}
			}
			docOS := strings.ToLower(frontMatter.OS)
			sysOS := strings.ToLower(sys.OS)

			if docOS != sysOS {
				ui.PrintErrorMessage(fmt.Sprintf("OS mismatch for %s: Document requires '%s', System is '%s'", fileName, docOS, sysOS))
				return false, config.SystemInfo{}
			}

			systemArch := strings.ToLower(sys.Arch)
			isArchSupported := contains(frontMatter.Arch, systemArch)

			if !isArchSupported {
				ui.PrintErrorMessage(fmt.Sprintf("Architecture mismatch for %s: Document supports %v, System is %s", fileName, frontMatter.Arch, sys.Arch))
				return false, config.SystemInfo{}
			}

			if !config.CheckPreconditions(frontMatter.Preconditions.Tools) {
				return false, config.SystemInfo{}
			}

			ui.PrintPassed("Target matched: OS, Architecture, and Preconditions supported by 00_README.md")
			return true, config.SystemInfo{OS: docOS, Arch: systemArch}
		}
	}
	return false, config.SystemInfo{}
}

// setupAndValidate handles the initial system info, context creation, and validation
// for the classic markdown / guide-directory mode.
func setupAndValidate(cmd *cobra.Command, title string, description string) (*config.ExecContext, config.SystemInfo, string, error) {
	sys := config.SystemInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Distro: config.DetectDistro(),
	}

	ex, _ := os.Executable()
	currentPath := filepath.Dir(ex)
	ctx := config.NewExecContext(uuid.New().String(), currentPath)

	mdPath, _ := cmd.Flags().GetString("path")

	reportApplicable, _ := compareTarget(mdPath, sys)

	if !reportApplicable {
		err := fmt.Errorf("hardening guide '%s' is not compatible with your system (OS: %s, Arch: %s) or not all prerequisites are met", mdPath, sys.OS, sys.Arch)
		return nil, sys, "", err
	}

	return ctx, sys, mdPath, nil
}

// promptForSuites displays a multi-select prompt to the user to choose which suites to run.
// When all is true, or when stdin is not a TTY (e.g. CI / piped execution), all suites run
// without prompting.
func promptForSuites(suites []config.TestSuite, all bool) ([]config.TestSuite, error) {
	if all || !term.IsTerminal(int(os.Stdin.Fd())) {
		ui.PrintInfo(fmt.Sprintf("Executing ALL %d applicable suites.", len(suites)))
		return suites, nil
	}
	suiteMap := make(map[string]config.TestSuite)

	allSuitesKey := "[+++] Execute ALL Applicable Suites"
	options := []string{allSuitesKey}

	for _, s := range suites {
		optionName := fmt.Sprintf("[+] %s (%d checks)", s.Title, len(s.Checks))
		options = append(options, optionName)
		suiteMap[optionName] = s
	}

	var selectedTitles []string

	prompt := &survey.MultiSelect{
		Message:  "Select Hardening Suites to Execute (Use Space to toggle, Enter to confirm)",
		Options:  options,
		Default:  []string{allSuitesKey},
		PageSize: 15,
	}

	err := survey.AskOne(prompt, &selectedTitles)
	if err != nil {
		return nil, fmt.Errorf("selection cancelled by user")
	}

	if len(selectedTitles) == 0 {
		return []config.TestSuite{}, nil
	}

	var selectedSuites []config.TestSuite
	var executeAll bool

	for _, title := range selectedTitles {
		if title == allSuitesKey {
			executeAll = true
			break
		}
	}

	if executeAll {
		ui.PrintInfo(fmt.Sprintf("Executing ALL %d applicable suites.", len(suites)))
		return suites, nil
	}

	for _, title := range selectedTitles {
		if suite, ok := suiteMap[title]; ok {
			selectedSuites = append(selectedSuites, suite)
		}
	}

	ui.PrintInfo(fmt.Sprintf("Executing %d selected suites.", len(selectedSuites)))
	return selectedSuites, nil
}
