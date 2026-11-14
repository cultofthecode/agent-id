package activation

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// NeutralPaths are the agent-id-owned directories of a provider.
type NeutralPaths struct {
	// ConfigDir is the CLI's empty configuration directory.
	ConfigDir string
	// BinDir holds the launcher of the CLI.
	BinDir string
	// ShellDir holds the shell hook (#135): the zsh wrappers that ZDOTDIR
	// names and the script that BASH_ENV names.
	ShellDir string
	// ClaudeEnvFile is the CLAUDE_ENV_FILE of this exec: a file of its own in
	// ShellDir/sessions/<pid>.<random>/. Ensure leaves it empty. Only
	// SessionNeutral.EnsureSession fills it, for the exec mechanism.
	ClaudeEnvFile string
}

// SessionNeutral is the part of NeutralDirs that makes the per-exec files.
type SessionNeutral interface {
	EnsureSession(parent []string, paths NeutralPaths, cli provider.NeutralCLI) (string, error)
}

// NeutralDirs prepares and checks the neutral directories.
type NeutralDirs interface {
	Ensure(parent []string, providerName string, cli provider.NeutralCLI) (NeutralPaths, error)
}

// FileNeutralDirs is the NeutralDirs of the file system, under the cache
// directory (the token cache rule: $XDG_CACHE_HOME when absolute, otherwise
// $HOME/.cache).
type FileNeutralDirs struct{}

var binaryName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// LauncherScript is the content of the launcher of the CLI named binary. It
// starts "agent-id cli <binary>", which checks the arguments, takes a token,
// and replaces itself with the real CLI (#23, spec
// .ai/specs/2026-11-13-gh-shim.md). It holds no token and no profile. When
// AGENT_ID_BIN is not an executable file, it refuses with exit 125 and never
// starts the real CLI, so a removed binary cannot let the shell fall through
// to the developer's CLI.
func LauncherScript(binary string) string {
	code := "AID-" + strings.ToUpper(binary) + "-TOKEN"
	return "#!/bin/sh\n" +
		"if [ -n \"${AGENT_ID_BIN:-}\" ] && [ -x \"$AGENT_ID_BIN\" ]; then\n" +
		"  exec \"$AGENT_ID_BIN\" cli " + binary + " -- \"$@\"\n" +
		"fi\n" +
		"printf '%s\\n' 'agent-id: " + code + ": " + binary + " was not started because the agent-id binary is not available (AGENT_ID_BIN). Run agent-id doctor outside the agent context.' >&2\n" +
		"exit 125\n"
}

// CacheDir returns the cache directory for the environment parent.
func CacheDir(parent []string) (string, error) {
	env := parseEnv(parent)
	if x := env["XDG_CACHE_HOME"]; filepath.IsAbs(x) {
		return x, nil
	}
	if home := env["HOME"]; filepath.IsAbs(home) {
		return filepath.Join(home, ".cache"), nil
	}
	return "", failf(ErrNeutralDir, "cannot find the cache directory: HOME is not an absolute path")
}

// Ensure implements NeutralDirs.
func (FileNeutralDirs) Ensure(parent []string, providerName string, cli provider.NeutralCLI) (NeutralPaths, error) {
	if !binaryName.MatchString(cli.Binary) || !binaryName.MatchString(providerName) {
		return NeutralPaths{}, failf(ErrNeutralDir, "the provider's CLI or name is not a plain identifier")
	}
	cache, err := CacheDir(parent)
	if err != nil {
		return NeutralPaths{}, err
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return NeutralPaths{}, failf(ErrNeutralDir, "%s: %v", cache, errText(err))
	}
	paths := NeutralPaths{
		ConfigDir: filepath.Join(cache, "agent-id", providerName, "cli-config"),
		BinDir:    filepath.Join(cache, "agent-id", providerName, "neutral-bin"),
		ShellDir:  filepath.Join(cache, "agent-id", providerName, "shell-hook"),
	}
	for _, dir := range []string{filepath.Join(cache, "agent-id"), filepath.Dir(paths.ConfigDir), paths.ConfigDir, paths.BinDir, paths.ShellDir} {
		if err := ensurePrivateDir(dir); err != nil {
			return NeutralPaths{}, err
		}
	}
	for _, name := range cli.ForbiddenConfigFiles {
		if _, err := os.Lstat(filepath.Join(paths.ConfigDir, name)); err == nil {
			return NeutralPaths{}, neutralErr(paths.ConfigDir, "contains "+name, "remove it, and change CLI settings or log in only outside the agent context")
		}
	}
	if err := ensureFile(filepath.Join(paths.BinDir, cli.Binary), LauncherScript(cli.Binary), 0o700); err != nil {
		return NeutralPaths{}, err
	}
	for name, content := range ShellHookFiles(hookNames("git", cli.Binary)) {
		if err := ensureFile(filepath.Join(paths.ShellDir, name), content, 0o600); err != nil {
			return NeutralPaths{}, err
		}
	}
	return paths, nil
}

func neutralErr(path, problem, fix string) error {
	return failf(ErrNeutralDir, "%s: %s; agent-id keeps this directory private and empty of credentials; %s", path, problem, fix)
}

