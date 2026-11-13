package github

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/cliint"
	"github.com/cultofthecode/agent-id/internal/credential"
	"github.com/cultofthecode/agent-id/internal/gitint/credentialhelper"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/github/githubtest"
	"github.com/cultofthecode/agent-id/internal/testenv"
	"github.com/cultofthecode/agent-id/internal/token"
)

// The mint-failure scenarios of #25 (E02-S11) for the two surfaces that hand
// a token to another program: the gh shim (internal/cliint) and the Git
// credential helper (internal/gitint/credentialhelper). #88 covers the chain
// below the surfaces and the process level with a broken key. This file runs
// the real adapter chain (key file, adapter, token manager, fake GitHub)
// behind both surfaces. Each scenario must refuse, start nothing, give no
// credential, and leave the decoy personal credentials unused.

const decoyHostsYML = "github.com:\n    oauth_token: " + personalDecoy + "\n"

type surfaces struct {
	*chain
	execs   []string
	realDir string
	binDir  string
}

type recordingExec struct{ s *surfaces }

func (r recordingExec) Exec(path string, _, _ []string) error {
	r.s.execs = append(r.s.execs, path)
	return nil
}

func newSurfaces(t *testing.T) *surfaces {
	t.Helper()
	s := &surfaces{chain: newChain(t)}
	root := t.TempDir()
	s.realDir = filepath.Join(root, "real")
	s.binDir = filepath.Join(root, "neutral-bin")
	for _, d := range []string{s.realDir, s.binDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.realDir, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return s
}

// scenario changes the chain so that the next mint fails. base is the API
// URL for the adapter ("" keeps the fake App's).
type scenario struct {
	name  string
	setup func(s *surfaces)
	base  string
	want  error
	// reason is the fixed phrase of the shim, class the word of the helper.
	reason, class string
}

func (s *surfaces) provider(base string) (provider.Provider, error) {
	if base == "" {
		base = s.app.URL
	}
	pem, err := credential.File{Path: s.keyPath}.PrivateKeyPEM()
	if err != nil {
		return nil, err
	}
	return newProvider(Config{AppID: githubtest.AppID, InstallationID: installationID, PrivateKeyPEM: pem}, api{baseURL: base, client: testenv.NewHTTPClient(s.t)})
}

func (s *surfaces) shim(base string) *cliint.Handler {
	a := newActivator(s.t, resolvedBlock)
	return &cliint.Handler{
		Profile:     func([]string) (string, error) { return "claude", nil },
		Activator:   func(string) (provider.Activator, error) { return a, nil },
		Provider:    func(string) (provider.Provider, error) { return s.provider(base) },
		NeutralDirs: func([]string, provider.Activator) (string, string, error) { return s.t.TempDir(), s.binDir, nil },
		Tokens:      s.manager(),
		Execer:      recordingExec{s},
	}
}

func (s *surfaces) runShim(base string) error {
	parent := []string{
		"PATH=" + s.binDir + ":" + s.realDir + ":/usr/bin",
		"GH_TOKEN=" + personalDecoy, "GITHUB_TOKEN=" + personalDecoy,
		"GH_CONFIG_DIR=" + filepath.Dir(s.hosts),
	}
	return s.shim(base).Handle(context.Background(), cliint.Input{Binary: "gh", Args: []string{"pr", "create", "--title", "x"}, Parent: parent})
}

func (s *surfaces) helper(base string) *credentialhelper.Handler {
	return &credentialhelper.Handler{
		Host: func(string) (string, error) { return host, nil },
		Credential: func(ctx context.Context, _ string) (provider.GitCredential, error) {
			prov, err := s.provider(base)
			if err != nil {
				return provider.GitCredential{}, &credentialhelper.ClassedError{Class: "provider or private key", Err: err}
			}
			tok, err := s.manager().Token(ctx, prov)
			if errors.Is(err, token.ErrUnusableToken) {
				return provider.GitCredential{}, &credentialhelper.ClassedError{Class: "unusable token", Err: err}
			}
			if err != nil {
				return provider.GitCredential{}, err
			}
			return prov.GitCredential(tok), nil
		},
	}
}

func (s *surfaces) runHelper(base string) (credentialhelper.Output, error) {
	return s.helper(base).Handle(context.Background(), credentialhelper.Input{
		Profile: "claude", Operation: credentialhelper.OpGet,
		Stdin: strings.NewReader("protocol=https\nhost=github.com\npath=o/r.git\n\n"),
	})
}

// assertShimRefused checks the gh half of one scenario.
func (s *surfaces) assertShimRefused(err error, reason string) {
	s.t.Helper()
	var r *cliint.Refusal
	if !errors.As(err, &r) {
		s.t.Fatalf("error = %v, want a *cliint.Refusal", err)
	}
	if r.Code != "AID-GH-TOKEN" || r.ExitCode() != cliint.ExitRefused || !strings.Contains(r.Message, "gh was not started") {
		s.t.Errorf("refusal = %v (exit %d)", r, r.ExitCode())
	}
	if reason != "" && !strings.Contains(r.Message, reason) {
		s.t.Errorf("refusal %q lacks the reason %q", r.Message, reason)
	}
	if len(s.execs) != 0 {
		s.t.Errorf("gh started after a failed mint: %v", s.execs)
	}
	s.assertNothingLeaked(r.Error())
}

// assertHelperRefused checks the Git half of one scenario.
func (s *surfaces) assertHelperRefused(out credentialhelper.Output, err error, class string) {
	s.t.Helper()
	if err == nil || out.Stdout != "quit=1\n" {
		s.t.Fatalf("stdout %q error %v, want quit=1 and an error", out.Stdout, err)
	}
	want := `git-credential: cannot get a token for profile "claude" (` + class + ")"
	if !strings.HasPrefix(err.Error(), want) {
		s.t.Errorf("error = %q, want prefix %q", err, want)
	}
	if strings.Contains(out.Stdout, "password=") || strings.Contains(out.Stdout, "username=") {
		s.t.Errorf("the helper gave a credential: %q", out.Stdout)
	}
	s.assertNothingLeaked(out.Stdout + err.Error())
}

func (s *surfaces) assertNothingLeaked(text string) {
	s.t.Helper()
	checkNoSecret(s.t, text, personalDecoy)
	for _, issued := range s.app.Tokens() {
		checkNoSecret(s.t, text, issued.Value)
	}
	if strings.Contains(text, "BEGIN") {
		s.t.Error("key material in the output")
	}
	if data, _ := os.ReadFile(s.hosts); string(data) != decoyHostsYML {
		s.t.Error("the decoy hosts.yml changed")
	}
}

func downBase(t *testing.T) string {
	t.Helper()
	l := testenv.Listen(t)
	base := "http://" + l.Addr().String()
	_ = l.Close()
	return base
}

func TestFailClosedSurfacesMintFailure(t *testing.T) {
	pemOnly := "-----BEGIN RSA PRIVATE KEY-----\nQUFB\n-----END RSA PRIVATE KEY-----\n"
	tests := []scenario{
		{name: "missing key", setup: func(s *surfaces) { _ = os.Remove(s.keyPath) }, class: "provider or private key"},
		{name: "invalid PEM", setup: func(s *surfaces) { s.setKey(0o600, []byte(pemOnly)) }, class: "provider or private key"},
		{name: "not a PEM", setup: func(s *surfaces) { s.setKey(0o600, []byte("hello")) }, class: "provider or private key"},
		{name: "401 from GitHub", setup: func(s *surfaces) { s.app.FailTokens(http.StatusUnauthorized) }, want: provider.ErrUnauthorized, reason: "the host rejected the App credentials", class: "unauthorized"},
		{name: "403 from GitHub", setup: func(s *surfaces) { s.app.FailTokens(http.StatusForbidden) }, want: provider.ErrForbidden, reason: "the host denied access", class: "forbidden"},
		{name: "404 from GitHub", setup: func(s *surfaces) { s.app.FailTokens(http.StatusNotFound) }, want: provider.ErrNotFound, reason: "the host does not know the App installation", class: "not found"},
		{name: "network error", setup: func(s *surfaces) {}, base: "down", want: provider.ErrUnavailable, reason: "the host is unavailable", class: "unavailable"},
		{name: "expired token from GitHub", setup: func(s *surfaces) { s.app.SetTokenLifetime(0) }, want: token.ErrUnusableToken, reason: "the host returned an unusable token", class: "unusable token"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/gh shim", func(t *testing.T) {
			s := newSurfaces(t)
			tt.setup(s)
			base := tt.base
			if base == "down" {
				base = downBase(t)
			}
			err := s.runShim(base)
			s.assertShimRefused(err, tt.reason)
			s.assertCacheHoldsNoToken()
		})
		t.Run(tt.name+"/git credential helper", func(t *testing.T) {
			s := newSurfaces(t)
			tt.setup(s)
			base := tt.base
			if base == "down" {
				base = downBase(t)
			}
			out, err := s.runHelper(base)
			s.assertHelperRefused(out, err, tt.class)
			s.assertCacheHoldsNoToken()
		})
	}
}

