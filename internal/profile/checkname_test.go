package profile

import (
	"errors"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestStoreCheckName(t *testing.T) {
	testenv.Isolate(t)
	store := NewStore(configPath(t), acmeRegistry(t))
	for _, name := range []string{"a", "a--b", "claude", "claude-2", "x" + strings.Repeat("9", 31)} {
		if err := store.CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", "Claude", "my_bot", "claude-", "-a", "1a", "x" + strings.Repeat("9", 32), "a\u202eb"}
	for _, name := range invalid {
		err := store.CheckName(name)
		if !errors.Is(err, ErrConfigInvalid) || !strings.Contains(err.Error(), "invalid profile name") {
			t.Errorf("CheckName(%q) = %v, want ErrConfigInvalid naming the rule", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "\u202e") {
			t.Errorf("CheckName echoes a format character: %q", err)
		}
	}
	secrets := []string{acmePrefix + "abc", "x" + acmePrefix, "-----BEGIN RSA PRIVATE KEY-----", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2ln", "a\nb", "a\x00b"}
	for _, name := range secrets {
		err := store.CheckName(name)
		if !errors.Is(err, ErrInlineSecret) {
			t.Errorf("CheckName(secret %d) = %v, want ErrInlineSecret", len(name), err)
			continue
		}
		if strings.Contains(err.Error(), name) || strings.Contains(err.Error(), "BEGIN") || strings.Contains(err.Error(), "eyJ") {
			t.Errorf("CheckName echoes the secret: %q", err)
		}
	}
}

func TestStorePath(t *testing.T) {
	testenv.Isolate(t)
	path := configPath(t)
	if got := NewStore(path, nil).Path(); got != path {
		t.Errorf("Path = %q, want %q", got, path)
	}
}
