package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

// addAcme returns a change that adds an acme profile named name.
func addAcme(name string, id int) func(*Config) error {
	return func(c *Config) error {
		c.Profiles[name] = Profile{Name: name, Provider: "acme", Host: acmeHost, Settings: json.RawMessage(fmt.Sprintf(`{"id":%d,"key":"file:/keys/%s.pem"}`, id, name))}
		return nil
	}
}

func TestWriteNewFile(t *testing.T) {
	testenv.Isolate(t)
	path := configPath(t)
	if err := NewStore(path, acmeRegistry(t)).Write(addAcme("claude", 123456)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want := `{
  "version": 1,
  "profiles": {
    "claude": {
      "provider": "acme",
      "host": "git.acme.test",
      "acme": {
        "id": 123456,
        "key": "file:/keys/claude.pem"
      }
    }
  }
}
`
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file = %q, %v; want %q", data, err, want)
	}
	checkMode(t, path, 0o600)
	checkMode(t, filepath.Dir(path), 0o700)
	checkEntries(t, filepath.Dir(path), FileName, FileName+".lock")
	if _, err := NewStore(path, acmeRegistry(t)).Resolve("claude"); err != nil {
		t.Errorf("Resolve after Write: %v", err)
	}
}

func TestWriteKeepsUnregisteredProfiles(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	store := NewStore(path, acmeRegistry(t))
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Write(addAcme("claude", 42)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(after.Profiles["gl-bot"].Settings); got != glBotSettings {
		t.Errorf("gl-bot settings after a write = %s, want %s", got, glBotSettings)
	}
	if !reflect.DeepEqual(after.Profiles["gl-bot"], before.Profiles["gl-bot"]) || !reflect.DeepEqual(after.Profiles["reviewer"], before.Profiles["reviewer"]) {
		t.Errorf("untouched profiles changed: %+v", after.Profiles)
	}
	if got := string(after.Profiles["claude"].Settings); got != `{"id":42,"key":"file:/keys/claude.pem"}` {
		t.Errorf("claude settings = %s", got)
	}
	var doc map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	glBot := doc["profiles"].(map[string]any)["gl-bot"].(map[string]any)
	if glBot["provider"] != "gitlab" || glBot["host"] != "gitlab.example" || glBot["gitlab"].(map[string]any)["url"] != "<a&b>" {
		t.Errorf("gl-bot in the file = %v", glBot)
	}
}

func TestWriteRefusesAndKeepsTheFile(t *testing.T) {
	errChange := errors.New("change failed")
	tests := []struct {
		name    string
		initial string
		change  func(*Config) error
		want    error
		msg     string
	}{
		{"invalid host", exampleConfig, func(c *Config) error {
			p := c.Profiles["claude"]
			p.Host = "https://git.acme.test"
			c.Profiles["claude"] = p
			return nil
		}, ErrUnsupportedHost, "profiles.claude.host"},
		{"adapter rejects the block", exampleConfig, func(c *Config) error {
			p := c.Profiles["claude"]
			p.Settings = json.RawMessage(`{"id":1,"key":"keyring:x"}`)
			c.Profiles["claude"] = p
			return nil
		}, ErrUnsupportedKeyReference, "keyring references are reserved"},
		{"inline secret", exampleConfig, func(c *Config) error {
			p := c.Profiles["claude"]
			p.Settings = json.RawMessage(`{"id":1,"key":"file:/k.pem","x":"` + acmePrefix + `S3cr3t"}`)
			c.Profiles["claude"] = p
			return nil
		}, ErrInlineSecret, "profiles.claude.acme.x"},
		{"settings not JSON", exampleConfig, func(c *Config) error {
			p := c.Profiles["claude"]
			p.Settings = json.RawMessage(`{`)
			c.Profiles["claude"] = p
			return nil
		}, ErrConfigInvalid, "a provider block is not valid JSON"},
		{"name does not match the key", exampleConfig, func(c *Config) error {
			c.Profiles["other"] = c.Profiles["claude"]
			return nil
		}, ErrConfigInvalid, `profiles.other: the profile is named "claude"`},
		{"invalid profile name", exampleConfig, addAcme("Bad", 1), ErrConfigInvalid, "invalid profile name"},
		{"wrong version", exampleConfig, func(c *Config) error { c.Version = 2; return nil }, ErrUnsupportedVersion, "newer"},
		{"change fails", exampleConfig, func(*Config) error { return errChange }, errChange, "change failed"},
		{"existing file invalid", `{"version": 1, "profiles": {}, "x": 1}`, addAcme("claude", 1), ErrConfigInvalid, `unknown member "x"`},
		{"existing file newer", `{"version": 2, "profiles": {}}`, addAcme("claude", 1), ErrUnsupportedVersion, "upgrade agent-id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testenv.Isolate(t)
			path := writeFile(t, env, tt.initial, 0o600)
			err := NewStore(path, acmeRegistry(t)).Write(tt.change)
			checkError(t, err, tt.want, tt.msg)
			if data, _ := os.ReadFile(path); string(data) != tt.initial {
				t.Errorf("the file changed after a refused write: %s", data)
			}
			checkEntries(t, filepath.Dir(path), FileName, FileName+".lock")
		})
	}
}

