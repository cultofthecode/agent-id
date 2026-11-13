//go:build darwin || linux

package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestMissingInterpreterGives127FromTheRealExecve(t *testing.T) {
	testenv.Isolate(t)
	script := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(script, []byte("#!/nonexistent/aid-interpreter\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	h, _ := newHandler(t)
	h.Execer = SyscallExecer{}
	err := h.Handle(context.Background(), input(script))
	if err == nil || code(err) != ExitNotFound || !strings.Contains(err.Error(), "interpreter or loader not found") {
		t.Fatalf("error %v (code %d), want 127 with the interpreter message", err, code(err))
	}
}

func TestNonExecutableScriptGives126FromTheRealExecve(t *testing.T) {
	testenv.Isolate(t)
	script := filepath.Join(t.TempDir(), "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, _ := newHandler(t)
	h.Execer = SyscallExecer{}
	err := h.Handle(context.Background(), input(script))
	if err == nil || code(err) != ExitNotExecutable {
		t.Fatalf("error %v (code %d), want 126", err, code(err))
	}
}
