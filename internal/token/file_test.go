package token

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestCheckOwnedDir(t *testing.T) {
	newManager(t, newClock())
	const uid, other = 501, 502
	tests := []struct {
		name   string
		mode   fs.FileMode
		owner  uint32
		check  func(string, fs.FileMode, uint32, uint32) error
		accept bool
	}{
		{"private cache dir", fs.ModeDir | 0o700, uid, checkDir, true},
		{"cache dir 0750", fs.ModeDir | 0o750, uid, checkDir, false},
		{"cache dir 0705", fs.ModeDir | 0o705, uid, checkDir, false},
		{"cache dir 0777", fs.ModeDir | 0o777, uid, checkDir, false},
		{"cache dir of another user", fs.ModeDir | 0o700, other, checkDir, false},
		{"cache dir symlink", fs.ModeSymlink | 0o777, uid, checkDir, false},
		{"cache dir is a file", 0o600, uid, checkDir, false},
		{"parent 0700", fs.ModeDir | 0o700, uid, checkParent, true},
		{"parent 0755", fs.ModeDir | 0o755, uid, checkParent, true},
		{"parent 0777", fs.ModeDir | 0o777, uid, checkParent, false},
		{"parent 0757", fs.ModeDir | 0o757, uid, checkParent, false},
		{"parent 0775", fs.ModeDir | 0o775, uid, checkParent, false},
		{"parent of another user", fs.ModeDir | 0o700, other, checkParent, false},
		{"parent symlink", fs.ModeSymlink | 0o755, uid, checkParent, false},
	}
	for _, tt := range tests {
		err := tt.check("/cache/agent-id/tokens", tt.mode, tt.owner, uid)
		if tt.accept != (err == nil) || (err != nil && !errors.Is(err, ErrInsecureCache)) {
			t.Errorf("%s: %v, want accept=%v or ErrInsecureCache", tt.name, err, tt.accept)
		}
	}
}

func TestUsableEntryFile(t *testing.T) {
	newManager(t, newClock())
	const uid = 501
	tests := []struct {
		name  string
		mode  fs.FileMode
		owner uint32
		size  int64
		want  bool
	}{
		{"0600", 0o600, uid, 100, true},
		{"0400", 0o400, uid, 100, false},
		{"0644", 0o644, uid, 100, false},
		{"0660", 0o660, uid, 100, false},
		{"setuid", fs.ModeSetuid | 0o600, uid, 100, false},
		{"other owner", 0o600, 502, 100, false},
		{"too large", 0o600, uid, maxEntrySize + 1, false},
		{"FIFO", fs.ModeNamedPipe | 0o600, uid, 0, false},
		{"directory", fs.ModeDir | 0o600, uid, 0, false},
	}
	for _, tt := range tests {
		if got := usableEntryFile(tt.mode, tt.owner, uid, tt.size); got != tt.want {
			t.Errorf("%s: usableEntryFile = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestWriteEntryIsPrivateAndAtomic(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	seed(t, m, src, testenv.FakeToken("one"), c.In(time.Hour))
	seed(t, m, src, testenv.FakeToken("two"), c.In(time.Hour))
	info, err := os.Stat(entryPath(t, m, src))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("entry mode = %v, %v; want 0600", info.Mode(), err)
	}
	assertOnlyEntries(t, m.Dir)
	token, ok := readEntry(openDir(t, m), entryName(t, src), mustKey(t, src), uint32(os.Getuid()))
	if !ok || token.Value != testenv.FakeToken("two") {
		t.Error("readEntry does not return the last written token")
	}
}

func TestReadEntryRefusesUnsafeFiles(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	path := entryPath(t, m, src)
	key := mustKey(t, src)
	uid := uint32(os.Getuid())
	seed(t, m, src, testenv.FakeToken("loose"), c.In(time.Hour))
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := readEntry(openDir(t, m), filepath.Base(path), key, uid); ok {
		t.Error("readEntry accepted a 0644 entry")
	}
	target := filepath.Join(t.TempDir(), "target.json")
	if err := os.WriteFile(target, encodeEntry(key, provider.Token{Value: testenv.FakeToken("target"), ExpiresAt: c.In(time.Hour)}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, ok := readEntry(openDir(t, m), filepath.Base(path), key, uid); ok {
		t.Error("readEntry followed a symbolic link")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { _, ok := readEntry(openDir(t, m), filepath.Base(path), key, uid); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Error("readEntry accepted a FIFO")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readEntry blocked on a FIFO")
	}
}

func TestWriteEntryFailsInAReadOnlyDirectory(t *testing.T) {
	skipAsRoot(t)
	c := newClock()
	m := newManager(t, c)
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.Dir, 0o700) })
	src := newSource(c)
	err := writeEntry(openDir(t, m), entryName(t, src), encodeEntry(mustKey(t, src), provider.Token{Value: testenv.FakeToken("ro"), ExpiresAt: c.In(time.Hour)}))
	if !errors.Is(err, ErrCacheWrite) {
		t.Errorf("writeEntry in a read-only directory: %v, want ErrCacheWrite", err)
	}
	assertNoToken(t, err, testenv.FakeToken("ro"))
	assertOnlyEntries(t, m.Dir)
}

func TestLockFileRefusesASymlink(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lock := lockPath(entryPath(t, m, newSource(c)))
	if err := os.Symlink(filepath.Join(t.TempDir(), "elsewhere"), lock); err != nil {
		t.Fatal(err)
	}
	if _, err := lockFile(context.Background(), openDir(t, m), filepath.Base(lock), time.Second); !errors.Is(err, ErrInsecureCache) {
		t.Errorf("lockFile on a symlink: %v, want ErrInsecureCache", err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(lock, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockFile(context.Background(), openDir(t, m), filepath.Base(lock), time.Second); !errors.Is(err, ErrInsecureCache) {
		t.Errorf("lockFile on a FIFO: %v, want ErrInsecureCache", err)
	}
}

func TestLockFileInAReadOnlyDirectory(t *testing.T) {
	skipAsRoot(t)
	c := newClock()
	m := newManager(t, c)
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(m.Dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.Dir, 0o700) })
	if _, err := lockFile(context.Background(), openDir(t, m), "x"+lockSuffix, time.Second); !errors.Is(err, ErrCacheWrite) {
		t.Errorf("lockFile in a read-only directory: %v, want ErrCacheWrite", err)
	}
}

// assertOnlyEntries fails the test when dir holds a temporary file.
func assertOnlyEntries(t *testing.T, dir string) {
	t.Helper()
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !strings.HasSuffix(name.Name(), entrySuffix) && !strings.HasSuffix(name.Name(), lockSuffix) {
			t.Errorf("unexpected file %s left in the cache directory", name.Name())
		}
	}
}

// swappingSource is a test source that, when it mints, moves the cache
// directory to moved and puts a replacement at its path: a new directory or a
// symbolic link to elsewhere.
type swappingSource struct {
	*source
	dir, moved, elsewhere string
}

func (s *swappingSource) Token(ctx context.Context) (provider.Token, error) {
	if err := os.Rename(s.dir, s.moved); err != nil {
		return provider.Token{}, err
	}
	if s.elsewhere != "" {
		if err := os.Symlink(s.elsewhere, s.dir); err != nil {
			return provider.Token{}, err
		}
	} else if err := os.Mkdir(s.dir, 0o700); err != nil {
		return provider.Token{}, err
	}
	return s.source.Token(ctx)
}

func TestTokenWritesOnlyIntoTheCheckedDirectory(t *testing.T) {
	for _, replacement := range []string{"directory", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			c := newClock()
			m := newManager(t, c)
			src := newSource(c)
			swap := &swappingSource{source: src, dir: m.Dir, moved: m.Dir + ".checked"}
			if replacement == "symlink" {
				swap.elsewhere = filepath.Join(t.TempDir(), "elsewhere")
				mkdir(t, swap.elsewhere)
			}
			token, err := m.Token(context.Background(), swap)
			if err != nil {
				t.Fatalf("Token: %v", err)
			}
			if names, err := os.ReadDir(m.Dir); err != nil || len(names) != 0 {
				t.Errorf("the replacement %s holds %v (%v), want nothing", replacement, names, err)
			}
			root, err := os.OpenRoot(swap.moved)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = root.Close() })
			cached, ok := readEntry(root, entryName(t, src), mustKey(t, src), uint32(os.Getuid()))
			if !ok || cached != token {
				t.Errorf("the checked directory does not hold the minted token")
			}
			assertOnlyEntries(t, swap.moved)
		})
	}
}

