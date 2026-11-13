package activation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

type fakeProfiles struct {
	calls []string
	err   error
}

func (f *fakeProfiles) CheckName(string) error { f.calls = append(f.calls, "name"); return nil }
func (f *fakeProfiles) Resolve(n string) (profile.Profile, error) {
	f.calls = append(f.calls, "resolve")
	return profile.Profile{Name: n, Provider: "fake", Host: providertest.FakeHost}, f.err
}

type fakeSource struct {
	a     *providertest.FakeActivator
	calls *[]string
}

func (s fakeSource) Activator(profile.Profile) (provider.Activator, error) {
	*s.calls = append(*s.calls, "provider")
	return s.a, nil
}

type fakeNeutral struct {
	dir   string
	calls *[]string
}

func (n fakeNeutral) Ensure([]string, string, provider.NeutralCLI) (NeutralPaths, error) {
	*n.calls = append(*n.calls, "neutral")
	return NeutralPaths{ConfigDir: filepath.Join(n.dir, "cfg"), BinDir: filepath.Join(n.dir, "bin")}, nil
}

func newDeps(t *testing.T) (Deps, *providertest.FakeActivator, *[]string) {
	t.Helper()
	calls := &[]string{}
	a := providertest.NewFakeActivator()
	d := Deps{
		Profiles: &fakeProfiles{}, Providers: fakeSource{a, calls}, Neutral: fakeNeutral{t.TempDir(), calls},
		Checks: func(provider.Activator) []Check {
			return []Check{func(context.Context, string, []string) error { *calls = append(*calls, "check"); return nil }}
		},
	}
	return d, a, calls
}

func request(parent ...string) Request {
	return Request{Profile: "claude", Mechanism: MechanismExec, Binary: "/bin/agent-id", Dir: "/", Parent: parent}
}

