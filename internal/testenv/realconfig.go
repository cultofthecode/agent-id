package testenv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Main runs the tests of a package and fails the run when they changed the
// developer's Git or gh configuration: ~/.gitconfig, ~/.config/git, and the
// gh configuration directory. Call it from TestMain:
//
//	func TestMain(m *testing.M) { testenv.Main(m) }
//
// Main only hashes these files; it never prints their contents.
func Main(m *testing.M) {
	paths := personalConfigPaths(os.Getenv)
	before := snapshot(paths)
	code := m.Run()
	if changed := changedPaths(before, snapshot(paths)); len(changed) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "testenv: the tests changed the developer's configuration: %s\n", strings.Join(changed, ", "))
		code = 1
	}
	os.Exit(code)
}

// personalConfigPaths returns the Git and gh configuration of the developer
// that getenv describes, before any test isolates the environment.
func personalConfigPaths(getenv func(string) string) []string {
	home := getenv("HOME")
	configHome := getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	paths := []string{
		filepath.Join(home, ".gitconfig"),
		filepath.Join(configHome, "git"),
		filepath.Join(configHome, "gh"),
		filepath.Join(home, ".config", "gh"),
	}
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GH_CONFIG_DIR"} {
		if value := getenv(name); value != "" {
			paths = append(paths, value)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// snapshot returns a fingerprint of every file, directory, and symbolic link
// under paths, keyed by path. A missing path is recorded as absent.
func snapshot(paths []string) map[string]string {
	out := make(map[string]string)
	for _, root := range paths {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				out[path] = "absent"
				return nil
			}
			if err != nil {
				out[path] = "unreadable"
				return nil
			}
			out[path] = fingerprint(path, entry)
			return nil
		})
		if err != nil {
			out[root] = "unreadable"
		}
	}
	return out
}

// fingerprint describes one entry: its type and mode, and the hash of a
// file's content or a link's target.
func fingerprint(path string, entry fs.DirEntry) string {
	info, err := entry.Info()
	if err != nil {
		return "absent"
	}
	switch {
	case info.IsDir():
		return "dir " + info.Mode().String()
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "link unreadable"
		}
		return "link " + target
	default:
		return "file " + info.Mode().String() + " " + hashFile(path)
	}
}

// hashFile returns the SHA-256 of the content of the file at path.
func hashFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "unreadable"
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "unreadable"
	}
	return hex.EncodeToString(h.Sum(nil))
}

// changedPaths returns, sorted, every path whose fingerprint differs between
// before and after, including paths that appeared or disappeared.
func changedPaths(before, after map[string]string) []string {
	var changed []string
	for path, want := range before {
		if after[path] != want {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}
