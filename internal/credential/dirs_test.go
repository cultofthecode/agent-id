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

func TestChainOfCoversEveryHopOfASymlinkChain(t *testing.T) {
	env := testenv.Isolate(t)
	real := filepath.Join(env.Root, "real", "app.pem")
	writeKey(t, real, pkcs1PEM(t), 0o600)
	hop := filepath.Join(env.Root, "shared", "hop.pem")
	if err := os.MkdirAll(filepath.Dir(hop), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, hop); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.Home, "app.pem")
	if err := os.Symlink(hop, link); err != nil {
		t.Fatal(err)
	}
	chain := chainOf(link)
	for _, want := range []string{env.Home, filepath.Dir(hop), filepath.Dir(real)} {
		if !contains(chain, want) {
			t.Errorf("chain %v lacks %s", chain, want)
		}
	}
}

func TestSymlinkHopsStopsOnALoop(t *testing.T) {
	env := testenv.Isolate(t)
	a, b := filepath.Join(env.Home, "a"), filepath.Join(env.Home, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}
	if got := len(symlinkHops(a)); got != maxSymlinkHops {
		t.Errorf("hops = %d, want the limit %d", got, maxSymlinkHops)
	}
}

func TestDisplayPathHidesASymlinkedHome(t *testing.T) {
	env := testenv.Isolate(t)
	real := filepath.Join(env.Root, "realhome")
	if err := os.MkdirAll(filepath.Join(real, "keys"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.Root, "linkhome")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	if got := displayPath(filepath.Join(resolved, "keys", "app.pem"), link); got != "~/keys/app.pem" {
		t.Errorf("displayPath = %q, want ~/keys/app.pem", got)
	}
}

func writeRelativeLinkBehindDirectorySymlink(t *testing.T, root string) (alias string) {
	t.Helper()
	writeKey(t, filepath.Join(root, "real", "other", "app.pem"), pkcs1PEM(t), 0o600)
	if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../other/app.pem", filepath.Join(root, "real", "sub", "link")); err != nil {
		t.Fatal(err)
	}
	alias = filepath.Join(root, "alias")
	if err := os.Symlink(filepath.Join(root, "real", "sub"), alias); err != nil {
		t.Fatal(err)
	}
	return alias
}

func TestSymlinkHopsResolvesTheDirectoryOfARelativeLink(t *testing.T) {
	env := testenv.Isolate(t)
	alias := writeRelativeLinkBehindDirectorySymlink(t, env.Root)
	hops := symlinkHops(filepath.Join(alias, "link"))
	realRoot, err := filepath.EvalSymlinks(env.Root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realRoot, "real", "other", "app.pem")
	if len(hops) == 0 || hops[len(hops)-1] != want {
		t.Errorf("hops = %v, want [%s]", hops, want)
	}
}

func TestFileAcceptsRelativeLinkBehindDirectorySymlink(t *testing.T) {
	env := testenv.Isolate(t)
	alias := writeRelativeLinkBehindDirectorySymlink(t, env.Root)
	key, err := File{Path: filepath.Join(alias, "link")}.PrivateKeyPEM()
	if err != nil || key == nil {
		t.Fatalf("a valid key behind a symlinked directory was refused: %v", err)
	}
}

