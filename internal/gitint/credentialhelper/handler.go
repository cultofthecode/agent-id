package credentialhelper

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/cultofthecode/agent-id/internal/gitint"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// Handle runs one helper call. store and erase read and discard the request
// and persist nothing. get returns the credential only when the decision
// allows the request; for another host it returns an empty Output and no
// error; for every other outcome it returns `quit=1` and an error whose
// message holds no secret and no request value.
func (h *Handler) Handle(ctx context.Context, in Input) (Output, error) {
	switch in.Operation {
	case OpStore, OpErase:
		_, _ = io.Copy(io.Discard, io.LimitReader(in.Stdin, 64<<10))
		return Output{}, nil
	case OpGet:
		return h.get(ctx, in)
	}
	return Output{}, fmt.Errorf("%w (use get, store, or erase)", ErrUsage)
}

func quit(err error) (Output, error) {
	return Output{Stdout: gitint.QuitLine}, err
}

func (h *Handler) get(ctx context.Context, in Input) (Output, error) {
	req, err := gitint.ParseRequest(in.Stdin)
	if err != nil {
		return quit(fmt.Errorf("git-credential: refused: %w", err))
	}
	host, err := h.Host(in.Profile)
	if err != nil {
		return quit(denied(in.Profile, "profile"))
	}
	verdict, reason := gitint.Decide(req, host)
	switch verdict {
	case gitint.Skip:
		return Output{}, nil
	case gitint.Refuse:
		return quit(fmt.Errorf("git-credential: refused: %s", reason))
	}
	cred, err := h.Credential(ctx, in.Profile)
	if err != nil {
		return quit(denied(in.Profile, classOf(err)))
	}
	out, err := gitint.Render(cred, host)
	if err != nil {
		return quit(denied(in.Profile, "error"))
	}
	return Output{Stdout: out}, nil
}

func denied(profile, class string) error {
	return fmt.Errorf("git-credential: cannot get a token for profile %q (%s); run agent-id doctor --profile %q", profile, class, profile)
}

// classOf names the class of a failure with a fixed word. It never returns
// the text of err.
func classOf(err error) string {
	var classed *ClassedError
	switch {
	case errors.As(err, &classed):
		return classed.Class
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	case errors.Is(err, provider.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, provider.ErrForbidden):
		return "forbidden"
	case errors.Is(err, provider.ErrNotFound):
		return "not found"
	case errors.Is(err, provider.ErrRateLimited):
		return "rate limited"
	case errors.Is(err, provider.ErrUnavailable):
		return "unavailable"
	}
	return "error"
}
