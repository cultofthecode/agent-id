package activation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ResolveBinary returns the absolute path to record as AGENT_ID_BIN. It
// prefers the path through which agent-id was started (a stable symlink such
// as /opt/homebrew/bin/agent-id) when it is the same file as the running
// binary, and refuses a path that a Git configuration value could not carry.
func ResolveBinary(arg0, pathEnv string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", failf(ErrBinaryPath, "%v", err)
	}
	return resolveBinary(exe, arg0, pathEnv)
}

func resolveBinary(exe, arg0, pathEnv string) (string, error) {
	chosen := exe
	if candidate := startedPath(arg0, pathEnv); candidate != "" && sameFile(candidate, exe) {
		chosen = candidate
	}
	chosen = filepath.Clean(chosen)
	if !filepath.IsAbs(chosen) {
		return "", failf(ErrBinaryPath, "the path is not absolute")
	}
	if !utf8.ValidString(chosen) || strings.IndexFunc(chosen, unicode.IsControl) >= 0 {
		return "", failf(ErrBinaryPath, "the path has a control character or is not UTF-8")
	}
	return chosen, nil
}

// startedPath resolves arg0 to an absolute path, directly or through PATH.
func startedPath(arg0, pathEnv string) string {
	if arg0 == "" {
		return ""
	}
	if strings.Contains(arg0, "/") {
		abs, err := filepath.Abs(arg0)
		if err != nil {
			return ""
		}
		return abs
	}
	for _, dir := range strings.Split(pathEnv, ":") {
		if !filepath.IsAbs(dir) {
			continue
		}
		if p := filepath.Join(dir, arg0); isExecutable(p) {
			return p
		}
	}
	return ""
}

func sameFile(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

// isExecutable reports whether path is a regular file with an execute bit.
func isExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// LookPath finds name using the PATH of env, not the PATH of this process. A
// name with a slash is used as given. A match through a relative PATH element
// is refused, so a command never runs from the working directory by accident.
func LookPath(env []string, name string) (string, error) {
	if name == "" {
		return "", failf(ErrCommandNotFound, "%q: command not found", name)
	}
	if strings.Contains(name, "/") {
		return lookDirect(name)
	}
	relative := false
	for _, dir := range strings.Split(parseEnv(env)["PATH"], ":") {
		candidate := filepath.Join(dir, name)
		if dir == "" {
			candidate = name
		}
		if !isExecutable(candidate) {
			continue
		}
		if !filepath.IsAbs(dir) {
			relative = true
			continue
		}
		return candidate, nil
	}
	if relative {
		return "", failf(ErrCommandNotFound, "%q resolves to a relative path through an empty or \".\" PATH entry; use ./%s", name, name)
	}
	return "", failf(ErrCommandNotFound, "%q: command not found", name)
}

func lookDirect(name string) (string, error) {
	info, err := os.Stat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return "", failf(ErrCommandNotFound, "%q: no such file or directory", name)
	}
	if err != nil {
		return "", failf(ErrCommandNotExec, "%q: %s", name, "permission denied")
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", failf(ErrCommandNotExec, "%q: permission denied", name)
	}
	return name, nil
}
