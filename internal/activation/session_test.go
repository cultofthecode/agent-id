package activation

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

type sessionFixture struct {
	env    testenv.Env
	cli    provider.NeutralCLI
	parent []string
	paths  NeutralPaths
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	env := testenv.Isolate(t)
	f := &sessionFixture{env: env, cli: providertest.NewFakeActivator().NeutralCLI()}
	f.parent = []string{"HOME=" + env.Home, "XDG_CACHE_HOME=" + env.CacheHome}
	paths, err := FileNeutralDirs{}.Ensure(f.parent, "fake", f.cli)
	if err != nil {
		t.Fatal(err)
	}
	f.paths = paths
	return f
}

func (f *sessionFixture) session(t *testing.T, parent []string) string {
	t.Helper()
	path, err := FileNeutralDirs{}.EnsureSession(parent, f.paths, f.cli)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *sessionFixture) sessionsDir() string { return filepath.Join(f.paths.ShellDir, sessionsDir) }

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Ino
}

func TestParallelSessionsDoNotShareAFile(t *testing.T) {
	f := newSessionFixture(t)
	a := f.session(t, f.parent)
	if err := os.WriteFile(a, []byte(read(t, a)+"export GH_TOKEN=fake-a\ngh() { :; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, ino := read(t, a), inode(t, a)
	b := f.session(t, f.parent)
	if a == b || filepath.Dir(a) == filepath.Dir(b) {
		t.Fatalf("two sessions share a path: %s %s", a, b)
	}
	if read(t, a) != before || inode(t, a) != ino {
		t.Error("the start of session B changed the file of session A")
	}
	if got := read(t, b); strings.Contains(got, "fake-a") || strings.Contains(got, "gh() {") {
		t.Errorf("a line appended to A reached B:\n%s", got)
	}
	for _, p := range []string{a, b} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v %v", p, info, err)
		}
		dir, err := os.Stat(filepath.Dir(p))
		if err != nil || dir.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", filepath.Dir(p), dir, err)
		}
	}
	if info, err := os.Stat(f.sessionsDir()); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("sessions directory: %v %v", info, err)
	}
	if !strings.HasPrefix(filepath.Base(filepath.Dir(a)), strconv.Itoa(os.Getpid())+".") {
		t.Errorf("session name %q does not start with the pid", filepath.Base(filepath.Dir(a)))
	}
}

func TestNestedSessionSourcesTheOuterFile(t *testing.T) {
	f := newSessionFixture(t)
	devFile := filepath.Join(f.env.Home, "dev-claude-env.sh")
	outer := f.session(t, append(f.parent, "CLAUDE_ENV_FILE="+devFile))
	if !strings.Contains(read(t, outer), "'"+devFile+"'") {
		t.Fatalf("the outer file does not source the developer's file:\n%s", read(t, outer))
	}
	if err := os.WriteFile(outer, []byte(read(t, outer)+"export OUTER=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, ino := read(t, outer), inode(t, outer)
	nested := append(f.parent, VarContext+"=1", "CLAUDE_ENV_FILE="+outer, VarUserClaudeEnvFile+"="+devFile)
	inner := f.session(t, nested)
	if inner == outer {
		t.Fatal("the nested exec reuses the outer file")
	}
	if got := read(t, inner); !strings.Contains(got, ". '"+outer+"'") || strings.Contains(got, "'"+devFile+"'") {
		t.Errorf("the inner file does not source the outer file only:\n%s", got)
	}
	if read(t, outer) != before || inode(t, outer) != ino {
		t.Error("the nested exec changed the outer file")
	}
}

func TestSweepRemovesStaleSessionsOnly(t *testing.T) {
	f := newSessionFixture(t)
	dir := f.sessionsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, claudeEnvFile), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-age)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	dead := mk("111.dead", time.Minute)
	oldLive := mk("222.oldlive", sessionMaxAge+time.Hour)
	live := mk("222.live", time.Minute)
	odd := mk("not-a-session", sessionMaxAge+time.Hour)
	outside := filepath.Join(f.env.TempDir, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "keep")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "333.link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(dead, "link")
	if err := os.Symlink(outside, inner); err != nil {
		t.Fatal(err)
	}
	sweepSessions(dir, time.Now(), func(pid int) bool { return pid == 222 })
	exists := func(p string) bool { _, err := os.Lstat(p); return err == nil }
	if exists(dead) || exists(oldLive) {
		t.Error("a stale session directory is still there")
	}
	if !exists(live) || !exists(odd) || !exists(link) {
		t.Error("the sweep removed a live session, an unknown name, or a symlink entry")
	}
	if read(t, keep) != "keep" {
		t.Error("the sweep followed a symlink")
	}
}

func TestEnsureSessionSweepsAtStart(t *testing.T) {
	f := newSessionFixture(t)
	if err := os.MkdirAll(f.sessionsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(f.sessionsDir(), "2147483646.stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	f.session(t, f.parent)
	if _, err := os.Lstat(stale); err == nil {
		t.Error("the start of an exec left the directory of a dead process")
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) || pidAlive(2147483646) {
		t.Error("pidAlive is wrong")
	}
	if _, ok := sessionPid("-5.x"); ok {
		t.Error("a negative pid is accepted")
	}
}

func TestSessionRefusesPlantedLinksAndFiles(t *testing.T) {
	f := newSessionFixture(t)
	outside := filepath.Join(f.env.TempDir, "target")
	if err := os.WriteFile(outside, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Run("symlink at the session file", func(t *testing.T) {
		dir := filepath.Join(f.env.TempDir, "sess")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, claudeEnvFile)
		if err := os.Symlink(outside, path); err != nil {
			t.Fatal(err)
		}
		if err := createFile(path, "x"); err == nil {
			t.Error("createFile wrote through a symlink")
		}
		if read(t, outside) != "target" {
			t.Error("the target changed")
		}
	})
	t.Run("symlink as the sessions directory", func(t *testing.T) {
		dir := filepath.Join(f.env.TempDir, "elsewhere")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir, f.sessionsDir()); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(f.sessionsDir()) }()
		if _, err := (FileNeutralDirs{}).EnsureSession(f.parent, f.paths, f.cli); err == nil {
			t.Error("EnsureSession used a symlinked sessions directory")
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("something was written through the symlink: %v", entries)
		}
	})
	t.Run("symlink, fifo, and foreign mode at a shared file", func(t *testing.T) {
		link := filepath.Join(f.paths.ShellDir, "link")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if err := ensureFile(link, "x", 0o600); err == nil {
			t.Error("ensureFile followed a symlink")
		}
		fifo := filepath.Join(f.paths.ShellDir, "fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ensureFile(fifo, "x", 0o600); err == nil {
			t.Error("ensureFile accepted a fifo")
		}
		open := filepath.Join(f.paths.ShellDir, "open")
		if err := os.WriteFile(open, []byte("x"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(open, 0o666); err != nil {
			t.Fatal(err)
		}
		if err := ensureFile(open, "x", 0o600); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(open); info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", info.Mode().Perm())
		}
		if read(t, outside) != "target" {
			t.Error("the target changed")
		}
	})
}
