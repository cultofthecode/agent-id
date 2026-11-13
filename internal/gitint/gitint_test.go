package gitint

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
)

const (
	goodName  = "claude-dev[bot]"
	goodEmail = "12345678+claude-dev[bot]@users.noreply.github.com"
)

func TestIdentityGolden(t *testing.T) {
	got, err := Identity(provider.Identity{Name: goodName, Email: goodEmail})
	if err != nil {
		t.Fatal(err)
	}
	wantSet := map[string]string{
		"GIT_AUTHOR_NAME": goodName, "GIT_COMMITTER_NAME": goodName,
		"GIT_AUTHOR_EMAIL": goodEmail, "GIT_COMMITTER_EMAIL": goodEmail,
	}
	wantCfg := []Entry{{"commit.gpgSign", "false"}, {"tag.gpgSign", "false"}, {"tag.forceSignAnnotated", "false"}}
	if !reflect.DeepEqual(got.Set, wantSet) || !reflect.DeepEqual(got.GitConfig, wantCfg) {
		t.Errorf("Identity = %+v", got)
	}
}

func TestIdentityRejectsHostileValues(t *testing.T) {
	long := strings.Repeat("a", 256)
	names := []string{"", long, "a\nb", "a\rb", "a\x00b", "a\x7fb", "a\u0085b", "a b", "a b", "a<b", "a>b", " a", "a ", "a\xffb"}
	for _, n := range names {
		_, err := Identity(provider.Identity{Name: n, Email: goodEmail})
		if !errors.Is(err, ErrIdentityInvalid) {
			t.Errorf("name %q: err = %v", n, err)
		} else if n != "" && strings.Contains(err.Error(), n) {
			t.Errorf("name %q echoed in %q", n, err)
		}
	}
	emails := []string{"", "a@", "ab", "nodomain", "a@b@c", "a b@c", "a<b@c", "a>b@c", "a,b@c", "a\"b@c", "@c.d", "a@", strings.Repeat("a", 250) + "@b.cd", "é@example.com", "a\n@b.c"}
	for _, e := range emails {
		_, err := Identity(provider.Identity{Name: goodName, Email: e})
		if !errors.Is(err, ErrIdentityInvalid) {
			t.Errorf("email %q: err = %v", e, err)
		} else if len(e) > 2 && strings.Contains(err.Error(), e) {
			t.Errorf("email %q echoed in %q", e, err)
		}
	}
}

func TestIdentityBoundaries(t *testing.T) {
	for _, id := range []provider.Identity{
		{Name: strings.Repeat("a", 255), Email: "a@b"},
		{Name: "Åsa Ünïcode", Email: strings.Repeat("a", 252) + "@b"},
	} {
		if _, err := Identity(id); err != nil {
			t.Errorf("Identity(%d,%d bytes): %v", len(id.Name), len(id.Email), err)
		}
	}
}

func TestIdentityReturnsFreshState(t *testing.T) {
	a, _ := Identity(provider.Identity{Name: goodName, Email: goodEmail})
	a.Set["GIT_AUTHOR_NAME"] = "x"
	a.GitConfig[0].Value = "true"
	b, _ := Identity(provider.Identity{Name: goodName, Email: goodEmail})
	if b.Set["GIT_AUTHOR_NAME"] != goodName || b.GitConfig[0].Value != "false" {
		t.Errorf("state shared between calls: %+v", b)
	}
}
