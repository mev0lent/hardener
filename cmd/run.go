package cmd

import (
	"bytes"
	"fmt"
	"hardener/internal/config"
	"hardener/internal/dashboard"
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
	// --dry-run (fix only) used to be accepted and ignored, so every fix was
	// applied. A dry run audits and lists the fix each failing check would get.
	if dryRun {
		mode = executor.ModeAudit
		title += " (DRY RUN)"
		description = "In this mode, your system will be audited and the fix for each failing check is listed. No fix is applied and nothing is changed."
	}
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
	ctx.DryRun = dryRun

	// An unknown level used to fall back to baseline silently, so a typo or
	// "High" ran fewer checks than asked for.
	level, _ := cmd.Flags().GetString("security-level")
	level = config.NormalizeSecurityLevel(level)
	if !config.IsSecurityLevel(level) {
		return ui.ReturnError("Invalid flags", fmt.Errorf("--security-level %q is not one of: %s", level, strings.Join(config.SecurityLevels, ", ")))
	}
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
		// This used to replace ctx with a fresh one, dropping --profile and
		// --label in guide mode.
		sys, err = setupAndValidate(mdPath)
		if err != nil {
			return ui.ReturnError("System validation failed", err)
		}

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
	// These checks never ran in earlier versions either; say so once.
	if unknown := config.UnknownSecurityLevels(suites); len(unknown) > 0 {
		listed := strings.Join(unknown[:min(len(unknown), 10)], ", ")
		if len(unknown) > 10 {
			listed += fmt.Sprintf(" and %d more", len(unknown)-10)
		}
		ui.PrintInfo(fmt.Sprintf("Warning: %d check(s) have a security_level that is not one of %s and are skipped at every level: %s",
			len(unknown), strings.Join(config.SecurityLevels, ", "), listed))
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

	// Share the exact selection between the dashboard and the execution engine.
	selectedSuites = executor.FilterSuites(selectedSuites, ctx.Labels)
	if len(selectedSuites) == 0 {
		ui.PrintInfo("No suites match the requested labels.")
		return nil
	}
	work := func(observer executor.Observer, stop func() bool) error {
		suiteResults, checksPassed, fixesApplied, runErr := executor.RunSuitesObserved(
			ctx, mode, sys.OS, sys.Arch, selectedSuites, observer, stop,
		)
		reportType := "audit"
		if mode == executor.ModeFix {
			reportType = "fix"
		}
		executor.MakeReportWithStatus(sys, suiteResults, reportType, stop != nil && stop())
		executor.MakeScorings(checksPassed, fixesApplied)
		return runErr
	}
	useTUI, _ := cmd.Flags().GetBool("tui")
	if useTUI && dashboard.Available() {
		return runDashboard(ctx, mode, selectedSuites, work)
	}
	if useTUI {
		ui.PrintInfo("Interactive terminal unavailable; using plain output.")
	}
	return work(nil, nil)

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

	// LoadRuleset intentionally returns every suite regardless of its
	// declared os/arch scope (see its doc comment); this is the filtering
	// step it defers to the caller. Without it, every checksuite in a
	// ruleset.yaml runs on every machine, e.g. Intel-only checks executing
	// (and always failing) on Apple Silicon and vice versa.
	suites = config.FilterSuitesByPlatform(suites, sys)
	if len(suites) == 0 {
		return nil, sys, fmt.Errorf("no suites in ruleset %q are applicable to this system (%s/%s)", rulesetPath, sys.OS, sys.Arch)
	}

	return suites, sys, nil
}

func compareTarget(path string, sys config.SystemInfo) bool {
	readmePath := filepath.Join(path, "00_README.md")

	if !config.FileExists(readmePath) {
		ui.PrintErrorMessage(fmt.Sprintf("Required file '00_README.md' not found in path: %s", path))
		return false
	}

	files, err := os.ReadDir(path)
	if err != nil {
		ui.PrintErrorMessage(err.Error())
		return false
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
				return false
			}

			var frontMatter ReportInfo
			reader := bytes.NewReader(data)
			if _, err := frontmatter.Parse(reader, &frontMatter); err != nil {
				ui.PrintErrorMessage(fmt.Sprintf("could not parse YAML from %s: %v", fileName, err))
				return false
			}
			docOS := strings.ToLower(frontMatter.OS)
			sysOS := strings.ToLower(sys.OS)

			if docOS != sysOS {
				ui.PrintErrorMessage(fmt.Sprintf("OS mismatch for %s: Document requires '%s', System is '%s'", fileName, docOS, sysOS))
				return false
			}

			// Same arch matching as suites: case-insensitive, amd64 == x86_64.
			// The guide must still name its architectures.
			if len(frontMatter.Arch) == 0 || !config.ArchMatches(frontMatter.Arch, sys.Arch) {
				ui.PrintErrorMessage(fmt.Sprintf("Architecture mismatch for %s: Document supports %v, System is %s", fileName, frontMatter.Arch, sys.Arch))
				return false
			}

			if !config.CheckPreconditions(frontMatter.Preconditions.Tools) {
				return false
			}

			ui.PrintPassed("Target matched: OS, Architecture, and Preconditions supported by 00_README.md")
			return true
		}
	}
	return false
}

// setupAndValidate checks a guide directory's 00_README.md against the
// running system for the classic markdown / guide-directory mode.
func setupAndValidate(mdPath string) (config.SystemInfo, error) {
	sys := config.SystemInfo{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		Distro: config.DetectDistro(),
	}

	// sys is returned as built here; compareTarget used to return a rebuilt
	// copy that lost the distro in reports.
	if !compareTarget(mdPath, sys) {
		return sys, fmt.Errorf("hardening guide '%s' is not compatible with your system (OS: %s, Arch: %s) or not all prerequisites are met", mdPath, sys.OS, sys.Arch)
	}

	return sys, nil
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
