package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"version flag", []string{"--version"}, exitOK, "aidentity version ", ""},
		{"help", []string{"--help"}, exitOK, "aidentity", ""},
		{"unknown command fails", []string{"no-such-command"}, exitFailure, "", "aidentity: "},
		{"unknown flag fails", []string{"--no-such-flag"}, exitFailure, "", "aidentity: unknown flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testenv.Isolate(t)
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), newRoot(), tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr %q)", code, tt.wantCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantStdout)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}
