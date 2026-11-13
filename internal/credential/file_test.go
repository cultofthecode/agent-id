package credential

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

var _ Source = File{}

func TestMain(m *testing.M) {
	testenv.Main(m)
}

func writeKey(t *testing.T, path string, data []byte, mode fs.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestFileLoadsProtectedKey(t *testing.T) {
	env := testenv.Isolate(t)
	cases := map[string]struct {
		data func(*testing.T) []byte
		mode fs.FileMode
	}{
		"pkcs1.pem":    {pkcs1PEM, 0o600},
		"pkcs8.pem":    {pkcs8PEM, 0o600},
		"readonly.pem": {pkcs1PEM, 0o400},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(env.Home, "keys", name)
			writeKey(t, path, tc.data(t), tc.mode)
			key, err := File{Path: path}.PrivateKeyPEM()
			if err != nil {
				t.Fatalf("PrivateKeyPEM: %v", err)
			}
			assertPEMMatches(t, key)
		})
	}
}

func TestFileExpandsHome(t *testing.T) {
	env := testenv.Isolate(t)
	writeKey(t, filepath.Join(env.Home, ".config", "agent-id", "keys", "app.pem"), pkcs1PEM(t), 0o600)
	key, err := File{Path: "~/.config/agent-id/keys/app.pem"}.PrivateKeyPEM()
	if err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	assertPEMMatches(t, key)
}

func TestFileFollowsSymlinkToProtectedKey(t *testing.T) {
	env := testenv.Isolate(t)
	target := filepath.Join(env.Root, "dotfiles", "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o600)
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: link}.PrivateKeyPEM()
	if err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	assertPEMMatches(t, key)
}

func TestFileRejectsWorldReadableKey(t *testing.T) {
	env := testenv.Isolate(t)
	path := filepath.Join(env.Home, "app.pem")
	writeKey(t, path, pkcs1PEM(t), 0o644)
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "private key ~/app.pem:", "mode 0644", "run chmod 600 ~/app.pem")
}

func TestFileRejectsSymlinkToWorldReadableKey(t *testing.T) {
	env := testenv.Isolate(t)
	target := filepath.Join(env.Root, "dotfiles", "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o644)
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: link}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "private key ~/app.pem (symlink to "+resolved+")", "run chmod 600 "+resolved)
}

func TestFileRejectsChainedSymlinkToWorldReadableKey(t *testing.T) {
	env := testenv.Isolate(t)
	target := filepath.Join(env.Root, "dotfiles", "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o604)
	middle := filepath.Join(env.Root, "middle.pem")
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, middle); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(middle, link); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: link}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0604")
}

func TestFileRejectsMissingKey(t *testing.T) {
	env := testenv.Isolate(t)
	missing := filepath.Join(env.Home, "missing.pem")
	dangling := filepath.Join(env.Home, "dangling.pem")
	if err := os.Symlink(filepath.Join(env.Root, "nowhere.pem"), dangling); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{missing, dangling} {
		key, err := File{Path: path}.PrivateKeyPEM()
		assertRejected(t, key, err, ErrKeyNotFound, "private key ~/"+filepath.Base(path), "mode 0600")
	}
}

func TestFileRejectsIrregularFiles(t *testing.T) {
	env := testenv.Isolate(t)
	dir := filepath.Join(env.Home, "keys")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(env.Home, "fifo.pem")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{dir: "directory", fifo: "named pipe", "/dev/null": "device"} {
		key, err := loadWithin(t, path, 10*time.Second)
		assertRejected(t, key, err, ErrInsecureKeyFile, "not a regular file ("+want+")")
	}
}

func TestFileRejectsUnreadableKey(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 0000 file")
	}
	env := testenv.Isolate(t)
	path := filepath.Join(env.Home, "app.pem")
	writeKey(t, path, pkcs1PEM(t), 0)
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrKeyUnreadable, "private key ~/app.pem")
}

func TestFileRejectsInvalidContent(t *testing.T) {
	env := testenv.Isolate(t)
	oversized := append(pkcs1PEM(t), make([]byte, maxKeyFileSize)...)
	cases := map[string]struct {
		data []byte
		want string
	}{
		"empty.pem":     {nil, "does not start with a PEM block"},
		"ecdsa.pem":     {ecdsaPKCS8PEM(t), "not an RSA key"},
		"oversized.pem": {oversized, "larger than 64 KiB"},
		"two-keys.pem":  {append(pkcs1PEM(t), pkcs8PEM(t)...), "exactly one key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(env.Home, name)
			writeKey(t, path, tc.data, 0o600)
			key, err := File{Path: path}.PrivateKeyPEM()
			assertRejected(t, key, err, ErrInvalidKey, "private key ~/"+name+":", tc.want)
		})
	}
}

func TestFileRejectsInvalidReference(t *testing.T) {
	env := testenv.Isolate(t)
	writeKey(t, filepath.Join(env.Home, "app.pem"), pkcs1PEM(t), 0o600)
	t.Chdir(env.Home)
	for _, path := range []string{"", "app.pem", "./app.pem", "~", "~/", "~root/app.pem"} {
		key, err := File{Path: path}.PrivateKeyPEM()
		assertRejected(t, key, err, ErrInvalidReference)
	}
	t.Setenv("HOME", "")
	key, err := File{Path: "~/app.pem"}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInvalidReference, "HOME is not set")
}

func TestFileNeverCopiesTheKey(t *testing.T) {
	env := testenv.Isolate(t)
	path := filepath.Join(env.Home, "app.pem")
	writeKey(t, path, pkcs1PEM(t), 0o600)
	before := listTree(t, env.Root)
	if _, err := (File{Path: path}).PrivateKeyPEM(); err != nil {
		t.Fatalf("PrivateKeyPEM: %v", err)
	}
	if after := listTree(t, env.Root); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("loading the key changed the file tree:\nbefore %v\nafter  %v", before, after)
	}
}

func loadWithin(t *testing.T, path string, limit time.Duration) ([]byte, error) {
	t.Helper()
	type result struct {
		key []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		key, err := File{Path: path}.PrivateKeyPEM()
		done <- result{key, err}
	}()
	select {
	case r := <-done:
		return r.key, r.err
	case <-time.After(limit):
		t.Fatalf("loading %s did not return within %s", path, limit)
		return nil, nil
	}
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		entries = append(entries, path+" "+info.Mode().String()+" "+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return entries
}

func assertRejected(t *testing.T, key []byte, err error, sentinel error, want ...string) {
	t.Helper()
	if key != nil {
		t.Fatal("key bytes were returned together with an error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	if home := os.Getenv("HOME"); home != "" && strings.Contains(err.Error(), home) {
		t.Errorf("error = %q, want the home directory shown as ~", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error = %q, want it to contain %q", err, w)
		}
	}
	assertNoKeyMaterial(t, err)
}
