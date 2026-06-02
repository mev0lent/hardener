package rollback

import (
	"crypto/sha256"
	"fmt"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// oldText: content BEFORE fix applied | newText: content AFTER fix applied
func ComputeDelta(oldText, newText string) (string, string) {
	dmp := diffmatchpatch.New()

	//ui.PrintInfo("Old text: " + oldText)
	//ui.PrintInfo("New text: " + newText)

	checksum := fmt.Sprintf("%x", sha256.Sum256([]byte(newText)))

	diffs := dmp.DiffMain(oldText, newText, false)
	patches := dmp.PatchMake(oldText, diffs)
	return dmp.PatchToText(patches), checksum // <-- store this string (JSON, etc.)
}

func ApplyRollbackDelta(newText, delta string) (string, error) {
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
