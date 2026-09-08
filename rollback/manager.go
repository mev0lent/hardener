package rollback

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hardener/internal/config"
	"hardener/internal/ui"
)

const backupModeMask = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// Rulesets may use prose such as "System service runtime" for non-file checks.
func isBackupPath(path string) bool {
	return filepath.IsAbs(path)
}

func PreBackup(filePath string) ([]byte, fs.FileMode, error) {
	perm := fs.FileMode(0644)
	if !isBackupPath(filePath) {
		return nil, perm, nil
	}

	info, err := os.Stat(filePath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, perm, nil
	case errors.Is(err, os.ErrPermission):
		perm = 0600
	case err != nil:
		return nil, 0, fmt.Errorf("stat backup file %s: %w", filePath, err)
	default:
		if !info.Mode().IsRegular() {
			return nil, 0, fmt.Errorf("backup path %s is not a regular file", filePath)
		}
		perm = info.Mode() & backupModeMask
	}

	data, err := readFileContent(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, perm, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return data, perm, nil
}

func readFileContent(filePath string) ([]byte, error) {
	data, err := os.ReadFile(filePath)
	if !errors.Is(err, os.ErrPermission) {
		return data, err
	}

	cmd := exec.Command("sudo", "cat", filePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sudo read %s: %w (%s)", filePath, err, strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

func createEntry(ctx *config.ExecContext, filePath, checksum, delta string, perm fs.FileMode) (config.DeltaEntry, string, error) {
	if filePath == "" || checksum == "" {
		return config.DeltaEntry{}, "", errors.New("filePath or checksum is empty")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	return config.DeltaEntry{
		RunID:     ctx.RunID,
		Timestamp: ts,
		FilePath:  filePath,
		Checksum:  checksum,
		Delta:     delta,
		Perm:      uint32(perm & backupModeMask),
	}, ts, nil
}

func initializeRuns(deltaFile string) (map[string][]config.DeltaEntry, error) {
	runs := make(map[string][]config.DeltaEntry)
	data, err := os.ReadFile(deltaFile)
	if errors.Is(err, os.ErrNotExist) {
		return runs, nil
	}
	if err != nil {
		return runs, fmt.Errorf("read %s: %w", deltaFile, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return runs, nil
	}
	if err := json.Unmarshal(data, &runs); err != nil {
		return make(map[string][]config.DeltaEntry), fmt.Errorf("parse %s: %w", deltaFile, err)
	}
	// JSON null must not leave a nil map for PostDelta to append to.
	if runs == nil {
		runs = make(map[string][]config.DeltaEntry)
	}
	return runs, nil
}

func PostDelta(ctx *config.ExecContext, filePath string, oldContent []byte, origPerm fs.FileMode, _ config.Check) error {
	if !isBackupPath(filePath) {
		return nil
	}
	newContent, err := readFileContent(filePath)
	if err != nil {
		return fmt.Errorf("read post-fix file %s: %w", filePath, err)
	}

	deltaFile := filepath.Join(ctx.BaseDir, "runs.json")
	runs, err := initializeRuns(deltaFile)
	if err != nil {
		// Never overwrite existing history when reading or decoding it fails.
		return err
	}
	delta, checksum := ComputeDelta(string(newContent), string(oldContent))
	entry, _, err := createEntry(ctx, filePath, checksum, delta, origPerm)
	if err != nil {
		return err
	}
	runs[ctx.RunID] = append(runs[ctx.RunID], entry)

	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode rollback history: %w", err)
	}
	// Backups may contain sensitive configuration; restrict history access.
	return writeFileAtomic(deltaFile, append(data, '\n'), 0600)
}

func ApplyRun(ctx *config.ExecContext, files []string) error {
	deltaFile := filepath.Join(ctx.BaseDir, "runs.json")
	ui.PrintInfo(fmt.Sprintf("Looking up delta file: %s", deltaFile))
	runs, err := initializeRuns(deltaFile)
	if err != nil {
		return fmt.Errorf("rollback aborted: %w", err)
	}

	target := ctx.Timestamp
	if target == "" {
		// Generated RunIDs are UTC RFC3339 timestamps and sort chronologically.
		for runID := range runs {
			if runID > target {
				target = runID
			}
		}
	}
	entries := runs[target]
	if len(entries) == 0 {
		return fmt.Errorf("no rollback entries found for run %q", target)
	}
	if len(files) > 0 {
		entries = filterRollbackFiles(entries, files)
		if len(entries) == 0 {
			ui.PrintInfo("No matching files found in selected run")
			return nil
		}
	}
	return applyDelta(ctx, entries)
}

func filterRollbackFiles(entries []config.DeltaEntry, files []string) []config.DeltaEntry {
	wanted := make(map[string]struct{}, len(files))
	for _, file := range files {
		wanted[file] = struct{}{}
	}
	var filtered []config.DeltaEntry
	for _, entry := range entries {
		if _, ok := wanted[entry.FilePath]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func applyDelta(_ *config.ExecContext, entries []config.DeltaEntry) error {
	var errs []error
	failedFiles := make(map[string]bool)

	// Walk backwards without changing the caller's slice. Older deltas depend
	// on newer ones having succeeded, so stop a file's chain after a failure.
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if failedFiles[entry.FilePath] {
			continue
		}
		if err := restoreEntry(entry); err != nil {
			failedFiles[entry.FilePath] = true
			errs = append(errs, err)
			ui.PrintErrorMessage(err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("rollback finished with %d error(s): %w", len(errs), errors.Join(errs...))
	}
	if err := executePostRollbackHooks(entries); err != nil {
		return fmt.Errorf("files restored, but live-state synchronization failed: %w", err)
	}
	ui.PrintSummary("Rollback completed successfully")
	return nil
}

func restoreEntry(entry config.DeltaEntry) error {
	content, err := readFileContent(entry.FilePath)
	if err != nil {
		return fmt.Errorf("read rollback file %s: %w", entry.FilePath, err)
	}
	target, err := ApplyRollbackDelta(string(content), entry.Delta)
	if err != nil {
		return fmt.Errorf("apply rollback delta for %s: %w", entry.FilePath, err)
	}

	// Check the restored bytes before any write, including sudo fallbacks.
	targetBytes := []byte(target)
	actualHash := fmt.Sprintf("%x", sha256.Sum256(targetBytes))
	if entry.Checksum != "" && actualHash != entry.Checksum {
		return fmt.Errorf("rollback checksum mismatch for %s: got %s, expected %s; refusing to write",
			entry.FilePath, actualHash, entry.Checksum)
	}
	if bytes.Equal(content, targetBytes) {
		// Permission-only fixes still need their original mode restored.
		if err := RestorePermissions(entry.FilePath, fs.FileMode(entry.Perm)); err != nil {
			return fmt.Errorf("restore permissions for %s: %w", entry.FilePath, err)
		}
		ui.PrintInfo(fmt.Sprintf("restored permissions for %s (content unchanged)", entry.FilePath))
		return nil
	}
	if err := writeRollbackTarget(entry, targetBytes); err != nil {
		return err
	}
	ui.PrintInfo(fmt.Sprintf("restored %s successfully", entry.FilePath))
	return nil
}

// Write in the destination directory so rename stays on the same filesystem.
// Unique temporary names avoid collisions and following a fixed .tmp symlink.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".hardener-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	// Set special mode bits after writing, since a write may clear them.
	if err := f.Chmod(perm & backupModeMask); err != nil {
		return fmt.Errorf("set permissions for %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func writeRollbackTarget(entry config.DeltaEntry, data []byte) error {
	err := writeFileAtomic(entry.FilePath, data, fs.FileMode(entry.Perm))
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	ui.PrintInfo(fmt.Sprintf("Elevating write for %s", entry.FilePath))
	return WriteFileMaybeSudo(entry.FilePath, data, fs.FileMode(entry.Perm))
}

func executePostRollbackHooks(entries []config.DeltaEntry) error {
	var needsSysctl, needsSSH, needsAudit, needsReboot bool
	for _, entry := range entries {
		path := entry.FilePath
		needsSysctl = needsSysctl || strings.Contains(path, "sysctl")
		needsSSH = needsSSH || strings.Contains(path, "ssh")
		needsAudit = needsAudit || strings.Contains(path, "audit")
		needsReboot = needsReboot || strings.Contains(path, "modprobe")
	}
	needsReboot = needsReboot || needsSysctl
	ui.PrintHeader("Synchronizing Live-State")

	var errs []error
	run := func(args ...string) {
		output, err := exec.Command("sudo", args...).CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("sudo %s: %w (%s)",
				strings.Join(args, " "), err, strings.TrimSpace(string(output))))
		}
	}
	if needsSysctl && runtime.GOOS != "darwin" {
		run("sysctl", "--system")
	}
	if needsSSH {
		if runtime.GOOS == "darwin" {
			run("launchctl", "kickstart", "-k", "system/com.openssh.sshd")
		} else {
			run("systemctl", "restart", "ssh")
		}
	}
	if needsAudit {
		if runtime.GOOS == "darwin" {
			ui.PrintInfo("Audit rules updated: reboot recommended to fully reload on macOS.")
		} else {
			run("augenrules", "--load")
		}
	}
	if needsReboot {
		ui.PrintInfo("Kernel configuration restored: reboot required to reload live state.")
	}
	return errors.Join(errs...)
}

func getUnixOctal(perm fs.FileMode) string {
	octal := perm & fs.ModePerm
	if perm&fs.ModeSetuid != 0 {
		octal |= 04000
	}
	if perm&fs.ModeSetgid != 0 {
		octal |= 02000
	}
	if perm&fs.ModeSticky != 0 {
		octal |= 01000
	}
	return fmt.Sprintf("%04o", octal)
}

// The existing sudo tee fallback is a direct write, not an atomic replacement.
func WriteFileMaybeSudo(path string, data []byte, perm fs.FileMode) error {
	err := os.WriteFile(path, data, perm&backupModeMask)
	if err != nil {
		if !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("write %s: %w", path, err)
		}
		cmd := exec.Command("sudo", "tee", path)
		cmd.Stdin = bytes.NewReader(data)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		// Leave stdout unset so tee cannot print backed-up contents.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo write %s: %w (%s)", path, err, strings.TrimSpace(stderr.String()))
		}
	}
	if err := RestorePermissions(path, perm); err != nil {
		return fmt.Errorf("restore permissions for %s: %w", path, err)
	}
	return nil
}

func RestorePermissions(path string, perm fs.FileMode) error {
	err := os.Chmod(path, perm&backupModeMask)
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	output, err := exec.Command("sudo", "chmod", getUnixOctal(perm), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo chmod %s: %w (%s)", path, err, strings.TrimSpace(string(output)))
	}
	return nil
}
