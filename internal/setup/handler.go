package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// profileUsage is the hint of the --profile flag.
const profileUsage = "the agent profile to create or update"

// Store is the part of the profile store that setup uses.
type Store interface {
	CheckName(name string) error
	Load() (profile.Config, error)
	Write(change func(*profile.Config) error) error
	Path() string
}

// Handler runs setup with the registered onboarders and the profile store
// that OpenStore returns. OpenStore is called only when a setup runs, so
// that a broken environment fails setup and nothing else.
type Handler struct {
	Onboarders []provider.Onboarder
	OpenStore  func() (Store, error)
}

// Handle checks the input without I/O, refuses a profile of another
// provider before any network call, verifies the values through the
// onboarder, and only then writes the profile in one atomic write. A failure
// leaves the configuration file as it was.
func (h *Handler) Handle(ctx context.Context, in Input) (Output, error) {
	onboarder, err := h.onboarder(in.Provider)
	if err != nil {
		return Output{}, err
	}
	if err := checkMissing(onboarder, in); err != nil {
		return Output{}, err
	}
	store, err := h.OpenStore()
	if err != nil {
		return Output{}, err
	}
	if err := checkName(store, in.Profile); err != nil {
		return Output{}, err
	}
	request := provider.SetupRequest{Values: in.Values, Dir: in.Dir}
	if err := onboarder.Check(request); err != nil {
		return Output{}, err
	}
	if err := checkExisting(store, in); err != nil {
		return Output{}, err
	}
	result, err := onboarder.Onboard(ctx, request)
	if err != nil {
		return Output{}, err
	}
	if err := ctx.Err(); err != nil {
		return Output{}, fmt.Errorf("setup interrupted before the write: %w", err)
	}
	created, err := write(store, in, result)
	if err != nil {
		return Output{}, err
	}
	return output(in, result, created, store.Path()), nil
}

// onboarder returns the onboarder registered for name.
func (h *Handler) onboarder(name string) (provider.Onboarder, error) {
	for _, o := range h.Onboarders {
		if o.Name() == name {
			return o, nil
		}
	}
	return nil, failf(ErrUnknownProvider, "unknown provider %s", profile.Quote(name))
}

// checkMissing names every missing required flag in one error, each with
// its hint.
func checkMissing(o provider.Onboarder, in Input) error {
	var missing []string
	if in.Profile == "" {
		missing = append(missing, "--profile ("+profileUsage+")")
	}
	for _, field := range o.SetupFields() {
		if in.Values[field.Name] == "" {
			missing = append(missing, "--"+field.Name+" ("+field.Usage+")")
		}
	}
	switch {
	case len(missing) == 0:
		return nil
	case len(missing) == 1 && in.Profile == "":
		return failf(ErrProfileRequired, "--profile is required")
	default:
		return failf(ErrMissingFlags, "missing required flags: %s", strings.Join(missing, ", "))
	}
}

// checkName checks the profile name with the store's rules and words the
// error for the --profile flag, keeping its class. A secret-shaped name is
// never echoed.
func checkName(store Store, name string) error {
	err := store.CheckName(name)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, profile.ErrInlineSecret):
		return profile.Errorf(profile.ErrInlineSecret, "--profile looks like a token or key; it is not shown — remove it and revoke it if it was real")
	case errors.Is(err, profile.ErrConfigInvalid):
		return profile.Errorf(profile.ErrConfigInvalid, `invalid profile name %s: use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"`, profile.Quote(name))
	default:
		return err
	}
}

// checkExisting loads the configuration and refuses a profile of the same
// name and another provider. Only a configuration path at which nothing
// exists counts as empty; every other store error stops setup, a symlink
// whose target is missing included, which the write would refuse.
func checkExisting(store Store, in Input) error {
	config, err := store.Load()
	if errors.Is(err, profile.ErrConfigNotFound) && missing(store.Path()) {
		return nil
	}
	if err != nil {
		return err
	}
	return conflict(config, in, store.Path())
}

// missing reports whether nothing, not even a dangling symlink, exists at
// the absolute path.
func missing(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	_, err := os.Lstat(path)
	return errors.Is(err, fs.ErrNotExist)
}

// conflict returns ErrProviderConflict when config holds the profile of in
// for another provider.
func conflict(config profile.Config, in Input, path string) error {
	existing, ok := config.Profiles[in.Profile]
	if !ok || existing.Provider == in.Provider {
		return nil
	}
	return failf(ErrProviderConflict, "profile %s already exists for provider %s; choose another name or remove it from %s", profile.Quote(in.Profile), profile.Quote(existing.Provider), clean(path))
}

// write stores the verified profile under the store's lock, checking the
// conflict again, and reports whether the profile is new.
func write(store Store, in Input, result provider.SetupResult) (bool, error) {
	created := false
	err := store.Write(func(config *profile.Config) error {
		if err := conflict(*config, in, store.Path()); err != nil {
			return err
		}
		_, exists := config.Profiles[in.Profile]
		created = !exists
		config.Profiles[in.Profile] = profile.Profile{Name: in.Profile, Provider: in.Provider, Host: result.Host, Settings: result.Settings}
		return nil
	})
	return created, err
}

// output builds the summary of a written profile.
func output(in Input, result provider.SetupResult, created bool, path string) Output {
	return Output{
		Profile:      in.Profile,
		Provider:     in.Provider,
		Host:         result.Host,
		Account:      result.Account,
		Identity:     result.Identity,
		Details:      result.Details,
		Repositories: result.Repositories,
		Created:      created,
		ConfigPath:   path,
	}
}

// failure is a setup error whose text is the message alone and whose class
// is the sentinel it wraps.
type failure struct {
	message string
	class   error
}

func (e *failure) Error() string { return e.message }

func (e *failure) Unwrap() error { return e.class }

// failf returns a failure of class with the formatted message.
func failf(class error, format string, args ...any) error {
	return &failure{message: fmt.Sprintf(format, args...), class: class}
}
