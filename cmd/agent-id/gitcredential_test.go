package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/gitint/credentialhelper"
	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

func TestTokenFailureClasses(t *testing.T) {
	tests := []struct {
		err   error
		class string
	}{
		{token.ErrInsecureCache, "cache not private"},
		{token.ErrNoCacheDir, "cache not private"},
		{token.ErrCacheWrite, "cache write failed"},
		{token.ErrLockTimeout, "lock timeout"},
		{token.ErrUnusableToken, "unusable token"},
		{token.ErrInvalidKey, "error"},
	}
	for _, tt := range tests {
		got := tokenFailure(fmt.Errorf("wrapped: %w", tt.err))
		var classed *credentialhelper.ClassedError
		if !errors.As(got, &classed) || classed.Class != tt.class || strings.Contains(got.Error(), "wrapped") {
			t.Errorf("%v: got %v, want class %q", tt.err, got, tt.class)
		}
	}
	if got := tokenFailure(provider.ErrUnauthorized); !errors.Is(got, provider.ErrUnauthorized) {
		t.Errorf("a provider error must pass through, got %v", got)
	}
}

func TestGitCredentialCommandFailsClosed(t *testing.T) {
	f := newExecFixture(t)
	request := "protocol=https\nhost=github.com\npath=o/r.git\n\n"
	tests := []struct {
		name    string
		args    []string
		stdin   string
		code    int
		stdout  string
		message string
	}{
		{"unknown profile", []string{"git-credential", "--profile", "ghost", "get"}, request, 1, "quit=1\n", `cannot get a token for profile "ghost" (profile)`},
		{"key cannot load", []string{"git-credential", "--profile", "claude", "get"}, request, 1, "quit=1\n", `(provider or private key)`},
		{"other host", []string{"git-credential", "--profile", "claude", "get"}, "protocol=https\nhost=example.com\npath=o/r.git\n\n", 0, "", ""},
		{"http", []string{"git-credential", "--profile", "claude", "get"}, "protocol=http\nhost=github.com\npath=o/r.git\n\n", 1, "quit=1\n", "refused"},
		{"store", []string{"git-credential", "--profile", "ghost", "store"}, request + "password=x\n", 0, "", ""},
		{"erase", []string{"git-credential", "--profile", "ghost", "erase"}, request, 0, "", ""},
		{"unknown operation", []string{"git-credential", "--profile", "claude", "fill"}, request, 1, "", "unknown operation"},
		{"no profile flag", []string{"git-credential", "get"}, request, 1, "", "--profile is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.stdin = tt.stdin
			r := f.run(tt.args...)
			if r.code != tt.code || r.stdout != tt.stdout || !strings.Contains(r.stderr, tt.message) {
				t.Errorf("code %d stdout %q stderr %q, want %d %q containing %q", r.code, r.stdout, r.stderr, tt.code, tt.stdout, tt.message)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join(f.env.CacheHome, "agent-id", "tokens")); len(entries) != 0 {
		t.Errorf("the token cache holds %d entries after failures", len(entries))
	}
}
