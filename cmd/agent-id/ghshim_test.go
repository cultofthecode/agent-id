package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

// The process tests of the gh shim (#23, spec
// .ai/specs/2026-11-13-gh-shim.md). The real agent-id binary runs in exec; a
// fake gh stands later on PATH and records what it receives. Tokens are fake
// (ghs_aidtest_…) and sit in the token cache, so no test reaches a network.

type seedSource struct{ value string }

func (seedSource) Name() string         { return "github" }
func (seedSource) Host() string         { return "github.com" }
func (seedSource) TokenSubject() string { return "app-1-installation-2" }
func (s seedSource) Token(context.Context) (provider.Token, error) {
	return provider.Token{Value: s.value, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

type shimFixture struct {
	*execFixture
	argsLog, hashLog, envLog string
}

func newShimFixture(t *testing.T) *shimFixture {
	t.Helper()
	f := &shimFixture{execFixture: newExecFixture(t)}
	env := f.env
	keyPath := filepath.Join(env.Home, "app.pem")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	writeKey(t, keyPath, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})), 0o600)
	config := strings.ReplaceAll(execConfig, "file:/nonexistent.pem", "file:"+keyPath)
	if err := os.WriteFile(filepath.Join(env.ConfigHome, "agent-id", "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	f.argsLog, f.hashLog, f.envLog = filepath.Join(env.TempDir, "gh.args"), filepath.Join(env.TempDir, "gh.hash"), filepath.Join(env.TempDir, "gh.env")
	writeScript(t, filepath.Join(env.TempDir, "later", "gh"), `echo "$*" >> "`+f.argsLog+`"
if command -v sha256sum >/dev/null 2>&1; then h=$(printf %s "$GH_TOKEN" | sha256sum); else h=$(printf %s "$GH_TOKEN" | shasum -a 256); fi
echo "${h%% *}" >> "`+f.hashLog+`"
echo "GITHUB_TOKEN=${GITHUB_TOKEN-unset} GH_HOST=$GH_HOST GH_CONFIG_DIR=$GH_CONFIG_DIR GH_ENTERPRISE_TOKEN=$GH_ENTERPRISE_TOKEN ACTIVE=$AGENT_ID_CLI_ACTIVE" >> "`+f.envLog+`"
echo gh-output
`)
	f.workdir = env.Home
	f.extra = []string{"PATH=" + filepath.Join(env.TempDir, "later") + ":" + os.Getenv("PATH")}
	return f
}

func (f *shimFixture) seed(t *testing.T, value string) {
	t.Helper()
	dir, err := token.Dir(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := (&token.Manager{Dir: dir}).Token(context.Background(), seedSource{value}); err != nil {
		t.Fatalf("seed the token cache: %v", err)
	}
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (f *shimFixture) gh(args ...string) execRun {
	f.t.Helper()
	return f.run(append([]string{"exec", "--profile", "claude", "--", "gh"}, args...)...)
}

func TestGHShimGivesEachCallACurrentToken(t *testing.T) {
	f := newShimFixture(t)
	first, second := testenv.FakeToken("shim_first"), testenv.FakeToken("shim_second")
	f.seed(t, first)
	if r := f.gh("pr", "create", "--title", "x"); r.code != 0 || r.stdout != "gh-output\n" {
		t.Fatalf("first call: %d %q %q", r.code, r.stdout, r.stderr)
	}
	f.seed(t, second)
	if r := f.gh("pr", "view", "1"); r.code != 0 {
		t.Fatalf("second call: %d %q", r.code, r.stderr)
	}
	hashes := strings.Fields(readLog(t, f.hashLog))
	if len(hashes) != 2 || hashes[0] != hashOf(first) || hashes[1] != hashOf(second) {
		t.Errorf("hashes %v, want the hashes of the first and the second token", hashes)
	}
	if args := readLog(t, f.argsLog); args != "pr create --title x\npr view 1\n" {
		t.Errorf("gh arguments %q", args)
	}
}

func TestGHShimHidesPersonalTokensFromGH(t *testing.T) {
	f := newShimFixture(t)
	bot := testenv.FakeToken("shim_bot")
	f.seed(t, bot)
	f.extra = append(f.extra, "GH_TOKEN="+fakePersonal, "GITHUB_TOKEN="+fakePersonal, "GH_ENTERPRISE_TOKEN="+fakePersonal, "GITHUB_ENTERPRISE_TOKEN="+fakePersonal, "GH_HOST=ghe.example.com", "GH_CONFIG_DIR=/home/dev/.config/gh")
	if err := os.MkdirAll(f.env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hosts := filepath.Join(f.env.GHConfigDir, "hosts.yml")
	if err := os.WriteFile(hosts, []byte("github.com:\n    oauth_token: "+fakePersonal+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := f.gh("pr", "list")
	if r.code != 0 {
		t.Fatalf("code %d: %s", r.code, r.stderr)
	}
	if got := strings.TrimSpace(readLog(t, f.hashLog)); got != hashOf(bot) {
		t.Errorf("gh saw a token with hash %q, want the bot's %q", got, hashOf(bot))
	}
	cfg := filepath.Join(f.env.CacheHome, "agent-id", "github", "cli-config")
	want := "GITHUB_TOKEN=unset GH_HOST=github.com GH_CONFIG_DIR=" + cfg + " GH_ENTERPRISE_TOKEN=" + decoy + " ACTIVE=1\n"
	if got := readLog(t, f.envLog); got != want {
		t.Errorf("gh environment line %q, want %q", got, want)
	}
	if strings.Contains(r.stdout+r.stderr+readLog(t, f.envLog), fakePersonal) {
		t.Error("the personal token reached gh or the output")
	}
	if data, _ := os.ReadFile(hosts); !strings.Contains(string(data), fakePersonal) {
		t.Error("hosts.yml changed")
	}
	if _, err := os.Stat(filepath.Join(cfg, "hosts.yml")); err == nil {
		t.Error("the neutral configuration directory got a hosts.yml")
	}
}

func TestGHShimRefusesDeniedCommandsWithoutStartingGH(t *testing.T) {
	f := newShimFixture(t)
	f.seed(t, testenv.FakeToken("shim_denied"))
	tests := []struct {
		code string
		args []string
	}{
		{"AID-GH-AUTH", []string{"auth", "token"}},
		{"AID-GH-AUTH", []string{"auth", "status"}},
		{"AID-GH-AUTH", []string{"auth", "login", "--hostname", "ghe.example.com"}},
		{"AID-GH-AUTH", []string{"auth", "setup-git"}},
		{"AID-GH-USER", []string{"api", "user"}},
		{"AID-GH-USER", []string{"status"}},
		{"AID-GH-USER", []string{"gist", "list"}},
		{"AID-GH-USER", []string{"repo", "create", "x"}},
		{"AID-GH-ME", []string{"pr", "list", "--assignee", "@me"}},
		{"AID-GH-ME", []string{"repo", "view", "name"}},
		{"AID-GH-HOST", []string{"api", "--hostname", "ghe.example.com", "repos/o/r"}},
		{"AID-GH-HOST", []string{"pr", "list", "-R", "ghe.example.com/o/r"}},
		{"AID-GH-HOST", []string{"pr", "view", "https://ghe.example.com/o/r/pull/1"}},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			r := f.gh(tt.args...)
			if r.code != 125 || !strings.Contains(r.stderr, "agent-id: "+tt.code+": ") || r.stdout != "" {
				t.Errorf("code %d stdout %q stderr %q, want exit 125 and %s", r.code, r.stdout, r.stderr, tt.code)
			}
		})
	}
	if got := readLog(t, f.argsLog); got != "" {
		t.Errorf("gh started for a denied command: %q", got)
	}
}

func TestGHShimRefusesGHReposOnAnotherHost(t *testing.T) {
	f := newShimFixture(t)
	f.seed(t, testenv.FakeToken("shim_repo"))
	f.extra = append(f.extra, "GH_REPO=ghe.example.com/o/r")
	r := f.gh("pr", "list")
	if r.code != 125 || !strings.Contains(r.stderr, "AID-GH-HOST") || readLog(t, f.argsLog) != "" {
		t.Errorf("code %d stderr %q", r.code, r.stderr)
	}
}

func TestGHShimDoesNotRecurse(t *testing.T) {
	t.Run("the only gh is a link to the launcher", func(t *testing.T) {
		f := newShimFixture(t)
		f.seed(t, testenv.FakeToken("shim_loop"))
		if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 0 {
			t.Fatalf("exec: %d %s", r.code, r.stderr)
		}
		later := filepath.Join(f.env.TempDir, "later")
		if err := os.Remove(filepath.Join(later, "gh")); err != nil {
			t.Fatal(err)
		}
		launcher := filepath.Join(f.env.CacheHome, "agent-id", "github", "neutral-bin", "gh")
		if err := os.Symlink(launcher, filepath.Join(later, "gh")); err != nil {
			t.Fatal(err)
		}
		f.extra = []string{"PATH=" + later + ":/usr/bin:/bin"}
		r := f.gh("pr", "list")
		if r.code != 125 || !strings.Contains(r.stderr, "AID-GH-LOOP") {
			t.Errorf("code %d stderr %q, want exit 125 and AID-GH-LOOP", r.code, r.stderr)
		}
	})
	t.Run("a gh that starts gh through PATH", func(t *testing.T) {
		f := newShimFixture(t)
		f.seed(t, testenv.FakeToken("shim_marker"))
		writeScript(t, filepath.Join(f.env.TempDir, "later", "gh"), `echo "$*" >> "`+f.argsLog+`"
if [ "$1" = outer ]; then gh inner; echo "inner-rc=$?"; fi
`)
		r := f.gh("outer")
		if r.code != 0 || !strings.Contains(r.stdout, "inner-rc=125") || !strings.Contains(r.stderr, "AID-GH-LOOP") {
			t.Errorf("code %d stdout %q stderr %q, want the inner call refused with AID-GH-LOOP", r.code, r.stdout, r.stderr)
		}
		if got := readLog(t, f.argsLog); got != "outer\n" {
			t.Errorf("gh ran %q, want only the outer call", got)
		}
	})
	t.Run("no gh besides the launcher", func(t *testing.T) {
		f := newShimFixture(t)
		f.seed(t, testenv.FakeToken("shim_none"))
		f.extra = []string{"PATH=/usr/bin:/bin"}
		r := f.gh("pr", "list")
		if r.code != 125 || !strings.Contains(r.stderr, "AID-GH-NOCLI") {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
	})
}

func TestGHShimFailsClosedWithoutAToken(t *testing.T) {
	t.Run("token cache that is not private", func(t *testing.T) {
		f := newShimFixture(t)
		f.seed(t, testenv.FakeToken("shim_cache"))
		dir, _ := token.Dir(os.Getenv)
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		r := f.gh("pr", "list")
		if r.code != 125 || !strings.Contains(r.stderr, "AID-GH-TOKEN") || !strings.Contains(r.stderr, "the token cache is not private") || readLog(t, f.argsLog) != "" {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
	})
	t.Run("profile that is not ready", func(t *testing.T) {
		f := newShimFixture(t)
		r := f.run("exec", "--profile", "reviewer", "--", "gh", "pr", "list")
		if r.code != 125 || strings.Contains(r.stderr, "AID-GH-TOKEN") || readLog(t, f.argsLog) != "" {
			t.Errorf("code %d stderr %q, want exec to refuse the profile before gh", r.code, r.stderr)
		}
	})
	t.Run("outside an agent context", func(t *testing.T) {
		f := newShimFixture(t)
		f.seed(t, testenv.FakeToken("shim_outside"))
		r := f.run("cli", "gh", "--", "pr", "list")
		if r.code != 125 || !strings.Contains(r.stderr, "AID-GH-CONTEXT") || readLog(t, f.argsLog) != "" {
			t.Errorf("code %d stderr %q", r.code, r.stderr)
		}
	})
	t.Run("launcher without the agent-id binary", func(t *testing.T) {
		f := newShimFixture(t)
		if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 0 {
			t.Fatalf("exec: %d %s", r.code, r.stderr)
		}
		launcher := filepath.Join(f.env.CacheHome, "agent-id", "github", "neutral-bin", "gh")
		cmd := exec.Command(launcher, "pr", "list")
		cmd.Env = append(os.Environ(), f.extra...)
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if err == nil || !errorsAs(err, &exit) || exit.ExitCode() != 125 || !strings.Contains(string(out), "AID-GH-TOKEN") || readLog(t, f.argsLog) != "" {
			t.Errorf("err %v output %q", err, out)
		}
	})
}

func errorsAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

func TestGHShimStartsTheRealGH(t *testing.T) {
	real, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh is not installed")
	}
	f := newShimFixture(t)
	f.seed(t, testenv.FakeToken("shim_real"))
	f.extra = []string{"PATH=" + filepath.Dir(real) + ":/usr/bin:/bin"}
	r := f.gh("--version")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "gh version") {
		t.Errorf("gh --version through the shim: %d %q %q", r.code, r.stdout, r.stderr)
	}
}

func TestGHShimRefusesAConfigFileThatTheAgentStored(t *testing.T) {
	f := newShimFixture(t)
	f.seed(t, testenv.FakeToken("shim_config"))
	if r := f.gh("pr", "list"); r.code != 0 {
		t.Fatalf("first call: %d %s", r.code, r.stderr)
	}
	cfg := filepath.Join(f.env.CacheHome, "agent-id", "github", "cli-config")
	if err := os.WriteFile(filepath.Join(cfg, "config.yml"), []byte("aliases:\n    t: auth token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readLog(t, f.argsLog)
	r := f.gh("t")
	if r.code != 125 || !strings.Contains(r.stderr, "contains config.yml") || readLog(t, f.argsLog) != before {
		t.Errorf("shim: code %d stderr %q, want exit 125 and a refusal that names config.yml", r.code, r.stderr)
	}
	if r := f.run("exec", "--profile", "claude", "--", "true"); r.code != 125 || !strings.Contains(r.stderr, "contains config.yml") {
		t.Errorf("exec: code %d stderr %q, want exit 125", r.code, r.stderr)
	}
	if err := os.Remove(filepath.Join(cfg, "config.yml")); err != nil {
		t.Fatal(err)
	}
	if r := f.gh("pr", "list"); r.code != 0 {
		t.Errorf("after the removal: code %d stderr %q", r.code, r.stderr)
	}
}
