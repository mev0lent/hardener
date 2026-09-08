package rollback

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// A versioned prefix distinguishes byte snapshots from legacy text patches.
// The payload is the source SHA-256, a colon, and the base64-encoded target.
const snapshotPrefix = "hardener-snapshot-v1:"

// ComputeDelta records source -> target and returns the target's checksum.
// For rollback, source is the post-fix content and target is the pre-fix content.
func ComputeDelta(source, target string) (string, string) {
	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(target)))
	if isDiffableText(source) && isDiffableText(target) {
		if delta, ok := computeTextDelta(source, target); ok {
			return delta, checksum
		}
	}

	// Binary plists and other arbitrary bytes must never go through rune-based
	// diffing or JSON string encoding, both of which can replace invalid UTF-8.
	sourceHash := fmt.Sprintf("%x", sha256.Sum256([]byte(source)))
	return snapshotPrefix + sourceHash + ":" + base64.StdEncoding.EncodeToString([]byte(target)), checksum
}

func isDiffableText(content string) bool {
	return utf8.ValidString(content) && !strings.ContainsRune(content, '\x00') && !strings.HasPrefix(content, "bplist00")
}

func computeTextDelta(source, target string) (delta string, ok bool) {
	// Keep a dependency panic local: the caller can still save a lossless
	// snapshot after the fix has already changed the file.
	defer func() {
		if recover() != nil {
			delta, ok = "", false
		}
	}()
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(source, target, false)
	delta = dmp.PatchToText(dmp.PatchMake(source, diffs))
	restored, err := ApplyRollbackDelta(source, delta)
	return delta, err == nil && restored == target
}

func ApplyRollbackDelta(newText, delta string) (oldText string, err error) {
	defer func() {
		if r := recover(); r != nil {
			oldText, err = "", fmt.Errorf("rollback patch panicked: %v", r)
		}
	}()
	if strings.HasPrefix(delta, snapshotPrefix) {
		sourceHash, encoded, ok := strings.Cut(strings.TrimPrefix(delta, snapshotPrefix), ":")
		hashBytes, hashErr := hex.DecodeString(sourceHash)
		if !ok || hashErr != nil || len(hashBytes) != sha256.Size {
			return "", fmt.Errorf("invalid rollback snapshot header")
		}
		target, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("invalid rollback snapshot: %w", err)
		}
		// Whole-file restoration cannot merge edits made since the backup.
		// Allow an already-restored file, but refuse unrelated current bytes.
		currentHash := fmt.Sprintf("%x", sha256.Sum256([]byte(newText)))
		if currentHash != sourceHash && newText != string(target) {
			return "", fmt.Errorf("snapshot source checksum mismatch — file may have changed since backup")
		}
		return string(target), nil
	}

	// Existing runs.json entries contain untagged diff-match-patch text.
	dmp := diffmatchpatch.New()
	patches, err := dmp.PatchFromText(delta)
	if err != nil {
		return "", err
	}
	oldText, results := dmp.PatchApply(patches, newText)
	for i, ok := range results {
		if !ok {
			return "", fmt.Errorf("patch %d of %d failed to apply — file may have changed since backup", i+1, len(results))
		}
	}
	return oldText, nil
}
