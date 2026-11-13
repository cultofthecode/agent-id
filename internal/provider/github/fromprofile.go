package github

import (
	"fmt"

	"github.com/cultofthecode/agent-id/internal/profile"
)

// FromProfile returns the adapter for a profile of provider github: it
// decodes the profile's github block, loads the App's private key through
// loadKey (the composition root wires it to the credential source), and
// carries the cached bot identity. A failure of loadKey is returned
// unchanged, so that the caller can tell a key problem from a configuration
// problem; the adapter never reads the key file itself.
func FromProfile(p profile.Profile, loadKey func(profile.KeyRef) ([]byte, error)) (*Provider, error) {
	settings, err := DecodeProfileSettings(p.Host, p.Settings)
	if err != nil {
		return nil, fmt.Errorf("profile %s: %w", profile.Quote(p.Name), err)
	}
	pem, err := loadKey(settings.PrivateKey)
	if err != nil {
		return nil, err
	}
	defer clear(pem)
	return New(Config{AppID: settings.AppID, InstallationID: settings.InstallationID, PrivateKeyPEM: pem, Cached: settings.Identity})
}
