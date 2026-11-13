// Package profile is the profile store of agent-id: it locates, reads,
// validates, resolves, and writes the profile configuration v1
// (.ai/specs/2026-11-12-profile-config.md).
//
// The package is provider-agnostic (ADR #35). It owns the core schema
// (version, profiles, and each profile's provider, host, and provider block),
// key references, the inline-secret scan, and the file rules. A provider
// block is opaque here: the composition root registers one SettingsValidator
// per provider, together with the provider's token prefixes, and the core
// hands each block to it. The package imports no provider package, so that
// adapters can import it.
//
// Parse is the pure validation of a document. Store adds the file system:
// Load checks ownership and permissions before it parses, Resolve selects a
// profile by its explicit name, and Write replaces the file atomically under a
// lock.
package profile

import (
	"encoding/json"
	"maps"
	"slices"
)

// Version is the configuration version this agent-id reads and writes.
const Version = 1

// Config is a validated profile configuration.
type Config struct {
	Version  int
	Profiles map[string]Profile
}

// Profile is one agent profile. Settings is its provider block, opaque to the
// core; the adapter registered for Provider decodes it.
type Profile struct {
	Name     string
	Provider string
	Host     string
	Settings json.RawMessage
}

// Names returns the profile names of c in lexical order.
func (c Config) Names() []string {
	return slices.Sorted(maps.Keys(c.Profiles))
}
