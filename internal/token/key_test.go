package token

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestKeyForRejectsHostileFields(t *testing.T) {
	c := newClock()
	newManager(t, c)
	hostile := []string{"", "../x", "a/b", "a\x00b", "a\nb", "a b", "-x", ".x", strings.Repeat("a", 129), "ä"}
	for _, value := range hostile {
		for _, field := range []string{"name", "host", "subject"} {
			src := newSource(c)
			switch field {
			case "name":
				src.name = value
			case "host":
				src.host = value
			case "subject":
				src.subject = value
			}
			if _, err := KeyFor(src); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("KeyFor with %s %q: error %v, want ErrInvalidKey", field, value, err)
			}
		}
	}
	src := newSource(c)
	src.subject = strings.Repeat("a", 128)
	if _, err := KeyFor(src); err != nil {
		t.Errorf("KeyFor with a 128-character subject: %v", err)
	}
}

func TestKeyFileNamesSeparateKeys(t *testing.T) {
	newManager(t, newClock())
	base := Key{Provider: "fake", Host: "h.test", Subject: "s1", Scope: ScopeFull}
	others := []Key{
		{Provider: "fake", Host: "h.test", Subject: "s2", Scope: ScopeFull},
		{Provider: "fake2", Host: "h.test", Subject: "s1", Scope: ScopeFull},
		{Provider: "fake", Host: "h2.test", Subject: "s1", Scope: ScopeFull},
		{Provider: "fake", Host: "h.test", Subject: "s1", Scope: "other"},
		{Provider: "fake", Host: "h.tests", Subject: "1", Scope: ScopeFull},
	}
	name := base.fileName()
	if !regexp.MustCompile(`^[0-9a-f]{64}\.json$`).MatchString(name) {
		t.Errorf("fileName() = %q, want 64 hex digits and %s", name, entrySuffix)
	}
	for _, other := range others {
		if other.fileName() == name {
			t.Errorf("%v and %v share a file name", base, other)
		}
	}
}

func TestDir(t *testing.T) {
	newManager(t, newClock())
	tests := []struct {
		xdg, home, want string
		err             error
	}{
		{"/x/cache", "/h", "/x/cache/agent-id/tokens", nil},
		{"", "/h", "/h/.cache/agent-id/tokens", nil},
		{"relative/cache", "/h", "/h/.cache/agent-id/tokens", nil},
		{"", "", "", ErrNoCacheDir},
		{"", "relative", "", ErrNoCacheDir},
		{"relative", "", "", ErrNoCacheDir},
	}
	for _, tt := range tests {
		got, err := Dir(func(name string) string {
			return map[string]string{"XDG_CACHE_HOME": tt.xdg, "HOME": tt.home}[name]
		})
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("Dir(XDG_CACHE_HOME=%q, HOME=%q) = %q, %v; want %q, %v", tt.xdg, tt.home, got, err, tt.want, tt.err)
		}
	}
}