func TestActivateOrderAndBaseline(t *testing.T) {
	testenv.Isolate(t)
	d, a, calls := newDeps(t)
	act, err := Activate(context.Background(), d, request("FAKE_CLI_TOKEN="+testenv.FakeToken("personal"), "GIT_ASKPASS=/x", "SSH_ASKPASS=/y"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(*calls, ","); got != "provider,neutral,check" {
		t.Errorf("order %s", got)
	}
	env := parseEnv(act.Env)
	if env["FAKE_CLI_TOKEN"] != providertest.FakeDecoy || env["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("baseline missing: %v", act.Env)
	}
	for _, gone := range []string{"GIT_ASKPASS", "SSH_ASKPASS", "FAKE_CLI_TOKEN_ALIAS"} {
		if _, ok := env[gone]; ok {
			t.Errorf("%s survived", gone)
		}
	}
	if env["GIT_CONFIG_KEY_0"] != "credential.helper" || env["GIT_CONFIG_VALUE_0"] != "" || env["GIT_CONFIG_KEY_1"] != "http.https://"+a.Host()+"/.extraHeader" || env["GIT_CONFIG_KEY_2"] != "core.askPass" {
		t.Errorf("git baseline: %v", act.Env)
	}
	if !strings.HasPrefix(env["PATH"], filepath.Join(filepath.Dir(env["FAKE_CLI_CONFIG_DIR"]), "bin")) {
		t.Errorf("PATH %q", env["PATH"])
	}
	if !slices.Contains(act.Drop, "GIT_ASKPASS") || !slices.Contains(act.Drop, "FAKE_CLI_TOKEN") {
		t.Errorf("drop %v", act.Drop)
	}
}

func TestActivateRefusals(t *testing.T) {
	testenv.Isolate(t)
	t.Run("empty drop list fails closed", func(t *testing.T) {
		d, a, calls := newDeps(t)
		a.Variables = nil
		if _, err := Activate(context.Background(), d, request()); !errors.Is(err, ErrPlanUnsafe) || slices.Contains(*calls, "check") {
			t.Errorf("error %v calls %v", err, *calls)
		}
	})
	t.Run("not ready keeps the hint and stops before the neutral directories", func(t *testing.T) {
		d, a, calls := newDeps(t)
		a.ReadyError = errors.New("bot identity is not resolved; run agent-id setup fake --profile claude")
		_, err := Activate(context.Background(), d, request())
		if !errors.Is(err, ErrProfileNotReady) || !strings.Contains(err.Error(), "agent-id setup fake --profile claude") || slices.Contains(*calls, "neutral") {
			t.Errorf("error %v calls %v", err, *calls)
		}
	})
	t.Run("nesting comes before the profile is read", func(t *testing.T) {
		d, _, calls := newDeps(t)
		parent := request("AGENT_ID_CONTEXT=1", "AGENT_ID_ACTIVE_PROFILE=other", "AGENT_ID_GIT_CONFIG_BASE=0", "AGENT_ID_GIT_CONFIG_END=0")
		_, err := Activate(context.Background(), d, parent)
		if !errors.Is(err, ErrNestedProfile) || len(*calls) != 0 {
			t.Errorf("error %v calls %v", err, *calls)
		}
	})
	t.Run("inherited git comes before the neutral directories", func(t *testing.T) {
		d, _, calls := newDeps(t)
		_, err := Activate(context.Background(), d, request("GIT_CONFIG_COUNT=1"))
		if !errors.Is(err, ErrInheritedGitConfig) || slices.Contains(*calls, "neutral") {
			t.Errorf("error %v calls %v", err, *calls)
		}
	})
	t.Run("a failing check stops the start", func(t *testing.T) {
		d, _, _ := newDeps(t)
		d.Checks = func(provider.Activator) []Check {
			return []Check{func(context.Context, string, []string) error { return failf(ErrSSHRemote, "boom") }}
		}
		if _, err := Activate(context.Background(), d, request()); !errors.Is(err, ErrSSHRemote) {
			t.Errorf("error %v", err)
		}
	})
}

type fakeRunner struct {
	answers map[string]string
	errs    map[string]error
	ran     []string
}

func (f *fakeRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.ran = append(f.ran, key)
	if err, ok := f.errs[key]; ok {
		return nil, err
	}
	return []byte(f.answers[key]), nil
}

func gitList(kv ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteString("global\x00file:/h/.gitconfig\x00" + kv[i] + "\n" + kv[i+1] + "\x00")
	}
	return b.String()
}

const listCmd = "git config --list -z --show-origin --show-scope"

func runRemoteChecks(t *testing.T, r *fakeRunner) error {
	t.Helper()
	for _, check := range NewChecks(providertest.NewFakeActivator(), r) {
		if err := check(context.Background(), "/work", []string{"HOME=" + t.TempDir()}); err != nil {
			return err
		}
	}
	return nil
}

func repoRunner(fetch, push, list string) *fakeRunner {
	return &fakeRunner{answers: map[string]string{
		listCmd: list, "git rev-parse --is-inside-work-tree": "true\n", "git remote": "origin\n",
		"git ls-remote --get-url origin": fetch + "\n", "git remote get-url --push --all origin": push + "\n",
	}}
}

func TestRemoteChecks(t *testing.T) {
	testenv.Isolate(t)
	host := providertest.FakeHost
	tests := []struct {
		name string
		r    *fakeRunner
		want error
	}{
		{"https remote passes", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", ""), nil},
		{"unrelated ssh host with unrelated alias passes", func() *fakeRunner {
			r := repoRunner("git@gitlab.example:o/r.git", "git@gitlab.example:o/r.git", "")
			r.answers["ssh -G -- gitlab.example"] = "hostname gitlab.example\n"
			return r
		}(), nil},
		{"scp remote", repoRunner("git@"+host+":o/r.git", "git@"+host+":o/r.git", ""), ErrSSHRemote},
		{"ssh pushurl", repoRunner("https://"+host+"/o/r.git", "ssh://git@ssh."+host+":443/o/r.git", ""), ErrSSHRemote},
		{"alias to the host", func() *fakeRunner {
			r := repoRunner("work:o/r.git", "work:o/r.git", "")
			r.answers["ssh -G -- work"] = "user git\nhostname " + host + "\n"
			return r
		}(), ErrSSHRemote},
		{"ssh -G fails", func() *fakeRunner {
			r := repoRunner("work:o/r.git", "work:o/r.git", "")
			r.errs = map[string]error{"ssh -G -- work": errors.New("no ssh")}
			return r
		}(), ErrRemoteCheck},
		{"git config fails", func() *fakeRunner {
			r := repoRunner("", "", "")
			r.errs = map[string]error{"git remote": &exec.ExitError{}}
			return r
		}(), ErrRemoteCheck},
		{"password", repoRunner("https://u:secret@"+host+"/o/r.git", "https://"+host+"/o/r.git", ""), ErrRemoteCredentials},
		{"header", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://"+host+"/o/r.git.extraheader", "Authorization: Basic c2VjcmV0")), ErrRemoteHeader},
		{"rewrite to ssh", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("url.git@"+host+":.insteadof", "https://"+host+"/")), ErrSSHRewrite},
		{"unusable ssh host is refused", repoRunner("git@bad_host!:o/r.git", "git@bad_host!:o/r.git", ""), ErrRemoteCheck},
		{"trailing dot ssh host", repoRunner("git@"+host+".:o/r.git", "git@"+host+".:o/r.git", ""), ErrSSHRemote},
		{"header scoped under the host", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://"+host+"/o/.extraheader", "Authorization: Basic x", "http.https://"+host+"/.extraheader", "")), ErrRemoteHeader},
		{"wildcard header for another git host", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://*."+host+"/.extraheader", "Authorization: Basic x")), ErrRemoteHeader},
		{"wildcard header with a middle wildcard label", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://ssh.*.agent-id.test/.extraheader", "Authorization: Basic x")), ErrRemoteHeader},
		{"wildcard header with a wildcard after the first label", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://git.*.test/.extraheader", "Authorization: Basic x", "http.https://"+host+"/.extraheader", "")), nil},
		{"wildcard header that ranks below the host reset passes", repoRunner("https://"+host+"/o/r.git", "https://"+host+"/o/r.git", gitList("http.https://*.*.test/.extraheader", "Authorization: Basic x", "http.https://"+host+"/.extraheader", "")), nil},
		{"no git", &fakeRunner{errs: map[string]error{listCmd: exec.ErrNotFound}}, nil},
		{"not a repository", &fakeRunner{answers: map[string]string{listCmd: ""}, errs: map[string]error{"git rev-parse --is-inside-work-tree": &exec.ExitError{}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runRemoteChecks(t, tt.r)
			if !errors.Is(err, tt.want) && (tt.want != nil || err != nil) {
				t.Fatalf("error %v, want %v", err, tt.want)
			}
			if err != nil {
				for _, leak := range []string{"secret", "c2VjcmV0"} {
					if strings.Contains(err.Error(), leak) {
						t.Errorf("message leaks %q", leak)
					}
				}
			}
		})
	}
}

func TestNetrcCheck(t *testing.T) {
	env := testenv.Isolate(t)
	hosts := []string{"git.agent-id.test"}
	child := []string{"HOME=" + env.Home}
	if err := checkNetrc(child, hosts); err != nil {
		t.Fatalf("no file: %v", err)
	}
	for name, content := range map[string]string{
		"machine":   "machine git.agent-id.test login x password hunter2\n",
		"default":   "default login x password hunter2\n",
		"uppercase": "machine GIT.AGENT-ID.TEST login x password hunter2\n",
		"garbage":   "wat\n",
	} {
		if err := os.WriteFile(filepath.Join(env.Home, ".netrc"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		err := checkNetrc(child, hosts)
		if !errors.Is(err, ErrNetrc) || strings.Contains(err.Error(), "hunter2") || !strings.Contains(err.Error(), ".netrc") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(env.Home, ".netrc"), []byte("machine other.example login x password y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkNetrc(child, hosts); err != nil {
		t.Errorf("another host: %v", err)
	}
	other := filepath.Join(env.Home, "custom")
	if err := os.WriteFile(other, []byte("default login x password y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkNetrc(append(child, "NETRC="+other), hosts); !errors.Is(err, ErrNetrc) {
		t.Errorf("NETRC: %v", err)
	}
}

func TestResolveBinaryAndLookPath(t *testing.T) {
	env := testenv.Isolate(t)
	real := filepath.Join(env.TempDir, "versioned", "agent-id")
	writeExec(t, real)
	link := filepath.Join(env.TempDir, "bin", "agent-id")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveBinary(real, link, ""); err != nil || got != link {
		t.Errorf("stable symlink: %q %v", got, err)
	}
	if got, err := resolveBinary(real, "agent-id", filepath.Dir(link)); err != nil || got != link {
		t.Errorf("through PATH: %q %v", got, err)
	}
	if got, _ := resolveBinary(real, "/elsewhere/agent-id", ""); got != real {
		t.Errorf("unrelated arg0: %q", got)
	}
	if _, err := resolveBinary(filepath.Join(env.TempDir, "a\nb"), "", ""); !errors.Is(err, ErrBinaryPath) {
		t.Errorf("newline: %v", err)
	}

	dir := filepath.Join(env.TempDir, "path")
	writeExec(t, filepath.Join(dir, "tool"))
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	childPath := []string{"PATH=" + dir}
	if got, err := LookPath(childPath, "tool"); err != nil || got != filepath.Join(dir, "tool") {
		t.Errorf("tool: %q %v", got, err)
	}
	if _, err := LookPath(childPath, "plain"); !errors.Is(err, ErrCommandNotFound) {
		t.Errorf("non-executable skipped: %v", err)
	}
	if _, err := LookPath([]string{"PATH="}, "sh"); !errors.Is(err, ErrCommandNotFound) {
		t.Errorf("empty PATH: %v", err)
	}
	t.Chdir(dir)
	if _, err := LookPath([]string{"PATH=:."}, "tool"); !errors.Is(err, ErrCommandNotFound) || !strings.Contains(err.Error(), "./tool") {
		t.Errorf("relative PATH entry: %v", err)
	}
	if _, err := LookPath(childPath, "./plain"); !errors.Is(err, ErrCommandNotExec) {
		t.Errorf("slash non-executable: %v", err)
	}
	if _, err := LookPath(childPath, "./missing"); !errors.Is(err, ErrCommandNotFound) {
		t.Errorf("slash missing: %v", err)
	}
}

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func TestNeutralDirsOnDisk(t *testing.T) {
	env := testenv.Isolate(t)
	cli := providertest.NewFakeActivator().NeutralCLI()
	parent := []string{"HOME=" + env.Home, "XDG_CACHE_HOME=" + env.CacheHome}
	paths, err := FileNeutralDirs{}.Ensure(parent, "fake", cli)
	if err != nil {
		t.Fatal(err)
	}
	stub, err := os.ReadFile(filepath.Join(paths.BinDir, cli.Binary))
	if err != nil || string(stub) != LauncherScript(cli.Binary) {
		t.Errorf("stub %q %v", stub, err)
	}
	info, _ := os.Stat(filepath.Join(env.CacheHome, "agent-id"))
	if info.Mode().Perm() != 0o700 {
		t.Errorf("parent mode %v", info.Mode().Perm())
	}
	if _, err := (FileNeutralDirs{}).Ensure([]string{"HOME=relative"}, "fake", cli); !errors.Is(err, ErrNeutralDir) {
		t.Errorf("relative HOME: %v", err)
	}
	if _, err := (FileNeutralDirs{}).Ensure(parent, "fake", provider.NeutralCLI{Binary: "../x"}); !errors.Is(err, ErrNeutralDir) {
		t.Errorf("bad binary name: %v", err)
	}
}
