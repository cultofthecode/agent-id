package testenv

import (
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCheckFakeToken(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		wantFake   bool
		wantReport string
	}{
		{"fake token", FakeToken("1"), true, ""},
		{"installation token", "ghs_realLookingToken", false, `prefix "ghs_"`},
		{"personal token", "ghp_realLookingToken", false, `prefix "ghp_"`},
		{"OAuth token", "gho_realLookingToken", false, `prefix "gho_"`},
		{"fine-grained token", "github_pat_realLookingToken", false, `prefix "github_pat_"`},
		{"fake prefix later in the token", "ghs_x" + FakeTokenPrefix, false, `prefix "ghs_"`},
		{"empty", "", false, ""},
		{"not a GitHub token", "password", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewRecorder(t)
			if got := CheckFakeToken(rec, "the fake", tt.token); got != tt.wantFake {
				t.Errorf("CheckFakeToken(%q) = %v, want %v", tt.name, got, tt.wantFake)
			}
			errs := rec.Errors()
			if tt.wantReport == "" {
				if len(errs) != 0 {
					t.Errorf("reported %q, want no error", errs)
				}
				return
			}
			if !rec.Reported(tt.wantReport) {
				t.Errorf("reported %q, want an error containing %q", errs, tt.wantReport)
			}
			for _, e := range errs {
				if strings.Contains(e, tt.token) {
					t.Errorf("the report %q contains the token", e)
				}
			}
		})
	}
}

func TestNewHTTPClientReachesTheFakes(t *testing.T) {
	Isolate(t)
	fake := NewFakeGitHub(t)
	fake.Handle("GET /app", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	})
	resp, err := NewHTTPClient(t).Get(fake.URL + "/app")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /app = %d, want 200", resp.StatusCode)
	}
}

func TestNewHTTPClientBlocksEverythingElse(t *testing.T) {
	tests := []struct {
		name string
		url  string
		host string
	}{
		{"GitHub API", "https://api.github.com/app", "api.github.com"},
		{"GitHub", "https://github.com/cultofthecode/agent-id.git/info/refs", "github.com"},
		{"localhost by name", "http://localhost:18000/", "localhost:18000"},
		{"port of orbit", "http://127.0.0.1:19000/", "127.0.0.1:19000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewRecorder(t)
			_, err := NewHTTPClient(rec).Get(tt.url)
			if !errors.Is(err, errNetworkBlocked) {
				t.Errorf("GET %s: error %v, want errNetworkBlocked", tt.url, err)
			}
			if !rec.Reported("blocked an HTTP request to " + tt.host) {
				t.Errorf("reported %q, want the blocked request to %s", rec.Errors(), tt.host)
			}
		})
	}
}

func TestIsolateBlocksGitFromReachingTheNetwork(t *testing.T) {
	git := lookPath(t, "git")
	rec := NewRecorder(t)
	Isolate(rec)
	out, err := exec.Command(git, "ls-remote", "https://github.com/cultofthecode/agent-id.git").CombinedOutput()
	if err == nil {
		t.Fatalf("git ls-remote reached GitHub: %s", out)
	}
	if !rec.Reported("blocked a network request to github.com:443") {
		t.Errorf("reported %q, want the blocked request to github.com:443 (git: %s)", rec.Errors(), out)
	}
}

func TestIsolateBlocksGHFromReachingTheNetwork(t *testing.T) {
	gh := lookPath(t, "gh")
	rec := NewRecorder(t)
	Isolate(rec)
	t.Setenv("GH_TOKEN", FakeToken("gh"))
	out, err := exec.Command(gh, "api", "/app").CombinedOutput()
	if err == nil {
		t.Fatalf("gh api reached GitHub: %s", out)
	}
	if !rec.Reported("blocked a network request to api.github.com:443") {
		t.Errorf("reported %q, want the blocked request to api.github.com:443 (gh: %s)", rec.Errors(), out)
	}
}

func TestIsolateSetsTheProxyGuard(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")
	t.Setenv("NO_PROXY", "*")
	Isolate(t)
	for _, name := range proxyVariables {
		if got := os.Getenv(name); !strings.HasPrefix(got, "http://127.0.0.1:") {
			t.Errorf("%s = %q, want the guard on 127.0.0.1", name, got)
		}
	}
	for _, name := range noProxyVariables {
		if got := os.Getenv(name); got != "127.0.0.1,localhost" {
			t.Errorf("%s = %q, want 127.0.0.1,localhost", name, got)
		}
	}
}

// lookPath returns the path of the executable name, and skips the test when
// it is not installed.
func lookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not on PATH", name)
	}
	return path
}
