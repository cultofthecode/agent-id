package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// Setup fields of the GitHub onboarder.
const (
	fieldAppID          = "app-id"
	fieldInstallationID = "installation-id"
	fieldPrivateKey     = "private-key"
)

// Hints of the setup fields: where to find each value.
const (
	usageAppID          = "the App ID on the App's settings page"
	usageInstallationID = "the number at the end of the installation's settings URL"
	usagePrivateKey     = "the .pem file downloaded from the App's settings"
)

// ErrSetupValue is the class of a setup value that Check refuses.
var ErrSetupValue = errors.New("github: invalid setup value")

// setupFailure is an onboarding error whose text is a message for the user
// alone and whose class is the error it wraps, so that errors.Is works
// without the class's own text in the message.
type setupFailure struct {
	message string
	class   error
}

func (e *setupFailure) Error() string { return e.message }

func (e *setupFailure) Unwrap() error { return e.class }

// failf returns a setupFailure of class with the formatted message.
func failf(class error, format string, args ...any) error {
	return &setupFailure{message: fmt.Sprintf(format, args...), class: class}
}

// Onboarder is the GitHub onboarding of an existing GitHub App (setup spec).
// It implements provider.Onboarder: it loads the App's key through the
// injected key loader, mints one installation token, resolves the bot, lists
// the installation's repositories, and returns the github profile block with
// the identity cache. It drops the token and never prints it.
type Onboarder struct {
	loadKey func(profile.KeyRef) ([]byte, error)
	api     api
}

// NewOnboarder returns the GitHub onboarder on api.github.com. loadKey
// returns the PEM bytes of the key a reference names; the composition root
// wires it to the credential source.
func NewOnboarder(loadKey func(profile.KeyRef) ([]byte, error)) *Onboarder {
	return &Onboarder{loadKey: loadKey}
}

// Name implements provider.Onboarder.
func (o *Onboarder) Name() string { return providerName }

// SetupFields implements provider.Onboarder.
func (o *Onboarder) SetupFields() []provider.SetupField {
	return []provider.SetupField{
		{Name: fieldAppID, Usage: usageAppID},
		{Name: fieldInstallationID, Usage: usageInstallationID},
		{Name: fieldPrivateKey, Usage: usagePrivateKey},
	}
}

// Check implements provider.Onboarder with parseSetupValues.
func (o *Onboarder) Check(r provider.SetupRequest) error {
	_, err := parseSetupValues(r.Values, r.Dir)
	return err
}

// setupValues are the checked setup values.
type setupValues struct {
	appID          int64
	installationID int64
	key            profile.KeyRef
}

// parseSetupValues checks the setup values without I/O and returns them. A
// value that looks like a token or key, or holds a control or format character, is
// refused before it is normalized, and no error echoes a value.
func parseSetupValues(values map[string]string, dir string) (setupValues, error) {
	appID, err := parseIDValue(values, fieldAppID, usageAppID)
	if err != nil {
		return setupValues{}, err
	}
	installationID, err := parseIDValue(values, fieldInstallationID, usageInstallationID)
	if err != nil {
		return setupValues{}, err
	}
	key, err := parseKeyValue(values[fieldPrivateKey], dir)
	if err != nil {
		return setupValues{}, err
	}
	return setupValues{appID: appID, installationID: installationID, key: key}, nil
}

// parseIDValue parses the ID of field.
func parseIDValue(values map[string]string, field, usage string) (int64, error) {
	value := values[field]
	if value == "" {
		return 0, failf(ErrSetupValue, "--%s is required (%s)", field, usage)
	}
	if err := refuseSecret(field, value); err != nil {
		return 0, err
	}
	id, ok := ParseID(value)
	if !ok {
		return 0, failf(ErrSetupValue, "--%s must be a positive integer (%s)", field, usage)
	}
	return id, nil
}

// parseKeyValue turns the --private-key value into a key reference: a
// file: reference as written, ~/… as file:~/…, an absolute path as
// file:<path>, and a relative path joined to dir. Every path is cleaned and
// symlinks are not resolved.
func parseKeyValue(value, dir string) (profile.KeyRef, error) {
	if value == "" {
		return profile.KeyRef{}, failf(ErrSetupValue, "--%s is required (%s)", fieldPrivateKey, usagePrivateKey)
	}
	if err := refuseSecret(fieldPrivateKey, value); err != nil {
		return profile.KeyRef{}, err
	}
	reference, err := keyReference(value, dir)
	if err != nil {
		return profile.KeyRef{}, err
	}
	ref, err := profile.ParseKeyRef(reference)
	if err != nil {
		return profile.KeyRef{}, fmt.Errorf("--%s: %w", fieldPrivateKey, err)
	}
	return ref, nil
}

// keyReference returns the key reference text of a --private-key value.
func keyReference(value, dir string) (string, error) {
	switch {
	case strings.HasPrefix(value, profile.SchemeFile+":"), strings.HasPrefix(value, profile.SchemeKeyring+":"):
		return value, nil
	case strings.HasPrefix(value, "~/"), filepath.IsAbs(value):
		return profile.SchemeFile + ":" + value, nil
	case !filepath.IsAbs(dir):
		return "", failf(ErrSetupValue, "--%s is a relative path, but the working directory is unknown; pass an absolute path", fieldPrivateKey)
	default:
		return profile.SchemeFile + ":" + filepath.Join(dir, value), nil
	}
}

