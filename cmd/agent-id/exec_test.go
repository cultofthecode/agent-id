package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

const execConfig = `{"version":1,"profiles":{
"claude":{"provider":"github","host":"github.com","github":{"app_id":1,"installation_id":2,"private_key":"file:/nonexistent.pem","identity":{"login":"claude-dev[bot]","user_id":3,"resolved_at":"2026-11-12T10:00:00Z"}}},
"reviewer":{"provider":"github","host":"github.com","github":{"app_id":1,"installation_id":2,"private_key":"file:/nonexistent.pem"}},
"other":{"provider":"github","host":"github.com","github":{"app_id":1,"installation_id":2,"private_key":"file:/nonexistent.pem","identity":{"login":"other[bot]","user_id":4,"resolved_at":"2026-11-12T10:00:00Z"}}}}}`

const (
	fakePersonal = "ghp_aidtest_personal"
	decoy        = "agent-id-no-token"
)

var secretShapes = regexp.MustCompile(`ghs_|ghp_|gho_|-----BEGIN|eyJ[A-Za-z0-9_-]{12,}\.`)

type execRun struct {
	stdout, stderr string
	code           int
	signal         syscall.Signal
}

type execFixture struct {
	t       *testing.T
	env     testenv.Env
	binary  string
	extra   []string
	workdir string
}

func newExecFixture(t *testing.T) *execFixture {
	t.Helper()
	env := testenv.Isolate(t)
	dir := filepath.Join(env.ConfigHome, "agent-id")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(execConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return &execFixture{t: t, env: env, binary: testenv.BuildBinary(t, env, "github.com/cultofthecode/agent-id/cmd/agent-id"), workdir: env.Home}
}

func (f *execFixture) run(args ...string) execRun {
	f.t.Helper()
	cmd := exec.Command(f.binary, args...)
	cmd.Dir = f.workdir
	cmd.Env = append(os.Environ(), f.extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := execRun{stdout: stdout.String(), stderr: stderr.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		r.code = exit.ExitCode()
		if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			r.signal = ws.Signal()
		}
	} else if err != nil {
		f.t.Fatalf("run: %v", err)
	}
	if secretShapes.MatchString(r.stdout+r.stderr) && !strings.Contains(r.stdout, fakePersonal) {
		f.t.Errorf("output holds a token-shaped value")
	}
	return r
}

func TestExecExitCodesAndErrors(t *testing.T) {
	f := newExecFixture(t)
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"exit status", []string{"exec", "--profile", "claude", "--", "sh", "-c", "exit 7"}, 7, ""},
		{"no dashes", []string{"exec", "--profile", "claude", "sh", "-c", "exit 3"}, 3, ""},
		{"not found", []string{"exec", "--profile", "claude", "--", "does-not-exist"}, 127, `agent-id: exec: "does-not-exist": command not found`},
		{"missing profile flag", []string{"exec", "--", "true"}, 125, "agent-id: exec: --profile is required"},
		{"unknown flag", []string{"exec", "--bogus", "--profile", "claude", "--", "true"}, 125, "unknown flag"},
		{"no command", []string{"exec", "--profile", "claude"}, 125, "no command given"},
		{"unknown profile", []string{"exec", "--profile", "nobody", "--", "true"}, 125, `agent-id: exec: profile "nobody" not found`},
		{"no identity", []string{"exec", "--profile", "reviewer", "--", "true"}, 125, "agent-id setup github --profile reviewer"},
		{"not executable", []string{"exec", "--profile", "claude", "--", "/etc/hosts"}, 126, "permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := f.run(tt.args...)
			if r.code != tt.code || !strings.Contains(r.stderr, tt.stderr) {
				t.Errorf("code %d stderr %q, want %d containing %q", r.code, r.stderr, tt.code, tt.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout %q, want nothing", r.stdout)
			}
		})
	}
}

