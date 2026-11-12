package main

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMain(m *testing.M) { testenv.Main(m) }

func TestBinary(t *testing.T) {
	env := testenv.Isolate(t)
	binary := testenv.BuildBinary(t, env, "github.com/cultofthecode/agent-id/cmd/aidentity")
	if !strings.HasPrefix(binary, env.Root) {
		t.Fatalf("binary %s is outside the test root %s", binary, env.Root)
	}
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version", []string{"--version"}, exitOK, "aidentity version ", ""},
		{"unknown command", []string{"no-such-command"}, exitFailure, "", "aidentity: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, tt.args...)
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			code := exitOK
			var exitErr *exec.ExitError
			if err := cmd.Run(); errors.As(err, &exitErr) {
				code = exitErr.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.HasPrefix(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want the prefix %q", stdout.String(), tt.wantStdout)
			}
			if !strings.HasPrefix(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want the prefix %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
