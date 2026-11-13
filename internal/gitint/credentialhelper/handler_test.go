package credentialhelper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider"
	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

const testHost = "github.com"

// hostedFake is the fake provider on a host of the test's choosing.
type hostedFake struct {
	*providertest.Fake
	host string
}

func (h hostedFake) Host() string { return h.host }

func (h hostedFake) GitCredential(t provider.Token) provider.GitCredential {
	c := h.Fake.GitCredential(t)
	c.Host = h.host
	c.Username = "x-access-token"
	return c
}

func newHandler(prov hostedFake, calls *int) *Handler {
	return &Handler{
		Host: func(name string) (string, error) {
			if name != "claude" {
				return "", fmt.Errorf("profile %q not found", name)
			}
			return prov.host, nil
		},
		Credential: func(ctx context.Context, _ string) (provider.GitCredential, error) {
			if calls != nil {
				*calls++
			}
			tok, err := prov.Token(ctx)
			if err != nil {
				return provider.GitCredential{}, err
			}
			return prov.GitCredential(tok), nil
		},
	}
}

func newFake() hostedFake { return hostedFake{Fake: providertest.NewFake(), host: testHost} }

func get(h *Handler, op, request string) (Output, error) {
	return h.Handle(context.Background(), Input{Profile: "claude", Operation: op, Stdin: strings.NewReader(request)})
}

const goodRequest = "protocol=https\nhost=github.com\npath=owner/repo.git\n\n"

func TestGetAnswersForTheProfileHost(t *testing.T) {
	testenv.Isolate(t)
	prov := newFake()
	out, err := get(newHandler(prov, nil), OpGet, goodRequest)
	if err != nil {
		t.Fatal(err)
	}
	tokens := prov.Tokens()
	if len(tokens) != 1 {
		t.Fatalf("%d tokens minted", len(tokens))
	}
	if want := "username=x-access-token\npassword=" + tokens[0].Value + "\n"; out.Stdout != want {
		t.Errorf("stdout = %q, want %q", out.Stdout, want)
	}
}

func TestGetStaysSilentForOtherHostsAndNeverMints(t *testing.T) {
	testenv.Isolate(t)
	for _, host := range []string{"example.com", "github.com.evil.example", "evilgithub.com", "api.github.com", "github.com."} {
		t.Run(host, func(t *testing.T) {
			calls := 0
			out, err := get(newHandler(newFake(), &calls), OpGet, "protocol=https\nhost="+host+"\npath=o/r\n")
			if err != nil || out.Stdout != "" || calls != 0 {
				t.Errorf("out %q err %v calls %d, want nothing", out.Stdout, err, calls)
			}
		})
	}
}

func TestGetRefusesTheProfileHostWithQuit(t *testing.T) {
	testenv.Isolate(t)
	requests := map[string]string{
		"http":         "protocol=http\nhost=github.com\npath=o/r\n",
		"ssh":          "protocol=ssh\nhost=github.com\npath=o/r\n",
		"no path":      "protocol=https\nhost=github.com\n",
		"dot-dot path": "protocol=https\nhost=github.com\npath=../x\n",
		"deep path":    "protocol=https\nhost=github.com\npath=o/r/extra\n",
		"password":     "protocol=https\nhost=github.com\npath=o/r\npassword=x\n",
		"repeated":     "protocol=https\nhost=github.com\nhost=github.com\npath=o/r\n",
		"NUL":          "protocol=https\nhost=github.com\x00\npath=o/r\n",
		"port":         "protocol=https\nhost=github.com:443\npath=o/r\n",
		"user-info":    "protocol=https\nhost=user@github.com\npath=o/r\n",
		"CR":           "protocol=https\r\nhost=github.com\r\npath=o/r\r\n",
		"no equals":    "protocol=https\nhost\n",
	}
	for name, request := range requests {
		t.Run(name, func(t *testing.T) {
			calls := 0
			out, err := get(newHandler(newFake(), &calls), OpGet, request)
			if err == nil || out.Stdout != "quit=1\n" || calls != 0 {
				t.Errorf("out %q err %v calls %d, want quit=1, an error, and no mint", out.Stdout, err, calls)
			}
			if err != nil && strings.Contains(err.Error(), "evil") {
				t.Errorf("error echoes the request: %v", err)
			}
		})
	}
}

func TestGetFailsClosedWhenNoTokenComes(t *testing.T) {
	testenv.Isolate(t)
	secret := testenv.FakeToken("must_not_leak")
	causes := []struct {
		name  string
		cause error
		class string
	}{
		{"unauthorized", provider.ErrUnauthorized, "unauthorized"},
		{"forbidden", provider.ErrForbidden, "forbidden"},
		{"not found", provider.ErrNotFound, "not found"},
		{"rate limited", provider.ErrRateLimited, "rate limited"},
		{"unavailable", provider.ErrUnavailable, "unavailable"},
		{"canceled", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "canceled"},
		{"classed", &ClassedError{Class: "private key", Err: errors.New(secret)}, "private key"},
		{"unknown", errors.New("boom " + secret), "error"},
	}
	for _, tt := range causes {
		t.Run(tt.name, func(t *testing.T) {
			h := newHandler(newFake(), nil)
			h.Credential = func(context.Context, string) (provider.GitCredential, error) {
				return provider.GitCredential{}, fmt.Errorf("mint: %w (%s)", tt.cause, secret)
			}
			out, err := get(h, OpGet, goodRequest)
			if err == nil || out.Stdout != "quit=1\n" {
				t.Fatalf("out %q err %v", out.Stdout, err)
			}
			want := fmt.Sprintf("git-credential: cannot get a token for profile \"claude\" (%s); run agent-id doctor --profile \"claude\"", tt.class)
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
			if strings.Contains(err.Error()+out.Stdout, secret) || strings.Contains(err.Error(), "boom") {
				t.Errorf("a cause text reached the output")
			}
		})
	}
}

