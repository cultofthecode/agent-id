package profile

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// SettingsValidator is the port a provider adapter satisfies to own its
// provider block (ADR #35). The core never interprets a provider block; it
// hands the block to the validator registered for the profile's provider.
//
// ValidateSettings receives the profile's host, already checked as a host
// name, and the provider block as a JSON object that is valid UTF-8, has no
// duplicate member at any depth, and passed the inline-secret scan. It decides
// which hosts the provider accepts and checks every member of the block
// strictly. A returned error wraps one of the core sentinels (build it with
// Errorf) and its text starts with a path relative to the profile, such as
// `host: …` or `github.app_id: …`; the core prefixes `profiles.<name>.`. An
// error that wraps no core sentinel is reported as ErrConfigInvalid.
type SettingsValidator interface {
	ValidateSettings(host string, settings json.RawMessage) error
}

// Registration binds a provider name to the validator of its provider block
// and to the token prefixes of that provider, which the inline-secret scan
// rejects anywhere in the file.
type Registration struct {
	Name          string
	Validator     SettingsValidator
	TokenPrefixes []string
}

// Registry holds the providers this agent-id supports. The composition root
// builds it; the core keeps no list of providers. A nil *Registry supports no
// provider.
type Registry struct {
	providers map[string]Registration
}

// NewRegistry returns a registry of regs. It fails when a name is not a
// valid provider name or appears twice, a validator is nil, or a token prefix
// is shorter than three characters, because such a prefix would reject
// ordinary identifiers.
func NewRegistry(regs ...Registration) (*Registry, error) {
	registry := &Registry{providers: make(map[string]Registration, len(regs))}
	for _, reg := range regs {
		if err := checkRegistration(reg); err != nil {
			return nil, err
		}
		if _, ok := registry.providers[reg.Name]; ok {
			return nil, fmt.Errorf("profile: provider %s registered twice", Quote(reg.Name))
		}
		reg.TokenPrefixes = slices.Clone(reg.TokenPrefixes)
		registry.providers[reg.Name] = reg
	}
	return registry, nil
}

// checkRegistration reports why reg cannot be registered.
func checkRegistration(reg Registration) error {
	if !validName(reg.Name) {
		return fmt.Errorf("profile: invalid provider name %s", Quote(reg.Name))
	}
	if reg.Validator == nil {
		return fmt.Errorf("profile: provider %s has no settings validator", Quote(reg.Name))
	}
	for _, prefix := range reg.TokenPrefixes {
		if len(prefix) < 3 || strings.ContainsFunc(prefix, func(r rune) bool { return r <= ' ' }) {
			return fmt.Errorf("profile: provider %s has an invalid token prefix", Quote(reg.Name))
		}
	}
	return nil
}

// Supports reports whether provider has a registered validator.
func (r *Registry) Supports(provider string) bool {
	_, ok := r.lookup(provider)
	return ok
}

// Supported returns the registered provider names in lexical order.
func (r *Registry) Supported() []string {
	if r == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r.providers))
}

// tokenPrefixes returns the token prefixes of every registered provider.
func (r *Registry) tokenPrefixes() []string {
	var prefixes []string
	for _, name := range r.Supported() {
		prefixes = append(prefixes, r.providers[name].TokenPrefixes...)
	}
	return prefixes
}

// lookup returns the registration of provider.
func (r *Registry) lookup(provider string) (Registration, bool) {
	if r == nil {
		return Registration{}, false
	}
	reg, ok := r.providers[provider]
	return reg, ok
}
