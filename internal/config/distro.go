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
	id, _ := readOsRelease()
	if id != "" {
		return id
	}
	return runtime.GOOS
}

// DetectDistroFamily returns the family (or families) the current distro
// belongs to. It reads /etc/os-release, first honoring ID_LIKE, then falling
// back to a hand-maintained map from ID to family. The returned slice is
// ordered from most-specific to least-specific, e.g. ["ubuntu", "debian"] on
// Ubuntu, ["rocky", "rhel", "fedora"] on Rocky Linux.
//
// Family names used by rulesets:
//
//	debian  — Debian, Ubuntu, Kali, Linux Mint, ...
//	rhel    — Rocky, AlmaLinux, RHEL, Oracle Linux, CentOS Stream, ...
//	fedora  — Fedora (also included in the rhel chain)
//	suse    — openSUSE Leap/Tumbleweed, SLES
//	arch    — Arch, Manjaro, EndeavourOS
//
// The list always starts with the concrete distro ID so `ResolveForDistro`
// can try exact matches first.
func DetectDistroFamily() []string {
	id, likes := readOsRelease()
	if id == "" {
		return []string{runtime.GOOS}
	}

	chain := []string{id}
	for _, l := range likes {
		chain = appendUnique(chain, l)
	}
	// Ensure a canonical family is present even when ID_LIKE is missing.
	for _, fam := range knownFamily(id) {
		chain = appendUnique(chain, fam)
	}
	return chain
}

// readOsRelease returns (ID, ID_LIKE tokens). Both are lowercased and stripped.
func readOsRelease() (string, []string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", nil
	}
	var id string
	var likes []string
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "ID="):
			id = normalizeOSReleaseValue(strings.TrimPrefix(line, "ID="))
		case strings.HasPrefix(line, "ID_LIKE="):
			raw := normalizeOSReleaseValue(strings.TrimPrefix(line, "ID_LIKE="))
			for _, tok := range strings.Fields(raw) {
				if tok != "" {
					likes = append(likes, tok)
				}
			}
		}
	}
	return id, likes
}

func normalizeOSReleaseValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	return strings.ToLower(v)
}

// knownFamily maps a distro ID to its canonical family chain, used when
// /etc/os-release is missing ID_LIKE (some minimal Arch/openSUSE images).
func knownFamily(id string) []string {
	switch id {
	case "ubuntu", "kali", "linuxmint", "mint", "pop", "elementary", "zorin":
		return []string{"debian"}
	case "rocky", "almalinux", "ol", "oracle", "centos":
		return []string{"rhel", "fedora"}
	case "rhel", "redhat":
		return []string{"fedora"}
	case "fedora":
		return []string{"rhel"}
	case "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles", "sled":
		return []string{"suse"}
	case "manjaro", "endeavouros", "artix", "garuda":
		return []string{"arch"}
	}
	return nil
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}
