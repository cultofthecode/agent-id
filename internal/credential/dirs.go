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

const maxSymlinkHops = 40

// symlinkHops follows the symlink chain that starts at path, one hop at a
// time. The directory of a relative link is resolved through EvalSymlinks
// before the join, so a symlinked directory on the way does not produce a hop
// that does not exist. When that directory cannot be resolved, the textual
// directory stays and the later directory check fails closed on it.
func symlinkHops(path string) []string {
	var hops []string
	for current := path; len(hops) < maxSymlinkHops; {
		link, err := os.Readlink(current)
		if err != nil {
			break
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(resolvedDir(current), link)
		}
		current = filepath.Clean(link)
		hops = append(hops, current)
	}
	return hops
}

func resolvedDir(path string) string {
	dir := filepath.Dir(path)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// chainOf lists every directory whose owner or mode decides who can replace
// the key file: the directories above the path, above every symlink hop, and
// above the target of every symlinked directory on the way. The visited set
// ends a symlink loop.
func chainOf(path string) []string {
	var chain []string
	visited := map[string]bool{}
	queue := []string{path}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		chain = append(chain, ancestors(current)...)
		queue = append(queue, symlinkHopsOfPrefixes(current)...)
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		chain = append(chain, ancestors(resolved)...)
	}
	return unique(chain)
}

func symlinkHopsOfPrefixes(path string) []string {
	var hops []string
	for prefix := path; ; prefix = filepath.Dir(prefix) {
		if info, err := os.Lstat(prefix); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			hops = append(hops, symlinkHops(prefix)...)
		}
		if prefix == filepath.Dir(prefix) {
			return hops
		}
	}
}

func unique(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	out := paths[:0]
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
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
	if shown, ok := underHome(path, filepath.Clean(home)); ok {
		return shown
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		if shown, ok := underHome(path, resolved); ok {
			return shown
		}
	}
	return path
}

func underHome(path, home string) (string, bool) {
	if path == home {
		return "~", true
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~/" + rest, true
	}
	return "", false
}
