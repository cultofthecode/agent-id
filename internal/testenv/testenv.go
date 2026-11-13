// Package testenv isolates a test from the developer's machine.
//
// Every test that runs aidentity code, git, or gh calls Isolate first. Isolate
// points HOME, the XDG directories, the global Git configuration, the gh
// configuration directory, and TMPDIR into a fresh directory under
// $TMPDIR/agent-id/, limits git to local and HTTPS transports, and removes every inherited credential and identity
// variable, so that a test can never read or write ~/.gitconfig, the keychain,
// the ghapp or gh configuration, or a real token. Isolate also routes every
// HTTP request that leaves 127.0.0.1 to a guard that fails the test.
//
// On top of Isolate, the package provides the fakes and process helpers that
// end-to-end tests need: NewFakeAPI for a host API, NewGitServer for Git over
// HTTPS, NewBareRepository and Clone for repositories, BuildBinary for
// executables, NewHTTPClient for in-process clients, and Main for the check
// that a test run left the developer's configuration unchanged. Only test
// files and the test helper packages import this package; depguard enforces
// it.
package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RootDirName is the directory under the system temporary directory that
// holds every isolated test environment of agent-id.
const RootDirName = "agent-id"

// inheritedVariables are removed from the environment of an isolated test:
// credentials, identity, and configuration that would let a test reach the
// developer's accounts or change how Git and gh behave.
var inheritedVariables = []string{
	"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "GH_REPO",
	"GIT_ASKPASS", "SSH_ASKPASS", "SSH_AUTH_SOCK", "GIT_SSH", "GIT_SSH_COMMAND",
	"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM",
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE",
	"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE",
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE",
	"GIT_SSL_NO_VERIFY", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH", "GIT_PROXY_COMMAND",
	"GHAPP_APP_ID", "GHAPP_INSTALLATION_ID", "GHAPP_PRIVATE_KEY_PATH",
	"CLAUDE_ENV_FILE",
}

// allowedGitProtocols are the only transports git may use in an isolated
// test: local paths and HTTPS through the proxy guard. ssh reads the real
// ~/.ssh and the SSH agent no matter what HOME says, and git:// bypasses the
// proxy, so neither may run.
const allowedGitProtocols = "file:https"

// inheritedPrefixes are variable name prefixes removed from the environment
// of an isolated test.
var inheritedPrefixes = []string{"AGENT_ID_", "AIDENTITY_", "GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_", "GHAPP_"}

// Env describes the isolated environment of one test.
type Env struct {
	Root        string
	Home        string
	ConfigHome  string
	CacheHome   string
	StateHome   string
	DataHome    string
	GitConfig   string
	GHConfigDir string
	TempDir     string
}

// Isolate gives the test a fresh environment under $TMPDIR/agent-id/ and
// removes it when the test ends. It also points the proxy variables at a
// guard that fails the test when git, gh, or another child process tries to
// reach the network. A test that calls Isolate cannot run in
// parallel, because it changes the process environment through t.Setenv.
func Isolate(t testing.TB) Env {
	t.Helper()
	env := newEnv(t, rootDir(t))
	for _, dir := range []string{env.Home, env.ConfigHome, env.CacheHome, env.StateHome, env.DataHome, env.GHConfigDir, env.TempDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("testenv: create %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(env.GitConfig, nil, 0o600); err != nil {
		t.Fatalf("testenv: create %s: %v", env.GitConfig, err)
	}
	removeInherited(t)
	setVariables(t, env)
	guardNetwork(t)
	return env
}

// rootDir creates a unique directory under $TMPDIR/agent-id/ and registers
// its removal.
func rootDir(t testing.TB) string {
	t.Helper()
	base := filepath.Join(os.TempDir(), RootDirName)
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("testenv: create %s: %v", base, err)
	}
	root, err := os.MkdirTemp(base, "test-")
	if err != nil {
		t.Fatalf("testenv: create a directory under %s: %v", base, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// newEnv returns the layout of an isolated environment under root.
func newEnv(t testing.TB, root string) Env {
	t.Helper()
	home := filepath.Join(root, "home")
	config := filepath.Join(home, ".config")
	return Env{
		Root:        root,
		Home:        home,
		ConfigHome:  config,
		CacheHome:   filepath.Join(home, ".cache"),
		StateHome:   filepath.Join(home, ".local", "state"),
		DataHome:    filepath.Join(home, ".local", "share"),
		GitConfig:   filepath.Join(home, ".gitconfig"),
		GHConfigDir: filepath.Join(config, "gh"),
		TempDir:     filepath.Join(root, "tmp"),
	}
}

// removeInherited unsets every inherited credential, identity, and
// configuration variable for the duration of the test.
func removeInherited(t testing.TB) {
	t.Helper()
	names := append([]string(nil), inheritedVariables...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		for _, prefix := range inheritedPrefixes {
			if strings.HasPrefix(name, prefix) {
				names = append(names, name)
			}
		}
	}
	for _, name := range names {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("testenv: unset %s: %v", name, err)
		}
	}
}

// setVariables points the process environment at env.
func setVariables(t testing.TB, env Env) {
	t.Helper()
	vars := map[string]string{
		"HOME":                  env.Home,
		"XDG_CONFIG_HOME":       env.ConfigHome,
		"XDG_CACHE_HOME":        env.CacheHome,
		"XDG_STATE_HOME":        env.StateHome,
		"XDG_DATA_HOME":         env.DataHome,
		"GIT_CONFIG_GLOBAL":     env.GitConfig,
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_ALLOW_PROTOCOL":    allowedGitProtocols,
		"GIT_TERMINAL_PROMPT":   "0",
		"GCM_INTERACTIVE":       "never",
		"GH_CONFIG_DIR":         env.GHConfigDir,
		"GH_PROMPT_DISABLED":    "1",
		"GH_NO_UPDATE_NOTIFIER": "1",
		"TMPDIR":                env.TempDir,
	}
	for name, value := range vars {
		t.Setenv(name, value)
	}
}
