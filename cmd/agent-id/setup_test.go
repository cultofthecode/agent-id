package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/setup"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// githubFields mirror the GitHub onboarder's fields for the fake onboarder;
// the real ones are tested in the adapter and in the binary below.
var githubFields = []provider.SetupField{
	{Name: "app-id", Usage: "the App ID"},
	{Name: "installation-id", Usage: "the installation ID"},
	{Name: "private-key", Usage: "the key file"},
}

// githubResult is a verified GitHub App as the fake onboarder reports it.
func githubResult(appID int64, login string, userID int64, repositories ...string) provider.SetupResult {
	settings := fmt.Sprintf(`{"app_id":%d,"installation_id":789012,"private_key":"file:/keys/claude.pem","identity":{"login":%q,"user_id":%d,"resolved_at":"2026-11-13T10:00:00Z"}}`, appID, login, userID)
	return provider.SetupResult{
		Host:         "github.com",
		Settings:     json.RawMessage(settings),
		Account:      login,
		Identity:     provider.Identity{Name: login, Email: fmt.Sprintf("%d+%s@users.noreply.github.com", userID, login)},
		Details:      []provider.SetupDetail{{Label: "App", Value: fmt.Sprint(appID)}, {Label: "Installation", Value: "789012"}, {Label: "Private key", Value: "file:/keys/claude.pem"}},
		Repositories: repositories,
	}
}

var setupArgs = []string{"setup", "github", "--profile", "claude", "--app-id", "123456", "--installation-id", "789012", "--private-key", "/keys/claude.pem"}

// runSetup runs args in-process with a root whose only onboarder is fake and
// whose store is the production store.
func runSetup(t *testing.T, fake *providertest.FakeOnboarder, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), buildRoot([]provider.Onboarder{fake}, openStore), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// jwtShape matches a JSON Web Token.
var jwtShape = regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)

// checkNoSecretOutput fails when output holds a token prefix, a PEM marker,
// or a JWT.
func checkNoSecretOutput(t *testing.T, output string) {
	t.Helper()
	if strings.Contains(output, "ghs_") || strings.Contains(output, "-----BEGIN") || jwtShape.MatchString(output) {
		t.Errorf("output holds a secret shape: %q", output)
	}
}

// homeFiles returns the content hash of every file under the isolated home
// and the gh configuration directory.
func homeFiles(t *testing.T, env testenv.Env) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, dir := range []string{env.Home, env.GHConfigDir} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = fmt.Sprintf("%x", sha256.Sum256(data))
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	return files
}

// changedFiles returns the paths that differ between two homeFiles results.
func changedFiles(before, after map[string]string) []string {
	var changed []string
	for path, hash := range after {
		if before[path] != hash {
			changed = append(changed, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path)
		}
	}
	slices.Sort(changed)
	return changed
}

// personalConfig writes a personal ~/.gitconfig and a gh hosts.yml in the
// isolated environment.
func personalConfig(t *testing.T, env testenv.Env) {
	t.Helper()
	if err := os.WriteFile(env.GitConfig, []byte("[user]\n\tname = Developer\n\temail = dev@example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.GHConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.GHConfigDir, "hosts.yml"), []byte("github.com:\n    user: developer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSetupWritesAVerifiedProfile(t *testing.T) {
	env := testenv.Isolate(t)
	personalConfig(t, env)
	before := homeFiles(t, env)
	fake := providertest.NewFakeOnboarder("github", githubFields, githubResult(123456, "claude-dev[bot]", 12345678, "o/a", "o/b"))
	code, stdout, stderr := runSetup(t, fake, setupArgs...)
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"Profile:       claude (created)", "Identity:      claude-dev[bot]", "Commit email:  12345678+claude-dev[bot]@users.noreply.github.com", "Repositories:  2\n               o/a\n               o/b\n", "Next: agent-id exec --profile claude -- <command>"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, want it to contain %q", stdout, want)
		}
	}
	if stderr != "" {
		t.Errorf("stderr = %q", stderr)
	}
	checkNoSecretOutput(t, stdout+stderr)
	store, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	config, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p := config.Profiles["claude"]; p.Provider != "github" || p.Host != "github.com" || !strings.Contains(string(p.Settings), `"login":"claude-dev[bot]"`) {
		t.Errorf("profile = %+v", p)
	}
	configPath := store.Path()
	want := []string{configPath, configPath + ".lock"}
	if got := changedFiles(before, homeFiles(t, env)); !slices.Equal(got, want) {
		t.Errorf("changed files = %v, want only %v", got, want)
	}
}

func TestSetupUpdatesTheProfile(t *testing.T) {
	testenv.Isolate(t)
	fake := providertest.NewFakeOnboarder("github", githubFields, githubResult(1, "app-one[bot]", 11))
	if code, _, stderr := runSetup(t, fake, setupArgs...); code != exitOK {
		t.Fatalf("first setup: %s", stderr)
	}
	other := githubResult(3, "app-three[bot]", 33)
	if code, _, stderr := runSetup(t, providertest.NewFakeOnboarder("github", githubFields, other), "setup", "github", "--profile", "reviewer", "--app-id", "3", "--installation-id", "4", "--private-key", "/k.pem"); code != exitOK {
		t.Fatalf("reviewer setup: %s", stderr)
	}
	store, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	fake.SetResult(githubResult(2, "app-two[bot]", 22))
	code, stdout, stderr := runSetup(t, fake, setupArgs...)
	if code != exitOK {
		t.Fatalf("second setup: %s", stderr)
	}
	if !strings.Contains(stdout, "Profile:       claude (updated)") || !strings.Contains(stdout, "Identity:      app-two[bot]") {
		t.Errorf("stdout = %q", stdout)
	}
	after, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after.Profiles["claude"].Settings), `"app_id":2`) || !strings.Contains(string(after.Profiles["claude"].Settings), `"login":"app-two[bot]"`) {
		t.Errorf("claude = %s", after.Profiles["claude"].Settings)
	}
	if !bytes.Equal(after.Profiles["reviewer"].Settings, before.Profiles["reviewer"].Settings) {
		t.Errorf("reviewer changed")
	}
}

