package diagnose

import (
	"errors"
	"strings"
	"testing"
)

var testPatterns = NewPatterns([]string{"ghs_", "ghp_", "gho_", "ghu_", "ghr_", "github_pat_", "fake_"})

const fakeJWT = "eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxMjMifQ.c2lnbmF0dXJl"

func TestRedact(t *testing.T) {
	long := strings.Repeat("x", 10*1024)
	tests := []struct {
		name, in, want string
	}{
		{"pem in the middle", "key -----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY----- after", "key [redacted] after"},
		{"pem without an end marker", "key -----BEGIN PRIVATE KEY-----\nMIIB", "key [redacted]"},
		{"jwt in a url", "https://x.test/?t=" + fakeJWT + "&a=1", "https://x.test/?t=[redacted]&a=1"},
		{"prefix at the start", "ghs_abc123 tail", "[redacted] tail"},
		{"prefix in the middle", "value=ghp_AbC_9", "value=[redacted]"},
		{"prefix next to punctuation", "(github_pat_11AA22),", "([redacted]),"},
		{"prefix of the fake provider", "token fake_xyz.", "token [redacted]."},
		{"user-info with password", "https://user:pw@github.com/o/r", "https://[redacted]@github.com/o/r"},
		{"user-info that is a token", "https://abc@github.com/o/r", "https://[redacted]@github.com/o/r"},
		{"ssh user is kept", "ssh://git@github.com/o/r", "ssh://git@github.com/o/r"},
		{"ssh user with a password", "ssh://git:pw@github.com/o/r", "ssh://[redacted]@github.com/o/r"},
		{"scp-like user is kept", "git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"authorization header", "http.extraHeader Authorization: Bearer abc.def", "http.extraHeader Authorization: [redacted]"},
		{"password label", "helper password=abc", "helper password= [redacted]"},
		{"oauth label", "oauth_token: abc", "oauth_token: [redacted]"},
		{"control characters", "a\x1b[31mb\nc", "a?[31mb?c"},
		{"plain text", "claude-dev[bot] matches", "claude-dev[bot] matches"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testPatterns.Redact(tt.in); got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	got := testPatterns.Redact(long)
	if n := len([]rune(got)); n != maxValue || !strings.HasSuffix(got, "…") {
		t.Errorf("Redact of a 10 KiB value has %d characters, want %d ending with an ellipsis", n, maxValue)
	}
}

func TestRedactOutputAlwaysPassesGuard(t *testing.T) {
	hostile := []string{
		"ghs_x", "-----BEGIN", fakeJWT, "https://a:b@h/", "https://tok@h/", "Authorization: Bearer x", "password=x", "oauth_token: x",
		"credential.https://u:ghp_x@github.com.helper", "url.https://tok@host/.insteadOf", "GH_TOKEN=ghs_aaaa\nAuthorization: x",
	}
	for _, in := range hostile {
		if err := testPatterns.Guard([]byte(testPatterns.Redact(in))); err != nil {
			t.Errorf("Guard(Redact(%q)): %v", in, err)
		}
	}
}

func TestGuard(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		withheld bool
	}{
		{"clean text", "ok  config.load  loaded", false},
		{"redacted values", "Authorization: [redacted]\nhttps://[redacted]@h/", false},
		{"ssh report", "origin resolves to ssh://git@github.com/o/r and git@github.com:o/r.git", false},
		{"token", "x ghs_abc", true},
		{"pem", "-----BEGIN PRIVATE KEY-----", true},
		{"jwt", fakeJWT, true},
		{"user-info", "https://u:p@github.com/", true},
		{"authorization value", "Authorization: Bearer x", true},
		{"password value", "password=hunter2", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testPatterns.Guard([]byte(tt.in))
			if (err != nil) != tt.withheld || err != nil && !errors.Is(err, ErrWithheld) {
				t.Errorf("Guard(%q) = %v, want withheld %v", tt.in, err, tt.withheld)
			}
		})
	}
}

func TestAbbreviateHome(t *testing.T) {
	tests := []struct{ name, text, home, want string }{
		{"inside home", "/home/dev/.config/agent-id/config.json", "/home/dev", "~/.config/agent-id/config.json"},
		{"home itself", "/home/dev", "/home/dev", "~"},
		{"prefix collision", "/home/devx/file", "/home/dev", "/home/devx/file"},
		{"not a path start", "/mnt/home/dev/file", "/home/dev", "/mnt/home/dev/file"},
		{"in a sentence", "key /home/dev/k.pem: mode 0644", "/home/dev", "key ~/k.pem: mode 0644"},
		{"trailing slash in home", "/home/dev/x", "/home/dev/", "~/x"},
		{"relative path", "keys/claude.pem", "/home/dev", "keys/claude.pem"},
		{"empty home", "/home/dev/x", "", "/home/dev/x"},
		{"root home", "/etc/x", "/", "/etc/x"},
		{"twice", "/home/dev/a and /home/dev/b", "/home/dev", "~/a and ~/b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AbbreviateHome(tt.text, tt.home); got != tt.want {
				t.Errorf("AbbreviateHome(%q, %q) = %q, want %q", tt.text, tt.home, got, tt.want)
			}
		})
	}
}
