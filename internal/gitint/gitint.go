// Package gitint turns a provider's bot identity into the process-scoped Git
// data of an agent context: the author and committer variables and the
// entries that turn personal signing off (spec
// .ai/specs/2026-11-13-git-commit-identity.md, #21). Every function is pure,
// names no forge, and writes nothing.
package gitint

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// ErrIdentityInvalid is returned for an identity that Git could not take into
// a commit object unchanged. Its message never contains the value.
var ErrIdentityInvalid = errors.New("gitint: the bot identity is malformed")

// Entry is one process-scoped Git configuration entry.
type Entry struct{ Key, Value string }

// Contribution is what the identity adds to a child environment.
type Contribution struct {
	// Set maps variable names to values.
	Set map[string]string
	// GitConfig lists entries to append, in order, to GIT_CONFIG_*.
	GitConfig []Entry
}

// Identity returns the author and committer variables for id and the entries
// that turn commit signing, tag signing, and forced signing of tags off. It
// fails with ErrIdentityInvalid, naming the field and the rule but never the
// value, when the name or the email is not safe in an ident line.
func Identity(id provider.Identity) (Contribution, error) {
	if rule := nameRule(id.Name); rule != "" {
		return Contribution{}, fmt.Errorf("%w (name: %s)", ErrIdentityInvalid, rule)
	}
	if rule := emailRule(id.Email); rule != "" {
		return Contribution{}, fmt.Errorf("%w (email: %s)", ErrIdentityInvalid, rule)
	}
	return Contribution{
		Set: map[string]string{
			"GIT_AUTHOR_NAME":     id.Name,
			"GIT_COMMITTER_NAME":  id.Name,
			"GIT_AUTHOR_EMAIL":    id.Email,
			"GIT_COMMITTER_EMAIL": id.Email,
		},
		GitConfig: []Entry{
			{Key: "commit.gpgSign", Value: "false"},
			{Key: "tag.gpgSign", Value: "false"},
			{Key: "tag.forceSignAnnotated", Value: "false"},
		},
	}, nil
}

func nameRule(name string) string {
	switch {
	case name == "" || len(name) > 255:
		return "length must be 1 to 255 bytes"
	case !utf8.ValidString(name):
		return "not valid UTF-8"
	case strings.ContainsFunc(name, forbiddenInName):
		return "contains a control character, a line separator, '<' or '>'"
	case name != strings.TrimSpace(name) || strings.HasPrefix(name, " ") || strings.HasSuffix(name, " "):
		return "has leading or trailing space"
	}
	return ""
}

func forbiddenInName(r rune) bool {
	return unicode.IsControl(r) || r == ' ' || r == ' ' || r == '<' || r == '>'
}

func emailRule(email string) string {
	if len(email) < 3 || len(email) > 254 {
		return "length must be 3 to 254 bytes"
	}
	for i := 0; i < len(email); i++ {
		if c := email[i]; c < 0x21 || c > 0x7e || strings.IndexByte("<>,\"", c) >= 0 {
			return "must be printable ASCII without space, '<', '>', ',' or '\"'"
		}
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || strings.Contains(domain, "@") || local == "" || domain == "" {
		return "must have exactly one '@' with a local part and a domain"
	}
	return ""
}
