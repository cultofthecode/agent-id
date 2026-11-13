package testenv

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// realTokenPrefixes open the GitHub credentials that a test must never see:
// personal, OAuth, user-to-server, installation, and refresh tokens, and
// fine-grained personal access tokens.
var realTokenPrefixes = []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"}

// proxyVariables are the proxy settings that git (curl), gh, and other Go
// programs read. Isolate points all of them at the network guard.
var proxyVariables = []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"}

// noProxyVariables list the hosts that bypass the proxy. Isolate empties them
// so that the guard sees loopback requests of proxy-aware clients.
var noProxyVariables = []string{"NO_PROXY", "no_proxy"}

// errNetworkBlocked is returned by the client of NewHTTPClient for a request
// that would leave the fakes on 127.0.0.1.
var errNetworkBlocked = errors.New("testenv: network access is blocked in tests")

// LooksLikeRealToken reports whether token has the shape of a GitHub
// credential and was not issued by a fake.
func LooksLikeRealToken(token string) bool {
	return realTokenPrefix(token) != ""
}

// CheckFakeToken reports whether token was issued by a fake. When token looks
// like a real GitHub credential, it also fails the test; source names the fake
// that received it. The message never contains the token.
func CheckFakeToken(t testing.TB, source, token string) bool {
	if prefix := realTokenPrefix(token); prefix != "" {
		t.Errorf("testenv: %s received a real-looking GitHub token (prefix %q); tests use testenv.FakeToken", source, prefix)
		return false
	}
	return IsFakeToken(token)
}

// realTokenPrefix returns the GitHub credential prefix of token, or "" when
// token is a fake token or has no such prefix.
func realTokenPrefix(token string) string {
	if IsFakeToken(token) {
		return ""
	}
	for _, prefix := range realTokenPrefixes {
		if strings.HasPrefix(token, prefix) {
			return prefix
		}
	}
	return ""
}

// NewHTTPClient returns an HTTP client for code under test that reaches only
// 127.0.0.1 with a port in FirstPort–LastPort. A request to any other
// address, such as api.github.com, fails the test and returns an error
// without leaving the machine.
func NewHTTPClient(t testing.TB) *http.Client {
	t.Helper()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: guardTransport{t: t, next: transport}}
}

// guardTransport passes requests to the fakes on to next and blocks every
// other request.
type guardTransport struct {
	t    testing.TB
	next http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (g guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !isFakeAddress(r.URL.Host) {
		g.t.Errorf("testenv: blocked an HTTP request to %s; tests talk only to fakes on 127.0.0.1:%d–%d", r.URL.Host, FirstPort, LastPort)
		return nil, fmt.Errorf("%w: %s", errNetworkBlocked, r.URL.Host)
	}
	return g.next.RoundTrip(r)
}

// isFakeAddress reports whether hostport is 127.0.0.1 with a port in
// FirstPort–LastPort, where the fakes listen.
func isFakeAddress(hostport string) bool {
	host, portText, err := net.SplitHostPort(hostport)
	if err != nil || host != "127.0.0.1" {
		return false
	}
	port, err := strconv.Atoi(portText)
	return err == nil && port >= FirstPort && port <= LastPort
}

// guardNetwork starts a proxy and points the proxy variables of the process at
// it, so that git, gh, and other child processes cannot reach the network. The
// proxy forwards only to 127.0.0.1 with a port in FirstPort–LastPort and fails
// the test on every other request. NO_PROXY is emptied so that a proxy-aware
// client such as git (curl) sends loopback requests through the guard too.
// Go programs such as gh never proxy loopback addresses; their loopback
// traffic is the residual limit this guard cannot confine.
func guardNetwork(t testing.TB) {
	t.Helper()
	server := httptest.NewUnstartedServer(guardHandler(t))
	_ = server.Listener.Close()
	server.Listener = Listen(t)
	server.Start()
	t.Cleanup(server.Close)
	for _, name := range proxyVariables {
		t.Setenv(name, server.URL)
	}
	for _, name := range noProxyVariables {
		t.Setenv(name, "")
	}
}

// guardHandler returns the proxy handler: it tunnels and forwards requests for
// the fakes and fails the test for every other address.
func guardHandler(t testing.TB) http.Handler {
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.Host
		if r.Method == http.MethodConnect {
			target = r.RequestURI
		}
		if !isFakeAddress(target) {
			t.Errorf("testenv: blocked a network request to %s; tests talk only to fakes on 127.0.0.1:%d–%d", target, FirstPort, LastPort)
			http.Error(w, "blocked by the agent-id test environment", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodConnect {
			tunnel(w, target)
			return
		}
		forward(w, r, transport)
	})
}

// tunnel connects the client of a CONNECT request to target.
func tunnel(w http.ResponseWriter, target string) {
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	go pipe(upstream, client)
	pipe(client, upstream)
}

// pipe copies src to dst and closes both when src ends.
func pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

// forward sends a plain HTTP proxy request to its target and relays the answer.
func forward(w http.ResponseWriter, r *http.Request, transport http.RoundTripper) {
	out := r.Clone(r.Context())
	out.RequestURI = ""
	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	for name, values := range resp.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