// refuseSecret refuses a value of field that looks like a token, a JWT, or
// a PEM block (the profile store's inline-secret rule with the GitHub token
// prefixes), or holds a control or format character. The value is never
// echoed.
func refuseSecret(field, value string) error {
	if strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
		return failf(ErrSetupValue, "--%s contains a control or format character; it is not shown", field)
	}
	if profile.LooksSecret(value, tokenPrefixes) {
		return failf(ErrSetupValue, "--%s looks like a token or key; it is not shown — remove it and revoke it if it was real", field)
	}
	return nil
}

// Onboard implements provider.Onboarder: it checks the values, loads the
// key, mints one installation token, resolves the bot, lists the
// repositories with the token, and returns the github block with the
// identity cache, resolved now (UTC, to the second).
func (o *Onboarder) Onboard(ctx context.Context, r provider.SetupRequest) (provider.SetupResult, error) {
	values, err := parseSetupValues(r.Values, r.Dir)
	if err != nil {
		return provider.SetupResult{}, err
	}
	p, err := o.adapter(values)
	if err != nil {
		return provider.SetupResult{}, err
	}
	bot, repositories, err := verify(ctx, p, values)
	if err != nil {
		return provider.SetupResult{}, err
	}
	return o.result(values, bot, repositories, p.now())
}

// adapter loads the key and builds the adapter for values. The PEM bytes
// are cleared once the key is parsed.
func (o *Onboarder) adapter(values setupValues) (*Provider, error) {
	pemBytes, err := o.loadKey(values.key)
	if err != nil {
		return nil, err
	}
	defer clear(pemBytes)
	p, err := newProvider(Config{AppID: values.appID, InstallationID: values.installationID, PrivateKeyPEM: pemBytes}, o.api)
	if errors.Is(err, ErrInvalidKey) {
		return nil, failf(ErrInvalidKey, "--%s %s: not a GitHub App private key (one unencrypted RSA key of at least 2048 bits, PEM)", fieldPrivateKey, values.key.Value)
	}
	return p, err
}

// verify mints one installation token, resolves the bot, and lists the
// repositories the token can access, sorted as "owner/name". The token is
// dropped when verify returns.
func verify(ctx context.Context, p *Provider, values setupValues) (BotUser, []string, error) {
	token, err := p.Token(ctx)
	if err != nil {
		return BotUser{}, nil, setupError(err, values, true)
	}
	bot, err := p.Bot(ctx)
	if err != nil {
		return BotUser{}, nil, setupError(err, values, false)
	}
	listed, err := p.Repositories(ctx, token)
	if err != nil {
		return BotUser{}, nil, setupError(err, values, false)
	}
	repositories := make([]string, 0, len(listed))
	for _, repository := range listed {
		repositories = append(repositories, repository.Owner+"/"+repository.Name)
	}
	slices.Sort(repositories)
	return bot, repositories, nil
}

// setupError turns an adapter error into the setup message of its class.
// tokenStep tells whether the error came from the token request, where 404
// means a wrong installation. Every result wraps the original error; only
// the "cannot reach GitHub" message shows its text, which names the
// operation and status and never a token.
func setupError(err error, values setupValues, tokenStep bool) error {
	switch {
	case errors.Is(err, provider.ErrUnauthorized):
		return failf(err, "GitHub rejected the App credentials: check that --%s %d is the App's ID and that --%s belongs to that App and is not revoked; a clock more than a minute off also causes this", fieldAppID, values.appID, fieldPrivateKey)
	case errors.Is(err, provider.ErrNotFound) && tokenStep:
		return failf(err, "installation %d of App %d not found: check --%s and that the App is installed", values.installationID, values.appID, fieldInstallationID)
	case errors.Is(err, provider.ErrForbidden):
		return failf(err, "GitHub denied the App (suspended or blocked): check the App's installation")
	case errors.Is(err, provider.ErrRateLimited):
		return failf(err, "GitHub is rate limiting this machine; try again later")
	default:
		return fmt.Errorf("cannot reach GitHub: %w", err)
	}
}

// result builds the setup result of a verified App.
func (o *Onboarder) result(values setupValues, bot BotUser, repositories []string, now time.Time) (provider.SetupResult, error) {
	identity, err := CommitIdentity(bot.Login, bot.UserID)
	if err != nil {
		return provider.SetupResult{}, err
	}
	settings, err := json.Marshal(ProfileSettings{
		AppID:          values.appID,
		InstallationID: values.installationID,
		PrivateKey:     values.key,
		Identity:       &BotIdentity{Login: bot.Login, UserID: bot.UserID, ResolvedAt: now.UTC().Truncate(time.Second)},
	})
	if err != nil {
		return provider.SetupResult{}, err
	}
	return provider.SetupResult{
		Host:     host,
		Settings: settings,
		Account:  bot.Login,
		Identity: identity,
		Details: []provider.SetupDetail{
			{Label: "App", Value: strconv.FormatInt(values.appID, 10)},
			{Label: "Installation", Value: strconv.FormatInt(values.installationID, 10)},
			{Label: "Private key", Value: values.key.String()},
		},
		Repositories: repositories,
	}, nil
}