func TestWriteKeepsTheFileWhenAValidatorReportsNotFound(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, profileDoc(`"p": {"provider": "class", "host": "class.test", "class": {}}, `+acmeProfile("other")), 0o600)
	before, _ := os.ReadFile(path)
	registry, err := NewRegistry(Registration{Name: "class", Validator: classValidator{class: ErrConfigNotFound}})
	if err != nil {
		t.Fatal(err)
	}
	err = NewStore(path, registry).Write(addAcme("claude", 1))
	checkError(t, err, ErrConfigInvalid, "key file not found")
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Errorf("the file changed: %s", after)
	}
}

func TestWriteRefusesADanglingSymlink(t *testing.T) {
	env := testenv.Isolate(t)
	path := configPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(env.Home, "dotfiles", "config.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	err := NewStore(path, acmeRegistry(t)).Write(addAcme("claude", 1))
	checkError(t, err, ErrConfigInvalid, "is a symlink whose target cannot be resolved")
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the dangling symlink was replaced: %v", err)
	}
}

func TestWriteRestrictsTheDirectory(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	chmod(t, filepath.Dir(path), 0o755)
	if err := NewStore(path, acmeRegistry(t)).Write(addAcme("claude", 1)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	checkMode(t, filepath.Dir(path), 0o700)
}

func TestRefusedWriteLeavesTheDirectoryMode(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	chmod(t, filepath.Dir(path), 0o755)
	err := NewStore(path, acmeRegistry(t)).Write(addAcme("Bad", 1))
	checkError(t, err, ErrConfigInvalid, "invalid profile name")
	checkMode(t, filepath.Dir(path), 0o755)
}

func TestWriteRefusesAnInsecureDirectory(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	chmod(t, filepath.Dir(path), 0o770)
	err := NewStore(path, acmeRegistry(t)).Write(addAcme("claude", 1))
	checkError(t, err, ErrInsecurePermissions, "chmod 700")
	err = NewStore("relative/config.json", acmeRegistry(t)).Write(addAcme("claude", 1))
	checkError(t, err, ErrConfigNotFound, "is not absolute")
}

func TestWriteConcurrentWritersKeepEveryProfile(t *testing.T) {
	testenv.Isolate(t)
	path := configPath(t)
	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- NewStore(path, acmeRegistry(t)).Write(addAcme(fmt.Sprintf("agent-%d", i), i+1))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Write: %v", err)
		}
	}
	config, err := NewStore(path, acmeRegistry(t)).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Profiles) != writers {
		t.Errorf("profiles after concurrent writes = %v, want %d", config.Names(), writers)
	}
	checkEntries(t, filepath.Dir(path), FileName, FileName+".lock")
}

