package credential

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const groupOrOtherWrite fs.FileMode = 0o022

type dirState struct {
	path       string
	mode       fs.FileMode
	owner      uint32
	ownerKnown bool
}

func checkDirs(dirs []dirState, user uint32) error {
	for _, dir := range dirs {
		if err := checkDir(dir, user); err != nil {
			return err
		}
	}
	return nil
}

func checkDir(dir dirState, user uint32) error {
	if !dir.ownerKnown {
		return fmt.Errorf("directory %s: %w: the owner of this parent directory of the key file cannot be determined",
			dir.path, ErrInsecureKeyFile)
	}
	if dir.owner != user && dir.owner != 0 {
		return fmt.Errorf("directory %s: %w: this parent directory of the key file is owned by uid %d, not by you (uid %d) or root; keep the key under a directory you own",
			dir.path, ErrInsecureKeyFile, dir.owner, user)
	}
	if dir.mode.Perm()&groupOrOtherWrite != 0 && dir.mode&fs.ModeSticky == 0 {
		return fmt.Errorf("directory %s: %w: this parent directory of the key file is writable by group or others (mode %04o), so another user could replace the key; run chmod go-w %s",
			dir.path, ErrInsecureKeyFile, dir.mode.Perm(), dir.path)
	}
	return nil
}

func ancestors(path string) []string {
	var out []string
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		out = append(out, dir)
		if dir == filepath.Dir(dir) {
			return out
		}
	}
}

func chainOf(path string) []string {
	chain := ancestors(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		chain = append(chain, ancestors(resolved)...)
	}
	seen := make(map[string]bool, len(chain))
	unique := chain[:0]
	for _, dir := range chain {
		if !seen[dir] {
			seen[dir] = true
			unique = append(unique, dir)
		}
	}
	return unique
}

func inspectDirs(path, home string) ([]dirState, error) {
	var states []dirState
	for _, dir := range chainOf(path) {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("directory %s: %w: %w; the parent directories of the key file must be readable",
				displayPath(dir, home), ErrKeyUnreadable, unwrapPathError(err))
		}
		state := dirState{path: displayPath(dir, home), mode: info.Mode()}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			state.owner = stat.Uid
			state.ownerKnown = true
		}
		states = append(states, state)
	}
	return states, nil
}

func displayPath(path, home string) string {
	if path == "" || !filepath.IsAbs(home) {
		return path
	}
	home = filepath.Clean(home)
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest
	}
	return path
}
