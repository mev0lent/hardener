package rollback

import (
	"bytes"
	"crypto/sha256"
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
	perm := fs.FileMode(0644)

	// Treat "no path" (empty, N/A, or a prose descriptor like "System service
	// runtime") the same as a missing file — nothing to back up.
	if filePath == "" || filePath == "N/A" || !strings.HasPrefix(filePath, "/") {
		return nil, perm, nil
	}

	info, err := os.Stat(filePath)
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
	// Skip anything that isn't an actual absolute filesystem path. Some
	// ruleset entries use prose values like "System service runtime" or
	// "Kernel runtime status" as affected_file for checks that don't touch
	// a file — treat those the same as "N/A" so the fix isn't reported as
	// failed just because we can't back up a non-existent path.
	if filePath == "" || filePath == "N/A" || !strings.HasPrefix(filePath, "/") {
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
		return ui.ReturnError("runs.json is corrupted and cannot be read — rollback aborted", err)
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

	// LIFO (Last-In, First-Out): reverse deltas in the opposite order of the
	// original applications. Required when the same file was modified by
	// multiple suites within one run, so the compositional post-fix state
	// unwinds correctly back to the pre-fix state.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	for idx, entry := range entries {
		// Read the current on-disk content — for the first iteration this is
		// the post-fix content; for later iterations it is the intermediate
		// state produced by earlier reverses in this loop.
		fileContent, err := readFileContent(entry.FilePath)
		if err != nil {
			errs = append(errs, ui.ReturnError("failed to read file "+entry.FilePath, err))
			continue
		}

		// Reverse this suite's delta.  ComputeDelta stored diff(post -> pre)
		// for this suite's PreBackup snapshot, so applying it to the current
		// content walks one step closer to the original pre-fix state.
		targetContent, err := ApplyRollbackDelta(string(fileContent), entry.Delta)
		if err != nil {
			errs = append(errs, ui.ReturnError("failed to apply rollback delta for "+entry.FilePath, err))
			continue
		}

		// Fidelity assertion — the invariant that hypothesis H5 in the term
		// paper predicts.  entry.Checksum was recorded as sha256(pre-fix)
		// when PostDelta stored the entry; the reverse-patch result must
		// match it.  If it does not, the delta application produced wrong
		// output and we MUST NOT write it to disk, otherwise the on-disk
		// state silently drifts from what the rollback claims to restore.
		actualHash := fmt.Sprintf("%x", sha256.Sum256([]byte(targetContent)))
		if entry.Checksum != "" && actualHash != entry.Checksum {
			errs = append(errs, ui.ReturnError(
				fmt.Sprintf("rollback fidelity failure for %s (entry %d/%d): "+
					"reverse-patch produced sha256=%s but expected %s; "+
					"aborting write to prevent silent corruption",
					entry.FilePath, idx+1, len(entries),
					shortHash(actualHash), shortHash(entry.Checksum)),
				errors.New("hash mismatch")))
			continue
		}

		// Skip the write if the current content already matches — happens on
		// identity deltas (pre == post) and avoids unnecessary I/O and sudo
		// prompts.  Report it so the operator can see the no-op explicitly.
		currentHash := fmt.Sprintf("%x", sha256.Sum256(fileContent))
		if currentHash == actualHash {
			ui.PrintInfo(fmt.Sprintf("no-op rollback for %s (already at pre-fix state)", entry.FilePath))
			continue
		}

		if werr := writeRollbackTarget(entry, []byte(targetContent)); werr != nil {
			errs = append(errs, werr)
			continue
		}
		ui.PrintInfo(fmt.Sprintf("restored %s (sha256=%s)", entry.FilePath, shortHash(actualHash)))
	}

	if len(errs) > 0 {
		ui.PrintErrorSummary("Rollback completed with errors", errs)
		return fmt.Errorf("rollback finished with %d error(s)", len(errs))
	}

	// Live-state synchronisation for subsystems that need a reload after
	// their persistent configuration was restored.
	executePostRollbackHooks(entries)

	ui.PrintSummary("Rollback completed successfully")
	return nil
}

// writeRollbackTarget atomically writes the reversed content back to the
// on-disk file, elevating via sudo where necessary.  Extracted from
// applyDelta so the fidelity-assertion path above stays readable.
func writeRollbackTarget(entry config.DeltaEntry, data []byte) error {
	tmpFile := entry.FilePath + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			ui.PrintInfo(fmt.Sprintf("Elevating: using sudo direct write for %s", entry.FilePath))
			if werr := WriteFileMaybeSudo(entry.FilePath, data, fs.FileMode(entry.Perm)); werr != nil {
				return ui.ReturnError("sudo write failed for "+entry.FilePath, werr)
			}
			return nil
		}
		return ui.ReturnError("failed to create temp file for "+entry.FilePath, err)
	}

	if _, werr := f.Write(data); werr != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		return ui.ReturnError("failed to write temp file for "+entry.FilePath, werr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(tmpFile)
		return ui.ReturnError("failed to close temp file for "+entry.FilePath, closeErr)
	}

	if renameErr := os.Rename(tmpFile, entry.FilePath); renameErr != nil {
		if errors.Is(renameErr, os.ErrPermission) {
			ui.PrintInfo(fmt.Sprintf("Elevating: using sudo fallback for %s", entry.FilePath))
			if werr := WriteFileMaybeSudo(entry.FilePath, data, fs.FileMode(entry.Perm)); werr != nil {
				_ = os.Remove(tmpFile)
				return ui.ReturnError("sudo write failed for "+entry.FilePath, werr)
			}
			_ = os.Remove(tmpFile)
			return nil
		}
		_ = os.Remove(tmpFile)
		return ui.ReturnError("failed to replace "+entry.FilePath, renameErr)
	}

	_ = RestorePermissions(entry.FilePath, fs.FileMode(entry.Perm))
	return nil
}

// shortHash returns the first 16 characters of a hex hash for compact
// diagnostic output.  Full hash comparisons happen on the untrimmed value.
func shortHash(h string) string {
	if len(h) <= 16 {
		return h
	}
	return h[:16] + "..."
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