func TestFailClosedSurfacesWorkWhenTheMintWorks(t *testing.T) {
	s := newSurfaces(t)
	if err := s.runShim(""); err != nil || len(s.execs) != 1 {
		t.Fatalf("shim control: %v, %d starts", err, len(s.execs))
	}
	out, err := s.runHelper("")
	if err != nil || !strings.Contains(out.Stdout, "password="+testenv.FakeTokenPrefix) {
		t.Fatalf("helper control: %q %v", out.Stdout, err)
	}
}

func TestFailClosedSurfacesNeverServeAnExpiredCachedToken(t *testing.T) {
	failures := []struct {
		name          string
		break_        func(s *surfaces)
		reason, class string
	}{
		{"401", func(s *surfaces) { s.app.FailTokens(401) }, "the host rejected the App credentials", "unauthorized"},
		{"403", func(s *surfaces) { s.app.FailTokens(403) }, "the host denied access", "forbidden"},
		{"404", func(s *surfaces) { s.app.FailTokens(404) }, "the host does not know the App installation", "not found"},
		{"503", func(s *surfaces) { s.app.FailTokens(503) }, "the host is unavailable", "unavailable"},
		{"key removed", func(s *surfaces) { _ = os.Remove(s.keyPath) }, "", "provider or private key"},
	}
	for _, tt := range failures {
		t.Run(tt.name+"/gh shim", func(t *testing.T) {
			s := newSurfaces(t)
			if err := s.runShim(""); err != nil {
				t.Fatal(err)
			}
			first := s.app.Tokens()
			s.execs = nil
			s.now = s.now.Add(2 * time.Hour)
			tt.break_(s)
			s.assertShimRefused(s.runShim(""), tt.reason)
			if len(first) == 0 {
				t.Fatal("the control call minted nothing")
			}
		})
		t.Run(tt.name+"/git credential helper", func(t *testing.T) {
			s := newSurfaces(t)
			if _, err := s.runHelper(""); err != nil {
				t.Fatal(err)
			}
			s.now = s.now.Add(2 * time.Hour)
			tt.break_(s)
			out, err := s.runHelper("")
			s.assertHelperRefused(out, err, tt.class)
		})
	}
}
