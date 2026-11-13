package token

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// testSubject is the token subject of the test sources.
const testSubject = "app-1-installation-2"

// clock is a settable test clock.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time               { return c.now }
func (c *clock) Advance(d time.Duration)      { c.now = c.now.Add(d) }
func newClock() *clock                        { return &clock{now: time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC)} }
func (c *clock) In(d time.Duration) time.Time { return c.now.Add(d) }

// source is a test Source whose tokens expire relative to the test clock.
type source struct {
	name, host, subject string
	clock               *clock
	lifetime            time.Duration
	value               string
	failure             error
	minted              []provider.Token
}

// newSource returns a source of fake one-hour tokens on c.
func newSource(c *clock) *source {
	return &source{name: "fake", host: "git.agent-id.test", subject: testSubject, clock: c, lifetime: time.Hour}
}

func (s *source) Name() string         { return s.name }
func (s *source) Host() string         { return s.host }
func (s *source) TokenSubject() string { return s.subject }

func (s *source) Token(ctx context.Context) (provider.Token, error) {
	if err := ctx.Err(); err != nil {
		return provider.Token{}, fmt.Errorf("test source: %w", err)
	}
	if s.failure != nil {
		return provider.Token{}, fmt.Errorf("test source: %w", s.failure)
	}
	value := s.value
	if value == "" {
		value = testenv.FakeToken(fmt.Sprintf("%s_%d", s.subject, len(s.minted)+1))
	}
	token := provider.Token{Value: value, ExpiresAt: s.clock.In(s.lifetime)}
	s.minted = append(s.minted, token)
	return token, nil
}

// newManager isolates the test and returns a Manager on the isolated cache
// directory with the test clock.
func newManager(t *testing.T, c *clock) *Manager {
	t.Helper()
	testenv.Isolate(t)
	dir, err := Dir(os.Getenv)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	return &Manager{Dir: dir, Now: c.Now}
}

// mustKey returns the key of src.
func mustKey(t *testing.T, src Source) Key {
	t.Helper()
	key, err := KeyFor(src)
	if err != nil {
		t.Fatalf("KeyFor: %v", err)
	}
	return key
}

// entryPath returns the path of src's entry in m.
func entryPath(t *testing.T, m *Manager, src Source) string {
	t.Helper()
	return filepath.Join(m.Dir, mustKey(t, src).fileName())
}

// seed writes an entry for src that expires at expiresAt.
func seed(t *testing.T, m *Manager, src Source, value string, expiresAt time.Time) {
	t.Helper()
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeEntry(m.Dir, entryPath(t, m, src), encodeEntry(mustKey(t, src), provider.Token{Value: value, ExpiresAt: expiresAt})); err != nil {
		t.Fatalf("writeEntry: %v", err)
	}
}

// assertNoToken fails the test when err contains any of the token values.
func assertNoToken(t *testing.T, err error, values ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, value := range values {
		if value != "" && strings.Contains(err.Error(), value) {
			t.Errorf("error %q contains a token", err.Error())
		}
	}
	if strings.Contains(err.Error(), testenv.FakeTokenPrefix) {
		t.Errorf("error %q contains a token prefix", err.Error())
	}
}

// skipAsRoot skips a test that relies on permissions root ignores.
func skipAsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}
