package rollback

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"hardener/internal/config"
	"hardener/internal/ui"
)

const backupModeMask = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky

// Rulesets may use prose such as "System service runtime" for non-file checks.
func isBackupPath(path string) bool {
	return filepath.IsAbs(path)
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
	// JSON null must not leave a nil map for Begin to append to.
	if runs == nil {
		runs = make(map[string][]config.DeltaEntry)
	}
	return runs, nil
}

func jsonIndent(runs map[string][]config.DeltaEntry) ([]byte, error) {
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
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
	var fileEntries, kernelEntries []config.DeltaEntry

	// Walk backwards without changing the caller's slice. Older deltas depend
	// on newer ones having succeeded, so stop a file's chain after a failure.
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		if entry.Kind == config.EntryKindSysctl {
			kernelEntries = append(kernelEntries, entry)
			continue
		}
		fileEntries = append(fileEntries, entry)
		if failedFiles[entry.FilePath] {
			continue
		}
		if err := restoreEntry(entry); err != nil {
			failedFiles[entry.FilePath] = true
			errs = append(errs, err)
			ui.PrintErrorMessage(err.Error())
		}
	}
	if len(errs) == 0 && len(fileEntries) > 0 {
		if err := executePostRollbackHooks(fileEntries); err != nil {
			errs = append(errs, fmt.Errorf("files restored, but live-state synchronization failed: %w", err))
		}
	}
	// Live kernel values go last: the hooks above re-apply sysctl files, and
	// the recorded value is what was active before the run, whatever the
	// files say. Newest first, so the oldest recorded value is written last.
	for _, entry := range kernelEntries {
		if err := restoreKernelValue(entry); err != nil {
			errs = append(errs, err)
			ui.PrintErrorMessage(err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("rollback finished with %d error(s): %w", len(errs), errors.Join(errs...))
	}
	ui.PrintSummary("Rollback completed successfully")
	return nil
}

func restoreEntry(entry config.DeltaEntry) error {
	path := entry.FilePath
	current, err := captureFile(path)
	if err != nil {
		return fmt.Errorf("read rollback file %s: %w", path, err)
	}
	if entry.Pending {
		ui.PrintInfo(fmt.Sprintf("%s: rollback record was never completed (the run ended during its fix); "+
			"restoring the recorded pre-fix state without verifying the post-fix state", path))
	}

	if entry.Created {
		if !current.exists {
			ui.PrintInfo(fmt.Sprintf("%s was created by the fix and is already absent", path))
			return nil
		}
		if err := checkPostState(entry, current); err != nil {
			return err
		}
		if err := removeRollbackTarget(path, current); err != nil {
			return err
		}
		ui.PrintInfo(fmt.Sprintf("removed %s (created by the fix)", path))
		return nil
	}

	if current.exists && sha256Hex(current.content) == entry.Checksum {
		// Already in the pre-fix state: a repeated rollback, or a fix that
		// only changed permissions. The original mode still needs restoring.
		if err := RestorePermissions(path, fs.FileMode(entry.Perm)); err != nil {
			return fmt.Errorf("restore permissions for %s: %w", path, err)
		}
		ui.PrintInfo(fmt.Sprintf("restored permissions for %s (content unchanged)", path))
		return nil
	}
	if err := checkPostState(entry, current); err != nil {
		return err
	}

	var target string
	switch {
	case entry.Pending:
		before, err := base64.StdEncoding.DecodeString(entry.Before)
		if err != nil {
			return fmt.Errorf("invalid pending rollback record for %s: %w", path, err)
		}
		target = string(before)
	case entry.Removed:
		target, err = ApplyRollbackDelta("", entry.Delta)
	default:
		target, err = ApplyRollbackDelta(string(current.content), entry.Delta)
	}
	if err != nil {
		return fmt.Errorf("apply rollback delta for %s: %w", path, err)
	}

	// Check the restored bytes before any write, including sudo fallbacks.
	targetBytes := []byte(target)
	actualHash := sha256Hex(targetBytes)
	if entry.Checksum != "" && actualHash != entry.Checksum {
		return fmt.Errorf("rollback checksum mismatch for %s: got %s, expected %s; refusing to write",
			path, actualHash, entry.Checksum)
	}
	if err := writeRollbackTarget(entry, targetBytes, current); err != nil {
		return err
	}
	ui.PrintInfo(fmt.Sprintf("restored %s successfully", path))
	return nil
}

// checkPostState refuses to touch a file that no longer holds what the fix
// left behind, so rollback cannot discard a change made after the run.
// Pending and pre-v1.3 records carry no post-fix checksum; for the latter the
// checksum of the patched result still guards the write.
func checkPostState(entry config.DeltaEntry, current fileState) error {
	if entry.Pending {
		return nil
	}
	if entry.Removed {
		if current.exists {
			return fmt.Errorf("%s was removed by the fix but exists again; refusing to overwrite", entry.FilePath)
		}
		return nil
	}
	if entry.PostChecksum == "" {
		return nil
	}
	if !current.exists {
		return fmt.Errorf("%s was removed after the fix; refusing to restore", entry.FilePath)
	}
	if got := sha256Hex(current.content); got != entry.PostChecksum {
		return fmt.Errorf("%s changed after the fix (sha256 %s, fix left %s); refusing to overwrite",
			entry.FilePath, got, entry.PostChecksum)
	}
	return nil
}

// verifyUnchanged is the last check before a rollback write: the file must
// still be exactly what restoreEntry read and verified at the start.
func verifyUnchanged(path string, expected fileState) error {
	now, err := captureFile(path)
	if err != nil {
		return fmt.Errorf("re-read %s before writing: %w", path, err)
	}
	if now.exists != expected.exists || !bytes.Equal(now.content, expected.content) {
		return fmt.Errorf("%s changed while rollback was running; refusing to overwrite", path)
	}
	return nil
}

// Write in the destination directory so rename stays on the same filesystem.
// Unique temporary names avoid collisions and following a fixed .tmp symlink.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	return writeFileAtomicGuarded(path, data, perm, nil)
}

// guard runs after the new content is fully written to the temporary file and
// immediately before the rename, keeping the window for a lost concurrent
// update as small as a rename allows.
func writeFileAtomicGuarded(path string, data []byte, perm fs.FileMode, guard func() error) error {
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
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func writeRollbackTarget(entry config.DeltaEntry, data []byte, expected fileState) error {
	guard := func() error { return verifyUnchanged(entry.FilePath, expected) }
	err := writeFileAtomicGuarded(entry.FilePath, data, fs.FileMode(entry.Perm), guard)
	if !errors.Is(err, os.ErrPermission) {
		return err
	}
	ui.PrintInfo(fmt.Sprintf("Elevating write for %s", entry.FilePath))
	if err := guard(); err != nil {
		return err
	}
	return WriteFileMaybeSudo(entry.FilePath, data, fs.FileMode(entry.Perm))
}

func removeRollbackTarget(path string, expected fileState) error {
	if err := verifyUnchanged(path, expected); err != nil {
		return err
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrPermission) {
		output, sudoErr := exec.Command("sudo", "rm", "-f", "--", path).CombinedOutput()
		if sudoErr != nil {
			return fmt.Errorf("sudo rm %s: %w (%s)", path, sudoErr, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
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
