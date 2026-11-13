package whoami

import (
	"context"
	"errors"
	"time"

	"github.com/cultofthecode/agent-id/internal/diagnose"
	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// Handler runs whoami. ReadContext is called first and decides whether
// anything else is touched: outside an agent context Profiles, OpenProvider,
// and Tokens are never called, and no child process is started.
type Handler struct {
	ReadContext  func() Context
	Profiles     func() (ProfileResolver, error)
	OpenProvider func(profile.Profile) (provider.Provider, error)
	Tokens       TokenSource
	Output       diagnose.Output
	Now          func() time.Time
}

// Handle returns the bytes to print and the exit status. A non-nil error is
// a *diagnose.ExitError for output that was withheld.
func (h *Handler) Handle(ctx context.Context, in Input) ([]byte, int, error) {
	report := h.report(ctx)
	out, err := h.Output.Whoami(report, in.JSON)
	if err != nil {
		return nil, diagnose.ExitWithheld, diagnose.WithheldError()
	}
	return out, report.ExitCode(), nil
}

// report builds the report: the personal answer, a damaged context, or the
// full agent report.
func (h *Handler) report(ctx context.Context) diagnose.WhoamiReport {
	c := h.ReadContext()
	if !c.Present {
		return diagnose.WhoamiReport{Context: diagnose.ContextPersonal}
	}
	r := diagnose.WhoamiReport{Context: diagnose.ContextAgent, Problem: c.Problem, Profile: c.Profile, Activation: c.Mechanism}
	if c.Problem != "" {
		r.Token = notChecked("damaged context")
		return r
	}
	return h.agentReport(ctx, r)
}

// notChecked is the token status of a report that could not look.
func notChecked(reason string) diagnose.WhoamiToken {
	return diagnose.WhoamiToken{Status: diagnose.TokenNotChecked, Reason: reason}
}

// agentReport fills r for an intact context: the profile, the provider's
// description, and the token status.
func (h *Handler) agentReport(ctx context.Context, r diagnose.WhoamiReport) diagnose.WhoamiReport {
	p, err := h.resolve(r.Profile)
	if err != nil {
		return notResolved(r, err)
	}
	prov, err := h.OpenProvider(p)
	if err != nil {
		f := diagnose.ClassifyError(err)
		r.Provider, r.Host = p.Provider, p.Host
		r.Token = diagnose.WhoamiToken{Status: diagnose.TokenUnavailable, Class: f.Class, Reason: f.Message}
		return r
	}
	r.Provider, r.Host = prov.Name(), prov.Host()
	r.Identity, r.Details = describe(prov)
	r.Token = h.token(ctx, prov)
	return r
}

// resolve returns the profile name resolved through the store.
func (h *Handler) resolve(name string) (profile.Profile, error) {
	store, err := h.Profiles()
	if err != nil {
		return profile.Profile{}, err
	}
	return store.Resolve(name)
}

// notResolved reports a profile that cannot be used: the token is not
// checked.
func notResolved(r diagnose.WhoamiReport, err error) diagnose.WhoamiReport {
	if errors.Is(err, profile.ErrProfileNotFound) {
		r.ProfileNote = "not found in the configuration"
		r.Token = notChecked("profile not found")
		return r
	}
	r.Token = notChecked(err.Error())
	return r
}

// describe splits the provider's Describe lines into the identity and the
// other details.
func describe(prov provider.Provider) (string, []diagnose.Detail) {
	identity := ""
	var details []diagnose.Detail
	for _, f := range prov.Describe() {
		if f.Label == "Identity" {
			identity = f.Value
			continue
		}
		details = append(details, diagnose.Detail{Label: f.Label, Value: f.Value})
	}
	return identity, details
}

// token obtains the token through the token manager and keeps only its
// expiry.
func (h *Handler) token(ctx context.Context, prov provider.Provider) diagnose.WhoamiToken {
	tok, err := h.Tokens.Token(ctx, prov)
	if err != nil {
		f := diagnose.ClassifyError(err)
		return diagnose.WhoamiToken{Status: diagnose.TokenUnavailable, Class: f.Class, Reason: f.Message}
	}
	return diagnose.WhoamiToken{Status: diagnose.TokenValid, ExpiresAt: tok.ExpiresAt, ExpiresIn: tok.ExpiresAt.Sub(h.now())}
}

func (h *Handler) now() time.Time {
	if h.Now == nil {
		return time.Now()
	}
	return h.Now()
}
