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

// Issue #112: Git sends a generic header to http://<host>:<port>/ although
// agent-id resets only https://<host>/. Real Git is the oracle: each case
// asks Git whether it sends the header, and the check must refuse whenever
// Git does.
func TestHeaderOnAnotherPortOrSchemeIsRefused(t *testing.T) {
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
	hostPort := strings.TrimPrefix(server.URL, "http://")
	host, _, _ := strings.Cut(hostPort, ":")
	a := providertest.NewFakeActivator()
	a.Hosts = []string{host}
	c := &remoteChecker{a: a}
	baseline := []string{"http.https://" + host + "/.extraheader", ""}
	cases := []struct {
		name string
		kv   []string
	}{
		{"generic header", []string{"http.extraheader", "X-L: 1"}},
		{"generic header and the baseline reset", append([]string{"http.extraheader", "X-L: 1"}, baseline...)},
		{"generic header and resets at the default http port", append([]string{"http.extraheader", "X-L: 1", "http.http://" + host + "/.extraheader", ""}, baseline...)},
		{"header scoped to the port", append([]string{"http.http://" + hostPort + "/.extraheader", "X-L: 1"}, baseline...)},
		{"header scoped to the http scheme", append([]string{"http.http://" + host + "/.extraheader", "X-L: 1"}, baseline...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			sent = nil
			mu.Unlock()
			args := []string{}
			for i := 0; i+1 < len(tc.kv); i += 2 {
				args = append(args, "-c", tc.kv[i]+"="+tc.kv[i+1])
			}
			args = append(args, "ls-remote", server.URL+"/org/r.git")
			_ = exec.Command("git", args...).Run()
			mu.Lock()
			got := slices.Contains(sent, "1")
			mu.Unlock()
			refused := c.checkHostHeaders(entries(tc.kv...)) != nil
			if got && !refused {
				t.Errorf("git sent the header to %s and the check passes", server.URL)
			}
			if !got && strings.Contains(tc.name, "resets at") {
				t.Errorf("case %q does not exercise a header that Git sends", tc.name)
			}
		})
	}
}

// Git applies a generic header to every URL, so no reset of a default port
// clears it. This table asks Git, through urlmatch, for ports and schemes
// that a plain HTTP listener cannot serve.
func TestGenericHeaderReachesEveryPortAndScheme(t *testing.T) {
	testenv.Isolate(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	cfg := t.TempDir() + "/gitconfig"
	for _, kv := range [][2]string{
		{"http.extraheader", "X-L: 1"},
		{"http.https://github.com/.extraheader", ""},
		{"http.http://github.com/.extraheader", ""},
	} {
		if err := exec.Command("git", "config", "--file", cfg, "--add", kv[0], kv[1]).Run(); err != nil {
			t.Fatal(err)
		}
	}
	a := providertest.NewFakeActivator()
	a.Hosts = []string{"github.com"}
	c := &remoteChecker{a: a}
	e := entries("http.extraheader", "X-L: 1", "http.https://github.com/.extraheader", "", "http.http://github.com/.extraheader", "")
	for _, target := range []string{"http://github.com:8080/", "https://github.com:8443/", "http://github.com:80443/", "https://github.com:80/", "http://github.com:1/x/y"} {
		out, _ := exec.Command("git", "config", "--file", cfg, "--get-urlmatch", "http.extraheader", target).Output()
		if strings.TrimSpace(string(out)) == "" {
			continue
		}
		if c.checkHostHeaders(e) == nil {
			t.Errorf("git sends a header to %s and the check passes", target)
		}
	}
	if len(HostRootHeaders(entries("http.extraheader", "X-L: 1", "http.https://github.com/.extraheader", ""), "github.com")) == 0 {
		t.Error("the https reset hides a generic header from the http scheme and from other ports")
	}
	if got := HostRootHeaders(nil, "github.com"); len(got) != 0 {
		t.Errorf("no header configured, got %v", got)
	}
}

func TestUnusedPortSkipsNamedPorts(t *testing.T) {
	e := entries("http.http://github.com:1/.extraheader", "X-L: 1", "http.https://github.com:2/.extraheader", "")
	if got := unusedPort(e); got != "3" {
		t.Errorf("unusedPort = %q, want 3", got)
	}
	if got := unusedPort(nil); got != "1" {
		t.Errorf("unusedPort(nil) = %q, want 1", got)
	}
}