func TestSetupFailuresWriteNothing(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"wrong installation", fmt.Errorf("installation 789012 of App 123456 not found: check --installation-id and that the App is installed: %w", provider.ErrNotFound), "agent-id: setup github: installation 789012 of App 123456 not found: check --installation-id"},
		{"rejected credentials", fmt.Errorf("GitHub rejected the App credentials: %w", provider.ErrUnauthorized), "agent-id: setup github: GitHub rejected the App credentials"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testenv.Isolate(t)
			before := homeFiles(t, env)
			fake := providertest.NewFakeOnboarder("github", githubFields, githubResult(1, "a[bot]", 1))
			fake.Fail(tt.err)
			code, stdout, stderr := runSetup(t, fake, setupArgs...)
			if code != exitFailure || stdout != "" || !strings.HasPrefix(stderr, tt.want) {
				t.Fatalf("exit %d, stdout %q, stderr %q; want exit 1 and %q", code, stdout, stderr, tt.want)
			}
			if got := changedFiles(before, homeFiles(t, env)); len(got) != 0 {
				t.Errorf("changed files = %v", got)
			}
		})
	}
}

func TestSetupCommandsOfTheProductionRoot(t *testing.T) {
	testenv.Isolate(t)
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), newRoot(), []string{"setup", "github", "--help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("help: exit %d, %s", code, stderr.String())
	}
	for _, flag := range []string{"--profile", "--app-id", "--installation-id", "--private-key", "the App ID on the App's settings page"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("help does not list %s:\n%s", flag, stdout.String())
		}
	}
	stderr.Reset()
	if code := run(context.Background(), newRoot(), []string{"setup", "gitlab"}, &stdout, &stderr); code != exitFailure {
		t.Errorf("setup gitlab: exit %d", code)
	}
}

// writeKeyFile writes a new 2048-bit RSA key with mode under the test root.
func writeKeyFile(t *testing.T, env testenv.Env, mode os.FileMode) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.Root, "app.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetupBinaryFailsBeforeTheNetwork(t *testing.T) {
	env := testenv.Isolate(t)
	binary := testenv.BuildBinary(t, env, "github.com/cultofthecode/agent-id/cmd/agent-id")
	personalConfig(t, env)
	openKey := writeKeyFile(t, env, 0o644)
	configPath, err := profile.Path(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	token := testenv.FakeToken("pasted")
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxIn0.c2lnbmF0dXJl"
	pemHeader := "-----BEGIN RSA PRIVATE KEY-----"
	args := func(profileName, key string) []string {
		return []string{"setup", "github", "--profile", profileName, "--app-id", "123456", "--installation-id", "789012", "--private-key", key}
	}
	tests := []struct {
		name   string
		config string
		args   []string
		want   []string
		secret string
	}{
		{"missing flags", "", []string{"setup", "github", "--profile", "claude", "--installation-id", "1"}, []string{"agent-id: setup github: missing required flags: --app-id (the App ID on the App's settings page), --private-key (the .pem file downloaded from the App's settings)"}, ""},
		{"key file too open", "", args("claude", openKey), []string{"agent-id: setup github: ", "chmod 600"}, ""},
		{"conflicting provider", `{"version":1,"profiles":{"claude":{"provider":"gitlab","host":"gitlab.com","gitlab":{"x":1}}}}`, args("claude", openKey), []string{`profile "claude" already exists for provider "gitlab"`}, ""},
		{"invalid configuration", `{"version":1,`, args("claude", openKey), []string{"agent-id: setup github: config " + configPath}, ""},
		{"token as key", "", args("claude", token), []string{"agent-id: setup github: --private-key looks like a token or key; it is not shown"}, token},
		{"JWT as key", "", args("claude", jwt), []string{"--private-key looks like a token or key"}, jwt},
		{"PEM header as key", "", args("claude", pemHeader), []string{"--private-key looks like a token or key"}, pemHeader},
		{"token as profile", "", args(token, openKey), []string{"agent-id: setup github: --profile looks like a token or key; it is not shown"}, token},
		{"JWT as profile", "", args(jwt, openKey), []string{"--profile looks like a token or key"}, jwt},
		{"PEM header as profile", "", args(pemHeader, openKey), []string{"--profile looks like a token or key"}, pemHeader},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.RemoveAll(filepath.Dir(configPath))
			if tt.config != "" {
				if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath, []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before := homeFiles(t, env)
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, tt.args...)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			var exitErr *exec.ExitError
			if err := cmd.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != exitFailure {
				t.Fatalf("run: %v, want exit 1 (stderr %q)", err, stderr.String())
			}
			for _, want := range tt.want {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q", stdout.String())
			}
			if tt.secret != "" && strings.Contains(stderr.String(), tt.secret) {
				t.Errorf("stderr echoes the secret: %q", stderr.String())
			}
			checkNoSecretOutput(t, stdout.String()+stderr.String())
			if got := changedFiles(before, homeFiles(t, env)); len(got) != 0 {
				t.Errorf("changed files = %v", got)
			}
		})
	}
}

var _ setup.Store = (*profile.Store)(nil)
