package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/provider"
)

// Members of the github profile block (profile-config spec, GitHub provider
// block).
const (
	memberAppID          = "app_id"
	memberInstallationID = "installation_id"
	memberPrivateKey     = "private_key"
	memberIdentity       = "identity"
	memberLogin          = "login"
	memberUserID         = "user_id"
	memberResolvedAt     = "resolved_at"
)

// maxID is the largest App, installation, or user ID the block accepts:
// 2^53−1, the largest integer every JSON reader keeps exactly.
const maxID int64 = 1<<53 - 1

// tokenPrefixes are the prefixes of GitHub tokens that the profile store's
// inline-secret scan rejects anywhere in the configuration. ProfileRegistration
// hands out a copy, so no importer can weaken the scan.
var tokenPrefixes = []string{"ghs_", "ghp_", "gho_", "ghu_", "ghr_", "github_pat_"}

// ProfileSettings is the github block of a profile: the App, its
// installation, the reference to the App's private key, and the cached bot
// identity, which is nil until setup or doctor resolves it.
type ProfileSettings struct {
	AppID          int64
	InstallationID int64
	PrivateKey     profile.KeyRef
	Identity       *BotIdentity
}

// BotIdentity is the cached bot user of the App: its login "<slug>[bot]",
// its user id, and when it was resolved, in UTC.
type BotIdentity struct {
	Login      string
	UserID     int64
	ResolvedAt time.Time
}

// Commit returns the commit author and committer derived from the cached
// identity (CommitIdentity).
func (b BotIdentity) Commit() (provider.Identity, error) {
	return CommitIdentity(b.Login, b.UserID)
}

// SettingsValidator validates the github block for the profile store. It
// implements profile.SettingsValidator.
type SettingsValidator struct{}

// ValidateSettings implements profile.SettingsValidator with
// DecodeProfileSettings.
func (SettingsValidator) ValidateSettings(host string, settings json.RawMessage) error {
	_, err := DecodeProfileSettings(host, settings)
	return err
}

// ProfileRegistration returns the registration of the github provider for
// profile.NewRegistry: its validator and its token prefixes.
func ProfileRegistration() profile.Registration {
	return profile.Registration{Name: providerName, Validator: SettingsValidator{}, TokenPrefixes: slices.Clone(tokenPrefixes)}
}

// DecodeProfileSettings decodes the github block of a profile on host
// strictly: host must be github.com, every member is known and of its type,
// IDs are positive JSON integers of at most 2^53−1, private_key is a key
// reference, and identity, when present, is complete. A member that appears
// twice is refused; the inline-secret scan is the profile store's (Parse runs
// it before the validator). Errors are built with
// profile.Errorf, start with a path relative to the profile, and never echo
// the private key reference.
func DecodeProfileSettings(profileHost string, settings json.RawMessage) (ProfileSettings, error) {
	if profileHost != host {
		return ProfileSettings{}, profile.Errorf(profile.ErrUnsupportedHost, "host: %s is not supported by the github provider; this version accepts only %s", profile.Quote(profileHost), host)
	}
	members, err := objectMembers(settings, providerName)
	if err != nil {
		return ProfileSettings{}, err
	}
	var s ProfileSettings
	for _, m := range members {
		if err := s.decodeMember(m); err != nil {
			return ProfileSettings{}, err
		}
	}
	if err := requireMembers(members, providerName, memberAppID, memberInstallationID, memberPrivateKey); err != nil {
		return ProfileSettings{}, err
	}
	return s, nil
}

// decodeMember decodes one member of the github block into s.
func (s *ProfileSettings) decodeMember(m member) error {
	path := providerName + "." + m.name
	var err error
	switch m.name {
	case memberAppID:
		s.AppID, err = decodeID(path, m.value)
	case memberInstallationID:
		s.InstallationID, err = decodeID(path, m.value)
	case memberPrivateKey:
		s.PrivateKey, err = decodeKeyRef(path, m.value)
	case memberIdentity:
		s.Identity, err = decodeIdentity(path, m.value)
	default:
		err = profile.Errorf(profile.ErrConfigInvalid, "%s: unknown member %s", providerName, profile.Quote(m.name))
	}
	return err
}

