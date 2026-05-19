package rollback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hardener/internal/config"
	"hardener/internal/ui"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

func PreBackup(filePath string) (oldContent []byte, origPerm fs.FileMode, err error) {
	info, err := os.Stat(filePath)
	perm := fs.FileMode(0644)

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, perm, nil
		}
		if errors.Is(err, os.ErrPermission) {
			perm = 0600
		} else {
			return nil, 0, err
		}
	} else {
		// SUID/SGID/Sticky Bits maskieren
		perm = info.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	}

	data, err := readFileContent(filePath)
	if err != nil {
		if strings.Contains(err.Error(), "No such file") {
			return nil, perm, nil
		}
		return nil, 0, err
	}

	return data, perm, nil
}

func readFileContent(filePath string) (content []byte, err error) {
	newContent, err := os.ReadFile(filePath)
	if errors.Is(err, os.ErrPermission) {
		cmd := exec.Command("sudo", "cat", filePath)
		data, cmdErr := cmd.Output()
		if cmdErr != nil {
			return nil, fmt.Errorf("(even) sudo read failed: %w", cmdErr)
		}
		return data, nil
	}
	return newContent, err
}

func createEntry(ctx *config.ExecContext, filePath, checksum, delta string, perm fs.FileMode) (config.DeltaEntry, string, error) {
	if filePath == "" || checksum == "" {
		return config.DeltaEntry{}, "", ui.ReturnError("", fmt.Errorf("filePath or checksum is empty"))
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	entry := config.DeltaEntry{
		RunID:     ctx.RunID,
		Timestamp: ts,
		FilePath:  filePath,
		Checksum:  checksum,
		Delta:     delta,
		Perm:      uint32(perm),
	}
	return entry, ts, nil
}

func initializeRuns(deltaFile string) (map[string][]config.DeltaEntry, error) {
	var runs map[string][]config.DeltaEntry

	data, err := os.ReadFile(deltaFile)
	if err != nil || len(data) == 0 {
		// No file yet → initialize
		return make(map[string][]config.DeltaEntry), nil
	}

	// Try to unmarshal existing data
	if err := json.Unmarshal(data, &runs); err != nil {
		// corrupted JSON → return empty and propagate error
		return make(map[string][]config.DeltaEntry), fmt.Errorf("failed to parse runs.json: %w", err)
	}

	return runs, nil
}

func PostDelta(ctx *config.ExecContext, filePath string, oldContent []byte, origPerm fs.FileMode, check config.Check) error {
	if filePath == "" || filePath == "N/A" {
		return nil
	}
	// try normal read
	newContent, err := readFileContent(filePath)
	if err != nil {
		return ui.ReturnError("", err)
	}

	// compute delta (rollback direction)
	delta, checksum := ComputeDelta(string(newContent), string(oldContent))

	// create metadata entry
	entry, _, err := createEntry(ctx, filePath, checksum, delta, origPerm)
	if err != nil {
		return ui.ReturnError("", err)
	}

	deltaFile := filepath.Join(ctx.BaseDir, "runs.json")
	//ui.PrintInfo(fmt.Sprintf("Delta file created: %s", deltaFile))
	runs, err := initializeRuns(deltaFile)
	if err != nil {
		ui.PrintErrorMessage(fmt.Sprintf("warning: %v", err))
	}

	// Timestamp is the key
	runs[ctx.RunID] = append(runs[ctx.RunID], entry)

	// Write back atomically
	tmpFile := deltaFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return ui.ReturnError("failed to create temp file", err)
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")

	encodeErr := enc.Encode(runs)
	if encodeErr != nil {
		err := f.Close()
		if err != nil {
			return err
		}
		return ui.ReturnError("failed to encode runs", encodeErr)
	}

	if closeErr := f.Close(); closeErr != nil {
		return closeErr
	}

	if renameErr := os.Rename(tmpFile, deltaFile); renameErr != nil {
		return ui.ReturnError("failed to replace runs file", renameErr)
	}
	//ui.PrintInfo("runs.json created")

	return nil
}

func ApplyRun(ctx *config.ExecContext, files []string) error {
	deltaFile := filepath.Join(ctx.BaseDir, "runs.json")
	ui.PrintInfo(fmt.Sprintf("Looking up delta file: %s", deltaFile))
	runs, err := initializeRuns(deltaFile)
	if err != nil {
		ui.PrintErrorMessage(fmt.Sprintf("warning: %v", err))
	}
	// pick target timestamp
	target := ctx.Timestamp // set via CLI or fallback
	if target == "" {
		// no timestamp provided → pick latest
		var newest string
		for runID := range runs {
			if runID > newest { // Lexicographical sort works for RFC3339
				newest = runID
			}
		}
		target = newest
	}

	entries := runs[target]

	if len(entries) == 0 {
		return ui.ReturnError("", errors.New("no runs found"))
	}

	if len(files) == 0 {
		err = applyDelta(ctx, entries)
		if err != nil {
			return ui.ReturnError("", err)
		}

	} else {
		entries = filterRollbackFiles(entries, files)
		if len(entries) == 0 {
			ui.PrintErrorMessage("no matching files found in current run")
			return nil
		}
		err = applyDelta(ctx, entries)
		if err != nil {
			return ui.ReturnError("", err)
		}
	}
	return nil
}

func filterRollbackFiles(entries []config.DeltaEntry, files []string) []config.DeltaEntry {
	var filtered []config.DeltaEntry
	for _, entry := range entries {
		if slices.Contains(files, entry.FilePath) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func applyDelta(ctx *config.ExecContext, entries []config.DeltaEntry) error {
	var errs []error

	// 1. LIFO (Last-In, First-Out): Neueste Änderungen zuerst zurückrollen
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	for _, entry := range entries {
		fileContent, err := readFileContent(entry.FilePath)
		if err != nil {
			errs = append(errs, ui.ReturnError("failed to read file "+entry.FilePath, err))
			continue
		}

		oldContent, err := ApplyRollbackDelta(string(fileContent), entry.Delta)
		if err != nil {
			errs = append(errs, ui.ReturnError("failed to apply rollback delta for "+entry.FilePath, err))
			continue
		}

		tmpFile := entry.FilePath + ".tmp"
		f, err := os.Create(tmpFile)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				ui.PrintInfo(fmt.Sprintf("Elevating: using sudo direct write for %s", entry.FilePath))
				if err := WriteFileMaybeSudo(entry.FilePath, []byte(oldContent), fs.FileMode(entry.Perm)); err != nil {
					errs = append(errs, ui.ReturnError("sudo write failed for "+entry.FilePath, err))
				}
				continue
			}
			errs = append(errs, ui.ReturnError("failed to create temp file for "+entry.FilePath, err))
			continue
		}

		_, _ = f.Write([]byte(oldContent))
		_ = f.Close()

		if renameErr := os.Rename(tmpFile, entry.FilePath); renameErr != nil {
			if errors.Is(renameErr, os.ErrPermission) {
				ui.PrintInfo(fmt.Sprintf("Elevating: using sudo fallback for %s", entry.FilePath))
				if err := WriteFileMaybeSudo(entry.FilePath, []byte(oldContent), fs.FileMode(entry.Perm)); err != nil {
					errs = append(errs, ui.ReturnError("sudo write failed for "+entry.FilePath, err))
				}
				_ = os.Remove(tmpFile)
				continue
			}
			errs = append(errs, ui.ReturnError("failed to replace "+entry.FilePath, renameErr))
			_ = os.Remove(tmpFile)
			continue
		}

		// Berechtigungen erzwingen (SUID Bit!)
		_ = RestorePermissions(entry.FilePath, fs.FileMode(entry.Perm))
		ui.PrintInfo(fmt.Sprintf("restored %s successfully", entry.FilePath))
	}

	if len(errs) > 0 {
		ui.PrintErrorSummary("Rollback completed with errors", errs)
		return fmt.Errorf("rollback finished with %d error(s)", len(errs))
	}

	// 2. Live-Status Synchronisation
	executePostRollbackHooks(entries)

	ui.PrintSummary("Rollback completed successfully")
	return nil
}

func executePostRollbackHooks(entries []config.DeltaEntry) {
	needsSysctl, needsSSH, needsAudit, needsReboot := false, false, false, false

	for _, e := range entries {
		if strings.Contains(e.FilePath, "sysctl") {
			needsSysctl = true
			needsReboot = true
		}
		if strings.Contains(e.FilePath, "ssh") {
			needsSSH = true
		}
		if strings.Contains(e.FilePath, "audit") {
			needsAudit = true
		}
		if strings.Contains(e.FilePath, "modprobe") {
			needsReboot = true
		}
	}

	ui.PrintHeader("Synchronizing Live-State")

	if needsSysctl && runtime.GOOS != "darwin" {
		_ = exec.Command("sudo", "sysctl", "--system").Run()
	}
	if needsSSH {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("sudo", "launchctl", "kickstart", "-k", "system/com.openssh.sshd").Run()
		} else {
			_ = exec.Command("sudo", "systemctl", "restart", "ssh").Run()
		}
	}
	if needsAudit {
		if runtime.GOOS == "darwin" {
			ui.PrintInfo("Audit rules updated: reboot recommended to fully reload on macOS.")
		} else {
			_ = exec.Command("sudo", "augenrules", "--load").Run()
		}
	}

	if needsReboot {
		ui.PrintErrorMessage("KERNEL ROLLBACK: Reboot required for 100% live-state restoration.")
	}
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

func WriteFileMaybeSudo(path string, data []byte, perm fs.FileMode) error {
	err := os.WriteFile(path, data, perm)
	if err == nil {
		return nil
	}

	if errors.Is(err, os.ErrPermission) {
		cmd := exec.Command("sudo", "tee", path)
		cmd.Stdin = bytes.NewReader(data)
		if cmdErr := cmd.Run(); cmdErr != nil {
			return fmt.Errorf("sudo write failed: %w", cmdErr)
		}
		_ = RestorePermissions(path, perm)
		return nil
	}
	return err
}

func RestorePermissions(path string, perm fs.FileMode) error {
	if err := os.Chmod(path, perm); err != nil {
		if errors.Is(err, os.ErrPermission) {
			cmd := exec.Command("sudo", "chmod", getUnixOctal(perm), path)
			return cmd.Run()
		}
		return err
	}
	return nil
}