func TestExecSignals(t *testing.T) {
	f := newExecFixture(t)
	ready := filepath.Join(f.env.TempDir, "ready")
	cmd := exec.Command(f.binary, "exec", "--profile", "claude", "--", "sh", "-c", `trap "exit 42" TERM; : > "$READY"; while :; do sleep 1; done`)
	cmd.Dir = f.workdir
	cmd.Env = append(os.Environ(), "READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ready)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 42 {
		t.Errorf("SIGTERM: %v, want exit status 42", err)
	}

	r := f.run("exec", "--profile", "claude", "--", "sh", "-c", "kill -KILL $$")
	if r.signal != syscall.SIGKILL {
		t.Errorf("killed command: %+v, want termination by SIGKILL", r)
	}
}

func waitFor(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s was not created", path)
}

func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	testenv.Git(t, dir, args...)
}

// TestExecNeutralBaselineIsolation is the Definition of Done of #20 (#68):
// with a fake HOME holding a recording credential helper, a fake hosts.yml
// and fake tokens, the child sees none of them.
func TestExecNeutralBaselineIsolation(t *testing.T) {
	f := newExecFixture(t)
	env := f.env
	helperLog := filepath.Join(env.TempDir, "helper.log")
	helper := filepath.Join(env.TempDir, "helper.sh")
	writeScript(t, helper, `echo "called $*" >> "`+helperLog+`"`+"\n"+`printf 'username=dev\npassword=personal\n'`+"\n")
	askLog := filepath.Join(env.TempDir, "ask.log")
	ask := filepath.Join(env.TempDir, "ask.sh")
	writeScript(t, ask, `echo asked >> "`+askLog+`"`+"\necho personal\n")
	ghLog := filepath.Join(env.TempDir, "gh.log")
	writeScript(t, filepath.Join(env.TempDir, "later", "gh"), `echo called >> "`+ghLog+`"`+"\n")
	if err := os.MkdirAll(env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(env.GHConfigDir, "hosts.yml")
	if err := os.WriteFile(hosts, []byte("github.com:\n    oauth_token: "+fakePersonal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := testenv.NewGitServer(t, env)
	testenv.NewBareRepository(t, env, "o-r")

	repo := filepath.Join(env.Home, "work")
	gitIn(t, env.Home, "init", "--quiet", repo)
	for _, kv := range [][]string{
		{"credential.helper", "!" + helper}, {"credential.https://github.com.helper", "!" + helper}, {"core.askPass", ask},
	} {
		gitIn(t, repo, "config", "--global", kv[0], kv[1])
		gitIn(t, repo, "config", "--local", kv[0], kv[1])
	}
	system := filepath.Join(env.TempDir, "system.gitconfig")
	if err := os.WriteFile(system, []byte("[credential]\n\thelper = !"+helper+"\n[credential \"https://github.com\"]\n\thelper = !"+helper+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "config", "--global", "url."+server.URL+"/.insteadOf", "https://github.com/")
	f.workdir = repo
	f.extra = []string{
		"GIT_CONFIG_NOSYSTEM=0", "GIT_CONFIG_SYSTEM=" + system,
		"GH_TOKEN=" + fakePersonal, "GITHUB_TOKEN=" + fakePersonal, "GH_ENTERPRISE_TOKEN=" + fakePersonal, "GITHUB_ENTERPRISE_TOKEN=" + fakePersonal,
		"GIT_ASKPASS=" + ask, "SSH_ASKPASS=" + ask,
		"PATH=" + filepath.Join(env.TempDir, "later") + ":" + os.Getenv("PATH"),
	}
	script := `printf 'protocol=https\nhost=github.com\npath=o/r.git\n\n' | git credential fill; echo "credential-rc=$?"
git ls-remote https://github.com/o-r.git; echo "ls-remote-rc=$?"
gh api user; echo "gh-rc=$?"
env`
	r := f.run("exec", "--profile", "claude", "--", "sh", "-c", script)
	if r.code != 0 {
		t.Fatalf("exec: code %d: %s", r.code, r.stderr)
	}
	out := r.stdout
	for _, bad := range []string{"username=", "password=", fakePersonal, "personal"} {
		if strings.Contains(out, bad) {
			t.Errorf("the child saw %q", bad)
		}
	}
	for _, want := range []string{"credential-rc=128", "ls-remote-rc=128", "gh-rc=125", "GH_TOKEN=" + decoy, "GH_ENTERPRISE_TOKEN=" + decoy} {
		if !strings.Contains(out, want) {
			t.Errorf("child output lacks %q", want)
		}
	}
	if !strings.Contains(r.stderr, "agent-id: AID-GH-USER:") {
		t.Errorf("gh api user was not refused by the shim; stderr %q", r.stderr)
	}
	for _, absent := range []string{"\nGITHUB_TOKEN=", "\nGITHUB_ENTERPRISE_TOKEN=", "\nGIT_ASKPASS=", "\nSSH_ASKPASS="} {
		if strings.Contains("\n"+out, absent) {
			t.Errorf("the child environment has %q", strings.TrimSpace(absent))
		}
	}
	cacheCfg := filepath.Join(env.CacheHome, "agent-id", "github", "cli-config")
	if !strings.Contains(out, "GH_CONFIG_DIR="+cacheCfg) {
		t.Errorf("GH_CONFIG_DIR is not agent-id's directory")
	}
	if _, err := os.Stat(filepath.Join(cacheCfg, "hosts.yml")); err == nil {
		t.Error("the neutral config directory has hosts.yml")
	}
	for name, path := range map[string]string{"credential helper": helperLog, "askpass": askLog, "fake gh": ghLog} {
		if got := readLog(t, path); got != "" {
			t.Errorf("%s recorded a call: %q", name, got)
		}
	}
	for _, req := range server.Requests() {
		if req.Username != "" || req.Password != "" {
			t.Errorf("the Git server received credentials")
		}
	}
	if data, _ := os.ReadFile(hosts); !strings.Contains(string(data), fakePersonal) {
		t.Error("hosts.yml was changed")
	}
}

func TestExecRealGHGetsTheDecoy(t *testing.T) {
	f := newExecFixture(t)
	gh, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh is not installed")
	}
	if err := os.MkdirAll(f.env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.env.GHConfigDir, "hosts.yml"), []byte("github.com:\n    oauth_token: "+fakePersonal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := f.run("exec", "--profile", "claude", "--", gh, "auth", "token")
	if strings.TrimSpace(r.stdout) != decoy {
		t.Errorf("gh auth token printed %q (exit %d), want the decoy", r.stdout, r.code)
	}
}

func TestExecRefusals(t *testing.T) {
	f := newExecFixture(t)
	repo := filepath.Join(f.env.Home, "work")
	gitIn(t, f.env.Home, "init", "--quiet", repo)
	f.workdir = repo
	tests := []struct {
		name  string
		setup func(t *testing.T)
		want  string
	}{
		{"scp remote", func(t *testing.T) { gitIn(t, repo, "remote", "add", "origin", "git@github.com:o/r.git") }, `remote "origin" reaches github.com over SSH (scp form)`},
		{"ssh pushurl", func(t *testing.T) {
			gitIn(t, repo, "remote", "set-url", "origin", "https://github.com/o/r.git")
			gitIn(t, repo, "remote", "set-url", "--push", "origin", "ssh://git@ssh.github.com:443/o/r.git")
		}, "over SSH (ssh://)"},
		{"password in URL", func(t *testing.T) {
			gitIn(t, repo, "remote", "set-url", "origin", "https://u:secret@github.com/o/r.git")
			gitIn(t, repo, "remote", "set-url", "--push", "origin", "https://u:secret@github.com/o/r.git")
		}, "has a password in its URL"},
		{"surviving header", func(t *testing.T) {
			gitIn(t, repo, "remote", "set-url", "origin", "https://github.com/o/r.git")
			gitIn(t, repo, "remote", "set-url", "--push", "origin", "https://github.com/o/r.git")
			gitIn(t, repo, "config", "http.https://github.com/o/r.git.extraheader", "Authorization: Basic c2VjcmV0")
		}, "extra HTTP header (Authorization)"},
		{"global rewrite", func(t *testing.T) {
			gitIn(t, repo, "config", "--unset-all", "http.https://github.com/o/r.git.extraheader")
			gitIn(t, repo, "remote", "remove", "origin")
			gitIn(t, repo, "config", "--global", "url.git@github.com:.insteadOf", "https://github.com/")
		}, "rewrites https://github.com/ to SSH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			r := f.run("exec", "--profile", "claude", "--", "true")
			if r.code != 125 || !strings.Contains(r.stderr, tt.want) {
				t.Errorf("code %d stderr %q, want 125 containing %q", r.code, r.stderr, tt.want)
			}
			for _, leak := range []string{"secret", "c2VjcmV0"} {
				if strings.Contains(r.stderr, leak) {
					t.Errorf("the message leaks %q", leak)
				}
			}
		})
	}
	t.Run("GIT_DIR cannot hide a repository's rewrite", func(t *testing.T) {
		other := filepath.Join(f.env.Home, "other")
		gitIn(t, f.env.Home, "init", "--quiet", other)
		gitIn(t, other, "config", "url.git@github.com:.insteadOf", "https://github.com/")
		outside := filepath.Join(f.env.Home, "outside")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		f.workdir, f.extra = outside, []string{"GIT_DIR=" + filepath.Join(other, ".git")}
		defer func() { f.workdir, f.extra = repo, nil }()
		r := f.run("exec", "--profile", "claude", "--", "true")
		if r.code != 125 || !strings.Contains(r.stderr, "rewrites https://github.com/ to SSH") {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
	})
	t.Run("ssh alias", func(t *testing.T) {
		gitIn(t, repo, "config", "--global", "--unset-all", "url.git@github.com:.insteadOf")
		gitIn(t, repo, "remote", "add", "origin", "work:o/r.git")
		shim := filepath.Join(f.env.TempDir, "shim")
		writeScript(t, filepath.Join(shim, "ssh"), "echo 'hostname github.com'\n")
		f.extra = []string{"PATH=" + shim + ":" + os.Getenv("PATH")}
		defer func() { f.extra = nil }()
		r := f.run("exec", "--profile", "claude", "--", "true")
		if r.code != 125 || !strings.Contains(r.stderr, `alias "work"`) {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
		gitIn(t, repo, "remote", "remove", "origin")
	})
	t.Run("netrc", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(f.env.Home, ".netrc"), []byte("machine github.com login x password hunter2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		r := f.run("exec", "--profile", "claude", "--", "true")
		if r.code != 125 || !strings.Contains(r.stderr, ".netrc has credentials for github.com") || strings.Contains(r.stderr, "hunter2") {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
	})
}

func TestExecNeutralDirectories(t *testing.T) {
	f := newExecFixture(t)
	root := filepath.Join(f.env.CacheHome, "agent-id", "github")
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 0 {
		t.Fatalf("first start: %d %s", r.code, r.stderr)
	}
	stub := filepath.Join(root, "neutral-bin", "gh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho real\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 0 {
		t.Fatalf("rewrite of the stub: %d %s", r.code, r.stderr)
	}
	if data, _ := os.ReadFile(stub); !strings.Contains(string(data), `cli gh -- "$@"`) {
		t.Error("a launcher with other content was not rewritten")
	}
	if err := os.Chmod(filepath.Join(root, "cli-config"), 0o770); err != nil {
		t.Fatal(err)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "writable or readable by group or others") {
		t.Errorf("group-writable: %d %q", r.code, r.stderr)
	}
	if err := os.Chmod(filepath.Join(root, "cli-config"), 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(root, "cli-config", "hosts.yml")
	if err := os.WriteFile(hosts, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "contains hosts.yml") {
		t.Errorf("hosts.yml: %d %q", r.code, r.stderr)
	}
	_ = os.Remove(hosts)
	_ = os.Remove(stub)
	if err := os.Symlink("/bin/sh", stub); err != nil {
		t.Fatal(err)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "symlink") {
		t.Errorf("symlinked stub: %d %q", r.code, r.stderr)
	}
	_ = os.Remove(stub)
	_ = os.RemoveAll(filepath.Join(root, "cli-config"))
	if err := os.Symlink(f.env.TempDir, filepath.Join(root, "cli-config")); err != nil {
		t.Fatal(err)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "symlink") {
		t.Errorf("symlinked directory: %d %q", r.code, r.stderr)
	}
}

func TestExecNesting(t *testing.T) {
	f := newExecFixture(t)
	self := f.binary
	inner := func(profile string) []string {
		return []string{"exec", "--profile", "claude", "--", self, "exec", "--profile", profile, "--", "env"}
	}
	same := f.run(inner("claude")...)
	outer := f.run("exec", "--profile", "claude", "--", "env")
	if same.code != 0 || outer.code != 0 {
		t.Fatalf("codes %d %d: %s%s", same.code, outer.code, same.stderr, outer.stderr)
	}
	norm := func(s string) string {
		var keep []string
		for _, l := range strings.Split(s, "\n") {
			if !strings.HasPrefix(l, "AGENT_ID_MECHANISM=") && !strings.HasPrefix(l, "_=") {
				keep = append(keep, l)
			}
		}
		return strings.Join(keep, "\n")
	}
	if norm(same.stdout) != norm(outer.stdout) {
		t.Errorf("nested environment differs:\n%s\n---\n%s", same.stdout, outer.stdout)
	}
	if strings.Count(same.stdout, "GIT_CONFIG_KEY_0=") != 1 || strings.Count(same.stdout, "GIT_CONFIG_COUNT=6") != 1 {
		t.Errorf("entries duplicated:\n%s", same.stdout)
	}
	diff := f.run(inner("other")...)
	if diff.code != 125 || !strings.Contains(diff.stderr, `"claude"`) || !strings.Contains(diff.stderr, `"other"`) {
		t.Errorf("different profile: %d %q", diff.code, diff.stderr)
	}
	f.extra = []string{"AGENT_ID_CONTEXT=2"}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "incompatible agent-id") {
		t.Errorf("unknown version: %d %q", r.code, r.stderr)
	}
}

func TestExecLeavesTheParentAlone(t *testing.T) {
	f := newExecFixture(t)
	before := os.Environ()
	snapshot := map[string]bool{}
	_ = filepath.WalkDir(f.env.Home, func(p string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(f.env.Home, p)
		snapshot[rel] = true
		return nil
	})
	r := f.run("exec", "--profile", "claude", "--", "sh", "-c", `printf %s "$AGENT_ID_ACTIVE_PROFILE"`)
	if r.stdout != "claude" || r.code != 0 {
		t.Errorf("marker: %q (%d)", r.stdout, r.code)
	}
	if got := os.Environ(); strings.Join(got, "\x00") != strings.Join(before, "\x00") {
		t.Error("the invoking process environment changed")
	}
	if _, set := os.LookupEnv("AGENT_ID_ACTIVE_PROFILE"); set {
		t.Error("the invoking process has the marker")
	}
	if _, err := os.Stat(f.env.GitConfig); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(f.env.GitConfig); len(data) != 0 {
		t.Error("the global Git config was written")
	}
	var created []string
	_ = filepath.WalkDir(f.env.Home, func(p string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(f.env.Home, p)
		if _, known := snapshot[rel]; err == nil && !known && !strings.HasPrefix(rel, filepath.Join(".cache", "agent-id")) {
			created = append(created, rel)
		}
		return nil
	})
	if len(created) > 0 {
		t.Errorf("unexpected files: %v", created)
	}
}

// TestNetrcWouldLeakWithoutTheRefusal shows why exec refuses a .netrc: Git
// sends its credentials with no helper involved (DoD 2a).
func TestNetrcWouldLeakWithoutTheRefusal(t *testing.T) {
	env := testenv.Isolate(t)
	server := testenv.NewGitServer(t, env)
	testenv.NewBareRepository(t, env, "o-r")
	host := strings.TrimPrefix(server.URL, "https://")
	host, _, _ = strings.Cut(host, ":")
	token := testenv.FakeToken("netrc")
	if err := os.WriteFile(filepath.Join(env.Home, ".netrc"), []byte("machine "+host+" login x-access-token password "+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "-c", "credential.helper=", "ls-remote", server.RepositoryURL("o-r"))
	cmd.Dir = env.Home
	_ = cmd.Run()
	sent := false
	for _, r := range server.Requests() {
		sent = sent || r.Password == token
	}
	if !sent {
		t.Skip("this Git version does not read .netrc for the fake server")
	}
}
