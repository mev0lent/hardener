package rollback

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"hardener/internal/config"
)

// fileState is what a path looked like at one point in time. A missing file
// is a state of its own: a fix that creates a file must be undone by removing
// it, not by leaving an empty one behind.
type fileState struct {
	exists  bool
	content []byte
	perm    fs.FileMode
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func captureFile(path string) (fileState, error) {
	var perm fs.FileMode
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fileState{}, nil
	case errors.Is(err, os.ErrPermission):
		// The parent directory is not searchable for this user. Ask sudo for
		// the real mode instead of guessing one that rollback would apply.
		exists, sudoPerm, regular, err := sudoStat(path)
		if err != nil {
			return fileState{}, err
		}
		if !exists {
			return fileState{}, nil
		}
		if !regular {
			return fileState{}, fmt.Errorf("backup path %s is not a regular file", path)
		}
		perm = sudoPerm
	case err != nil:
		return fileState{}, fmt.Errorf("stat backup file %s: %w", path, err)
	default:
		if !info.Mode().IsRegular() {
			return fileState{}, fmt.Errorf("backup path %s is not a regular file", path)
		}
		perm = info.Mode() & backupModeMask
	}

	data, err := readFileContent(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileState{}, nil
	}
	if err != nil {
		return fileState{}, err
	}
	return fileState{exists: true, content: data, perm: perm}, nil
}

// sudoStat reports existence, permission bits and file type of a path that
// the current user cannot stat directly.
func sudoStat(path string) (exists bool, perm fs.FileMode, regular bool, err error) {
	// GNU stat prints the raw mode in hex, BSD stat in octal.
	flags, base := "-c %f", 16
	if runtime.GOOS == "darwin" {
		flags, base = "-f %p", 8
	}
	script := `[ -e "$1" ] || exit 3; exec stat -L ` + flags + ` -- "$1"`
	out, err := exec.Command("sudo", "sh", "-c", script, "sh", path).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 3 {
		return false, 0, false, nil
	}
	if err != nil {
		return false, 0, false, fmt.Errorf("sudo stat %s: %w", path, err)
	}
	raw, err := strconv.ParseUint(strings.TrimSpace(string(out)), base, 32)
	if err != nil {
		return false, 0, false, fmt.Errorf("sudo stat %s: unexpected output %q", path, out)
	}
	return true, unixModeToFileMode(uint32(raw)), raw&0170000 == 0100000, nil
}

func unixModeToFileMode(raw uint32) fs.FileMode {
	mode := fs.FileMode(raw & 0777)
	if raw&04000 != 0 {
		mode |= fs.ModeSetuid
	}
	if raw&02000 != 0 {
		mode |= fs.ModeSetgid
	}
	if raw&01000 != 0 {
		mode |= fs.ModeSticky
	}
	return mode
}

// Txn records the state a fix is about to change. Begin persists the pre-fix
// state before the fix runs (write-ahead), so a fix interrupted half-way can
// still be rolled back; Commit then completes the record with the post-fix
// state.
type Txn struct {
	ctx     *config.ExecContext
	file    string
	fileID  string
	pre     fileState
	kernels []kernelValue
}

type kernelValue struct {
	id     string
	path   string
	before []byte
}

// Begin captures everything the fix may change and writes it to runs.json.
// An error means the pre-fix state is not safely recorded; the caller must
// not run the fix.
func Begin(ctx *config.ExecContext, check config.Check) (*Txn, error) {
	t := &Txn{ctx: ctx}
	now := time.Now().UTC().Format(time.RFC3339)
	base := config.DeltaEntry{RunID: ctx.RunID, Timestamp: now, CheckID: check.ID, Pending: true}
	var entries []config.DeltaEntry

	if isBackupPath(check.AffectedFile) {
		pre, err := captureFile(check.AffectedFile)
		if err != nil {
			return nil, fmt.Errorf("back up %s: %w", check.AffectedFile, err)
		}
		t.file, t.fileID, t.pre = check.AffectedFile, uuid.NewString(), pre
		entry := base
		entry.ID = t.fileID
		entry.FilePath = t.file
		entry.Perm = uint32(pre.perm & backupModeMask)
		entry.Created = !pre.exists
		if pre.exists {
			entry.Checksum = sha256Hex(pre.content)
			entry.Before = base64.StdEncoding.EncodeToString(pre.content)
		}
		entries = append(entries, entry)
	}

	for _, path := range kernelPaths(check) {
		value, err := readFileContent(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("record kernel value %s: %w", path, err)
		}
		kv := kernelValue{id: uuid.NewString(), path: path, before: value}
		t.kernels = append(t.kernels, kv)
		entry := base
		entry.ID = kv.id
		entry.Kind = config.EntryKindSysctl
		entry.FilePath = path
		entry.Checksum = sha256Hex(value)
		entry.Before = base64.StdEncoding.EncodeToString(value)
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return t, nil
	}
	err := updateHistory(ctx.BaseDir, func(runs map[string][]config.DeltaEntry) {
		runs[ctx.RunID] = append(runs[ctx.RunID], entries...)
	})
	if err != nil {
		return nil, fmt.Errorf("write rollback record: %w", err)
	}
	return t, nil
}