func TestGetFailsClosedForAnUnknownProfile(t *testing.T) {
	testenv.Isolate(t)
	calls := 0
	h := newHandler(newFake(), &calls)
	out, err := h.Handle(context.Background(), Input{Profile: "ghost", Operation: OpGet, Stdin: strings.NewReader(goodRequest)})
	if err == nil || out.Stdout != "quit=1\n" || calls != 0 || !strings.Contains(err.Error(), `"ghost" (profile)`) {
		t.Errorf("out %q err %v calls %d", out.Stdout, err, calls)
	}
}

func TestGetRefusesAnUnsafeProviderAnswer(t *testing.T) {
	testenv.Isolate(t)
	secret := testenv.FakeToken("inject")
	answers := map[string]provider.GitCredential{
		"other host":  {Protocol: "https", Host: "example.com", Username: "u", Password: secret},
		"http":        {Protocol: "http", Host: testHost, Username: "u", Password: secret},
		"newline":     {Protocol: "https", Host: testHost, Username: "u", Password: secret + "\nquit=0"},
		"no password": {Protocol: "https", Host: testHost, Username: "u"},
	}
	for name, cred := range answers {
		t.Run(name, func(t *testing.T) {
			h := newHandler(newFake(), nil)
			h.Credential = func(context.Context, string) (provider.GitCredential, error) { return cred, nil }
			out, err := get(h, OpGet, goodRequest)
			if err == nil || out.Stdout != "quit=1\n" || strings.Contains(err.Error(), "ghs_") {
				t.Errorf("out %q err %v", out.Stdout, err)
			}
		})
	}
}

func TestStoreAndEraseDoNothing(t *testing.T) {
	env := testenv.Isolate(t)
	before := tree(t, env.Root)
	for _, op := range []string{OpStore, OpErase} {
		calls := 0
		h := newHandler(newFake(), &calls)
		h.Host = func(string) (string, error) { t.Error("store and erase must not resolve the profile"); return "", nil }
		out, err := get(h, op, goodRequest+"username=x-access-token\npassword="+testenv.FakeToken("stored")+"\n")
		if err != nil || out.Stdout != "" || calls != 0 {
			t.Errorf("%s: out %q err %v calls %d", op, out.Stdout, err, calls)
		}
	}
	if after := tree(t, env.Root); after != before {
		t.Errorf("store or erase changed the files under the test root:\nbefore %s\nafter  %s", before, after)
	}
}

func tree(t *testing.T, root string) string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		names = append(names, fmt.Sprintf("%s %d %d", path, info.Size(), info.Mode()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(names, "\n")
}

func TestStoreReadsAtMost64KiB(t *testing.T) {
	testenv.Isolate(t)
	r := &countingReader{}
	_, err := newHandler(newFake(), nil).Handle(context.Background(), Input{Profile: "claude", Operation: OpStore, Stdin: r})
	if err != nil || r.n > 64<<10 {
		t.Errorf("err %v, read %d bytes", err, r.n)
	}
}

type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n > 1<<20 {
		return 0, io.EOF
	}
	c.n += len(p)
	return len(p), nil
}

func TestUnknownOperationFailsWithoutOutput(t *testing.T) {
	testenv.Isolate(t)
	for _, op := range []string{"", "GET", "capability", "get ", "fill"} {
		out, err := get(newHandler(newFake(), nil), op, goodRequest)
		if !errors.Is(err, ErrUsage) || out.Stdout != "" {
			t.Errorf("operation %q: out %q err %v", op, out.Stdout, err)
		}
	}
}

func TestCommandPrintsTheAnswerAndTheFailure(t *testing.T) {
	testenv.Isolate(t)
	prov := newFake()
	cmd := NewCommand(newHandler(prov, nil))
	var stdout strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(goodRequest))
	cmd.SetArgs([]string{"--profile", "claude", "get"})
	if err := cmd.Execute(); err != nil || !strings.HasPrefix(stdout.String(), "username=x-access-token\npassword=ghs_aidtest_") {
		t.Fatalf("stdout %q err %v", stdout.String(), err)
	}

	prov.FailTokens(provider.ErrUnavailable)
	cmd = NewCommand(newHandler(prov, nil))
	stdout.Reset()
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(goodRequest))
	cmd.SetArgs([]string{"--profile", "claude", "get"})
	if err := cmd.Execute(); err == nil || stdout.String() != "quit=1\n" {
		t.Errorf("stdout %q err %v", stdout.String(), err)
	}

	cmd = NewCommand(newHandler(prov, nil))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"get"})
	if err := cmd.Execute(); err == nil {
		t.Error("a missing --profile must fail")
	}
}
