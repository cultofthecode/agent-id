package env

import (
	"context"
	"fmt"

	"github.com/cultofthecode/agent-id/internal/activation"
)

// Handler runs env.
type Handler struct {
	Deps activation.Deps
	// Binary returns the absolute path of the running agent-id binary.
	Binary func() (string, error)
}

// Handle activates the profile with the env mechanism and returns the block.
// On a failure it returns an *ExitError and no block.
func (h *Handler) Handle(ctx context.Context, in Input) (string, error) {
	if in.Profile == "" {
		return "", failure(ErrProfileRequired)
	}
	switch in.Shell {
	case "bash", "zsh":
	case "":
		return "", failure(ErrShellRequired)
	default:
		return "", failure(fmt.Errorf("unsupported shell %q; use bash or zsh", quoteShort(in.Shell)))
	}
	binary, err := h.Binary()
	if err != nil {
		return "", failure(err)
	}
	act, err := activation.Activate(ctx, h.Deps, activation.Request{
		Profile: in.Profile, Mechanism: activation.MechanismEnv, Binary: binary, Dir: in.Dir, Parent: in.Parent,
	})
	if err != nil {
		return "", failure(err)
	}
	block, err := activation.Render(in.Parent, act)
	if err != nil {
		return "", failure(err)
	}
	return block, nil
}

func quoteShort(s string) string {
	if len(s) > 32 {
		return s[:32] + "…"
	}
	return s
}
