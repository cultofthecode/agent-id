package cliint

import (
	"os"
	"path/filepath"
	"strings"
)

// FindCLI returns the real CLI: the first executable regular file named
// binary in pathEnv. It skips an entry that is not an absolute directory, the
// launcher's own directory, and any file that is the same file as the
// launcher (a link or a hard link to it). It returns errLoop when the only
// candidates were the launcher, and errNoCLI when there was none.
func FindCLI(pathEnv, binary, launcherDir string) (string, error) {
	launcher := filepath.Join(launcherDir, binary)
	launcherInfo, launcherErr := os.Stat(launcher)
	loop := false
	for _, dir := range strings.Split(pathEnv, ":") {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) == filepath.Clean(launcherDir) {
			continue
		}
		candidate := filepath.Join(dir, binary)
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if launcherErr == nil && os.SameFile(info, launcherInfo) {
			loop = true
			continue
		}
		return candidate, nil
	}
	if loop {
		return "", errLoop
	}
	return "", errNoCLI
}
