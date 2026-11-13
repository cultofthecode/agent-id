package cliint

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestScrub(t *testing.T) {
	prefixes := []string{testenv.FakeTokenPrefix}
	tests := []struct {
		name, in, want string
	}{
		{"plain text", "the host is unavailable", "the host is unavailable"},
		{"empty", "", withheld},
		{"token prefix", "failed with " + testenv.FakeToken("x"), withheld},
		{"PEM marker", "-----BEGIN RSA PRIVATE KEY-----", withheld},
		{"JWT shape", "bad eyJhbGciOiJSUzI1NiJ9.payload", withheld},
		{"newline", "line one\nline two", withheld},
		{"carriage return", "a\rb", withheld},
		{"NUL", "a\x00b", withheld},
		{"escape", "a\x1b[31mb", withheld},
		{"too long", strings.Repeat("a", maxReason+1), withheld},
		{"at the limit", strings.Repeat("a", maxReason), strings.Repeat("a", maxReason)},
		{"not printable", "a\u200bb", withheld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Scrub(tt.in, prefixes); got != tt.want {
				t.Errorf("Scrub(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

var neutral = provider.NeutralCLI{
	ConfigDirVar: "FAKE_CONFIG_DIR",
	Decoys:       map[string]string{"FAKE_TOKEN": "decoy", "FAKE_ENTERPRISE_TOKEN": "decoy"},
	Binary:       "fakecli",
}

var drop = []string{"FAKE_TOKEN", "FAKE_ENTERPRISE_TOKEN", "FAKE_ALT_TOKEN"}

func TestBuildEnv(t *testing.T) {
	parent := []string{
		"PATH=/a:/b", "FAKE_TOKEN=personal", "FAKE_ALT_TOKEN=personal", "FAKE_ENTERPRISE_TOKEN=personal",
		"FAKE_CONFIG_DIR=/home/dev/.config/fake", "FAKE_HOST=ghe.example.com", "KEEP=1", "NOEQUALS", "=novalue", "AGENT_ID_CLI_ACTIVE=",
	}
	got, ok := BuildEnv(parent, neutral, drop, []string{"FAKE_TOKEN=bot-token", "FAKE_HOST=example.com"}, "/cache/cli-config")
	if !ok {
		t.Fatal("BuildEnv refused a valid environment")
	}
	want := []string{
		"PATH=/a:/b", "KEEP=1",
		"AGENT_ID_CLI_ACTIVE=1", "FAKE_CONFIG_DIR=/cache/cli-config", "FAKE_ENTERPRISE_TOKEN=decoy", "FAKE_HOST=example.com", "FAKE_TOKEN=bot-token",
	}
	if !slices.Equal(got, want) {
		t.Errorf("BuildEnv =\n%q\nwant\n%q", got, want)
	}
	for _, kv := range got {
		if strings.Contains(kv, "personal") {
			t.Errorf("the personal value survived in %q", kv)
		}
	}
}

func TestBuildEnvKeepsEachNameOnce(t *testing.T) {
	got, _ := BuildEnv([]string{"A=1", "A=2", "FAKE_TOKEN=x", "FAKE_TOKEN=y"}, neutral, drop, []string{"FAKE_TOKEN=t"}, "/c")
	seen := map[string]int{}
	for _, kv := range got {
		name, _, _ := strings.Cut(kv, "=")
		seen[name]++
	}
	if seen["FAKE_TOKEN"] != 1 {
		t.Errorf("FAKE_TOKEN appears %d times", seen["FAKE_TOKEN"])
	}
}

func TestBuildEnvRefusesHostileProviderEntries(t *testing.T) {
	for _, entry := range []string{
		"", "NOEQUALS", "=value", "lower=value", "FAKE_TOKEN=", "FAKE_TOKEN=a\nB=c", "FAKE_TOKEN=a\x00b",
		"AGENT_ID_CONTEXT=1", "AGENT_ID_CLI_ACTIVE=0", "1BAD=x", "BAD NAME=x", "BAD-NAME=x",
	} {
		if env, ok := BuildEnv([]string{"A=1"}, neutral, drop, []string{entry}, "/c"); ok || env != nil {
			t.Errorf("BuildEnv accepted the provider entry %q", entry)
		}
	}
}

func TestFindCLI(t *testing.T) {
	env := testenv.Isolate(t)
	root := env.TempDir
	mk := func(dir, body string, mode os.FileMode) string {
		t.Helper()
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "fakecli")
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	launcher := mk("bin", "launcher", 0o700)
	binDir := filepath.Dir(launcher)
	real1 := mk("real1", "real", 0o700)
	notExec := mk("noexec", "x", 0o600)
	_ = notExec
	if err := os.MkdirAll(filepath.Join(root, "dirnamed", "fakecli"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link", "fakecli")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(launcher, link); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(root, "hard", "fakecli")
	if err := os.MkdirAll(filepath.Dir(hard), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(launcher, hard); err != nil {
		t.Fatal(err)
	}
	j := func(dirs ...string) string { return strings.Join(dirs, ":") }
	d := func(n string) string { return filepath.Join(root, n) }
	tests := []struct {
		name string
		path string
		want string
		err  error
	}{
		{"skips the launcher directory", j(binDir, d("real1")), real1, nil},
		{"skips a trailing-slash spelling of it", j(binDir+"/", d("real1")), real1, nil},
		{"skips an unclean spelling of it", j(binDir+"/../bin", d("real1")), real1, nil},
		{"skips a file that is not executable", j(d("noexec"), d("real1")), real1, nil},
		{"skips a directory with the name", j(d("dirnamed"), d("real1")), real1, nil},
		{"skips a missing directory", j(d("missing"), d("real1")), real1, nil},
		{"skips a relative entry", j("real1", ".", "", d("real1")), real1, nil},
		{"skips a symlink to the launcher", j(d("link"), d("real1")), real1, nil},
		{"skips a hard link to the launcher", j(d("hard"), d("real1")), real1, nil},
		{"only the launcher", j(binDir), "", errNoCLI},
		{"only links to the launcher", j(d("link"), d("hard")), "", errLoop},
		{"empty PATH", "", "", errNoCLI},
		{"nothing", j(d("missing")), "", errNoCLI},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindCLI(tt.path, "fakecli", binDir)
			if got != tt.want || err != tt.err {
				t.Errorf("FindCLI = %q, %v; want %q, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

func TestCodeFor(t *testing.T) {
	for binary, want := range map[string]string{"gh": "AID-GH-TOKEN", "glab": "AID-GLAB-TOKEN", "": "AID-CLI-TOKEN", "../x": "AID-CLI-TOKEN", "GH": "AID-CLI-TOKEN", "a b": "AID-CLI-TOKEN"} {
		if got := codeFor(binary, kindToken); got != want {
			t.Errorf("codeFor(%q) = %q, want %q", binary, got, want)
		}
	}
}
