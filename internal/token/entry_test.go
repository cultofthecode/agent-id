package token

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestEntryRoundTrip(t *testing.T) {
	newManager(t, newClock())
	key := Key{Provider: "fake", Host: "h.test", Subject: testSubject, Scope: ScopeFull}
	token := provider.Token{Value: testenv.FakeToken("entry"), ExpiresAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}
	got, err := decodeEntry(encodeEntry(key, token), key)
	if err != nil || got != token {
		t.Fatalf("decodeEntry(encodeEntry()) = %v, %v; want %v", got, err, token)
	}
}

func TestDecodeEntryRejectsHostileContent(t *testing.T) {
	newManager(t, newClock())
	key := Key{Provider: "fake", Host: "h.test", Subject: testSubject, Scope: ScopeFull}
	secret := testenv.FakeToken("hostile")
	valid := `"version":1,"provider":"fake","host":"h.test","subject":"` + testSubject + `","scope":"full","token":"` + secret + `","expires_at":"2026-11-13T09:00:00Z"`
	replace := func(old, new string) string { return "{" + strings.Replace(valid, old, new, 1) + "}" }
	tests := map[string]string{
		"empty":             "",
		"truncated":         "{" + valid[:40],
		"not an object":     `["` + secret + `"]`,
		"trailing data":     "{" + valid + "}{}",
		"unknown member":    "{" + valid + `,"extra":1}`,
		"duplicate member":  "{" + valid + `,"token":"` + secret + `"}`,
		"missing member":    replace(`,"scope":"full"`, ""),
		"nested value":      replace(`"scope":"full"`, `"scope":{"a":1}`),
		"version as string": replace(`"version":1`, `"version":"1"`),
		"version 2":         replace(`"version":1`, `"version":2`),
		"other subject":     replace(`"subject":"`+testSubject, `"subject":"app-1-installation-3`),
		"other provider":    replace(`"provider":"fake"`, `"provider":"other"`),
		"other scope":       replace(`"scope":"full"`, `"scope":"repo"`),
		"empty token":       replace(`"token":"`+secret, `"token":"`),
		"token with \\n":    replace(`"token":"`+secret, `"token":"`+secret+`\n`),
		"token with NUL":    replace(`"token":"`+secret, `"token":"`+secret+`\u0000`),
		"token with space":  replace(`"token":"`+secret, `"token":"a `+secret),
		"non-ASCII token":   replace(`"token":"`+secret, `"token":"`+secret+`ä`),
		"token too long":    replace(`"token":"`+secret, `"token":"`+strings.Repeat("a", maxTokenLength+1)),
		"zero expiry":       replace(`"expires_at":"2026-11-13T09:00:00Z"`, `"expires_at":"0001-01-01T00:00:00Z"`),
		"bad expiry":        replace(`"expires_at":"2026-11-13T09:00:00Z"`, `"expires_at":"`+secret+`"`),
	}
	for name, data := range tests {
		token, err := decodeEntry([]byte(data), key)
		if !errors.Is(err, errInvalidEntry) || token != (provider.Token{}) {
			t.Errorf("%s: decodeEntry = %v, %v; want errInvalidEntry and the zero token", name, token, err)
		}
		assertNoToken(t, err, secret)
	}
}

func TestCheckValue(t *testing.T) {
	newManager(t, newClock())
	for _, value := range []string{"a", testenv.FakeToken("x"), strings.Repeat("~", maxTokenLength)} {
		if err := checkValue(value); err != nil {
			t.Errorf("checkValue(%d characters): %v", len(value), err)
		}
	}
	for _, value := range []string{"", "a b", "a\tb", "a\rb", "a\x7f", "é", strings.Repeat("a", maxTokenLength+1)} {
		err := checkValue(value)
		if !errors.Is(err, ErrUnusableToken) {
			t.Errorf("checkValue(%q): %v, want ErrUnusableToken", value, err)
		}
		assertNoToken(t, err, value)
	}
}

func TestFreshAtTheMarginBoundary(t *testing.T) {
	newManager(t, newClock())
	now := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	margin := 5 * time.Minute
	tests := []struct {
		expiresIn time.Duration
		want      bool
	}{
		{30 * time.Minute, true},
		{margin + time.Second, true},
		{margin, false},
		{2 * time.Minute, false},
		{-time.Minute, false},
	}
	for _, tt := range tests {
		if got := fresh(provider.Token{ExpiresAt: now.Add(tt.expiresIn)}, now, margin); got != tt.want {
			t.Errorf("fresh(expires in %v) = %v, want %v", tt.expiresIn, got, tt.want)
		}
	}
}

func TestCheckMintedRejectsShortLivedTokens(t *testing.T) {
	newManager(t, newClock())
	now := time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)
	value := testenv.FakeToken("short")
	for _, lifetime := range []time.Duration{-time.Hour, 0, time.Minute, DefaultMargin} {
		err := checkMinted(provider.Token{Value: value, ExpiresAt: now.Add(lifetime)}, now, DefaultMargin)
		if !errors.Is(err, ErrUnusableToken) {
			t.Errorf("checkMinted(lifetime %v): %v, want ErrUnusableToken", lifetime, err)
		}
		assertNoToken(t, err, value)
	}
	if err := checkMinted(provider.Token{Value: value, ExpiresAt: now.Add(time.Hour)}, now, DefaultMargin); err != nil {
		t.Errorf("checkMinted(one hour): %v", err)
	}
}