func errText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// ensurePrivateDir creates dir with mode 0700 when it is missing and checks
// it on an opened descriptor: not a symlink, owned by the user, and closed to
// group and others. It never changes the mode of an existing directory.
func ensurePrivateDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return failf(ErrNeutralDir, "%s: %v", dir, errText(err))
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
			return neutralErr(dir, "is a symlink or not a directory", "remove it")
		}
		return failf(ErrNeutralDir, "%s: %v", dir, err)
	}
	defer func() { _ = syscall.Close(fd) }()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return failf(ErrNeutralDir, "%s: %v", dir, err)
	}
	if int(st.Uid) != os.Getuid() {
		return neutralErr(dir, "is owned by another user", "remove it")
	}
	if st.Mode&0o077 != 0 {
		return neutralErr(dir, "is writable or readable by group or others", "run chmod 700 on it")
	}
	return nil
}

// ensureFile makes path a regular, private file with the given content and
// mode, rewriting it atomically when its content or mode differ, and refusing
// a symlink or a file of another user. It opens the file without following a
// symlink and holds the same exclusive flock as the SessionStart hook (#98)
// from the read to the rename, so an append cannot fall between them.
func ensureFile(path, content string, mode os.FileMode) error {
	want := []byte(content)
	f, err := openLocked(path)
	if err != nil {
		return err
	}
	if f == nil {
		return writeFile(path, want, mode)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	got, err := io.ReadAll(f)
	if err == nil && bytes.Equal(got, want) && info.Mode().Perm() == mode {
		return nil
	}
	return writeFile(path, want, mode)
}

// openLocked opens an existing path for reading, locked, and checks the
// opened descriptor. It returns nil without an error when path is missing.
func openLocked(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case errors.Is(err, syscall.ELOOP):
		return nil, neutralErr(path, "is a symlink or not a regular file", "remove it")
	case err != nil:
		return nil, failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, neutralErr(path, "is a symlink or not a regular file", "remove it")
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		_ = f.Close()
		return nil, neutralErr(path, "is owned by another user", "remove it")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, failf(ErrNeutralDir, "%s: cannot lock: %v", path, err)
	}
	return f, nil
}

func writeFile(path string, content []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stub-")
	if err != nil {
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	name := tmp.Name()
	_, werr := tmp.Write(content)
	cerr := tmp.Chmod(mode)
	if err := errors.Join(werr, cerr, tmp.Close()); err != nil {
		_ = os.Remove(name)
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	return nil
}

// sessionMaxAge is the age after which the next exec removes a session
// directory even when its pid is alive, because the system can reuse a pid.
// A session that runs longer than this loses its CLAUDE_ENV_FILE.
const sessionMaxAge = 30 * 24 * time.Hour

// EnsureSession implements SessionNeutral. It removes the stale session
// directories, makes a new private directory sessions/<pid>.<random>/ and
// writes the CLAUDE_ENV_FILE script of this exec into it, and returns the
// path of the script. exec replaces its own process and cannot remove the
// directory at exit, so the next exec does it.
func (FileNeutralDirs) EnsureSession(parent []string, paths NeutralPaths, cli provider.NeutralCLI) (string, error) {
	if !binaryName.MatchString(cli.Binary) {
		return "", failf(ErrNeutralDir, "the provider's CLI is not a plain identifier")
	}
	sessions := filepath.Join(paths.ShellDir, sessionsDir)
	if err := ensurePrivateDir(paths.ShellDir); err != nil {
		return "", err
	}
	if err := ensurePrivateDir(sessions); err != nil {
		return "", err
	}
	sweepSessions(sessions, time.Now(), pidAlive)
	dir, err := newSessionDir(sessions)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, claudeEnvFile)
	script := ClaudeEnvScript(userClaudeEnvFile(parent, paths.ShellDir), hookNames("git", cli.Binary))
	if err := createFile(path, script); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

func newSessionDir(sessions string) (string, error) {
	for range 8 {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", failf(ErrNeutralDir, "%s: %v", sessions, err)
		}
		dir := filepath.Join(sessions, fmt.Sprintf("%d.%x", os.Getpid(), b))
		if err := os.Mkdir(dir, 0o700); errors.Is(err, fs.ErrExist) {
			continue
		} else if err != nil {
			return "", failf(ErrNeutralDir, "%s: %v", dir, errText(err))
		}
		if err := ensurePrivateDir(dir); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
		return dir, nil
	}
	return "", failf(ErrNeutralDir, "%s: cannot find an unused session name", sessions)
}

// createFile creates path as a new private file, never following a symlink
// and never reusing an existing file, and writes content under the lock.
func createFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) || errors.Is(err, syscall.ELOOP) {
			return neutralErr(path, "exists already (a symlink or a file)", "remove it")
		}
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	lerr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	_, werr := f.WriteString(content)
	if err := errors.Join(lerr, werr, f.Close()); err != nil {
		_ = os.Remove(path)
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	}
	return nil
}

// sweepSessions removes the session directories of dead processes and the
// ones older than sessionMaxAge. It removes only real directories of the
// current user whose name is <pid>.<random>, and never follows a symlink. A
// failure leaves the directory for the next exec.
func sweepSessions(sessions string, now time.Time, alive func(pid int) bool) {
	entries, err := os.ReadDir(sessions)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(sessions, e.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
			continue
		}
		pid, ok := sessionPid(e.Name())
		if !ok || (alive(pid) && now.Sub(info.ModTime()) < sessionMaxAge) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}

func sessionPid(name string) (int, bool) {
	head, _, ok := strings.Cut(name, ".")
	pid, err := strconv.Atoi(head)
	return pid, ok && err == nil && pid > 0
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
