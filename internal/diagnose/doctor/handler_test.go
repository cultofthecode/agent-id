package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

// acceptAll validates every provider block of the fake provider.
type acceptAll struct{}

func (acceptAll) ValidateSettings(string, json.RawMessage) error { return nil }

const configBody = `{"version":1,"profiles":{"claude":{"provider":"fake","host":"git.agent-id.test","fake":{"app":1}},"other":{"provider":"acme","host":"acme.test","acme":{}}}}`

type fakeGit struct {
	version string
	verr    error
	urls    []string
	inRepo  bool
	rerr    error
}

func (g fakeGit) Version(context.Context) (string, error) { return g.version, g.verr }
func (g fakeGit) RemoteURLs(context.Context) ([]string, bool, error) {
	return g.urls, g.inRepo, g.rerr
}

// failingWrites makes the identity refresh fail.
type failingWrites struct{ Store }

func (failingWrites) Write(func(*profile.Config) error) error {
	return errors.New("read-only file system")
}

type rig struct {
	h    *Handler
	fake *providertest.Fake
	path string
	env  []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	env := testenv.Isolate(t)
	dir := filepath.Join(env.ConfigHome, "agent-id")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, err := profile.NewRegistry(profile.Registration{Name: "fake", Validator: acceptAll{}, TokenPrefixes: []string{"ghs_", "ghp_", testenv.FakeTokenPrefix}})
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{fake: providertest.NewFake(), path: path}
	store := profile.NewStore(path, registry)
	r.h = &Handler{
		OpenStore:    func() (Store, error) { return store, nil },
		Supports:     registry.Supports,
		OpenProvider: func(profile.Profile) (provider.Provider, error) { return r.fake, nil },
		Git:          fakeGit{version: "git version 2.54.0"},
		CacheDir:     func() (string, error) { return filepath.Join(env.CacheHome, "agent-id", "tokens"), nil },
		Environ:      func() []string { return r.env },
		Output:       diagnose.Output{Patterns: diagnose.NewPatterns(registry.TokenPrefixes()), Home: env.Home},
	}
	return r
}

func (r *rig) run(t *testing.T, asJSON bool) (string, int) {
	t.Helper()
	out, code, err := r.h.Handle(context.Background(), Input{Profile: "claude", JSON: asJSON})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	return string(out), code
}

// statuses returns the status of the first result of each id.
func statuses(t *testing.T, out string) map[string]diagnose.Status {
	t.Helper()
	var doc struct{ Results []diagnose.Result }
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	m := map[string]diagnose.Status{}
	for _, r := range doc.Results {
		if _, ok := m[r.ID]; !ok {
			m[r.ID] = r.Status
		}
	}
	return m
}

func want(t *testing.T, got map[string]diagnose.Status, id string, status diagnose.Status) {
	t.Helper()
	if got[id] != status {
		t.Errorf("%s = %q, want %q", id, got[id], status)
	}
}

func TestHealthyProfile(t *testing.T) {
	r := newRig(t)
	out, code := r.run(t, true)
	got := statuses(t, out)
	for _, id := range []string{"config.load", "config.profile", "provider.token", "provider.identity", "git.version", "env.tokenlike", "gh.host"} {
		want(t, got, id, diagnose.StatusOK)
	}
	want(t, got, "config.unsupported", diagnose.StatusInfo)
	want(t, got, "token.cache", diagnose.StatusSkip)
	want(t, got, "key.file", diagnose.StatusOK)
	want(t, got, "provider.repo", diagnose.StatusSkip)
	want(t, got, "git.remote.ssh", diagnose.StatusSkip)
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	text, _ := r.run(t, false)
	if !strings.HasPrefix(text, "agent-id doctor — profile claude (fake, git.agent-id.test)\n") || !strings.Contains(text, "0 failures, 0 warnings,") {
		t.Errorf("text report:\n%s", text)
	}
}

