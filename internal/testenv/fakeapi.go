package testenv

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// Ports that agent-id tests may listen on. The range 19000–19999 belongs to
// orbit; other ports belong to other projects on the same host.
const (
	FirstPort = 18000
	LastPort  = 18999
)

// FakeTokenPrefix opens every token that a fake issues, so that a fixture can
// never be mistaken for a real GitHub credential.
const FakeTokenPrefix = "ghs_aidtest_"

// FakeToken returns the fake installation token with the given suffix.
func FakeToken(suffix string) string {
	return FakeTokenPrefix + suffix
}

// IsFakeToken reports whether token was issued by a fake.
func IsFakeToken(token string) bool {
	return strings.HasPrefix(token, FakeTokenPrefix)
}

// FakeAPI is a local HTTP stand-in for the API of a code host. It knows no
// provider: the fakes of a provider (internal/provider/github/githubtest)
// register their routes on it. Every other request answers 404 with a JSON
// message.
type FakeAPI struct {
	URL string

	mux      *http.ServeMux
	mu       sync.Mutex
	requests []*http.Request
}

// NewFakeAPI starts a fake API on 127.0.0.1 with a port in
// FirstPort–LastPort and stops it when the test ends.
func NewFakeAPI(t testing.TB) *FakeAPI {
	t.Helper()
	fake := &FakeAPI{mux: http.NewServeMux()}
	server := httptest.NewUnstartedServer(http.HandlerFunc(fake.serve))
	_ = server.Listener.Close()
	server.Listener = Listen(t)
	server.Start()
	t.Cleanup(server.Close)
	fake.URL = server.URL
	return fake
}

// Handle registers handler for pattern, in the syntax of http.ServeMux.
func (f *FakeAPI) Handle(pattern string, handler http.HandlerFunc) {
	f.mux.HandleFunc(pattern, handler)
}

// Requests returns the method and path of every request the fake received,
// in order.
func (f *FakeAPI) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.requests))
	for _, r := range f.requests {
		out = append(out, r.Method+" "+r.URL.Path)
	}
	return out
}

func (f *FakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Clone(r.Context()))
	f.mu.Unlock()
	if _, pattern := f.mux.Handler(r); pattern == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		return
	}
	f.mux.ServeHTTP(w, r)
}

// Listen returns a TCP listener on 127.0.0.1 with a free port in
// FirstPort–LastPort, and closes it when the test ends.
func Listen(t testing.TB) net.Listener {
	t.Helper()
	size := LastPort - FirstPort + 1
	start := rand.IntN(size)
	for i := range size {
		port := FirstPort + (start+i)%size
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			t.Cleanup(func() { _ = l.Close() })
			return l
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatalf("testenv: listen on 127.0.0.1:%d: %v", port, err)
		}
	}
	t.Fatalf("testenv: no free port in %d–%d", FirstPort, LastPort)
	return nil
}
