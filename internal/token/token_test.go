package token

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

var _ Source = provider.Provider(nil)

func TestCachedTokenValidFor30MinutesMakesNoRequest(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	cached := testenv.FakeToken("cached")
	seed(t, m, src, cached, c.In(30*time.Minute))
	token, err := m.Token(context.Background(), src)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token.Value != cached || len(src.minted) != 0 {
		t.Errorf("Token minted %d tokens, want the cached token and no request", len(src.minted))
	}
}

func TestTokenExpiringIn2MinutesIsRefreshed(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	old := testenv.FakeToken("old")
	seed(t, m, src, old, c.In(2*time.Minute))
	token, err := m.Token(context.Background(), src)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if len(src.minted) != 1 || token != src.minted[0] {
		t.Fatalf("Token minted %d tokens, want one new token", len(src.minted))
	}
	cached, ok := readEntry(entryPath(t, m, src), mustKey(t, src), uint32(os.Getuid()))
	if !ok || cached != token {
		t.Error("the new token is not in the cache")
	}
	again, err := m.Token(context.Background(), src)
	if err != nil || again != token || len(src.minted) != 1 {
		t.Errorf("second Token = %v, %v after %d mints; want the cached token", again, err, len(src.minted))
	}
}

func TestTokenRefreshesAsTheClockPassesTheMargin(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	first := mustManagerToken(t, m, src)
	c.Advance(54 * time.Minute)
	if got := mustManagerToken(t, m, src); got != first {
		t.Error("Token refreshed a token that is valid for six more minutes")
	}
	c.Advance(time.Minute)
	if got := mustManagerToken(t, m, src); got == first || len(src.minted) != 2 {
		t.Errorf("Token did not refresh at the margin: %d mints", len(src.minted))
	}
}

func TestTwoInstallationsNeverShareAnEntry(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	first, second := newSource(c), newSource(c)
	second.subject = "app-1-installation-3"
	a := mustManagerToken(t, m, first)
	b := mustManagerToken(t, m, second)
	if a == b || len(first.minted) != 1 || len(second.minted) != 1 {
		t.Fatalf("two installations got %d and %d mints, want one each and two tokens", len(first.minted), len(second.minted))
	}
	if got := mustManagerToken(t, m, first); got != a {
		t.Error("the first installation got the second installation's token")
	}
	if got := mustManagerToken(t, m, second); got != b {
		t.Error("the second installation got the first installation's token")
	}
	if err := os.Rename(entryPath(t, m, second), entryPath(t, m, first)); err != nil {
		t.Fatal(err)
	}
	if got := mustManagerToken(t, m, first); got == b {
		t.Error("an entry copied from another installation was served")
	}
}

func TestTwoFakeProvidersWithOtherSubjects(t *testing.T) {
	m := newManager(t, newClock())
	m.Now = nil
	first, second := providertest.NewFake(), providertest.NewFake()
	second.SetTokenSubject("fake-installation-2")
	a := mustManagerToken(t, m, first)
	b := mustManagerToken(t, m, second)
	if len(first.Tokens()) != 1 || len(second.Tokens()) != 1 || a != first.Tokens()[0] || b != second.Tokens()[0] {
		t.Error("two subjects did not get one token each")
	}
}

func TestRefreshFailureFailsClosed(t *testing.T) {
	failures := []error{provider.ErrUnauthorized, provider.ErrForbidden, provider.ErrNotFound, provider.ErrUnavailable, provider.ErrRateLimited}
	for _, failure := range failures {
		c := newClock()
		m := newManager(t, c)
		src := newSource(c)
		old := testenv.FakeToken("stale")
		seed(t, m, src, old, c.In(2*time.Minute))
		src.failure = failure
		token, err := m.Token(context.Background(), src)
		if !errors.Is(err, failure) || token != (provider.Token{}) {
			t.Errorf("refresh with %v: %v, %v; want the provider error and the zero token", failure, token, err)
		}
		assertNoToken(t, err, old)
	}
}

func TestNoFallbackToAnInheritedPersonalToken(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	personal := testenv.FakeToken("personal")
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "FAKE_CLI_TOKEN"} {
		t.Setenv(name, personal)
	}
	src := newSource(c)
	src.failure = provider.ErrUnauthorized
	token, err := m.Token(context.Background(), src)
	if !errors.Is(err, provider.ErrUnauthorized) || token != (provider.Token{}) {
		t.Errorf("denied mint with personal tokens in the environment: %v, %v; want the provider error and the zero token", token, err)
	}
	assertNoToken(t, err, personal)
	if _, err := os.Stat(entryPath(t, m, src)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a denied mint wrote an entry")
	}
}

