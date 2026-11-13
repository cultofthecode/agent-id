package profile

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestResolveExample(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	store := NewStore(configPath(t), acmeRegistry(t))
	got, err := store.Resolve("claude")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Name != "claude" || got.Provider != "acme" || got.Host != acmeHost || string(got.Settings) != `{"id":123456,"key":"file:~/.config/agent-id/keys/claude.pem"}` {
		t.Errorf("Resolve(claude) = %+v (settings %s)", got, got.Settings)
	}
	if _, err := os.Stat(path + ".lock"); !os.IsNotExist(err) {
		t.Errorf("a read created the lock file: %v", err)
	}
}

func TestResolveFailures(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	store := NewStore(path, acmeRegistry(t))
	_, err := store.Resolve("writer")
	checkError(t, err, ErrProfileNotFound, `profile "writer" not found in `+path+"; available: claude, gl-bot, reviewer")
	_, err = store.Resolve("")
	checkError(t, err, ErrProfileNotFound, `profile "" not found`)
	_, err = store.Resolve("gl-bot")
	checkError(t, err, ErrUnsupportedProvider, `profile "gl-bot": provider "gitlab" is not supported by this agent-id (supported: acme); upgrade agent-id or fix the provider name`)
	for _, name := range []string{"claude", "reviewer"} {
		if _, err := store.Resolve(name); err != nil {
			t.Errorf("Resolve(%q) next to an unsupported profile: %v", name, err)
		}
	}
	_, err = NewStore(path, nil).Resolve("claude")
	checkError(t, err, ErrUnsupportedProvider, "(supported: none)")

	writeFile(t, env, profileDoc(""), 0o600)
	_, err = store.Resolve("claude")
	checkError(t, err, ErrProfileNotFound, "no profiles configured")
}

func TestLoadNotFound(t *testing.T) {
	env := testenv.Isolate(t)
	path := configPath(t)
	_, err := NewStore(path, acmeRegistry(t)).Load()
	checkError(t, err, ErrConfigNotFound, "no agent-id configuration at "+path+"; run agent-id setup acme --profile <name>")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = NewStore(path, nil).Resolve("claude")
	checkError(t, err, ErrConfigNotFound, "run agent-id setup <provider> --profile <name>")
	_, err = NewStore("relative/config.json", nil).Load()
	checkError(t, err, ErrConfigNotFound, "is not absolute")
	if _, err := os.Stat(filepath.Join(env.ConfigHome, DirName, FileName)); !os.IsNotExist(err) {
		t.Errorf("Load created the file: %v", err)
	}
}

func TestLoadWrapsErrorsWithThePath(t *testing.T) {
	env := testenv.Isolate(t)
	secret := acmePrefix + "S3cr3tV4lue"
	path := writeFile(t, env, profileDoc(`"claude": {"provider": "acme", "host": "git.acme.test", "acme": {"id": 1, "key": "`+secret+`"}}`), 0o600)
	_, err := NewStore(path, acmeRegistry(t)).Resolve("claude")
	checkError(t, err, ErrInlineSecret, "config "+path+": profiles.claude.acme.key: contains what looks like a token or private key")
	if strings.Contains(err.Error(), "S3cr3t") {
		t.Errorf("error %q echoes the secret", err)
	}
	writeFile(t, env, `{"version": 2, "profiles": {}}`, 0o600)
	_, err = NewStore(path, acmeRegistry(t)).Load()
	checkError(t, err, ErrUnsupportedVersion, "config "+path+": version 2 is newer")
}

