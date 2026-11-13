package credentialhelper

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/gitint"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// The process tests run a real git against the fake Git server. The test
// binary is the helper: git starts it as `<binary> git-credential --profile
// claude get`, and TestMain runs the real command with a fake provider.

const (
	childVar     = "CREDHELPER_TEST_CHILD"
	childHostVar = "CREDHELPER_TEST_HOST"
	childFailVar = "CREDHELPER_TEST_FAIL"
)

func TestMain(m *testing.M) {
	if os.Getenv(childVar) == "1" {
		os.Exit(runChild())
	}
	testenv.Main(m)
}

func runChild() int {
	prov := hostedFake{Fake: providertest.NewFake(), host: os.Getenv(childHostVar)}
	if class := os.Getenv(childFailVar); class != "" {
		prov.FailTokens(map[string]error{"unauthorized": provider.ErrUnauthorized, "unavailable": provider.ErrUnavailable}[class])
	}
	cmd := NewCommand(newHandler(prov, nil))
	cmd.SetArgs(os.Args[2:])
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-id:", err)
		return ExitFailure
	}
	return 0
}

type gitFixture struct {
	t      *testing.T
	env    testenv.Env
	server *testenv.GitServer
	host   string
	work   string
	log    string
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	env := testenv.Isolate(t)
	server := testenv.NewGitServer(t, env)
	testenv.NewBareRepository(t, env, "r")
	if err := os.MkdirAll(filepath.Join(testenv.RepositoriesDir(env), "o"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(testenv.RepositoriesDir(env), "r.git"), filepath.Join(testenv.RepositoriesDir(env), "o", "r.git")); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	f := &gitFixture{t: t, env: env, server: server, host: u.Host, log: filepath.Join(env.TempDir, "personal.log")}
	f.work = filepath.Join(env.Home, "work")
	testenv.Git(t, env.Home, "init", "--quiet", "--initial-branch=main", f.work)
	testenv.Git(t, f.work, "config", "user.name", "t")
	testenv.Git(t, f.work, "config", "user.email", "t@example.invalid")
	testenv.Git(t, f.work, "commit", "--quiet", "--allow-empty", "-m", "x")
	f.plantPersonalCredentials()
	return f
}

func (f *gitFixture) plantPersonalCredentials() {
	f.t.Helper()
	helper := filepath.Join(f.env.TempDir, "personal-helper.sh")
	ask := filepath.Join(f.env.TempDir, "personal-ask.sh")
	script := func(path, body string) {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			f.t.Fatal(err)
		}
	}
	script(helper, `echo "helper $*" >> "`+f.log+`"`+"\nprintf 'username=dev\\npassword=personal\\n'\n")
	script(ask, `echo ask >> "`+f.log+`"`+"\necho personal\n")
	testenv.Git(f.t, f.work, "config", "--global", "credential.helper", "!"+helper)
	testenv.Git(f.t, f.work, "config", "--global", "core.askPass", ask)
}

// activate sets the process-scoped entries of an agent context: the reset of
// inherited helpers and askpass, then the transport group of gitint for host.
func (f *gitFixture) activate(host string, scoped bool) {
	f.t.Helper()
	binary, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	entries, err := gitint.TransportEntries(binary, "claude", host)
	if err != nil {
		f.t.Fatal(err)
	}
	all := []gitint.Entry{{Key: "credential.helper", Value: ""}, {Key: "core.askPass", Value: ""}}
	if !scoped {
		entries[0].Key = "credential.helper"
	}
	all = append(all, entries...)
	f.t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(len(all)))
	for i, e := range all {
		f.t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", i), e.Key)
		f.t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", i), e.Value)
	}
	f.t.Setenv(childVar, "1")
	f.t.Setenv(childHostVar, host)
	f.t.Setenv("GIT_ASKPASS", "")
	f.t.Setenv("SSH_ASKPASS", "")
}

func (f *gitFixture) git(args ...string) (string, error) {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.work
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *gitFixture) personalUsed() string {
	data, _ := os.ReadFile(f.log)
	return string(data)
}

func (f *gitFixture) assertNoCredentialReachedTheServer() {
	f.t.Helper()
	for _, r := range f.server.Requests() {
		if r.Username != "" || r.Password != "" {
			f.t.Errorf("the server received credentials on %s %s", r.Method, r.Path)
		}
	}
	if len(f.server.Pushes()) != 0 {
		f.t.Errorf("%d pushes reached the server", len(f.server.Pushes()))
	}
}

func (f *gitFixture) assertPushedAsTheApp() {
	f.t.Helper()
	pushes := f.server.Pushes()
	if len(pushes) == 0 {
		f.t.Fatal("no push reached the server")
	}
	for _, p := range pushes {
		if p.Username != "x-access-token" || !testenv.IsFakeToken(p.Password) {
			f.t.Errorf("push credentials: user %q, fake token %v", p.Username, testenv.IsFakeToken(p.Password))
		}
	}
	if used := f.personalUsed(); used != "" {
		f.t.Errorf("personal helper or askpass ran: %q", used)
	}
}

