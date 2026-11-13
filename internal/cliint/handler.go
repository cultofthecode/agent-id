package cliint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/token"
)

// Execer replaces the process with the CLI. It returns only on failure.
type Execer interface {
	Exec(path string, argv, env []string) error
}

// Tokens hands out tokens that stay valid for a while. *token.Manager is one.
type Tokens interface {
	Token(ctx context.Context, src token.Source) (provider.Token, error)
}

// Handler runs the shim. The composition root fills the ports.
type Handler struct {
	// Profile returns the profile of the agent context that parent carries,
	// and an error when there is none or it is invalid.
	Profile func(parent []string) (string, error)
	// Activator returns the exec-time view of a profile's provider. It reads
	// no key and makes no request.
	Activator func(profile string) (provider.Activator, error)
	// Provider returns the provider of a profile. It loads the key.
	Provider func(profile string) (provider.Provider, error)
	// NeutralDirs prepares the agent-id-owned directories and returns the
	// CLI's configuration directory and the launcher's directory.
	NeutralDirs func(parent []string, a provider.Activator) (configDir, binDir string, err error)
	Tokens      Tokens
	Execer      Execer
}

// Handle checks the invocation and replaces the process with the real CLI. On
// success a real Execer never returns. Every other outcome is a *Refusal.
func (h *Handler) Handle(ctx context.Context, in Input) error {
	profileName, err := h.Profile(in.Parent)
	if err != nil {
		return refuse(in.Binary, kindContext, "this command works only inside an agent context started with \"agent-id exec\" (%s)", Scrub(err.Error(), nil))
	}
	a, err := h.Activator(profileName)
	if err != nil {
		return refuse(in.Binary, kindContext, "cannot open profile %q (%s)", profileName, Scrub(err.Error(), nil))
	}
	cli := a.NeutralCLI()
	if in.Binary != cli.Binary {
		return refuse(in.Binary, kindContext, "profile %q does not use %q", profileName, in.Binary)
	}
	if hasMarker(in.Parent) {
		return refuse(in.Binary, kindLoop, "%s was started from %s that agent-id started; agent-id does not run it twice", in.Binary, in.Binary)
	}
	if err := a.CheckCLI(in.Args, in.Parent); err != nil {
		return asRefusal(in.Binary, a, err)
	}
	configDir, binDir, err := h.NeutralDirs(in.Parent, a)
	if err != nil {
		return refuse(in.Binary, kindContext, "cannot prepare the agent-id directories (%s); %s was not started", Scrub(err.Error(), a.TokenPrefixes()), in.Binary)
	}
	path, err := FindCLI(pathOf(in.Parent), in.Binary, binDir)
	switch {
	case errors.Is(err, errLoop):
		return refuse(in.Binary, kindLoop, "the only %s on PATH is agent-id's own launcher; install %s outside the agent-id directory", in.Binary, in.Binary)
	case err != nil:
		return refuse(in.Binary, kindNoCLI, "%s is not installed or not on PATH; install it, then run the command again", in.Binary)
	}
	env, err := h.cliEnvironment(ctx, in, profileName, a, configDir)
	if err != nil {
		return err
	}
	if err := h.recheckDirs(in, a, configDir, binDir); err != nil {
		return err
	}
	if err := h.Execer.Exec(path, append([]string{in.Binary}, in.Args...), env); err != nil {
		return refuse(in.Binary, kindNoCLI, "cannot start %s (%s)", in.Binary, Scrub(err.Error(), a.TokenPrefixes()))
	}
	return nil
}

// recheckDirs runs the neutral directory checks again just before the exec,
// so that a file that a background process of the agent wrote while the token
// was minted stops the call. The directories must also be the same ones.
func (h *Handler) recheckDirs(in Input, a provider.Activator, configDir, binDir string) error {
	again, bin, err := h.NeutralDirs(in.Parent, a)
	if err != nil || again != configDir || bin != binDir {
		reason := "the directories changed"
		if err != nil {
			reason = Scrub(err.Error(), a.TokenPrefixes())
		}
		return refuse(in.Binary, kindContext, "cannot prepare the agent-id directories (%s); %s was not started", reason, in.Binary)
	}
	return nil
}

func (h *Handler) cliEnvironment(ctx context.Context, in Input, profileName string, a provider.Activator, configDir string) ([]string, error) {
	denied := func(reason string) error {
		return refuse(in.Binary, kindToken, "could not obtain a token for profile %q (%s). %s was not started; run \"agent-id doctor\".", profileName, reason, in.Binary)
	}
	if err := a.Ready(); err != nil {
		return nil, denied(Scrub(err.Error(), a.TokenPrefixes()))
	}
	p, err := h.Provider(profileName)
	if err != nil {
		return nil, denied(Scrub(err.Error(), a.TokenPrefixes()))
	}
	tok, err := h.Tokens.Token(ctx, p)
	if err != nil {
		return nil, denied(reasonFor(err))
	}
	if tok.Value == "" {
		return nil, denied("the token is empty")
	}
	env, ok := BuildEnv(in.Parent, a.NeutralCLI(), a.CredentialVariables(), p.CLIEnvironment(tok), configDir)
	if !ok {
		return nil, denied("the provider gave an unusable environment")
	}
	return env, nil
}

// reasonFor says, without a secret, why a token could not be obtained. It
// names only the classes that it knows; any other error text could hold a
// value that nobody expected, so it is not shown.
func reasonFor(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "interrupted"
	case errors.Is(err, provider.ErrUnauthorized):
		return "the host rejected the App credentials"
	case errors.Is(err, provider.ErrForbidden):
		return "the host denied access"
	case errors.Is(err, provider.ErrNotFound):
		return "the host does not know the App installation"
	case errors.Is(err, provider.ErrRateLimited):
		return "the host is rate limiting the agent"
	case errors.Is(err, provider.ErrUnavailable):
		return "the host is unavailable"
	case errors.Is(err, token.ErrInsecureCache):
		return "the token cache is not private"
	case errors.Is(err, token.ErrLockTimeout):
		return "timed out waiting for another agent-id process"
	case errors.Is(err, token.ErrCacheWrite), errors.Is(err, token.ErrNoCacheDir):
		return "the token cache cannot be used"
	case errors.Is(err, token.ErrUnusableToken):
		return "the host returned an unusable token"
	}
	return "an unexpected failure"
}

func asRefusal(binary string, a provider.Activator, err error) error {
	var r *provider.CLIRefusal
	if errors.As(err, &r) {
		return &Refusal{Code: r.Code, Message: r.Message}
	}
	return refuse(binary, kindContext, "the rules of %s failed (%s)", binary, Scrub(err.Error(), a.TokenPrefixes()))
}

func refuse(binary, kind, format string, args ...any) error {
	return &Refusal{Code: codeFor(binary, kind), Message: fmt.Sprintf(format, args...)}
}

func hasMarker(parent []string) bool {
	for _, kv := range parent {
		if v, ok := strings.CutPrefix(kv, VarActive+"="); ok && v != "" {
			return true
		}
	}
	return false
}

func pathOf(parent []string) string {
	for _, kv := range parent {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}
