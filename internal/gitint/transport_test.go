package gitint

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
)

const host = "github.com"

func parse(t *testing.T, input string) Request {
	t.Helper()
	req, err := ParseRequest(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseRequest(%q): %v", input, err)
	}
	return req
}

func TestParseRequestReadsTheAttributes(t *testing.T) {
	req := parse(t, "protocol=https\nhost=github.com\npath=o/r.git\nusername=u\ncapability[]=authtype\nwwwauth[]=Basic\nurl=http://evil\n\nhost=ignored\n")
	if req.Protocol != "https" || req.Host != "github.com" || req.Path != "o/r.git" || req.Username != "u" || req.hasPassword {
		t.Errorf("request = %+v", req)
	}
	if got := parse(t, "protocol=https\nhost=github.com"); got.Host != "github.com" {
		t.Errorf("no final newline: %+v", got)
	}
}

func TestParseRequestRefusesHostileInput(t *testing.T) {
	tooMany := strings.Repeat("x[]=1\n", maxRequestLines+1)
	tests := map[string]string{
		"NUL byte":           "host=github.com\x00\n",
		"NUL in the key":     "ho\x00st=github.com\n",
		"line without equal": "host\n",
		"empty key":          "=value\n",
		"repeated host":      "host=github.com\nhost=evil.example\n",
		"repeated protocol":  "protocol=https\nprotocol=http\n",
		"repeated path":      "path=o/r\npath=o/s\n",
		"repeated username":  "username=a\nusername=b\n",
		"repeated password":  "password=a\npassword=b\n",
		"CR in the host":     "host=github.com\r\n",
		"CR in the protocol": "protocol=https\r\nhost=github.com\n",
		"CR in the middle":   "host=evil.example\rhost=github.com\n",
		"too many lines":     tooMany,
		"too long":           "host=" + strings.Repeat("a", maxRequestBytes) + "\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ParseRequest(strings.NewReader(input))
			if !errors.Is(err, ErrRequestInvalid) {
				t.Fatalf("err = %v, want ErrRequestInvalid", err)
			}
			for _, secret := range []string{"evil.example", "github.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error %q echoes the input", err)
				}
			}
		})
	}
}

func TestDecide(t *testing.T) {
	good := "protocol=https\nhost=github.com\npath=owner/repo.git\n"
	tests := []struct {
		name  string
		input string
		want  Verdict
	}{
		{"https push", good, Allow},
		{"upper-case host", "protocol=https\nhost=GitHub.COM\npath=o/r\n", Allow},
		{"path with trailing slash", "protocol=https\nhost=github.com\npath=o/r/\n", Allow},
		{"path with dots and dashes", "protocol=https\nhost=github.com\npath=a.b-c_d/e.f\n", Allow},
		{"username is ignored", good + "username=anyone\n", Allow},
		{"other host", "protocol=https\nhost=example.com\npath=o/r\n", Skip},
		{"look-alike prefix", "protocol=https\nhost=github.com.evil.example\npath=o/r\n", Skip},
		{"look-alike suffix", "protocol=https\nhost=evilgithub.com\npath=o/r\n", Skip},
		{"subdomain", "protocol=https\nhost=api.github.com\npath=o/r\n", Skip},
		{"trailing dot", "protocol=https\nhost=github.com.\npath=o/r\n", Skip},
		{"port", "protocol=https\nhost=github.com:443\npath=o/r\n", Refuse},
		{"user-info", "protocol=https\nhost=user@github.com\npath=o/r\n", Refuse},
		{"user-info and port", "protocol=https\nhost=u:p@github.com:8443\npath=o/r\n", Refuse},
		{"user-info look-alike", "protocol=https\nhost=github.com@evil.example\npath=o/r\n", Skip},
		{"port on a look-alike", "protocol=https\nhost=evil.example:443\npath=o/r\n", Skip},
		{"leading space", "protocol=https\nhost= github.com\npath=o/r\n", Skip},
		{"one letter missing", "protocol=https\nhost=gitub.com\npath=o/r\n", Skip},
		{"non-ASCII look-alike", "protocol=https\nhost=githuь.com\npath=o/r\n", Skip},
		{"empty host", "protocol=https\npath=o/r\n", Skip},
		{"no host but url", "protocol=https\nurl=https://github.com/o/r\npath=o/r\n", Skip},
		{"http", "protocol=http\nhost=github.com\npath=o/r\n", Refuse},
		{"ssh", "protocol=ssh\nhost=github.com\npath=o/r\n", Refuse},
		{"no protocol", "host=github.com\npath=o/r\n", Refuse},
		{"upper-case protocol", "protocol=HTTPS\nhost=github.com\npath=o/r\n", Refuse},
		{"no path", "protocol=https\nhost=github.com\n", Refuse},
		{"dot-dot path", "protocol=https\nhost=github.com\npath=../r\n", Refuse},
		{"dot-dot repository", "protocol=https\nhost=github.com\npath=o/..\n", Refuse},
		{"dot owner", "protocol=https\nhost=github.com\npath=./r\n", Refuse},
		{"one segment", "protocol=https\nhost=github.com\npath=o\n", Refuse},
		{"three segments", "protocol=https\nhost=github.com\npath=o/r/extra\n", Refuse},
		{"leading slash", "protocol=https\nhost=github.com\npath=/o/r\n", Refuse},
		{"percent", "protocol=https\nhost=github.com\npath=o/%2e%2e\n", Refuse},
		{"query", "protocol=https\nhost=github.com\npath=o/r?x=1\n", Refuse},
		{"space", "protocol=https\nhost=github.com\npath=o/r x\n", Refuse},
		{"password", good + "password=p\n", Refuse},
		{"empty password", good + "password=\n", Refuse},
		{"empty path", "protocol=https\nhost=github.com\npath=\n", Refuse},
		{"101-character part", "protocol=https\nhost=github.com\npath=" + strings.Repeat("a", 101) + "/r\n", Refuse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := Decide(parse(t, tt.input), host)
			if got != tt.want {
				t.Fatalf("Decide = %v (%q), want %v", got, reason, tt.want)
			}
			if (got == Refuse) == (reason == "") {
				t.Errorf("reason %q for verdict %v", reason, got)
			}
			for _, part := range []string{"x=1", "%2e", "o/r", "/r"} {
				if strings.Contains(reason, part) {
					t.Errorf("reason %q echoes the request", reason)
				}
			}
		})
	}
}

