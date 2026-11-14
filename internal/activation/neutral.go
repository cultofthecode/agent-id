package activation

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

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
// a symlink or a file of another user.
func ensureFile(path, content string, mode os.FileMode) error {
	want := []byte(content)
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return failf(ErrNeutralDir, "%s: %v", path, errText(err))
	case !info.Mode().IsRegular():
		return neutralErr(path, "is a symlink or not a regular file", "remove it")
	default:
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
			return neutralErr(path, "is owned by another user", "remove it")
		}
		got, err := os.ReadFile(path)
		if err == nil && bytes.Equal(got, want) && info.Mode().Perm() == mode {
			return nil
		}
	}
	return writeFile(path, want, mode)
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