func TestReadEntryRefusesASymlinkInsideTheDirectory(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	seed(t, m, src, testenv.FakeToken("real"), c.In(time.Hour))
	name := entryName(t, src)
	if err := os.Rename(filepath.Join(m.Dir, name), filepath.Join(m.Dir, "other"+entrySuffix)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other"+entrySuffix, filepath.Join(m.Dir, name)); err != nil {
		t.Fatal(err)
	}
	if _, ok := readEntry(openDir(t, m), name, mustKey(t, src), uint32(os.Getuid())); ok {
		t.Error("readEntry followed a relative symbolic link inside the cache directory")
	}
}

func TestLockFileRefusesASymlinkInsideTheDirectory(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	root := openDir(t, m)
	lock := lockPath(entryName(t, newSource(c)))
	if err := os.WriteFile(filepath.Join(m.Dir, "other"+lockSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("other"+lockSuffix, filepath.Join(m.Dir, lock)); err != nil {
		t.Fatal(err)
	}
	if _, err := lockFile(context.Background(), root, lock, time.Second); !errors.Is(err, ErrInsecureCache) {
		t.Errorf("lockFile on a relative symlink inside the cache directory: %v, want ErrInsecureCache", err)
	}
}

func TestCheckRootRefusesAnotherDirectory(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	root := openDir(t, m)
	other := filepath.Join(t.TempDir(), "other")
	mkdir(t, other)
	otherInfo, err := os.Lstat(other)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := os.Lstat(m.Dir)
	if err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	if err := checkRoot(root, m.Dir, uid, checked); err != nil {
		t.Errorf("checkRoot of the checked directory: %v", err)
	}
	if err := checkRoot(root, m.Dir, uid, otherInfo); !errors.Is(err, ErrInsecureCache) || !strings.Contains(err.Error(), "was replaced") {
		t.Errorf("checkRoot of a replaced directory: %v, want ErrInsecureCache saying it was replaced", err)
	}
	if err := os.Chmod(m.Dir, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(m.Dir, 0o700) })
	if err := checkRoot(root, m.Dir, uid, checked); !errors.Is(err, ErrInsecureCache) {
		t.Errorf("checkRoot of a 0750 directory: %v, want ErrInsecureCache", err)
	}
}
