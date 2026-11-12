package provider

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestFormattingRedactsSecrets(t *testing.T) {
	const secret = "s3cret-value"
	token := Token{Value: secret, ExpiresAt: time.Date(2026, 11, 12, 22, 0, 0, 0, time.UTC)}
	credential := GitCredential{Protocol: "https", Host: "git.example.test", Username: "agent", Password: secret}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
		for _, value := range []any{token, credential, &token, []Token{token}} {
			if got := fmt.Sprintf(verb, value); strings.Contains(got, secret) {
				t.Errorf("Sprintf(%s, %T) = %q, contains the secret", verb, value, got)
			}
		}
	}
	if got := fmt.Sprint(token); !strings.Contains(got, "2026-11-12T22:00:00Z") {
		t.Errorf("Sprint(token) = %q, want the expiry", got)
	}
	if got := fmt.Sprint(credential); !strings.Contains(got, "git.example.test") || !strings.Contains(got, "agent") {
		t.Errorf("Sprint(credential) = %q, want host and username", got)
	}
}