// decodeIdentity decodes the identity object at path.
func decodeIdentity(path string, value json.RawMessage) (*BotIdentity, error) {
	members, err := objectMembers(value, path)
	if err != nil {
		return nil, err
	}
	var b BotIdentity
	for _, m := range members {
		if err := b.decodeMember(path, m); err != nil {
			return nil, err
		}
	}
	if err := requireMembers(members, path, memberLogin, memberUserID, memberResolvedAt); err != nil {
		return nil, err
	}
	return &b, nil
}

// decodeMember decodes one member of the identity object at path into b.
func (b *BotIdentity) decodeMember(path string, m member) error {
	memberPath := path + "." + m.name
	var err error
	switch m.name {
	case memberLogin:
		b.Login, err = decodeLogin(memberPath, m.value)
	case memberUserID:
		b.UserID, err = decodeID(memberPath, m.value)
	case memberResolvedAt:
		b.ResolvedAt, err = decodeResolvedAt(memberPath, m.value)
	default:
		err = profile.Errorf(profile.ErrConfigInvalid, "%s: unknown member %s", path, profile.Quote(m.name))
	}
	return err
}

// decodeID decodes a positive JSON integer of at most maxID without sign,
// fraction, exponent, or leading zero. A string is refused.
func decodeID(path string, value json.RawMessage) (int64, error) {
	if id, ok := ParseID(string(value)); ok {
		return id, nil
	}
	return 0, profile.Errorf(profile.ErrConfigInvalid, "%s: must be a positive integer", path)
}

