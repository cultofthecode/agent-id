package activation

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

func TestGenericHeaderReachesTheHTTPFormOfAHost(t *testing.T) {
	baseline := entries("http.https://github.com/.extraheader", "")
	generic := entries("http.extraheader", "X-L: 1")
	got := HostRootHeaders(append(slices.Clone(generic), baseline...), "github.com")
	if len(got) == 0 {
		t.Fatal("a generic header that only the https baseline resets passes (#107)")
	}
	for _, name := range got {
		if name != "X-L" {
			t.Errorf("header name = %q", name)
		}
	}
	withHTTPReset := append(append(slices.Clone(generic), baseline...), entries("http.http://github.com/.extraheader", "")...)
	if got := HostRootHeaders(withHTTPReset, "github.com"); len(got) != 0 {
		t.Errorf("a reset of the http host root leaves %q", got)
	}
	withLongReset := append(append(slices.Clone(generic), baseline...), entries("http.http://github.com//.extraheader", "")...)
	if got := HostRootHeaders(withLongReset, "github.com"); len(got) == 0 {
		t.Error("a reset with the path // clears the header below the http root")
	}
}

func TestHTTPFormIsClearedOnlyByAHostReset(t *testing.T) {
	a := providertest.NewFakeActivator()
	a.Hosts = []string{"github.com", "gist.github.com"}
	c := &remoteChecker{a: a}
	e := entries("http.extraheader", "X-L: 1", "http.https://github.com/.extraheader", "", "http.https://gist.github.com/.extraheader", "")
	if err := c.checkHostHeaders(e); err == nil {
		t.Fatal("generic header with only https resets passes")
	}
	e = append(e, entries("http.http://github.com/.extraheader", "")...)
	if err := c.checkHostHeaders(e); err == nil {
		t.Error("generic header still reaches http://gist.github.com/ and passes")
	}
	e = append(e, entries("http.http://gist.github.com/.extraheader", "")...)
	if err := c.checkHostHeaders(e); err != nil {
		t.Errorf("every host cleared on http and https, but refused: %v", err)
	}
}

func TestScopedHeaderOfTheHTTPFormIsRefused(t *testing.T) {
	a := providertest.NewFakeActivator()
	a.Hosts = []string{"github.com"}
	c := &remoteChecker{a: a}
	e := entries("http.http://github.com/org/.extraheader", "X-L: 1", "http.https://github.com/.extraheader", "")
	if err := c.checkHostHeaders(e); err == nil {
		t.Error("a header scoped to http://github.com/org/ passes")
	}
}

// TestRealGitSendsAGenericHeaderOverHTTP is the process test of #107. Real
// Git talks to a listener on 127.0.0.1. With the https baseline reset only,
// Git sends the generic header over http and the check refuses. With an
// http reset for the host, Git sends nothing and the check passes.
func TestRealGitSendsAGenericHeaderOverHTTP(t *testing.T) {
	testenv.Isolate(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	t.Setenv("GIT_ALLOW_PROTOCOL", "file:http:https")
	var mu sync.Mutex
	var sent []string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, r.Header.Get("X-L"))
		mu.Unlock()
		http.NotFound(w, r)
	}))
	_ = server.Listener.Close()
	server.Listener = testenv.Listen(t)
	server.Start()
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	a := providertest.NewFakeActivator()
	a.Hosts = []string{host}
	c := &remoteChecker{a: a}

	generic := entries("http.extraheader", "X-L: 1", "http.https://"+host+"/.extraheader", "")
	cases := []struct {
		name    string
		entries []ConfigEntry
		sends   bool
	}{
		{"https reset only", generic, true},
		{"http reset too", append(slices.Clone(generic), entries("http."+server.URL+"/.extraheader", "")...), false},
		{"http reset with a longer path", append(slices.Clone(generic), entries("http."+server.URL+"//.extraheader", "")...), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/r.git", "/org/sub/r.git"} {
				mu.Lock()
				sent = nil
				mu.Unlock()
				args := []string{}
				for _, e := range tc.entries {
					args = append(args, "-c", e.Key+"="+e.Value)
				}
				_ = exec.Command("git", append(args, "ls-remote", server.URL+path)...).Run()
				mu.Lock()
				got := slices.Contains(sent, "1")
				mu.Unlock()
				if got != tc.sends {
					t.Errorf("%s: git sent the header = %v, want %v", path, got, tc.sends)
				}
			}
			if refused := c.checkHostHeaders(tc.entries) != nil; refused != tc.sends {
				t.Errorf("check refuses = %v, git sends = %v", refused, tc.sends)
			}
		})
	}
}