// File returns the backed-up file, or "" when the check names none.
func (t *Txn) File() string { return t.file }

// Commit completes the pending records with the state the fix left behind.
// It must run whether or not the fix succeeded, since a failing fix may still
// have changed something. A record that cannot be completed stays pending and
// can still be rolled back from its pre-fix copy.
func (t *Txn) Commit() error {
	if t.fileID == "" && len(t.kernels) == 0 {
		return nil
	}
	// nil drops a record because the fix changed nothing it covers.
	final := make(map[string]*config.DeltaEntry)
	var errs []error

	if t.fileID != "" {
		post, err := captureFile(t.file)
		if err != nil {
			errs = append(errs, fmt.Errorf("read post-fix state of %s: %w", t.file, err))
		} else {
			final[t.fileID] = completeFileEntry(t.pre, post)
		}
	}
	for _, kv := range t.kernels {
		after, err := readFileContent(kv.path)
		if err != nil {
			errs = append(errs, fmt.Errorf("read post-fix kernel value %s: %w", kv.path, err))
			continue
		}
		if string(after) == string(kv.before) {
			final[kv.id] = nil
			continue
		}
		final[kv.id] = &config.DeltaEntry{
			Kind:         config.EntryKindSysctl,
			Checksum:     sha256Hex(kv.before),
			Before:       base64.StdEncoding.EncodeToString(kv.before),
			PostChecksum: sha256Hex(after),
		}
	}

	err := updateHistory(t.ctx.BaseDir, func(runs map[string][]config.DeltaEntry) {
		entries := runs[t.ctx.RunID]
		kept := entries[:0]
		for _, entry := range entries {
			done, ok := final[entry.ID]
			if !ok || entry.ID == "" {
				kept = append(kept, entry)
				continue
			}
			if done == nil {
				continue
			}
			done.ID, done.RunID, done.Timestamp, done.CheckID, done.FilePath =
				entry.ID, entry.RunID, entry.Timestamp, entry.CheckID, entry.FilePath
			kept = append(kept, *done)
		}
		if len(kept) == 0 {
			delete(runs, t.ctx.RunID)
		} else {
			runs[t.ctx.RunID] = kept
		}
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("complete rollback record: %w", err))
	}
	return errors.Join(errs...)
}

func completeFileEntry(pre, post fileState) *config.DeltaEntry {
	switch {
	case !pre.exists && !post.exists:
		return nil
	case !pre.exists:
		return &config.DeltaEntry{
			Created:      true,
			Perm:         uint32(post.perm & backupModeMask),
			PostChecksum: sha256Hex(post.content),
		}
	case !post.exists:
		delta, checksum := ComputeDelta("", string(pre.content))
		return &config.DeltaEntry{
			Removed:  true,
			Delta:    delta,
			Checksum: checksum,
			Perm:     uint32(pre.perm & backupModeMask),
		}
	default:
		delta, checksum := ComputeDelta(string(post.content), string(pre.content))
		return &config.DeltaEntry{
			Delta:        delta,
			Checksum:     checksum,
			Perm:         uint32(pre.perm & backupModeMask),
			PostChecksum: sha256Hex(post.content),
		}
	}
}

// updateHistory applies one read-modify-write to runs.json.
func updateHistory(baseDir string, change func(map[string][]config.DeltaEntry)) error {
	deltaFile := filepath.Join(baseDir, "runs.json")
	runs, err := initializeRuns(deltaFile)
	if err != nil {
		// Never overwrite existing history when reading or decoding it fails.
		return err
	}
	change(runs)
	data, err := jsonIndent(runs)
	if err != nil {
		return fmt.Errorf("encode rollback history: %w", err)
	}
	// Backups may contain sensitive configuration; restrict history access.
	return writeFileAtomic(deltaFile, data, 0600)
}
