package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

// The fail-closed process suite of #25 (E02-S11). Each scenario puts the
// profile's App key in a state that cannot yield a token, plants decoy
// personal credentials that record every use, and runs git push and
// gh pr create in an exec session. Every scenario must end in a refusal with
// zero decoy uses. Until #22 and #23 inject App tokens, exec never reads the
// key, so the refusal comes from the neutral baseline; the table keeps the
// property when those stories replace the baseline's entries.

type decoys struct {
	helperLog, askLog, ghLog, hosts string
	server                          *testenv.GitServer
	repo                            string
}

func plantDecoys(t *testing.T, f *execFixture) decoys {
	t.Helper()
	env := f.env
	d := decoys{
		helperLog: filepath.Join(env.TempDir, "helper.log"),
		askLog:    filepath.Join(env.TempDir, "ask.log"),
		ghLog:     filepath.Join(env.TempDir, "gh.log"),
		hosts:     filepath.Join(env.GHConfigDir, "hosts.yml"),
	}
	helper := filepath.Join(env.TempDir, "helper.sh")
	writeScript(t, helper, `echo "called $*" >> "`+d.helperLog+`"`+"\n"+`printf 'username=dev\npassword=personal\n'`+"\n")
	ask := filepath.Join(env.TempDir, "ask.sh")
	writeScript(t, ask, `echo asked >> "`+d.askLog+`"`+"\necho personal\n")
	writeScript(t, filepath.Join(env.TempDir, "later", "gh"), `echo called >> "`+d.ghLog+`"`+"\n")
	if err := os.MkdirAll(env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.hosts, []byte("github.com:\n    oauth_token: "+fakePersonal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d.server = testenv.NewGitServer(t, env)
	testenv.NewBareRepository(t, env, "o-r")
	d.repo = filepath.Join(env.Home, "work")
	gitIn(t, env.Home, "init", "--quiet", d.repo)
	gitIn(t, d.repo, "config", "--global", "credential.helper", "!"+helper)
	gitIn(t, d.repo, "config", "--global", "core.askPass", ask)
	gitIn(t, d.repo, "config", "--global", "url."+d.server.URL+"/.insteadOf", "https://github.com/")
	gitIn(t, d.repo, "config", "user.name", "t")
	gitIn(t, d.repo, "config", "user.email", "t@example.invalid")
	gitIn(t, d.repo, "commit", "--quiet", "--allow-empty", "-m", "x")
	gitIn(t, d.repo, "remote", "add", "origin", "https://github.com/o-r.git")
	f.workdir = d.repo
	f.extra = []string{
		"GH_TOKEN=" + fakePersonal, "GITHUB_TOKEN=" + fakePersonal, "GIT_ASKPASS=" + ask, "SSH_ASKPASS=" + ask,
		"PATH=" + filepath.Join(env.TempDir, "later") + ":" + os.Getenv("PATH"),
	}
	return d
}

func (d decoys) assertUnused(t *testing.T) {
	t.Helper()
	for name, path := range map[string]string{"credential helper": d.helperLog, "askpass": d.askLog, "personal gh": d.ghLog} {
		if got := readLog(t, path); got != "" {
			t.Errorf("%s recorded a use: %q", name, got)
		}
	}
	if data, _ := os.ReadFile(d.hosts); string(data) != "github.com:\n    oauth_token: "+fakePersonal+"\n" {
		t.Error("hosts.yml was read into a change")
	}
	for _, req := range d.server.Requests() {
		if req.Username != "" || req.Password != "" {
			t.Errorf("the Git server received credentials on %s %s", req.Method, req.Path)
		}
	}
	if pushes := d.server.Pushes(); len(pushes) != 0 {
		t.Errorf("%d pushes reached the server", len(pushes))
	}
}

func TestFailClosedSuiteInExec(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\nQUFB\n-----END RSA PRIVATE KEY-----\n"
	scenarios := []struct {
		name string
		key  func(t *testing.T, path string)
	}{
		{"missing key file", func(t *testing.T, path string) {}},
		{"key file with group and other access", func(t *testing.T, path string) { writeKey(t, path, pem, 0o644) }},
		{"key file nobody can read", func(t *testing.T, path string) { writeKey(t, path, pem, 0o000) }},
		{"not a PEM key", func(t *testing.T, path string) { writeKey(t, path, "hello", 0o600) }},
		{"corrupt PEM key", func(t *testing.T, path string) { writeKey(t, path, pem, 0o600) }},
		{"key reference is a directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			f := newExecFixture(t)
			keyPath := filepath.Join(f.env.Home, "app.pem")
			sc.key(t, keyPath)
			config := strings.ReplaceAll(execConfig, "file:/nonexistent.pem", "file:"+keyPath)
			if err := os.WriteFile(filepath.Join(f.env.ConfigHome, "agent-id", "config.json"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			d := plantDecoys(t, f)
			assertRefusals(t, f, d)
		})
	}
	t.Run("SSH remote", func(t *testing.T) {
		f := newExecFixture(t)
		d := plantDecoys(t, f)
		gitIn(t, d.repo, "remote", "set-url", "origin", "git@github.com:o/r.git")
		r := f.run("exec", "--profile", "claude", "--", "git", "push", "origin", "HEAD:refs/heads/x")
		if r.code != 125 || !strings.Contains(r.stderr, "over SSH") {
			t.Errorf("code %d stderr %q, want 125 and the SSH refusal", r.code, r.stderr)
		}
		d.assertUnused(t)
	})
	t.Run("empty GH_TOKEN with a decoy hosts.yml", func(t *testing.T) {
		f := newExecFixture(t)
		d := plantDecoys(t, f)
		f.extra = append(f.extra, "GH_TOKEN=", "GITHUB_TOKEN=")
		assertRefusals(t, f, d)
	})
}

func writeKey(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func assertRefusals(t *testing.T, f *execFixture, d decoys) {
	t.Helper()
	push := f.run("exec", "--profile", "claude", "--", "git", "push", "origin", "HEAD:refs/heads/agent")
	if push.code == 0 {
		t.Errorf("git push succeeded; stderr %q", push.stderr)
	}
	pr := f.run("exec", "--profile", "claude", "--", "gh", "pr", "create", "--title", "x", "--body", "y")
	if pr.code == 0 || !strings.Contains(pr.stderr, "agent-id: gh is not available in this agent context") {
		t.Errorf("gh pr create: code %d stderr %q, want a refusal with agent-id's message", pr.code, pr.stderr)
	}
	for _, out := range []string{push.stdout, push.stderr, pr.stdout, pr.stderr} {
		for _, bad := range []string{fakePersonal, "password=personal"} {
			if strings.Contains(out, bad) {
				t.Errorf("output shows %q", bad)
			}
		}
	}
	d.assertUnused(t)
}

// TestFailClosedDecoysRecordUse is the control of the suite: outside exec the
// same push reaches the decoy credential helper, so a silent decoy inside
// exec means something.
func TestFailClosedDecoysRecordUse(t *testing.T) {
	f := newExecFixture(t)
	d := plantDecoys(t, f)
	cmd := execCommand(f, "git", "push", "origin", "HEAD:refs/heads/agent")
	_ = cmd.Run()
	if readLog(t, d.helperLog) == "" {
		t.Error("the decoy credential helper recorded nothing outside exec; the suite proves nothing")
	}
	if len(d.server.Requests()) == 0 {
		t.Error("the control push did not reach the fake server")
	}
}

func execCommand(f *execFixture, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = f.workdir
	cmd.Env = append(os.Environ(), f.extra...)
	return cmd
}
