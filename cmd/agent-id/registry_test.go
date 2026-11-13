package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/profile"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestRegistry(t *testing.T) {
	testenv.Isolate(t)
	registry, err := newRegistry()
	if err != nil {
		t.Fatalf("newRegistry: %v", err)
	}
	if got := registry.Supported(); !slices.Equal(got, []string{"github"}) {
		t.Errorf("Supported = %v, want [github]", got)
	}
	for _, prefix := range []string{"ghs_", "ghp_", "gho_", "ghu_", "ghr_", "github_pat_"} {
		config := `{"version":1,"profiles":{"gl":{"provider":"gitlab","host":"gitlab.com","gitlab":{"v":"` + prefix + `x"}}}}`
		_, err := profile.Parse([]byte(config), registry)
		if !errors.Is(err, profile.ErrInlineSecret) {
			t.Errorf("prefix %s: error = %v, want ErrInlineSecret", prefix, err)
		}
		if err != nil && strings.Contains(err.Error(), prefix) {
			t.Errorf("prefix %s: error %q echoes the value", prefix, err)
		}
	}
}