func TestMissingConfiguration(t *testing.T) {
	r := newRig(t)
	if err := os.Remove(r.path); err != nil {
		t.Fatal(err)
	}
	out, code := r.run(t, true)
	got := statuses(t, out)
	want(t, got, "config.load", diagnose.StatusFail)
	want(t, got, "config.profile", diagnose.StatusSkip)
	want(t, got, "provider.token", diagnose.StatusSkip)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestUnknownProfile(t *testing.T) {
	r := newRig(t)
	out, code, err := r.h.Handle(context.Background(), Input{Profile: "ghost", JSON: true})
	if err != nil || code != 1 {
		t.Fatalf("Handle = %v, %d", err, code)
	}
	got := statuses(t, string(out))
	want(t, got, "config.load", diagnose.StatusOK)
	want(t, got, "config.profile", diagnose.StatusFail)
}

func TestTokenFailures(t *testing.T) {
	for _, failure := range []error{provider.ErrUnauthorized, provider.ErrForbidden, provider.ErrNotFound, provider.ErrRateLimited, provider.ErrUnavailable} {
		t.Run(failure.Error(), func(t *testing.T) {
			r := newRig(t)
			r.fake.FailTokens(failure)
			out, code := r.run(t, true)
			got := statuses(t, out)
			want(t, got, "provider.token", diagnose.StatusFail)
			want(t, got, "provider.identity", diagnose.StatusOK)
			want(t, got, "provider.repo", diagnose.StatusSkip)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
		})
	}
}

func TestKeyFailureStopsTheProviderChecks(t *testing.T) {
	r := newRig(t)
	r.h.OpenProvider = func(profile.Profile) (provider.Provider, error) {
		return nil, errors.Join(credential.ErrInsecureKeyFile, errors.New("/x/k.pem has mode 0644"))
	}
	out, code := r.run(t, true)
	got := statuses(t, out)
	want(t, got, "key.file", diagnose.StatusFail)
	want(t, got, "provider.token", diagnose.StatusSkip)
	want(t, got, "provider.identity", diagnose.StatusSkip)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestRepositoryAccess(t *testing.T) {
	tests := []struct {
		name   string
		urls   []string
		inRepo bool
		repos  []string
		rerr   error
		status diagnose.Status
	}{
		{"readable", []string{"https://git.agent-id.test/o/r.git"}, true, []string{"o/r"}, nil, diagnose.StatusOK},
		{"readable, other case", []string{"https://git.agent-id.test/O/R"}, true, []string{"o/r"}, nil, diagnose.StatusOK},
		{"not installed", []string{"https://git.agent-id.test/o/r"}, true, []string{"o/other"}, nil, diagnose.StatusFail},
		{"ssh remote on the host", []string{"git@git.agent-id.test:o/r.git"}, true, nil, nil, diagnose.StatusFail},
		{"remote with credentials", []string{"https://user:pw@git.agent-id.test/o/r"}, true, []string{"o/r"}, nil, diagnose.StatusOK},
		{"other host only", []string{"https://example.test/o/r"}, true, nil, nil, diagnose.StatusSkip},
		{"no remote", nil, true, nil, nil, diagnose.StatusSkip},
		{"outside a repository", nil, false, nil, nil, diagnose.StatusSkip},
		{"git cannot list", nil, true, nil, errors.New("boom"), diagnose.StatusFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.fake.SetRepositories(tt.repos...)
			r.h.Git = fakeGit{version: "git version 2.54.0", urls: tt.urls, inRepo: tt.inRepo, rerr: tt.rerr}
			out, _ := r.run(t, true)
			want(t, statuses(t, out), "provider.repo", tt.status)
		})
	}
}

func TestRepositoryListFailure(t *testing.T) {
	r := newRig(t)
	r.fake.FailRepositories(provider.ErrForbidden)
	r.h.Git = fakeGit{version: "git version 2.54.0", urls: []string{"https://git.agent-id.test/o/r"}, inRepo: true}
	out, code := r.run(t, true)
	want(t, statuses(t, out), "provider.repo", diagnose.StatusFail)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestMissingGitIsAGatherFailure(t *testing.T) {
	r := newRig(t)
	r.h.Git = fakeGit{verr: errors.New("git: executable file not found"), urls: []string{"https://git.agent-id.test/o/r"}, inRepo: true}
	out, code := r.run(t, true)
	got := statuses(t, out)
	want(t, got, "gather.git", diagnose.StatusFail)
	want(t, got, "provider.repo", diagnose.StatusSkip)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestIdentityRefreshWritesOnlyTheIdentity(t *testing.T) {
	r := newRig(t)
	refreshed := `{"app":1,"identity":"fake-bot[bot]"}`
	r.fake.SetIdentityCheck(provider.IdentityCheck{Bot: providertest.FakeBot, Match: false, Refresh: func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(refreshed), nil
	}})
	before, _ := os.ReadFile(r.path)
	out, code := r.run(t, true)
	want(t, statuses(t, out), "provider.identity", diagnose.StatusWarn)
	if code != 0 {
		t.Errorf("exit %d, want 0: a refresh is a warning", code)
	}
	after, _ := os.ReadFile(r.path)
	if bytes.Equal(before, after) {
		t.Fatal("the configuration was not refreshed")
	}
	var doc struct {
		Profiles map[string]json.RawMessage
	}
	if err := json.Unmarshal(after, &doc); err != nil {
		t.Fatal(err)
	}
	var claude struct {
		Provider, Host string
		Fake           map[string]any
	}
	if err := json.Unmarshal(doc.Profiles["claude"], &claude); err != nil || claude.Provider != "fake" || claude.Host != "git.agent-id.test" || claude.Fake["identity"] != "fake-bot[bot]" || claude.Fake["app"] != float64(1) {
		t.Errorf("claude after the refresh = %+v, %v", claude, err)
	}
	var other bytes.Buffer
	if err := json.Compact(&other, doc.Profiles["other"]); err != nil || other.String() != `{"provider":"acme","host":"acme.test","acme":{}}` {
		t.Errorf("the profile of another provider changed: %s", other.String())
	}
}

func TestIdentityRefreshFailureAndRateLimit(t *testing.T) {
	r := newRig(t)
	r.fake.SetIdentityCheck(provider.IdentityCheck{Bot: providertest.FakeBot, Refresh: func(s json.RawMessage) (json.RawMessage, error) { return s, nil }})
	store, _ := r.h.OpenStore()
	r.h.OpenStore = func() (Store, error) { return failingWrites{store}, nil }
	out, code := r.run(t, true)
	want(t, statuses(t, out), "provider.identity", diagnose.StatusWarn)
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}

	r = newRig(t)
	r.fake.FailIdentity(provider.ErrRateLimited)
	out, code = r.run(t, true)
	want(t, statuses(t, out), "provider.identity", diagnose.StatusWarn)
	if code != 0 {
		t.Errorf("a rate-limited lookup exits %d, want 0", code)
	}

	r = newRig(t)
	r.fake.FailIdentity(provider.ErrUnavailable)
	out, code = r.run(t, true)
	want(t, statuses(t, out), "provider.identity", diagnose.StatusFail)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestTokenCacheIsChecked(t *testing.T) {
	r := newRig(t)
	cache, _ := r.h.CacheDir()
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := r.run(t, true)
	want(t, statuses(t, out), "token.cache", diagnose.StatusFail)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if err := os.Chmod(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ = r.run(t, true)
	want(t, statuses(t, out), "token.cache", diagnose.StatusOK)
	entries, _ := os.ReadDir(cache)
	if len(entries) != 0 {
		t.Error("doctor wrote into the token cache")
	}
}

func TestEnvironmentChecks(t *testing.T) {
	r := newRig(t)
	r.env = []string{"PATH=/usr/bin", "GH_TOKEN=" + testenv.FakeToken("env"), "GH_HOST=ghe.test", "GH_REPO=ghe.test/o/r", "HOME=/x"}
	out, code := r.run(t, true)
	got := statuses(t, out)
	want(t, got, "env.tokenlike", diagnose.StatusWarn)
	want(t, got, "gh.host", diagnose.StatusWarn)
	if code != 0 {
		t.Errorf("exit %d, want 0: conflicts that agent mode neutralizes only warn", code)
	}
	if strings.Contains(out, testenv.FakeTokenPrefix) || !strings.Contains(out, "GH_TOKEN holds a token-shaped value") {
		t.Errorf("the variable must be reported by name only:\n%s", out)
	}
}

func TestDecoysNeverReachTheOutput(t *testing.T) {
	decoys := []string{testenv.FakeToken("one"), "ghs_" + "decoyghs1234567890", "ghp_" + "decoyghp1234567890", "-----BEGIN RSA PRIVATE KEY-----", "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxIn0.c2ln", "hunter2secret"}
	for _, asJSON := range []bool{false, true} {
		r := newRig(t)
		r.env = []string{"GH_TOKEN=" + decoys[0], "HOMEBREW_GITHUB_API_TOKEN=" + decoys[1], "OTHER=" + decoys[4], "GH_HOST=" + decoys[2]}
		r.h.Git = fakeGit{version: "git version " + decoys[1], urls: []string{"https://user:" + decoys[5] + "@git.agent-id.test/o/r"}, inRepo: true}
		r.h.OpenProvider = func(profile.Profile) (provider.Provider, error) {
			return nil, errors.Join(credential.ErrInvalidKey, errors.New(decoys[3]+"\nMIIB "+decoys[5]))
		}
		out, _ := r.run(t, asJSON)
		for _, d := range decoys[:5] {
			if strings.Contains(out, d) {
				t.Errorf("json=%v: the output holds the decoy %q:\n%s", asJSON, d, out)
			}
		}
		if err := r.h.Output.Patterns.Guard([]byte(out)); err != nil {
			t.Errorf("json=%v: the output fails the guard: %v", asJSON, err)
		}
		if !strings.Contains(out, "GH_TOKEN") || !strings.Contains(out, "HOMEBREW_GITHUB_API_TOKEN") {
			t.Errorf("json=%v: decoys must still be reported by variable name:\n%s", asJSON, out)
		}
	}
}

func TestCommand(t *testing.T) {
	r := newRig(t)
	run := func(args ...string) (string, error) {
		cmd := NewCommand(r.h)
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stdout)
		cmd.SetArgs(args)
		cmd.SilenceUsage, cmd.SilenceErrors = true, true
		err := cmd.Execute()
		return stdout.String(), err
	}
	for _, args := range [][]string{{}, {"--bogus=" + testenv.FakeToken("flag")}, {"--profile", "claude", "extra"}} {
		out, err := run(args...)
		var exit *diagnose.ExitError
		if !errors.As(err, &exit) || exit.Code != 2 || strings.Contains(exit.Message, testenv.FakeTokenPrefix) || out != "" {
			t.Errorf("%v: %q, %v; want exit 2 with nothing on stdout and no token in the message", args, out, err)
		}
	}
	out, err := run("--profile", "claude")
	if err != nil || !strings.Contains(out, "config.load") {
		t.Errorf("doctor --profile claude: %v\n%s", err, out)
	}
	r.fake.FailTokens(provider.ErrUnauthorized)
	_, err = run("--profile", "claude")
	var exit *diagnose.ExitError
	if !errors.As(err, &exit) || exit.Code != 1 || exit.Message != "" {
		t.Errorf("a failing check must exit 1 without a message: %v", err)
	}
}