func TestGitPushOverHTTPSUsesTheAppToken(t *testing.T) {
	f := newGitFixture(t)
	f.activate(f.host, true)
	if out, err := f.git("push", f.server.URL+"/o/r.git", "HEAD:refs/heads/agent"); err != nil {
		t.Fatalf("git push: %v\n%s", err, out)
	}
	f.assertPushedAsTheApp()
	if out, err := f.git("ls-remote", f.server.URL+"/o/r.git"); err != nil || !strings.Contains(out, "refs/heads/agent") {
		t.Errorf("ls-remote: %v\n%s", err, out)
	}
}

func TestGitCloneAndFetchUseTheAppToken(t *testing.T) {
	f := newGitFixture(t)
	f.activate(f.host, true)
	dest := filepath.Join(f.env.Home, "clone")
	if out, err := f.git("clone", "--quiet", f.server.URL+"/o/r.git", dest); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	reqs := f.server.Requests()
	if len(reqs) == 0 || !testenv.IsFakeToken(reqs[len(reqs)-1].Password) {
		t.Errorf("clone did not authenticate with the fake token: %+v", reqs)
	}
	if used := f.personalUsed(); used != "" {
		t.Errorf("personal helper or askpass ran: %q", used)
	}
}

func TestGitPushOverASSHStyleRemoteUsesTheAppToken(t *testing.T) {
	f := newGitFixture(t)
	f.activate(f.host, true)
	remote := "ssh://git@" + f.host + "/o/r.git"
	if out, err := f.git("push", remote, "HEAD:refs/heads/agent"); err != nil {
		t.Fatalf("git push over an ssh:// remote: %v\n%s", err, out)
	}
	f.assertPushedAsTheApp()
	if out, err := f.git("ls-remote", "--get-url", remote); err != nil || strings.TrimSpace(out) != f.server.URL+"/o/r.git" {
		t.Errorf("get-url = %q, %v", out, err)
	}
}

func TestGitRewritesTheScpFormOfAPortlessHost(t *testing.T) {
	f := newGitFixture(t)
	f.activate("github.com", true)
	for remote, want := range map[string]string{
		"git@github.com:o/r.git":          "https://github.com/o/r.git",
		"ssh://git@github.com/o/r.git":    "https://github.com/o/r.git",
		"git@github.com.evil.example:o/r": "git@github.com.evil.example:o/r",
		"ssh://git@gist.github.com/o/r":   "ssh://git@gist.github.com/o/r",
	} {
		out, err := f.git("ls-remote", "--get-url", remote)
		if err != nil || strings.TrimSpace(out) != want {
			t.Errorf("get-url %s = %q, %v; want %s", remote, out, err, want)
		}
	}
}

func TestGitGetsNoCredentialWhenTheMintFails(t *testing.T) {
	for _, class := range []string{"unauthorized", "unavailable"} {
		t.Run(class, func(t *testing.T) {
			f := newGitFixture(t)
			f.activate(f.host, true)
			t.Setenv(childFailVar, class)
			out, err := f.git("push", f.server.URL+"/o/r.git", "HEAD:refs/heads/agent")
			if err == nil {
				t.Fatal("git push succeeded without a token")
			}
			if !strings.Contains(out, `cannot get a token for profile "claude"`) {
				t.Errorf("git did not show the helper's message: %s", out)
			}
			if strings.Contains(out, "ghs_") || strings.Contains(out, "personal") {
				t.Errorf("output shows a credential: %s", out)
			}
			f.assertNoCredentialReachedTheServer()
			if used := f.personalUsed(); used != "" {
				t.Errorf("personal helper or askpass ran: %q", used)
			}
		})
	}
}

func TestGitGetsNothingFromTheHelperForAnotherHost(t *testing.T) {
	f := newGitFixture(t)
	f.activate("github.com", false)
	out, err := f.git("push", f.server.URL+"/o/r.git", "HEAD:refs/heads/agent")
	if err == nil {
		t.Fatalf("git push to a host that is not the profile's succeeded\n%s", out)
	}
	f.assertNoCredentialReachedTheServer()
	if used := f.personalUsed(); used != "" {
		t.Errorf("personal helper or askpass ran: %q", used)
	}
}

func TestGitCredentialFillRefusesWhatTheDecisionRefuses(t *testing.T) {
	f := newGitFixture(t)
	f.activate(f.host, false)
	requests := map[string]string{
		"http":     "protocol=http\nhost=" + f.host + "\npath=o/r.git\n\n",
		"no path":  "protocol=https\nhost=" + f.host + "\n\n",
		"bad path": "protocol=https\nhost=" + f.host + "\npath=../etc\n\n",
	}
	for name, input := range requests {
		cmd := exec.Command("git", "credential", "fill")
		cmd.Dir = f.work
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err == nil || strings.Contains(string(out), "password=") {
			t.Errorf("%s: git credential fill gave %q, %v", name, out, err)
		}
	}
	if used := f.personalUsed(); used != "" {
		t.Errorf("personal helper or askpass ran: %q", used)
	}
}