func TestWriteReplacesTheTargetOfASymlink(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	target := filepath.Join(env.Home, "dotfiles", "config.json")
	moveAndLink(t, path, target)
	if err := NewStore(path, acmeRegistry(t)).Write(addAcme("writer", 7)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v", err)
	}
	if _, err := NewStore(target, acmeRegistry(t)).Resolve("writer"); err != nil {
		t.Errorf("Resolve in the target: %v", err)
	}
	checkMode(t, target, 0o600)
	checkEntries(t, filepath.Dir(target), "config.json", "config.json.lock")
	checkEntries(t, filepath.Dir(path), FileName)
}

func TestWriteThroughASymlinkTakesTheLockOfTheTarget(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	target := filepath.Join(env.Home, "dotfiles", "config.json")
	moveAndLink(t, path, target)
	unlock, err := lock(target + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- NewStore(path, acmeRegistry(t)).Write(addAcme("writer", 7)) }()
	select {
	case err := <-done:
		unlock()
		t.Fatalf("Write through the symlink returned (%v) while the target's lock was held", err)
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Write did not finish after the lock was released")
	}
	if _, err := NewStore(target, acmeRegistry(t)).Resolve("writer"); err != nil {
		t.Errorf("Resolve in the target: %v", err)
	}
}

func TestWriteRefusesASymlinkThatChangesUnderTheLock(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	first := filepath.Join(env.Home, "dotfiles", "config.json")
	moveAndLink(t, path, first)
	second := filepath.Join(env.Home, "other", "config.json")
	if err := os.MkdirAll(filepath.Dir(second), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(exampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	repoint := func(c *Config) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Symlink(second, path); err != nil {
			return err
		}
		return addAcme("writer", 7)(c)
	}
	err := NewStore(path, acmeRegistry(t)).Write(repoint)
	checkError(t, err, ErrConfigInvalid, "the symlink changed during the write", "nothing was written")
	for _, file := range []string{first, second} {
		if data, err := os.ReadFile(file); err != nil || string(data) != exampleConfig {
			t.Errorf("%s changed: %q, %v", file, data, err)
		}
	}
	checkEntries(t, filepath.Dir(second), "config.json")
}

func TestWriteReportsAFailedDirectorySyncAsApplied(t *testing.T) {
	env := testenv.Isolate(t)
	path := writeFile(t, env, exampleConfig, 0o600)
	original := syncDir
	t.Cleanup(func() { syncDir = original })
	syncDir = func(string) error { return errors.New("injected sync failure") }
	err := NewStore(path, acmeRegistry(t)).Write(addAcme("writer", 7))
	checkError(t, err, ErrConfigInvalid, "the change was written, but it may not survive a crash", "injected sync failure")
	if strings.Contains(err.Error(), "cannot write:") {
		t.Errorf("error %q says the write failed", err)
	}
	if _, err := NewStore(path, acmeRegistry(t)).Resolve("writer"); err != nil {
		t.Errorf("Resolve after a write with a failed directory sync: %v", err)
	}
	checkEntries(t, filepath.Dir(path), FileName, FileName+".lock")
}

func TestWriteErrorOfAFailedRename(t *testing.T) {
	err := writeError(errors.New("rename failed"))
	checkError(t, err, ErrConfigInvalid, "cannot write: rename failed")
	if strings.Contains(err.Error(), "the change was written") {
		t.Errorf("error %q says the change was applied", err)
	}
	if writeError(nil) != nil {
		t.Error("writeError(nil) != nil")
	}
}

// checkMode fails the test unless path has permission bits mode.
func checkMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != mode {
		t.Errorf("mode of %s = %04o, want %04o", path, got, mode)
	}
}

// checkEntries fails the test unless dir holds exactly names, so that no
// temporary file remains.
func checkEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	slices.Sort(names)
	if !slices.Equal(got, names) {
		t.Errorf("entries of %s = %v, want %v", dir, got, names)
	}
}