// ParseID parses s as a decimal integer 1 ≤ n ≤ 2^53−1 with no sign, no
// leading zero, no fraction, no exponent, and no whitespace.
func ParseID(s string) (int64, bool) {
	if s == "" || s[0] == '0' || strings.Trim(s, "0123456789") != "" || len(s) > 16 {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > maxID {
		return 0, false
	}
	return n, true
}

// decodeKeyRef decodes the key reference at path. Its errors keep the class
// of profile.ParseKeyRef and never echo the value.
func decodeKeyRef(path string, value json.RawMessage) (profile.KeyRef, error) {
	text, ok := decodeString(value)
	if !ok {
		return profile.KeyRef{}, profile.Errorf(profile.ErrUnsupportedKeyReference, "%s: must be a string", path)
	}
	ref, err := profile.ParseKeyRef(text)
	if err != nil {
		return profile.KeyRef{}, profile.Errorf(profile.ErrUnsupportedKeyReference, "%s: %v", path, err)
	}
	return ref, nil
}

// decodeLogin decodes a bot login "<slug>[bot]".
func decodeLogin(path string, value json.RawMessage) (string, error) {
	text, ok := decodeString(value)
	if !ok {
		return "", profile.Errorf(profile.ErrConfigInvalid, "%s: must be a string", path)
	}
	if _, ok := botSlug(text); !ok {
		return "", profile.Errorf(profile.ErrConfigInvalid, `%s: %s is not a GitHub App bot login (expected "<slug>[bot]")`, path, profile.Quote(text))
	}
	return text, nil
}

// decodeResolvedAt decodes an RFC 3339 timestamp in UTC ("Z").
func decodeResolvedAt(path string, value json.RawMessage) (time.Time, error) {
	text, ok := decodeString(value)
	if ok && strings.HasSuffix(text, "Z") {
		if t, err := time.Parse(time.RFC3339, text); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, profile.Errorf(profile.ErrConfigInvalid, "%s: must be an RFC 3339 UTC timestamp", path)
}

// decodeString decodes a JSON string.
func decodeString(value json.RawMessage) (string, bool) {
	if len(value) == 0 || value[0] != '"' {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return text, true
}

// member is one member of a JSON object, in document order.
type member struct {
	name  string
	value json.RawMessage
}

// objectMembers returns the members of the JSON object value at path, in
// document order.
func objectMembers(value json.RawMessage, path string) ([]member, error) {
	notObject := profile.Errorf(profile.ErrConfigInvalid, "%s: must be an object", path)
	dec := json.NewDecoder(bytes.NewReader(value))
	dec.UseNumber()
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil, notObject
	}
	var members []member
	seen := map[string]bool{}
	for dec.More() {
		token, err := dec.Token()
		name, ok := token.(string)
		if err != nil || !ok {
			return nil, notObject
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, notObject
		}
		if seen[name] {
			return nil, profile.Errorf(profile.ErrConfigInvalid, "%s: duplicate member %s", path, profile.Quote(name))
		}
		seen[name] = true
		members = append(members, member{name: name, value: raw})
	}
	if token, err := dec.Token(); err != nil || token != json.Delim('}') {
		return nil, notObject
	}
	return members, nil
}

// requireMembers returns ErrConfigInvalid for the first of names that
// members lacks.
func requireMembers(members []member, path string, names ...string) error {
	for _, name := range names {
		if !hasMember(members, name) {
			return profile.Errorf(profile.ErrConfigInvalid, "%s: %s is missing", path, name)
		}
	}
	return nil
}

// hasMember reports whether members holds name.
func hasMember(members []member, name string) bool {
	for _, m := range members {
		if m.name == name {
			return true
		}
	}
	return false
}

// settingsJSON and identityJSON fix the member order of an encoded block.
type (
	settingsJSON struct {
		AppID          int64         `json:"app_id"`
		InstallationID int64         `json:"installation_id"`
		PrivateKey     string        `json:"private_key"`
		Identity       *identityJSON `json:"identity,omitempty"`
	}
	identityJSON struct {
		Login      string `json:"login"`
		UserID     int64  `json:"user_id"`
		ResolvedAt string `json:"resolved_at"`
	}
)

// MarshalJSON encodes the github block in the member order of the
// profile-config spec, with resolved_at in UTC to the second. It refuses
// settings that DecodeProfileSettings would refuse.
func (s ProfileSettings) MarshalJSON() ([]byte, error) {
	block := settingsJSON{AppID: s.AppID, InstallationID: s.InstallationID, PrivateKey: s.PrivateKey.String()}
	if s.Identity != nil {
		block.Identity = &identityJSON{
			Login:      s.Identity.Login,
			UserID:     s.Identity.UserID,
			ResolvedAt: s.Identity.ResolvedAt.UTC().Format(time.RFC3339),
		}
	}
	data, err := json.Marshal(block)
	if err != nil {
		return nil, err
	}
	if _, err := DecodeProfileSettings(host, data); err != nil {
		return nil, fmt.Errorf("github: invalid profile settings: %w", err)
	}
	return data, nil
}

// Update returns next as the settings that replace prev, where prev belonged
// to a profile on prevHost and next to one on nextHost. A cached bot belongs
// to one App on one host: when the host and the App ID are unchanged, prev's
// identity is carried over if next has none; when either changed, an identity
// of next that equals prev's is dropped, and only a newly resolved one is
// kept.
func Update(prevHost string, prev ProfileSettings, nextHost string, next ProfileSettings) ProfileSettings {
	sameApp := prevHost == nextHost && prev.AppID == next.AppID
	switch {
	case sameApp && next.Identity == nil && prev.Identity != nil:
		identity := *prev.Identity
		next.Identity = &identity
	case !sameApp && sameIdentity(prev.Identity, next.Identity):
		next.Identity = nil
	}
	return next
}

// sameIdentity reports whether a and b are both set and equal.
func sameIdentity(a, b *BotIdentity) bool {
	return a != nil && b != nil && a.Login == b.Login && a.UserID == b.UserID && a.ResolvedAt.Equal(b.ResolvedAt)
}