func TestFileRejectsKeyBehindNestedDirectorySymlinks(t *testing.T) {
	env := testenv.Isolate(t)
	shared := filepath.Join(env.Root, "s1")
	mid := filepath.Join(shared, "mid")
	final := filepath.Join(env.Root, "s2", "final")
	writeKey(t, filepath.Join(final, "app.pem"), pkcs1PEM(t), 0o600)
	if err := os.MkdirAll(mid, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(final, filepath.Join(mid, "inner")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mid, filepath.Join(env.Home, "keys")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.Home, "keys", "inner", "app.pem")
	if !chainReaches(t, chainOf(path), shared) {
		t.Errorf("chain lacks the directory above a symlinked directory")
	}
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0777", "chmod go-w")
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func chainReaches(t *testing.T, chain []string, want string) bool {
	t.Helper()
	resolved := mustEval(t, want)
	for _, dir := range chain {
		if got, err := filepath.EvalSymlinks(dir); err == nil && got == resolved {
			return true
		}
	}
	return false
}

func writeDotDotProbe(t *testing.T, root, home, linkText string) (path, open string) {
	t.Helper()
	writeKey(t, filepath.Join(root, "safe", "app.pem"), pkcs1PEM(t), 0o600)
	open = filepath.Join(root, "w", "a")
	if err := os.MkdirAll(filepath.Join(open, "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "safe", "app.pem"), filepath.Join(open, "x")); err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(home, "keys")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(open, "b"), filepath.Join(keys, "sub2")); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(keys, "link")
	if err := os.Symlink(linkText, path); err != nil {
		t.Fatal(err)
	}
	return path, open
}

func TestChainOfCoversDotDotAfterASymlinkedComponent(t *testing.T) {
	env := testenv.Isolate(t)
	path, open := writeDotDotProbe(t, env.Root, env.Home, "sub2/../x")
	if !chainReaches(t, chainOf(path), open) {
		t.Errorf("chain lacks the directory that holds the link after dot-dot")
	}
}

func TestFileRejectsDotDotAfterASymlinkedComponent(t *testing.T) {
	env := testenv.Isolate(t)
	path, _ := writeDotDotProbe(t, env.Root, env.Home, "sub2/../x")
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0777", "chmod go-w")
}

func TestFileRejectsAbsoluteLinkWithDotDotAfterASymlinkedComponent(t *testing.T) {
	env := testenv.Isolate(t)
	text := env.Home + "/keys/sub2/../x"
	path, _ := writeDotDotProbe(t, env.Root, env.Home, text)
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0777", "chmod go-w")
}

func TestLinkTarget(t *testing.T) {
	env := testenv.Isolate(t)
	root := mustEval(t, env.Root)
	if err := os.MkdirAll(filepath.Join(root, "real", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real", "deep"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	from := filepath.Join(root, "link")
	cases := map[string]string{
		"alias/../x":                            filepath.Join(root, "real", "x"),
		"./alias/./../x":                        filepath.Join(root, "real", "x"),
		"alias/../../x":                         filepath.Join(root, "x"),
		"missing/../x":                          filepath.Join(root, "x"),
		"../../../../../../../../../../../../x": "/x",
		root + "/alias/../x":                    filepath.Join(root, "real", "x"),
	}
	for link, want := range cases {
		if got, _ := linkTarget(from, link); got != want {
			t.Errorf("linkTarget(%q) = %q, want %q", link, got, want)
		}
	}
}

func TestFileRejectsUnsafeDirectoryBehindASymlinkBeforeDotDot(t *testing.T) {
	env := testenv.Isolate(t)
	writeKey(t, filepath.Join(env.Root, "safe", "app.pem"), pkcs1PEM(t), 0o600)
	if err := os.MkdirAll(filepath.Join(env.Root, "safe", "b"), 0o700); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(env.Root, "evil")
	if err := os.MkdirAll(evil, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(evil, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(env.Root, "safe", "b"), filepath.Join(evil, "s")); err != nil {
		t.Fatal(err)
	}
	keys := filepath.Join(env.Home, "keys")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(evil, "s"), filepath.Join(keys, "sub2")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(keys, "link")
	if err := os.Symlink("sub2/../app.pem", path); err != nil {
		t.Fatal(err)
	}
	if !chainReaches(t, chainOf(path), evil) {
		t.Errorf("chain lacks the directory that decides where sub2 points")
	}
	key, err := File{Path: path}.PrivateKeyPEM()
	assertRejected(t, key, err, ErrInsecureKeyFile, "mode 0777", "chmod go-w")
}
