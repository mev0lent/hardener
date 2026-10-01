package rollback

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"hardener/internal/config"
	"hardener/internal/ui"
)

// procSys is a variable so tests can point it at a temporary directory.
var procSys = "/proc/sys"

var (
	procSysPattern   = regexp.MustCompile(`/proc/sys/[A-Za-z0-9_./-]*[A-Za-z0-9_]`)
	sysctlKeyPattern = regexp.MustCompile(`\bsysctl\s+(?:-[A-Za-z]+\s+)*([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)+)`)
)

// kernelPaths finds the live kernel parameters a check reads or a fix sets,
// e.g. `cat /proc/sys/kernel/dmesg_restrict` or `sysctl -w kernel.x=1`.
// Restoring the sysctl.d file alone does not undo `sysctl -w`: `sysctl
// --system` only applies the keys that some file still sets, so a value the
// fix introduced stays active until reboot unless it is recorded here.
func kernelPaths(check config.Check) []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	seen := make(map[string]bool)
	var paths []string
	add := func(rel string) {
		path := filepath.Join(procSys, filepath.Clean("/"+rel))
		if seen[path] {
			return
		}
		seen[path] = true
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}
	}
	for _, text := range []string{check.Command, check.Fix} {
		for _, match := range procSysPattern.FindAllString(text, -1) {
			add(strings.TrimPrefix(match, "/proc/sys/"))
		}
		for _, match := range sysctlKeyPattern.FindAllStringSubmatch(text, -1) {
			add(strings.ReplaceAll(match[1], ".", "/"))
		}
	}
	return paths
}

func restoreKernelValue(entry config.DeltaEntry) error {
	before, err := base64.StdEncoding.DecodeString(entry.Before)
	if err != nil {
		return fmt.Errorf("invalid recorded kernel value for %s: %w", entry.FilePath, err)
	}
	current, err := readFileContent(entry.FilePath)
	if err != nil {
		return fmt.Errorf("read kernel value %s: %w", entry.FilePath, err)
	}
	if bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(before)) {
		return nil
	}
	err = os.WriteFile(entry.FilePath, before, 0)
	if errors.Is(err, os.ErrPermission) {
		cmd := exec.Command("sudo", "tee", entry.FilePath)
		cmd.Stdin = bytes.NewReader(before)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if runErr := cmd.Run(); runErr != nil {
			err = fmt.Errorf("sudo write: %w (%s)", runErr, strings.TrimSpace(stderr.String()))
		} else {
			err = nil
		}
	}
	if err != nil {
		return fmt.Errorf("restore kernel value %s: %w", entry.FilePath, err)
	}
	ui.PrintInfo(fmt.Sprintf("restored live kernel value %s = %s",
		strings.TrimPrefix(entry.FilePath, procSys+"/"), strings.TrimSpace(string(before))))
	return nil
}
