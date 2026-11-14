package testenv

import (
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestIsolatePointsEverythingIntoTheTestRoot(t *testing.T) {
	base := filepath.Join(os.TempDir(), RootDirName)
	t.Setenv("GH_TOKEN", "ghp_must_not_leak")
	t.Setenv("GITHUB_TOKEN", "ghp_must_not_leak")
	t.Setenv("AGENT_ID_PROFILE", "claude")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.name")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "eyJhbGciOiJSUzI1NiJ9.eyJmYWtlIjoxfQ.ZmFrZQ")
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "fake-runtime")
	env := Isolate(t)
	if !strings.HasPrefix(env.Root, base+string(filepath.Separator)) {
		t.Fatalf("root %s is not under %s", env.Root, base)
	}
	for _, name := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "GIT_CONFIG_GLOBAL", "GH_CONFIG_DIR", "TMPDIR"} {
		if got := os.Getenv(name); !strings.HasPrefix(got, env.Root) {
			t.Errorf("%s = %q, want a path under %s", name, got, env.Root)
		}
	}
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "AGENT_ID_PROFILE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "SSH_AUTH_SOCK", "ACTIONS_ID_TOKEN_REQUEST_TOKEN", "ACTIONS_RUNTIME_TOKEN"} {
		if value, ok := os.LookupEnv(name); ok {
			t.Errorf("%s is still set to %q", name, value)
		}
	}
	if got := os.Getenv("GIT_CONFIG_NOSYSTEM"); got != "1" {
		t.Errorf("GIT_CONFIG_NOSYSTEM = %q, want 1", got)
	}
}

func TestIsolatedGitWritesOnlyTheTemporaryGlobalConfig(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not on PATH")
	}
	env := Isolate(t)
	out, err := exec.Command(git, "config", "--global", "user.name", "isolated-test").CombinedOutput()
	if err != nil {
		t.Fatalf("git config --global: %v: %s", err, out)
	}
	data, err := os.ReadFile(env.GitConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "isolated-test") {
		t.Fatalf("temporary global config %s = %q, want the written value", env.GitConfig, data)
	}
	out, err = exec.Command(git, "config", "--show-origin", "--global", "user.name").CombinedOutput()
	if err != nil {
		t.Fatalf("git config --show-origin: %v: %s", err, out)
	}
	if !strings.Contains(string(out), env.Root) {
		t.Fatalf("git reads the global config from %q, want a file under %s", out, env.Root)
	}
}

func TestIsolateRemovesTheEnvironmentAtCleanup(t *testing.T) {
	var root string
	t.Run("isolated", func(t *testing.T) {
		root = Isolate(t).Root
	})
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("root %s still exists after the test: %v", root, err)
	}
}

func TestListenUsesTheAgentIDPortRange(t *testing.T) {
	l := Listen(t)
	_, portText, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if port < FirstPort || port > LastPort {
		t.Fatalf("port %d is outside %d–%d", port, FirstPort, LastPort)
	}
}

func TestFakeAPI(t *testing.T) {
	Isolate(t)
	fake := NewFakeAPI(t)
	fake.Handle("GET /app", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"slug":"claude-dev"}`)
	})
	tests := []struct {
		path       string
		wantStatus int
		wantBody   string
	}{
		{"/app", http.StatusOK, `{"slug":"claude-dev"}`},
		{"/unregistered", http.StatusNotFound, `{"message":"Not Found"}`},
	}
	for _, tt := range tests {
		resp, err := http.Get(fake.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != tt.wantStatus || string(body) != tt.wantBody {
			t.Errorf("GET %s = %d %q, want %d %q", tt.path, resp.StatusCode, body, tt.wantStatus, tt.wantBody)
		}
	}
	if got, want := strings.Join(fake.Requests(), ","), "GET /app,GET /unregistered"; got != want {
		t.Errorf("requests = %q, want %q", got, want)
	}
}

func TestFakeTokens(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{FakeToken("1"), true},
		{"ghs_realLookingToken", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsFakeToken(tt.token); got != tt.want {
			t.Errorf("IsFakeToken(%q) = %v, want %v", tt.token, got, tt.want)
		}
	}
}
