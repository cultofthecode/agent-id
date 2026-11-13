package credential

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestCheckDirsAcceptsSafeChains(t *testing.T) {
	chain := []dirState{
		{path: "~/keys", mode: fs.ModeDir | 0o700, owner: 501, ownerKnown: true},
		{path: "~", mode: fs.ModeDir | 0o755, owner: 501, ownerKnown: true},
		{path: "/Users", mode: fs.ModeDir | 0o755, owner: 0, ownerKnown: true},
		{path: "/tmp", mode: fs.ModeDir | fs.ModeSticky | 0o777, owner: 0, ownerKnown: true},
		{path: "/", mode: fs.ModeDir | 0o755, owner: 0, ownerKnown: true},
	}
	if err := checkDirs(chain, 501); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDirsRejectsUnsafeDirectories(t *testing.T) {
	cases := map[string]struct {
		dir  dirState
		want []string
	}{
		"group-writable": {
			dirState{path: "~/keys", mode: fs.ModeDir | 0o770, owner: 501, ownerKnown: true},
			[]string{"directory ~/keys:", "mode 0770", "run chmod go-w ~/keys"},
		},
		"world-writable": {
			dirState{path: "/srv/keys", mode: fs.ModeDir | 0o777, owner: 0, ownerKnown: true},
			[]string{"mode 0777", "run chmod go-w /srv/keys"},
		},
		"group-writable only": {
			dirState{path: "/srv/keys", mode: fs.ModeDir | 0o720, owner: 501, ownerKnown: true},
			[]string{"mode 0720"},
		},
		"other owner": {
			dirState{path: "/srv/keys", mode: fs.ModeDir | 0o755, owner: 1001, ownerKnown: true},
			[]string{"owned by uid 1001", "not by you (uid 501) or root"},
		},
		"sticky but other owner": {
			dirState{path: "/srv/keys", mode: fs.ModeDir | fs.ModeSticky | 0o777, owner: 1001, ownerKnown: true},
			[]string{"owned by uid 1001"},
		},
		"unknown owner": {
			dirState{path: "/srv/keys", mode: fs.ModeDir | 0o755},
			[]string{"owner of this parent directory"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := checkDirs([]dirState{tc.dir}, 501)
			if !errors.Is(err, ErrInsecureKeyFile) {
				t.Fatalf("error = %v, want %v", err, ErrInsecureKeyFile)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error = %q, want it to contain %q", err, w)
				}
			}
		})
	}
}

func TestDisplayPath(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/home/ann/keys/app.pem", "/home/ann", "~/keys/app.pem"},
		{"/home/ann", "/home/ann", "~"},
		{"/home/annex/app.pem", "/home/ann", "/home/annex/app.pem"},
		{"/srv/app.pem", "/home/ann", "/srv/app.pem"},
		{"/home/ann/app.pem", "", "/home/ann/app.pem"},
		{"/home/ann/app.pem", "relative", "/home/ann/app.pem"},
		{"", "/home/ann", ""},
	}
	for _, tc := range cases {
		if got := displayPath(tc.path, tc.home); got != tc.want {
			t.Errorf("displayPath(%q, %q) = %q, want %q", tc.path, tc.home, got, tc.want)
		}
	}
}

func TestChainOfCoversSymlinkTargets(t *testing.T) {
	env := testenv.Isolate(t)
	target := filepath.Join(env.Root, "dotfiles", "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o600)
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	chain := chainOf(link)
	resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{env.Home, resolvedDir, "/"} {
		if !contains(chain, want) {
			t.Errorf("chain %v lacks %s", chain, want)
		}
	}
	seen := map[string]bool{}
	for _, dir := range chain {
		if seen[dir] {
			t.Errorf("chain lists %s twice", dir)
		}
		seen[dir] = true
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestFileRejectsKeyInWritableDirectory(t *testing.T) {
	env := testenv.Isolate(t)
	dir := filepath.Join(env.Home, "keys")
	path := filepath.Join(dir, "app.pem")
	writeKey(t, path, pkcs1PEM(t), 0o600)
	for _, mode := range []fs.FileMode{0o770, 0o707, 0o777} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		key, err := File{Path: path}.PrivateKeyPEM()
		assertRejected(t, key, err, ErrInsecureKeyFile, "directory ~/keys:", "run chmod go-w ~/keys")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if key, err := (File{Path: path}).PrivateKeyPEM(); err != nil || key == nil {
		t.Fatalf("after chmod go-w: %v", err)
	}
}

func TestFileRejectsKeyBehindSymlinkIntoWritableDirectory(t *testing.T) {
	env := testenv.Isolate(t)
	shared := filepath.Join(env.Root, "shared")
	target := filepath.Join(shared, "app.pem")
	writeKey(t, target, pkcs1PEM(t), 0o600)
	if err := os.Chmod(shared, 0o775); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: link}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0775", "chmod go-w")
}

func TestFileRejectsKeyBehindWritableDirectorySymlink(t *testing.T) {
	env := testenv.Isolate(t)
	real := filepath.Join(env.Root, "real")
	writeKey(t, filepath.Join(real, "app.pem"), pkcs1PEM(t), 0o600)
	if err := os.Chmod(real, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(env.Home, "keys")); err != nil {
		t.Fatal(err)
	}
	key, err := File{Path: filepath.Join(env.Home, "keys", "app.pem")}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0777", "chmod go-w")
}
