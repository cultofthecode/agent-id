//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

// Names of the environment variables of the E2E run. Only AGENT_ID_E2E_KEY
// refers to secret material, and it holds a path, never the key.
const (
	envEnable         = "AGENT_ID_E2E"
	envPush           = "AGENT_ID_E2E_PUSH"
	envKey            = "AGENT_ID_E2E_KEY"
	envAppID          = "AGENT_ID_E2E_APP_ID"
	envInstallationID = "AGENT_ID_E2E_INSTALLATION_ID"
	envRepoURL        = "AGENT_ID_E2E_REPO_URL"
	envBotLogin       = "AGENT_ID_E2E_BOT_LOGIN"
)

const (
	profileName       = "e2e"
	badProfileName    = "e2e-bad"
	developerName     = "E2E Developer"
	developerEmail    = "e2e-developer@example.invalid"
	repoNameSuffix    = "-e2e"
	branchPrefix      = "agent-id-e2e/"
	commandTimeout    = 2 * time.Minute
	networkTestsTitle = "agent-id e2e"
)

// secretPattern matches the shapes of token and key material. No output of
// any command may match it.
var secretPattern = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{8,}|github_pat_[A-Za-z0-9_]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.`)

// config is the checked configuration of the run.
type config struct {
	keyPath        string
	appID          string
	installationID string
	repoURL        string
	repoSlug       string
	botLogin       string
	push           bool
	path           string
}

// run is the isolated environment of one test run.
type run struct {
	t      *testing.T
	cfg    config
	env    testenv.Env
	binary string
	repo   string
}

func TestMain(m *testing.M) {
	testenv.Main(m)
}

// TestEndToEnd is the scenario of issue #27: a developer and a bot work in
// one disposable repository.
func TestEndToEnd(t *testing.T) {
	cfg := loadConfig(t)
	r := newRun(t, cfg)
	botEmail := r.setupProfile()
	r.initRepository()
	r.t.Run("developer commits as the developer", func(t *testing.T) { r.developerCommit(t) })
	r.t.Run("bot commits as the bot", func(t *testing.T) { r.botCommit(t, botEmail) })
	r.t.Run("bot commits are not signed", func(t *testing.T) { r.checkUnsigned(t) })
	r.t.Run("invalid App credentials: no push, no fallback", func(t *testing.T) { r.badCredentialsPush(t) })
	r.t.Run("bot pushes a branch and opens a PR", func(t *testing.T) { r.pushAndPullRequest(t, botEmail) })
	r.t.Run("no secret in diagnostics", func(t *testing.T) { r.diagnostics(t) })
}

// loadConfig reads and checks the environment. It skips the test unless the
// run is enabled explicitly, and it refuses to run in CI or against a
// repository whose name does not end with "-e2e".
func loadConfig(t *testing.T) config {
	t.Helper()
	if os.Getenv(envEnable) != "1" {
		t.Skipf("%s is not 1: the E2E run is manual, see docs/runbooks/e2e.md", envEnable)
	}
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Fatal("the E2E run never runs in CI")
	}
	cfg := config{
		keyPath:        os.Getenv(envKey),
		appID:          os.Getenv(envAppID),
		installationID: os.Getenv(envInstallationID),
		repoURL:        os.Getenv(envRepoURL),
		botLogin:       os.Getenv(envBotLogin),
		push:           os.Getenv(envPush) == "1",
	}
	for name, value := range map[string]string{envKey: cfg.keyPath, envAppID: cfg.appID, envInstallationID: cfg.installationID, envRepoURL: cfg.repoURL, envBotLogin: cfg.botLogin} {
		if value == "" {
			t.Fatalf("%s is not set: see docs/runbooks/e2e.md", name)
		}
	}
	if !filepath.IsAbs(cfg.keyPath) {
		t.Fatalf("%s must be an absolute path", envKey)
	}
	cfg.repoSlug = repoSlug(t, cfg.repoURL)
	return cfg
}

// repoSlug returns owner/name of an https repository URL and fails unless
// the name ends with the disposable-repository suffix.
func repoSlug(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		t.Fatalf("%s must be a plain https URL without user-info", envRepoURL)
	}
	parts := strings.Split(strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/"), "/")
	if len(parts) != 2 || !strings.HasSuffix(parts[1], repoNameSuffix) {
		t.Fatalf("%s must name a repository owner/name where name ends with %q", envRepoURL, repoNameSuffix)
	}
	return parts[0] + "/" + parts[1]
}

// newRun isolates the process and builds the binary. The tests read their
// configuration before Isolate, which removes every AGENT_ID_ variable.
func newRun(t *testing.T, cfg config) *run {
	t.Helper()
	cfg.path = os.Getenv("PATH")
	env := testenv.Isolate(t)
	binary := testenv.BuildBinary(t, env, "github.com/cultofthecode/agent-id/cmd/agent-id")
	repo := filepath.Join(env.Root, "work")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	return &run{t: t, cfg: cfg, env: env, binary: binary, repo: repo}
}

// childEnv returns the environment of every child: the isolated directories
// and PATH, and nothing from the developer's shell. It carries no proxy
// variable, because the child must reach GitHub.
func (r *run) childEnv(extra ...string) []string {
	env := []string{
		"PATH=" + r.cfg.path,
		"HOME=" + r.env.Home,
		"XDG_CONFIG_HOME=" + r.env.ConfigHome,
		"XDG_CACHE_HOME=" + r.env.CacheHome,
		"XDG_STATE_HOME=" + r.env.StateHome,
		"XDG_DATA_HOME=" + r.env.DataHome,
		"GIT_CONFIG_GLOBAL=" + r.env.GitConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GH_CONFIG_DIR=" + r.env.GHConfigDir,
		"TMPDIR=" + r.env.TempDir,
	}
	return append(env, extra...)
}

// result is the outcome of one command.
type result struct {
	out  string
	code int
}

// exec runs name with args in dir and returns its combined output and exit
// code. It fails the test when the output holds secret-looking material, and
// then it prints no output.
func (r *run) exec(dir string, env []string, name string, args ...string) result {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	res := result{out: buf.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		res.code = exit.ExitCode()
	default:
		r.t.Fatalf("start %s: %v", filepath.Base(name), err)
	}
	if secretPattern.MatchString(res.out) {
		r.t.Fatalf("output of %s %s matches a token or key pattern: output withheld", filepath.Base(name), firstWord(args))
	}
	return res
}

func firstWord(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// agentID runs the agent-id binary.
func (r *run) agentID(dir string, args ...string) result {
	r.t.Helper()
	return r.exec(dir, r.childEnv(), r.binary, args...)
}

// asBot runs a command through agent-id exec with the profile.
func (r *run) asBot(profile string, args ...string) result {
	r.t.Helper()
	all := append([]string{"exec", "--profile", profile, "--"}, args...)
	return r.agentID(r.repo, all...)
}

// asDeveloper runs git as the developer: the developer's identity comes from
// the environment, and no agent-id variable is present.
func (r *run) asDeveloper(args ...string) result {
	r.t.Helper()
	env := r.childEnv(
		"GIT_AUTHOR_NAME="+developerName, "GIT_AUTHOR_EMAIL="+developerEmail,
		"GIT_COMMITTER_NAME="+developerName, "GIT_COMMITTER_EMAIL="+developerEmail,
	)
	return r.exec(r.repo, env, "git", args...)
}

// mustOK fails the test unless res has exit code 0.
func (r *run) mustOK(what string, res result) result {
	r.t.Helper()
	if res.code != 0 {
		r.t.Fatalf("%s: exit %d\n%s", what, res.code, res.out)
	}
	return res
}

// setupProfile runs agent-id setup github with the real App and returns the
// commit email of the bot from the summary.
func (r *run) setupProfile() string {
	r.t.Helper()
	res := r.mustOK("setup github", r.agentID(r.repo, "setup", "github", "--profile", profileName,
		"--app-id", r.cfg.appID, "--installation-id", r.cfg.installationID, "--private-key", r.cfg.keyPath))
	email := summaryValue(res.out, "Commit email:")
	if email == "" {
		r.t.Fatalf("setup summary has no commit email line:\n%s", res.out)
	}
	if got := summaryValue(res.out, "Identity:"); !strings.Contains(got, r.cfg.botLogin) {
		r.t.Fatalf("setup identity is %q, want the bot %q", got, r.cfg.botLogin)
	}
	return email
}

// summaryValue returns the value of a label line of the setup summary.
func summaryValue(out, label string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, label); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// initRepository creates the local working repository. It sets the remote
// URL but contacts no host.
func (r *run) initRepository() {
	r.t.Helper()
	r.mustOK("git init", r.asDeveloper("init", "-q", "-b", "main"))
	r.mustOK("git remote add", r.asDeveloper("remote", "add", "origin", r.cfg.repoURL))
}

// commitLine returns "author name <email>|committer name <email>" of HEAD.
func (r *run) commitLine(rev string) string {
	r.t.Helper()
	res := r.mustOK("git log", r.asDeveloper("log", "-1", "--format=%an <%ae>|%cn <%ce>", rev))
	return strings.TrimSpace(res.out)
}

func (r *run) developerCommit(t *testing.T) {
	t.Helper()
	write(t, filepath.Join(r.repo, "developer.txt"), "written by the developer\n")
	r.mustOK("git add", r.asDeveloper("add", "developer.txt"))
	r.mustOK("git commit", r.asDeveloper("commit", "-q", "-m", "e2e: developer commit"))
	want := fmt.Sprintf("%s <%s>|%s <%s>", developerName, developerEmail, developerName, developerEmail)
	if got := r.commitLine("HEAD"); got != want {
		t.Fatalf("developer commit identity = %q, want %q", got, want)
	}
}

func (r *run) botCommit(t *testing.T, botEmail string) {
	t.Helper()
	write(t, filepath.Join(r.repo, "bot.txt"), "written by the bot\n")
	r.mustOK("bot git add", r.asBot(profileName, "git", "add", "bot.txt"))
	r.mustOK("bot git commit", r.asBot(profileName, "git", "commit", "-q", "-m", "e2e: bot commit"))
	bot := r.cfg.botLogin + " <" + botEmail + ">"
	if got, want := r.commitLine("HEAD"), bot+"|"+bot; got != want {
		t.Fatalf("bot commit identity = %q, want %q", got, want)
	}
	if got := r.commitLine("HEAD~1"); strings.Contains(got, r.cfg.botLogin) {
		t.Fatalf("developer commit carries the bot identity after the bot commit")
	}
}

func (r *run) checkUnsigned(t *testing.T) {
	t.Helper()
	res := r.mustOK("git log signature", r.asDeveloper("log", "-2", "--format=%G?"))
	for _, status := range strings.Fields(res.out) {
		if status != "N" {
			t.Fatalf("a local commit has signature status %q, want N (not signed): attribution only, never verified", status)
		}
	}
}

// badCredentialsPush proves the negative case: with an App key that GitHub
// rejects, the bot push fails and no branch appears. The key is a throwaway
// RSA key made here, written 0600 in the isolated tree.
func (r *run) badCredentialsPush(t *testing.T) {
	t.Helper()
	r.writeBadProfile(t)
	branch := r.newBranchName("bad")
	res := r.asBot(badProfileName, "git", "push", "origin", "HEAD:refs/heads/"+branch)
	if res.code == 0 {
		t.Fatalf("push with invalid App credentials succeeded")
	}
	r.t.Cleanup(func() { r.assertBranchAbsent(branch) })
	t.Logf("push with invalid App credentials failed with exit %d", res.code)
}

// writeBadProfile adds a profile that uses the real App and installation
// IDs with a throwaway key, by copying the setup config and replacing the
// key reference.
func (r *run) writeBadProfile(t *testing.T) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	badKey := filepath.Join(r.env.Root, "bad-key.pem")
	write(t, badKey, string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})))
	if err := os.Chmod(badKey, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.env.ConfigHome, "agent-id", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version  int                        `json:"version"`
		Profiles map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var prof map[string]any
	if err := json.Unmarshal(doc.Profiles[profileName], &prof); err != nil {
		t.Fatal(err)
	}
	gh, _ := prof["github"].(map[string]any)
	if gh == nil {
		t.Fatal("setup config has no provider block")
	}
	gh["private_key"] = "file:" + badKey
	patched, err := json.Marshal(prof)
	if err != nil {
		t.Fatal(err)
	}
	doc.Profiles[badProfileName] = patched
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// newBranchName returns a unique branch name that the run owns.
func (r *run) newBranchName(tag string) string {
	return fmt.Sprintf("%s%s-%d", branchPrefix, tag, time.Now().UnixNano())
}

// requirePush skips the subtest unless the push phase is enabled.
func (r *run) requirePush(t *testing.T) {
	t.Helper()
	if !r.cfg.push {
		t.Skipf("%s is not 1: the push and PR steps need token injection (#22, #23)", envPush)
	}
}

// assertBranchAbsent checks with the bot that the branch does not exist. It
// does nothing when the push phase is off, because the bot cannot read the
// repository without token injection.
func (r *run) assertBranchAbsent(branch string) {
	if !r.cfg.push {
		return
	}
	res := r.asBot(profileName, "git", "ls-remote", "--heads", "origin", branch)
	if res.code != 0 || strings.TrimSpace(res.out) != "" {
		r.t.Errorf("branch %s is not absent after the negative push (exit %d)", branch, res.code)
	}
}

func (r *run) pushAndPullRequest(t *testing.T, botEmail string) {
	r.requirePush(t)
	branch := r.newBranchName("pr")
	t.Cleanup(func() { r.deleteBranch(branch) })
	r.mustOK("bot push", r.asBot(profileName, "git", "push", "origin", "HEAD:refs/heads/"+branch))
	number := r.openPullRequest(t, branch)
	t.Cleanup(func() { r.closePullRequest(number) })
	r.checkPullRequestAuthor(t, number)
	r.checkRemoteCommit(t, branch, botEmail)
}

func (r *run) openPullRequest(t *testing.T, branch string) string {
	t.Helper()
	res := r.mustOK("gh pr create", r.asBot(profileName, "gh", "pr", "create", "--repo", r.cfg.repoSlug,
		"--head", branch, "--base", "main", "--title", networkTestsTitle+": "+branch, "--body", "Created by the manual E2E run. Safe to close."))
	m := regexp.MustCompile(`/pull/(\d+)`).FindStringSubmatch(res.out)
	if m == nil {
		t.Fatalf("gh pr create printed no PR URL:\n%s", res.out)
	}
	return m[1]
}

func (r *run) checkPullRequestAuthor(t *testing.T, number string) {
	t.Helper()
	res := r.mustOK("gh pr view", r.asBot(profileName, "gh", "pr", "view", number, "--repo", r.cfg.repoSlug, "--json", "author", "--jq", ".author.login"))
	want := strings.TrimSuffix(r.cfg.botLogin, "[bot]")
	if got := strings.TrimSpace(res.out); got != want && got != r.cfg.botLogin && got != "app/"+want {
		t.Fatalf("PR author = %q, want the bot %q", got, r.cfg.botLogin)
	}
}

func (r *run) checkRemoteCommit(t *testing.T, branch, botEmail string) {
	t.Helper()
	res := r.mustOK("gh api commit", r.asBot(profileName, "gh", "api", "repos/"+r.cfg.repoSlug+"/commits/"+branch,
		"--jq", `[.commit.author.name, .commit.author.email, .commit.committer.name, .commit.committer.email, (.commit.verification.verified|tostring)]|join("|")`))
	got := strings.Split(strings.TrimSpace(res.out), "|")
	want := []string{r.cfg.botLogin, botEmail, r.cfg.botLogin, botEmail, "false"}
	if len(got) != len(want) {
		t.Fatalf("GitHub commit record has %d fields, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GitHub commit field %d = %q, want %q (verified must be false: attribution is not proof)", i, got[i], want[i])
		}
	}
}

// closePullRequest and deleteBranch run in cleanup. They never fail the
// test run before the other cleanups run, but they log a manual step.
func (r *run) closePullRequest(number string) {
	if res := r.asBot(profileName, "gh", "pr", "close", number, "--repo", r.cfg.repoSlug); res.code != 0 {
		r.t.Errorf("CLEANUP NEEDED: close PR #%s in %s", number, r.cfg.repoSlug)
	}
}

func (r *run) deleteBranch(branch string) {
	if res := r.asBot(profileName, "git", "push", "origin", "--delete", branch); res.code != 0 {
		r.t.Errorf("CLEANUP NEEDED: delete branch %s in %s", branch, r.cfg.repoSlug)
	}
}

// diagnostics runs whoami and doctor inside an agent context and requires
// exit 0. exec already fails the test on a token or key shape in the output.
// doctor prints the key file path by design, so the check does not forbid it.
func (r *run) diagnostics(t *testing.T) {
	t.Helper()
	for _, args := range [][]string{{"whoami"}, {"doctor", "--profile", profileName}} {
		res := r.asBot(profileName, append([]string{r.binary}, args...)...)
		if res.code != 0 {
			t.Fatalf("agent-id %s: exit %d (3 means the output guard withheld the report)\n%s", args[0], res.code, res.out)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
