package token

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// entryVersion is the format version of a cache entry. An entry of another
// version is a cache miss and is replaced.
const entryVersion = 1

// maxTokenLength bounds a token value.
const maxTokenLength = 4096

// entryMembers are the members of an entry, each required exactly once.
var entryMembers = []string{"version", "provider", "host", "subject", "scope", "token", "expires_at"}

// errInvalidEntry is the fixed reason of every decoding failure: a parser
// error can repeat its input, which holds a token.
var errInvalidEntry = errors.New("invalid token cache entry")

// entry is the JSON form of a cache entry.
type entry struct {
	Version   int       `json:"version"`
	Provider  string    `json:"provider"`
	Host      string    `json:"host"`
	Subject   string    `json:"subject"`
	Scope     string    `json:"scope"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// encodeEntry returns the cache entry of token under key.
func encodeEntry(key Key, token provider.Token) []byte {
	data, _ := json.Marshal(entry{
		Version: entryVersion, Provider: key.Provider, Host: key.Host, Subject: key.Subject, Scope: key.Scope,
		Token: token.Value, ExpiresAt: token.ExpiresAt.UTC(),
	})
	return append(data, '\n')
}

// decodeEntry returns the token of data when data is a valid entry of key.
// Its error is always errInvalidEntry.
func decodeEntry(data []byte, key Key) (provider.Token, error) {
	if err := checkMembers(data); err != nil {
		return provider.Token{}, errInvalidEntry
	}
	var e entry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return provider.Token{}, errInvalidEntry
	}
	if e.Version != entryVersion || (Key{e.Provider, e.Host, e.Subject, e.Scope}) != key {
		return provider.Token{}, errInvalidEntry
	}
	token := provider.Token{Value: e.Token, ExpiresAt: e.ExpiresAt}
	if checkValue(token.Value) != nil || token.ExpiresAt.IsZero() {
		return provider.Token{}, errInvalidEntry
	}
	return token, nil
}

// checkMembers checks that data is one flat JSON object with each member of
// entryMembers exactly once, scalar values, and nothing after it.
func checkMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if open, err := decoder.Token(); err != nil || open != json.Delim('{') {
		return errInvalidEntry
	}
	seen := map[string]bool{}
	for decoder.More() {
		name, err := decoder.Token()
		member, isString := name.(string)
		if err != nil || !isString || seen[member] || !isEntryMember(member) {
			return errInvalidEntry
		}
		seen[member] = true
		if value, err := decoder.Token(); err != nil || isDelim(value) {
			return errInvalidEntry
		}
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') || len(seen) != len(entryMembers) {
		return errInvalidEntry
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errInvalidEntry
	}
	return nil
}

func isEntryMember(name string) bool {
	for _, member := range entryMembers {
		if name == member {
			return true
		}
	}
	return false
}

func isDelim(value json.Token) bool {
	_, ok := value.(json.Delim)
	return ok
}

// checkValue accepts 1–4096 visible ASCII characters, so that a token can
// never inject a line into the Git credential protocol or an environment.
func checkValue(value string) error {
	if value == "" || len(value) > maxTokenLength {
		return fmt.Errorf("%w: the token must be 1–%d characters", ErrUnusableToken, maxTokenLength)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return fmt.Errorf("%w: the token contains a character that is not visible ASCII", ErrUnusableToken)
		}
	}
	return nil
}

// fresh reports whether token stays valid for longer than margin after now.
func fresh(token provider.Token, now time.Time, margin time.Duration) bool {
	return now.Add(margin).Before(token.ExpiresAt)
}

// checkMinted accepts a newly minted token only when its value is valid and
// it stays valid for longer than margin; anything else would make every later
// call mint again.
func checkMinted(token provider.Token, now time.Time, margin time.Duration) error {
	if err := checkValue(token.Value); err != nil {
		return err
	}
	if !fresh(token, now, margin) {
		return fmt.Errorf("%w: it expires at %s, within the refresh margin of %s; check the system clock", ErrUnusableToken, token.ExpiresAt.UTC().Format(time.RFC3339), margin)
	}
	return nil
}
