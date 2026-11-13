package github

import (
	"errors"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

var hostileToken = testenv.FakeToken("SHIM")

var testCLI = ghContext{profile: "reviewer", bot: "reviewer-app[bot]"}

func refusalCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		return ""
	}
	var r *provider.CLIRefusal
	if !errors.As(err, &r) {
		t.Fatalf("error %v is no *provider.CLIRefusal", err)
	}
	return r.Code
}

func TestCheckGH(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		parent []string
		want   string
	}{
		{"no arguments", nil, nil, ""},
		{"version", []string{"--version"}, nil, ""},
		{"help flag", []string{"--help"}, nil, ""},
		{"help topic", []string{"help", "auth"}, nil, ""},
		{"pr create", []string{"pr", "create", "--title", "x", "--body", "y"}, nil, ""},
		{"pr view with json", []string{"pr", "view", "1", "--json", "number,title"}, nil, ""},
		{"issue list", []string{"issue", "list", "--label", "bug"}, nil, ""},
		{"run watch", []string{"run", "watch", "123"}, nil, ""},
		{"api repo endpoint", []string{"api", "repos/o/r/pulls", "-f", "title=x"}, nil, ""},
		{"api users endpoint", []string{"api", "users/octocat"}, nil, ""},
		{"api graphql", []string{"api", "graphql", "-f", "query={viewer{login}}"}, nil, ""},
		{"api field holds a URL", []string{"api", "repos/o/r/issues", "-f", "body=see https://ghe.example.com/x"}, nil, ""},
		{"api field holds a URL as the whole value", []string{"api", "repos/o/r/issues", "-f", "body=https://ghe.example.com/x"}, nil, ""},
		{"body file path with dots", []string{"pr", "create", "--body-file", "./docs/x.md"}, nil, ""},
		{"repo view current", []string{"repo", "view", "--json", "nameWithOwner"}, nil, ""},
		{"repo view with owner", []string{"repo", "view", "o/r"}, nil, ""},
		{"repo clone with owner", []string{"repo", "clone", "o/r"}, nil, ""},
		{"repo list with owner", []string{"repo", "list", "someorg", "--limit", "5"}, nil, ""},
		{"repo on the profile host", []string{"pr", "list", "-R", "github.com/o/r"}, nil, ""},
		{"repo on the profile host, upper case", []string{"pr", "list", "-R", "GitHub.COM/o/r"}, nil, ""},
		{"scp form on the profile host", []string{"pr", "list", "-R", "git@github.com:o/r"}, nil, ""},
		{"combined short flags on the profile host", []string{"pr", "list", "-wR", "o/r"}, nil, ""},
		{"api path with a percent-encoded repo", []string{"api", "repos/o/r%2Fx"}, nil, ""},
		{"repo with owner only", []string{"pr", "list", "--repo=o/r"}, nil, ""},
		{"GH_REPO without host", []string{"pr", "list"}, []string{"GH_REPO=o/r"}, ""},
		{"api URL on the API host", []string{"api", "https://api.github.com/repos/o/r"}, nil, ""},
		{"uploads host", []string{"api", "--hostname", "uploads.github.com", "repos/o/r"}, nil, ""},

		{"alias set then run", []string{"alias", "set", "t", "auth token"}, nil, "AID-GH-USER"},
		{"alias alone", []string{"alias"}, nil, "AID-GH-USER"},
		{"config set", []string{"config", "set", "pager", "x"}, nil, "AID-GH-USER"},
		{"repo new", []string{"repo", "new", "x"}, nil, "AID-GH-USER"},
		{"repo ls", []string{"repo", "ls"}, nil, "AID-GH-USER"},
		{"agent alias", []string{"agent", "list"}, nil, "AID-GH-USER"},
		{"agents alias", []string{"agents"}, nil, "AID-GH-USER"},
		{"agent-tasks alias", []string{"agent-tasks"}, nil, "AID-GH-USER"},
		{"gists alias", []string{"gists"}, nil, "AID-GH-USER"},
		{"api percent-encoded user", []string{"api", "%75ser"}, nil, "AID-GH-USER"},
		{"api double-encoded user", []string{"api", "%2575ser"}, nil, "AID-GH-USER"},
		{"api encoded dot segments", []string{"api", "repos/%2e%2e/user"}, nil, "AID-GH-USER"},
		{"scp form in -R", []string{"pr", "list", "-R", "git@ghe.example.com:o/r"}, nil, "AID-GH-HOST"},
		{"scp form in --repo=", []string{"pr", "list", "--repo=git@ghe.example.com:o/r"}, nil, "AID-GH-HOST"},
		{"scp form in GH_REPO", []string{"pr", "list"}, []string{"GH_REPO=git@ghe.example.com:o/r"}, "AID-GH-HOST"},
		{"scp form in repo view", []string{"repo", "view", "git@ghe.example.com:o/r"}, nil, "AID-GH-HOST"},
		{"scp form in repo clone", []string{"repo", "clone", "git@acme.ghe.com:o/r"}, nil, "AID-GH-HOST"},
		{"scp form on github.localhost", []string{"repo", "clone", "git@github.localhost:o/r"}, nil, "AID-GH-HOST"},
		{"combined short flags with attached value", []string{"pr", "list", "-wRghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"combined short flags with next value", []string{"pr", "list", "-wR", "ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"combined short flags with scp value", []string{"pr", "list", "-wRgit@ghe.example.com:o/r"}, nil, "AID-GH-HOST"},
		{"underscore host as positional", []string{"repo", "clone", "x_y.github.localhost/o/r"}, nil, "AID-GH-HOST"},
		{"auth login", []string{"auth", "login"}, nil, "AID-GH-AUTH"},
		{"auth status", []string{"auth", "status"}, nil, "AID-GH-AUTH"},
		{"auth token", []string{"auth", "token"}, nil, "AID-GH-AUTH"},
		{"auth switch", []string{"auth", "switch"}, nil, "AID-GH-AUTH"},
		{"auth logout", []string{"auth", "logout", "--hostname", "github.com"}, nil, "AID-GH-AUTH"},
		{"auth refresh", []string{"auth", "refresh"}, nil, "AID-GH-AUTH"},
		{"auth setup-git", []string{"auth", "setup-git"}, nil, "AID-GH-AUTH"},
		{"auth git-credential", []string{"auth", "git-credential", "get"}, nil, "AID-GH-AUTH"},
		{"auth alone", []string{"auth"}, nil, "AID-GH-AUTH"},
		{"auth with another host", []string{"auth", "login", "-h", "ghe.example.com"}, nil, "AID-GH-AUTH"},

		{"status", []string{"status"}, nil, "AID-GH-USER"},
		{"gist create", []string{"gist", "create", "f"}, nil, "AID-GH-USER"},
		{"org list", []string{"org", "list"}, nil, "AID-GH-USER"},
		{"ssh-key", []string{"ssh-key", "list"}, nil, "AID-GH-USER"},
		{"gpg-key", []string{"gpg-key", "add"}, nil, "AID-GH-USER"},
		{"codespace", []string{"codespace", "list"}, nil, "AID-GH-USER"},
		{"codespace alias", []string{"cs", "list"}, nil, "AID-GH-USER"},
		{"agent-task", []string{"agent-task", "list"}, nil, "AID-GH-USER"},
		{"extension install", []string{"extension", "install", "o/gh-x"}, nil, "AID-GH-USER"},
		{"extension alias", []string{"ext", "install", "o/gh-x"}, nil, "AID-GH-USER"},
		{"extensions alias", []string{"extensions", "list"}, nil, "AID-GH-USER"},
		{"repo create", []string{"repo", "create", "x"}, nil, "AID-GH-USER"},
		{"repo fork", []string{"repo", "fork", "o/r"}, nil, "AID-GH-USER"},
		{"repo list without owner", []string{"repo", "list"}, nil, "AID-GH-USER"},
		{"repo list with a flag only", []string{"repo", "list", "--limit", "5"}, nil, "AID-GH-USER"},
		{"unknown option first", []string{"--repo", "o/r", "pr", "list"}, nil, "AID-GH-USER"},
		{"api user", []string{"api", "user"}, nil, "AID-GH-USER"},
		{"api /user", []string{"api", "/user"}, nil, "AID-GH-USER"},
		{"api user subpath", []string{"api", "user/orgs"}, nil, "AID-GH-USER"},
		{"api user with query", []string{"api", "user?x=1"}, nil, "AID-GH-USER"},
		{"api user upper case", []string{"api", "USER"}, nil, "AID-GH-USER"},
		{"api user with double slash", []string{"api", "//user"}, nil, "AID-GH-USER"},
		{"api user through a dot segment", []string{"api", "repos/../user"}, nil, "AID-GH-USER"},
		{"api user after flags", []string{"api", "-X", "GET", "-H", "Accept: x", "user"}, nil, "AID-GH-USER"},
		{"api user by URL", []string{"api", "https://api.github.com/user"}, nil, "AID-GH-USER"},
		{"api app", []string{"api", "app"}, nil, "AID-GH-USER"},
		{"api app installations", []string{"api", "/app/installations"}, nil, "AID-GH-USER"},

		{"assignee @me", []string{"pr", "create", "--assignee", "@me"}, nil, "AID-GH-ME"},
		{"assignee=@me", []string{"issue", "create", "--assignee=@me"}, nil, "AID-GH-ME"},
		{"assignee list with @me", []string{"issue", "edit", "1", "--add-assignee", "bob,@me"}, nil, "AID-GH-ME"},
		{"short flag attached to @me", []string{"issue", "list", "-a@me"}, nil, "AID-GH-ME"},
		{"@me in upper case", []string{"pr", "list", "--author", "@ME"}, nil, "AID-GH-ME"},
		{"repo view without owner", []string{"repo", "view", "name"}, nil, "AID-GH-ME"},
		{"repo clone without owner", []string{"repo", "clone", "name"}, nil, "AID-GH-ME"},
		{"repo clone without owner after a flag value", []string{"repo", "clone", "--branch", "main", "name"}, nil, "AID-GH-ME"},

		{"hostname flag", []string{"api", "--hostname", "ghe.example.com", "repos/o/r"}, nil, "AID-GH-HOST"},
		{"hostname equals", []string{"api", "--hostname=ghe.example.com", "repos/o/r"}, nil, "AID-GH-HOST"},
		{"hostname with port", []string{"api", "--hostname", "github.com:8443", "x"}, nil, "AID-GH-HOST"},
		{"hostname trailing dot", []string{"api", "--hostname", "github.com.", "x"}, nil, "AID-GH-HOST"},
		{"hostname look-alike", []string{"api", "--hostname", "github.com.evil.test", "x"}, nil, "AID-GH-HOST"},
		{"hostname tenancy", []string{"api", "--hostname", "acme.ghe.com", "x"}, nil, "AID-GH-HOST"},
		{"hostname other subdomain", []string{"api", "--hostname", "gist.github.com", "x"}, nil, "AID-GH-HOST"},
		{"hostname unicode look-alike", []string{"api", "--hostname", "gıthub.com", "x"}, nil, "AID-GH-HOST"},
		{"short repo flag with host", []string{"pr", "list", "-R", "ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"attached short repo flag", []string{"pr", "list", "-Rghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"repo equals with host", []string{"pr", "list", "--repo=ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"repo flag with URL", []string{"pr", "list", "-R", "https://ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"GH_REPO with host", []string{"pr", "list"}, []string{"GH_REPO=ghe.example.com/o/r"}, "AID-GH-HOST"},
		{"GH_REPO with URL", []string{"pr", "list"}, []string{"GH_REPO=https://ghe.example.com/o/r"}, "AID-GH-HOST"},
		{"pr view URL on another host", []string{"pr", "view", "https://ghe.example.com/o/r/pull/1"}, nil, "AID-GH-HOST"},
		{"URL with user-info", []string{"pr", "view", "https://user:pw@github.com/o/r/pull/1"}, nil, "AID-GH-HOST"},
		{"URL with a port", []string{"pr", "view", "https://github.com:444/o/r/pull/1"}, nil, "AID-GH-HOST"},
		{"URL that does not parse", []string{"pr", "view", "https://[::1/o/r/pull/1"}, nil, "AID-GH-HOST"},
		{"scheme-relative URL", []string{"pr", "view", "//ghe.example.com/o/r/pull/1"}, nil, "AID-GH-HOST"},
		{"api URL on another host", []string{"api", "https://ghe.example.com/api/v3/repos/o/r"}, nil, "AID-GH-HOST"},
		{"repo clone with host", []string{"repo", "clone", "ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
		{"host in the second position", []string{"pr", "list", "--state", "open", "-R", "ghe.example.com/o/r"}, nil, "AID-GH-HOST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := refusalCode(t, checkGH(testCLI, tt.args, tt.parent)); got != tt.want {
				t.Errorf("checkGH(%q) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestCheckGHMessagesHoldNoArgumentText(t *testing.T) {
	hostile := []struct {
		args   []string
		parent []string
	}{
		{[]string{"pr", "view", "https://user:" + hostileToken + "@ghe.example.com/o/r/pull/1"}, nil},
		{[]string{"api", "--hostname", hostileToken + ".example.com", "x"}, nil},
		{[]string{"api", "--hostname", "evil\nhost.example.com", "x"}, nil},
		{[]string{"pr", "list"}, []string{"GH_REPO=" + hostileToken + "/o/r"}},
		{[]string{hostileToken}, nil},
		{[]string{"auth", hostileToken}, nil},
		{[]string{"repo", hostileToken}, nil},
		{[]string{"api", "user", "-H", "Authorization: token " + hostileToken}, nil},
		{[]string{"issue", "list", "--assignee", "@me", "-H", hostileToken}, nil},
	}
	for _, h := range hostile {
		err := checkGH(testCLI, h.args, h.parent)
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), hostileToken) || strings.Contains(err.Error(), "pw") || strings.ContainsAny(err.Error(), "\n\r\x00") {
			t.Errorf("checkGH(%q) message holds argument text: %q", h.args, err.Error())
		}
		if !strings.Contains(err.Error(), "reviewer") {
			t.Errorf("message %q does not name the profile", err.Error())
		}
	}
}

func TestCheckGHRefusalsAreOnOneLine(t *testing.T) {
	for _, args := range [][]string{{"auth", "login"}, {"status"}, {"api", "user"}, {"pr", "list", "--assignee", "@me"}, {"api", "--hostname", "x.example.com", "y"}} {
		err := checkGH(testCLI, args, nil)
		if err == nil {
			t.Fatalf("checkGH(%q) allowed", args)
		}
		if strings.Contains(err.Error(), "\n") || !strings.HasPrefix(err.Error(), "AID-GH-") {
			t.Errorf("message = %q, want one line starting with its code", err.Error())
		}
	}
}

func TestActivatorCheckCLINamesTheBot(t *testing.T) {
	a := newActivator(t, resolvedBlock)
	err := a.CheckCLI([]string{"auth", "status"}, nil)
	if err == nil || !strings.Contains(err.Error(), "reviewer") || !strings.Contains(err.Error(), "[bot]") {
		t.Errorf("CheckCLI = %v, want a refusal that names the profile and the bot", err)
	}
	if err := a.CheckCLI([]string{"pr", "list"}, nil); err != nil {
		t.Errorf("CheckCLI(pr list) = %v, want nil", err)
	}
}