func TestLoadPermissions(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, env testenv.Env, path string)
		want    error
		msg     string
	}{
		{"file 0600", func(*testing.T, testenv.Env, string) {}, nil, ""},
		{"file 0644 readable by others", func(t *testing.T, _ testenv.Env, path string) { chmod(t, path, 0o644) }, nil, ""},
		{"file 0400", func(t *testing.T, _ testenv.Env, path string) { chmod(t, path, 0o400) }, nil, ""},
		{"file 0664", func(t *testing.T, _ testenv.Env, path string) { chmod(t, path, 0o664) }, ErrInsecurePermissions, "writable by group or others (mode 0664); run chmod 600 "},
		{"file 0602", func(t *testing.T, _ testenv.Env, path string) { chmod(t, path, 0o602) }, ErrInsecurePermissions, "mode 0602"},
		{"directory 0777", func(t *testing.T, _ testenv.Env, path string) { chmod(t, filepath.Dir(path), 0o777) }, ErrInsecurePermissions, "directory " + "%DIR%" + " is writable by group or others (mode 0777); run chmod 700 %DIR%"},
		{"directory 0750", func(t *testing.T, _ testenv.Env, path string) { chmod(t, filepath.Dir(path), 0o750) }, nil, ""},
		{"symlink to a group-writable target", func(t *testing.T, env testenv.Env, path string) {
			target := filepath.Join(env.Home, "dotfiles", "config.json")
			moveAndLink(t, path, target)
			chmod(t, target, 0o660)
		}, ErrInsecurePermissions, "mode 0660"},
		{"symlink into a writable directory", func(t *testing.T, env testenv.Env, path string) {
			target := filepath.Join(env.Home, "dotfiles", "config.json")
			moveAndLink(t, path, target)
			chmod(t, filepath.Dir(target), 0o777)
		}, ErrInsecurePermissions, "is writable by group or others (mode 0777)"},
		{"symlink chain through a writable directory", func(t *testing.T, env testenv.Env, path string) {
			final := filepath.Join(env.Home, "final", "config.json")
			middle := filepath.Join(env.Home, "open", "link.json")
			moveAndLink(t, path, final)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(middle), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(final, middle); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../../open/link.json", path); err != nil {
				t.Fatal(err)
			}
			chmod(t, filepath.Dir(middle), 0o777)
		}, ErrInsecurePermissions, "open is writable by group or others (mode 0777)"},
		{"symlink to a safe target", func(t *testing.T, env testenv.Env, path string) {
			moveAndLink(t, path, filepath.Join(env.Home, "dotfiles", "config.json"))
		}, nil, ""},
		{"a directory instead of a file", func(t *testing.T, _ testenv.Env, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, ErrConfigInvalid, "not a regular file"},
		{"a FIFO instead of a file", func(t *testing.T, _ testenv.Env, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, ErrConfigInvalid, "not a regular file"},
		{"a file instead of the directory", func(t *testing.T, env testenv.Env, path string) {
			dir := filepath.Dir(path)
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, ErrConfigInvalid, "is not a directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testenv.Isolate(t)
			path := writeFile(t, env, exampleConfig, 0o600)
			tt.prepare(t, env, path)
			_, err := NewStore(path, acmeRegistry(t)).Resolve("claude")
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				return
			}
			checkError(t, err, tt.want, "config "+path+": ", strings.ReplaceAll(tt.msg, "%DIR%", filepath.Dir(path)))
		})
	}
}

func TestSecureRejectsAnotherOwner(t *testing.T) {
	testenv.Isolate(t)
	other := fakeInfo{mode: 0o600, sys: &syscall.Stat_t{Uid: uint32(os.Getuid() + 1)}}
	checkError(t, secure(other, "", "600 x"), ErrInsecurePermissions, "not owned by the current user")
	checkError(t, secure(fakeInfo{mode: 0o700 | fs.ModeDir, sys: &syscall.Stat_t{Uid: uint32(os.Getuid() + 1)}}, "directory d is ", "700 d"), ErrInsecurePermissions, "directory d is not owned by the current user")
	own := fakeInfo{mode: 0o600, sys: &syscall.Stat_t{Uid: uint32(os.Getuid())}}
	if err := secure(own, "", "600 x"); err != nil {
		t.Errorf("secure(own file) = %v", err)
	}
}

// fakeInfo is a fs.FileInfo with a chosen mode and owner.
type fakeInfo struct {
	mode fs.FileMode
	sys  any
}

func (f fakeInfo) Name() string       { return "config.json" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return f.sys }

// chmod sets the mode of path.
func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// moveAndLink moves the file at path to target, in a new mode 0700
// directory, and puts a symlink to it at path.
func moveAndLink(t *testing.T, path, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