func TestDecideForAHostWithAPort(t *testing.T) {
	req := parse(t, "protocol=https\nhost=127.0.0.1:18001\npath=o/r\n")
	if v, _ := Decide(req, "127.0.0.1:18001"); v != Allow {
		t.Errorf("same host and port: %v", v)
	}
	if v, _ := Decide(req, "127.0.0.1:18002"); v != Refuse {
		t.Errorf("other port: %v", v)
	}
	if v, _ := Decide(req, ""); v != Skip {
		t.Errorf("no configured host: %v", v)
	}
}

func TestRender(t *testing.T) {
	good := provider.GitCredential{Protocol: "https", Host: host, Username: "x-access-token", Password: "ghs_aidtest_one"}
	got, err := Render(good, host)
	if err != nil || got != "username=x-access-token\npassword=ghs_aidtest_one\n" {
		t.Fatalf("Render = %q, %v", got, err)
	}
	bad := map[string]provider.GitCredential{
		"http":            {Protocol: "http", Host: host, Username: "u", Password: "p"},
		"other host":      {Protocol: "https", Host: "example.com", Username: "u", Password: "p"},
		"no host":         {Protocol: "https", Username: "u", Password: "p"},
		"empty username":  {Protocol: "https", Host: host, Password: "p"},
		"empty password":  {Protocol: "https", Host: host, Username: "u"},
		"newline in user": {Protocol: "https", Host: host, Username: "u\nquit=0", Password: "p"},
		"newline in pass": {Protocol: "https", Host: host, Username: "u", Password: "ghs_aidtest_x\npassword=y"},
		"CR in pass":      {Protocol: "https", Host: host, Username: "u", Password: "p\r"},
		"NUL in pass":     {Protocol: "https", Host: host, Username: "u", Password: "p\x00"},
	}
	for name, cred := range bad {
		t.Run(name, func(t *testing.T) {
			out, err := Render(cred, host)
			if !errors.Is(err, ErrCredentialInvalid) || out != "" {
				t.Fatalf("Render = %q, %v", out, err)
			}
			if strings.Contains(err.Error(), "ghs_") || strings.Contains(err.Error(), "quit") {
				t.Errorf("error %q echoes a value", err)
			}
		})
	}
}

func TestTransportEntries(t *testing.T) {
	got, err := TransportEntries("/opt/agent id/agent-id", "claude", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{
		{"credential.https://github.com.helper", "!'/opt/agent id/agent-id' git-credential --profile claude"},
		{"credential.https://github.com.useHttpPath", "true"},
		{"url.https://github.com/.insteadOf", "ssh://git@github.com/"},
		{"url.https://github.com/.insteadOf", "git@github.com:"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("entries = %v\nwant     %v", got, want)
	}
}

func TestTransportEntriesForAHostWithAPort(t *testing.T) {
	got, err := TransportEntries("/b", "p", "127.0.0.1:18001")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[2].Value != "ssh://git@127.0.0.1:18001/" {
		t.Errorf("entries = %v", got)
	}
}

func TestTransportEntriesQuoteTheBinary(t *testing.T) {
	got, err := TransportEntries("/tmp/it's/$(x)/agent-id", "claude", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if want := `!'/tmp/it'\''s/$(x)/agent-id' git-credential --profile claude`; got[0].Value != want {
		t.Errorf("helper = %q, want %q", got[0].Value, want)
	}
}

func TestTransportEntriesRefuseHostileValues(t *testing.T) {
	tests := map[string][3]string{
		"relative binary":  {"agent-id", "claude", "github.com"},
		"empty binary":     {"", "claude", "github.com"},
		"newline binary":   {"/b\n/agent-id", "claude", "github.com"},
		"NUL binary":       {"/b\x00", "claude", "github.com"},
		"escape binary":    {"/b\x1b[0m", "claude", "github.com"},
		"upper profile":    {"/b", "Claude", "github.com"},
		"profile with ;":   {"/b", "a;b", "github.com"},
		"profile with $":   {"/b", "a$b", "github.com"},
		"empty profile":    {"/b", "", "github.com"},
		"long profile":     {"/b", strings.Repeat("a", 33), "github.com"},
		"empty host":       {"/b", "claude", ""},
		"host with slash":  {"/b", "claude", "github.com/x"},
		"host with at":     {"/b", "claude", "u@github.com"},
		"host with space":  {"/b", "claude", "github.com x"},
		"host with quote":  {"/b", "claude", "github.com'"},
		"host with colon":  {"/b", "claude", "github.com:"},
		"host with a port": {"/b", "claude", "github.com:123456"},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := TransportEntries(in[0], in[1], in[2]); !errors.Is(err, ErrEntryInvalid) || got != nil {
				t.Errorf("TransportEntries = %v, %v", got, err)
			}
		})
	}
}
