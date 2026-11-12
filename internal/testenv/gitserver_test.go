package testenv

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewBareRepositoryAndCloneStayInTheTestRoot(t *testing.T) {
	env := Isolate(t)
	bare := NewBareRepository(t, env, "widgets")
	clone := Clone(t, env, bare)
	for _, dir := range []string{bare, clone} {
		if !strings.HasPrefix(dir, env.Root+string(filepath.Separator)) {
			t.Errorf("%s is not under the test root %s", dir, env.Root)
		}
	}
	if !strings.HasPrefix(clone, env.Home+string(filepath.Separator)) {
		t.Errorf("clone %s is not under the isolated HOME %s", clone, env.Home)
	}
	if got := Git(t, clone, "log", "--format=%s"); got != "Initial commit" {
		t.Errorf("log of the clone = %q, want the initial commit", got)
	}
	if got := Git(t, clone, "branch", "--show-current"); got != "main" {
		t.Errorf("branch of the clone = %q, want main", got)
	}
}

func TestValidRepositoryName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"widgets", true},
		{"agent-id", true},
		{"", false},
		{"../escape", false},
		{"a/b", false},
		{".hidden", false},
		{"..", false},
	}
	for _, tt := range tests {
		if got := validRepositoryName(tt.name); got != tt.want {
			t.Errorf("validRepositoryName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPushOverHTTPSRecordsTheFakeToken(t *testing.T) {
	env := Isolate(t)
	server := NewGitServer(t, env)
	bare := NewBareRepository(t, env, "widgets")
	token := FakeToken("push")
	useCredentials(t, "x-access-token", token)
	clone := Clone(t, env, server.RepositoryURL("widgets"))
	if err := os.WriteFile(filepath.Join(clone, "change.txt"), []byte("change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Git(t, clone, "add", "change.txt")
	Git(t, clone, append(seedIdentity, "commit", "--quiet", "-m", "Pushed by the test")...)
	Git(t, clone, "push", "--quiet", "origin", "main")

	pushes := server.Pushes()
	if len(pushes) == 0 {
		t.Fatalf("the server recorded no push; requests: %d", len(server.Requests()))
	}
	for _, push := range pushes {
		if push.Username != "x-access-token" || push.Password != token {
			t.Errorf("push %s %s used username %q and a password other than the fake token", push.Method, push.Path, push.Username)
		}
	}
	if got := Git(t, bare, "log", "-1", "--format=%s", "main"); got != "Pushed by the test" {
		t.Errorf("last commit of the bare repository = %q, want the pushed commit", got)
	}
}

func TestGitServerRejectsARealLookingToken(t *testing.T) {
	rec := NewRecorder(t)
	env := Isolate(t)
	server := NewGitServer(rec, env)
	NewBareRepository(t, env, "widgets")
	token := "ghs_" + "realLookingToken"
	useCredentials(t, "x-access-token", token)
	out, err := exec.Command("git", "ls-remote", server.RepositoryURL("widgets")).CombinedOutput()
	if err == nil {
		t.Fatalf("git ls-remote succeeded with a real-looking token: %s", out)
	}
	if !rec.Reported("the fake Git server received a real-looking GitHub token") {
		t.Errorf("reported %q, want the real-looking token", rec.Errors())
	}
	for _, e := range rec.Errors() {
		if strings.Contains(e, token) {
			t.Errorf("the report %q contains the token", e)
		}
	}
}

func TestGitServerChallengesARequestWithoutCredentials(t *testing.T) {
	env := Isolate(t)
	server := NewGitServer(t, env)
	NewBareRepository(t, env, "widgets")
	out, err := exec.Command("git", "ls-remote", server.RepositoryURL("widgets")).CombinedOutput()
	if err == nil {
		t.Fatalf("git ls-remote succeeded without credentials: %s", out)
	}
	requests := server.Requests()
	if len(requests) == 0 || requests[0].Username != "" || requests[0].Service != "git-upload-pack" {
		t.Errorf("requests = %+v, want an unauthenticated git-upload-pack request", requests)
	}
}

// useCredentials gives git in this test a credential helper, configured
// through GIT_CONFIG_COUNT like aidentity does, that answers with username
// and password.
func useCredentials(t *testing.T, username, password string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "!f() { test \"$1\" = get && printf 'username=%s\\npassword=%s\\n' '"+username+"' '"+password+"'; }; f")
}
