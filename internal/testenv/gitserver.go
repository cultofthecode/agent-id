package testenv

import (
	"bytes"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// GitServer serves the bare repositories of RepositoriesDir over HTTPS with
// git http-backend, on 127.0.0.1 with a port in FirstPort–LastPort. Like
// GitHub, it asks for Basic credentials, accepts only fake tokens as the
// password, and records the credentials of every request.
type GitServer struct {
	// URL is the base URL of the server, https://127.0.0.1:<port>.
	URL string
	// CAFile holds the PEM certificate of the server. NewGitServer points
	// GIT_SSL_CAINFO at it so that git trusts the server.
	CAFile string

	t        testing.TB
	backend  cgi.Handler
	mu       sync.Mutex
	requests []GitRequest
}

// GitRequest is one request that the Git server received.
type GitRequest struct {
	Method string
	Path   string
	// Service is git-upload-pack for a fetch and git-receive-pack for a push.
	Service string
	// Username and Password are the Basic credentials of the request, empty
	// when it had none.
	Username string
	Password string
}

// NewGitServer starts the Git server of env and stops it when the test ends.
// It sets GIT_SSL_CAINFO for the test. Call Isolate first.
func NewGitServer(t testing.TB, env Env) *GitServer {
	t.Helper()
	execPath := Git(t, env.Root, "--exec-path")
	if err := os.MkdirAll(RepositoriesDir(env), 0o700); err != nil {
		t.Fatalf("testenv: create %s: %v", RepositoriesDir(env), err)
	}
	s := &GitServer{
		t: t,
		backend: cgi.Handler{
			Path:       filepath.Join(execPath, "git-http-backend"),
			Env:        []string{"GIT_PROJECT_ROOT=" + RepositoriesDir(env), "GIT_HTTP_EXPORT_ALL=1"},
			InheritEnv: []string{"PATH", "HOME", "TMPDIR", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "XDG_CONFIG_HOME"},
		},
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	_ = server.Listener.Close()
	server.Listener = Listen(t)
	server.StartTLS()
	t.Cleanup(server.Close)
	s.URL = server.URL
	s.CAFile = filepath.Join(env.Root, "git-server-ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(s.CAFile, certificate, 0o600); err != nil {
		t.Fatalf("testenv: write %s: %v", s.CAFile, err)
	}
	t.Setenv("GIT_SSL_CAINFO", s.CAFile)
	return s
}

// RepositoryURL returns the HTTPS URL of the bare repository name.git.
func (s *GitServer) RepositoryURL(name string) string {
	return s.URL + "/" + name + ".git"
}

// Requests returns every request the server received, in order.
func (s *GitServer) Requests() []GitRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Pushes returns the requests of pushes that carried credentials, in order.
func (s *GitServer) Pushes() []GitRequest {
	var pushes []GitRequest
	for _, r := range s.Requests() {
		if r.Service == "git-receive-pack" && r.Password != "" {
			pushes = append(pushes, r)
		}
	}
	return pushes
}

// serve records r, answers 401 unless r carries a fake token, and passes
// authenticated requests to git http-backend.
func (s *GitServer) serve(w http.ResponseWriter, r *http.Request) {
	username, password, _ := r.BasicAuth()
	s.record(GitRequest{Method: r.Method, Path: r.URL.Path, Service: service(r), Username: username, Password: password})
	if !CheckFakeToken(s.t, "the fake Git server", password) {
		w.Header().Set("WWW-Authenticate", `Basic realm="agent-id fake Git server"`)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	if err := bufferChunkedBody(r); err != nil {
		http.Error(w, "cannot read the request body", http.StatusBadRequest)
		return
	}
	backend := s.backend
	backend.Env = append(slices.Clone(s.backend.Env), "REMOTE_USER="+username)
	backend.ServeHTTP(w, r)
}

// record appends r to the requests of the server.
func (s *GitServer) record(r GitRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r)
}

// service returns the Git service of a smart-HTTP request: the service query
// of an info/refs request, or the last path element of a service request.
func service(r *http.Request) string {
	if name := r.URL.Query().Get("service"); name != "" {
		return name
	}
	if name := path.Base(r.URL.Path); strings.HasPrefix(name, "git-") {
		return name
	}
	return ""
}

// bufferChunkedBody reads a body of unknown length into memory, because the
// CGI handler refuses chunked request bodies and git sends large packs
// chunked.
func bufferChunkedBody(r *http.Request) error {
	if r.ContentLength >= 0 {
		return nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	r.TransferEncoding = nil
	return nil
}
