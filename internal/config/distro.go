package config

import (
	"os"
	"runtime"
	"strings"
)

// DetectDistro reads /etc/os-release and returns the lowercase distro ID
// (e.g. "ubuntu", "debian", "arch", "fedora"). Falls back to runtime.GOOS
// when the file is absent or unparseable (e.g. on macOS or Windows).
func DetectDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, `"`)
			return strings.ToLower(strings.TrimSpace(id))
		}
	}
	return runtime.GOOS
}
