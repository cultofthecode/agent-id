package exec

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/cultofthecode/agent-id/internal/activation"
)

// Execer replaces the process with the command. It returns only on failure.
type Execer interface {
	Exec(path string, argv, env []string) error
}

// Handler runs exec.
type Handler struct {
	Deps   activation.Deps
	Execer Execer
	// Binary returns the absolute path of the running agent-id binary.
	Binary func() (string, error)
}

// Handle activates the profile and replaces the process with the command. On
// success the real Execer never returns; a failure is an *ExitError.
func (h *Handler) Handle(ctx context.Context, in Input) error {
	if in.Profile == "" {
		return failure(ExitFailure, ErrProfileRequired)
	}
	if len(in.Argv) == 0 {
		return failure(ExitFailure, ErrNoCommand)
	}
	binary, err := h.Binary()
	if err != nil {
		return failure(ExitFailure, err)
	}
	act, err := activation.Activate(ctx, h.Deps, activation.Request{
		Profile: in.Profile, Mechanism: activation.MechanismExec, Binary: binary, Dir: in.Dir, Parent: in.Parent,
	})
	if err != nil {
		return failure(ExitFailure, err)
	}
	path, err := activation.LookPath(act.Env, in.Argv[0])
	switch {
	case errors.Is(err, activation.ErrCommandNotFound):
		return failure(ExitNotFound, err)
	case err != nil:
		return failure(ExitNotExecutable, err)
	}
	if err := h.Execer.Exec(path, in.Argv, act.Env); err != nil {
		return failure(execCode(err), fmt.Errorf("%q: %s", in.Argv[0], reason(err)))
	}
	return nil
}

func execCode(err error) int {
	if errors.Is(err, syscall.ENOENT) {
		return ExitNotFound
	}
	return ExitNotExecutable
}

func reason(err error) string {
	switch {
	case errors.Is(err, syscall.ENOENT):
		return "command not found"
	case errors.Is(err, syscall.EACCES):
		return "permission denied"
	case errors.Is(err, syscall.ENOEXEC):
		return "exec format error"
	}
	return "cannot be executed"
}