func TestMintedTokenWithinTheMarginIsRefused(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	for _, lifetime := range []time.Duration{-time.Minute, 4 * time.Minute} {
		src.lifetime = lifetime
		token, err := m.Token(context.Background(), src)
		if !errors.Is(err, ErrUnusableToken) || token != (provider.Token{}) {
			t.Errorf("lifetime %v: %v, %v; want ErrUnusableToken", lifetime, token, err)
		}
		for _, minted := range src.minted {
			assertNoToken(t, err, minted.Value)
		}
	}
	if _, err := os.Stat(entryPath(t, m, src)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an unusable token was cached")
	}
	src.lifetime, src.value = time.Hour, "bad\ntoken"
	if _, err := m.Token(context.Background(), src); !errors.Is(err, ErrUnusableToken) {
		t.Errorf("token with a newline: %v, want ErrUnusableToken", err)
	}
}

func TestCorruptEntryIsReplaced(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	path := entryPath(t, m, src)
	for _, content := range []string{`{"version":1`, "garbage", ""} {
		if err := os.MkdirAll(m.Dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		token := mustManagerToken(t, m, src)
		if token != src.minted[len(src.minted)-1] {
			t.Errorf("corrupt entry %q: Token did not mint", content)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("corrupt entry %q: replacement mode %v, %v; want 0600", content, info.Mode(), err)
		}
	}
	seed(t, m, src, testenv.FakeToken("loose"), c.In(time.Hour))
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if token := mustManagerToken(t, m, src); token.Value == testenv.FakeToken("loose") {
		t.Error("a 0644 entry was served")
	}
}

func TestSymlinkAndFIFOEntriesAreReplacedWithoutTouchingTheTarget(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	path := entryPath(t, m, src)
	mkdir(t, m.Dir)
	target := filepath.Join(t.TempDir(), "target.json")
	original := encodeEntry(mustKey(t, src), provider.Token{Value: testenv.FakeToken("target"), ExpiresAt: c.In(time.Hour)})
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if token := mustManagerToken(t, m, src); token.Value == testenv.FakeToken("target") || len(src.minted) != 1 {
		t.Error("Token served the target of a symlinked entry")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != string(original) {
		t.Error("the symlink target was changed")
	}
	assertRegularEntry(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	mustManagerToken(t, m, src)
	if len(src.minted) != 2 {
		t.Errorf("a FIFO entry gave %d mints, want a second mint", len(src.minted))
	}
	assertRegularEntry(t, path)
}

func TestDirectoryInPlaceOfAnEntryFailsWithItsPath(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	path := entryPath(t, m, src)
	mkdir(t, filepath.Join(path, "child"))
	_, err := m.Token(context.Background(), src)
	if !errors.Is(err, ErrCacheWrite) || !strings.Contains(err.Error(), path) {
		t.Errorf("directory entry: %v, want ErrCacheWrite naming %s", err, path)
	}
	for _, minted := range src.minted {
		assertNoToken(t, err, minted.Value)
	}
}

func TestStaleTemporaryFilesAreRemoved(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	mkdir(t, m.Dir)
	stale := entryPath(t, m, src) + ".123456" + tempSuffix
	other := filepath.Join(m.Dir, "other"+entrySuffix+".1"+tempSuffix)
	for _, name := range []string{stale, other} {
		if err := os.WriteFile(name, []byte(testenv.FakeToken("crashed")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustManagerToken(t, m, src)
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Error("a temporary file of the refreshed entry was left behind")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a temporary file of another entry was removed without its lock")
	}
}

func TestFakeProviderDenialFailsClosed(t *testing.T) {
	m := newManager(t, newClock())
	m.Now = nil
	fake := providertest.NewFake()
	old := testenv.FakeToken("stale_fake")
	seed(t, m, fake, old, time.Now().Add(2*time.Minute))
	fake.FailTokens(provider.ErrUnauthorized)
	token, err := m.Token(context.Background(), fake)
	if !errors.Is(err, provider.ErrUnauthorized) || token != (provider.Token{}) {
		t.Errorf("denied refresh on providertest.Fake: %v, %v; want ErrUnauthorized and the zero token", token, err)
	}
	assertNoToken(t, err, old)
	fake.FailTokens(nil)
	if token := mustManagerToken(t, m, fake); token.Value == old {
		t.Error("the stale entry was served after the provider recovered")
	}
}

func TestFreshCacheIsPrivate(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	mustManagerToken(t, m, newSource(c))
	for _, dir := range []string{m.Dir, filepath.Dir(m.Dir)} {
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("%s mode %v, %v; want 0700", dir, info.Mode(), err)
		}
	}
	assertOnlyEntries(t, m.Dir)
}

func TestInsecureCacheDirectoryFailsBeforeAnyRequest(t *testing.T) {
	setups := map[string]func(t *testing.T, dir string){
		"tokens 0755": func(t *testing.T, dir string) { mkdir(t, dir); chmod(t, dir, 0o755) },
		"tokens symlink": func(t *testing.T, dir string) {
			mkdir(t, filepath.Dir(dir))
			target := filepath.Join(t.TempDir(), "elsewhere")
			mkdir(t, target)
			if err := os.Symlink(target, dir); err != nil {
				t.Fatal(err)
			}
		},
		"tokens is a file": func(t *testing.T, dir string) {
			mkdir(t, filepath.Dir(dir))
			if err := os.WriteFile(dir, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"parent 0777": func(t *testing.T, dir string) { mkdir(t, dir); chmod(t, filepath.Dir(dir), 0o777) },
		"parent symlink": func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "agent-id")
			mkdir(t, filepath.Join(target, "tokens"))
			if err := os.Symlink(target, filepath.Dir(dir)); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, setup := range setups {
		c := newClock()
		m := newManager(t, c)
		setup(t, m.Dir)
		src := newSource(c)
		if _, err := m.Token(context.Background(), src); !errors.Is(err, ErrInsecureCache) || len(src.minted) != 0 {
			t.Errorf("%s: %v after %d mints; want ErrInsecureCache and no request", name, err, len(src.minted))
		}
	}
}

func TestRelativeOrEmptyDirIsRefused(t *testing.T) {
	c := newClock()
	newManager(t, c)
	for _, dir := range []string{"", "tokens", "./cache/tokens"} {
		m := &Manager{Dir: dir, Now: c.Now}
		src := newSource(c)
		if _, err := m.Token(context.Background(), src); !errors.Is(err, ErrNoCacheDir) || len(src.minted) != 0 {
			t.Errorf("Dir %q: %v; want ErrNoCacheDir and no request", dir, err)
		}
	}
	if _, err := os.Stat("tokens"); !errors.Is(err, os.ErrNotExist) {
		t.Error("a relative Dir created a directory in the working directory")
	}
}

func TestNegativeDurationsUseTheDefaults(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	m.Margin, m.LockTimeout = -time.Hour, -time.Second
	src := newSource(c)
	seed(t, m, src, testenv.FakeToken("old"), c.In(-time.Minute))
	if token := mustManagerToken(t, m, src); token.Value == testenv.FakeToken("old") {
		t.Error("a negative margin served an expired token")
	}
	if m.margin() != DefaultMargin || m.lockTimeout() != DefaultLockTimeout {
		t.Error("negative durations do not select the defaults")
	}
}

func TestHeldLockTimesOutAndHonorsTheContext(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	mkdir(t, m.Dir)
	unlock, err := lockFile(context.Background(), lockPath(entryPath(t, m, src)), time.Second)
	if err != nil {
		t.Fatalf("lockFile: %v", err)
	}
	defer unlock()
	m.LockTimeout = 100 * time.Millisecond
	if _, err := m.Token(context.Background(), src); !errors.Is(err, ErrLockTimeout) || len(src.minted) != 0 {
		t.Errorf("held lock: %v; want ErrLockTimeout and no request", err)
	}
	m.LockTimeout = time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := m.Token(ctx, src); !errors.Is(err, context.DeadlineExceeded) || len(src.minted) != 0 {
		t.Errorf("held lock with a deadline: %v; want context.DeadlineExceeded and no request", err)
	}
}

func TestCanceledContextMintsNothing(t *testing.T) {
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Token(ctx, src); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled context: %v, want context.Canceled", err)
	}
	if _, err := os.Stat(entryPath(t, m, src)); !errors.Is(err, os.ErrNotExist) {
		t.Error("a canceled request wrote an entry")
	}
}

func TestCacheWriteFailureDiscardsTheToken(t *testing.T) {
	skipAsRoot(t)
	c := newClock()
	m := newManager(t, c)
	src := newSource(c)
	mkdir(t, m.Dir)
	unlock, err := lockFile(context.Background(), lockPath(entryPath(t, m, src)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	chmod(t, m.Dir, 0o500)
	t.Cleanup(func() { _ = os.Chmod(m.Dir, 0o700) })
	m.Dir = filepath.Clean(m.Dir)
	token, err := m.Token(context.Background(), src)
	if !errors.Is(err, ErrCacheWrite) {
		t.Fatalf("Token in a read-only cache: %v, want ErrCacheWrite", err)
	}
	if token != (provider.Token{}) || len(src.minted) != 1 {
		t.Errorf("Token = %v after %d mints, want the zero token", token, len(src.minted))
	}
	for _, minted := range src.minted {
		assertNoToken(t, err, minted.Value)
	}
}

func mustManagerToken(t *testing.T, m *Manager, src Source) provider.Token {
	t.Helper()
	token, err := m.Token(context.Background(), src)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	return token
}

func assertRegularEntry(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Errorf("entry %v, %v; want a regular 0600 file", info.Mode(), err)
	}
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
