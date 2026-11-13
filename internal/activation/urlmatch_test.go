package activation

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cultofthecode/agent-id/internal/provider/providertest"
	"github.com/cultofthecode/agent-id/internal/testenv"
)

// The examples of the post-merge review of #99 (issue #100). Each one is a
// header that Git sends to github.com or to a subdomain, and that the check
// once passed.
func TestReviewExamplesOfIssue100(t *testing.T) {
	hdr := "X-L: 1"
	t.Run("a reset with an encoded or query path does not reset", func(t *testing.T) {
		for _, reset := range []string{
			"https://github.com/org%2Fsub", "https://github.com/org%2fsub/", "https://github.com/org/sub?",
			"https://github.com/org/sub/?x", "https://github.com/org/sub#f", "https://github.com/org/sub%3F",
			"https://github.com/org/sub%23f",
		} {
			e := entries("http.https://github.com/org/sub/.extraheader", hdr, "http."+reset+".extraheader", "")
			got := EffectiveHeaders(e, "https://github.com/org/sub/r")
			if !slices.Equal(got, []string{"X-L"}) {
				t.Errorf("reset %q: headers %v", reset, got)
			}
			if names := scopedNames(e, "github.com"); !slices.Equal(names, []string{"X-L"}) {
				t.Errorf("reset %q: scoped headers %v", reset, names)
			}
		}
	})
	t.Run("a wildcard reset with a query does not reset", func(t *testing.T) {
		e := entries("http.https://*.github.com/x/.extraheader", hdr, "http.https://*.github.com/x?.extraheader", "")
		if got := EffectiveHeaders(e, "https://gist.github.com/x/y"); !slices.Equal(got, []string{"X-L"}) {
			t.Errorf("headers %v", got)
		}
		if names := scopedNames(e, "gist.github.com"); !slices.Equal(names, []string{"X-L"}) {
			t.Errorf("scoped headers %v", names)
		}
	})
	t.Run("a pattern that equals the path after Git normalization resets", func(t *testing.T) {
		for _, reset := range []string{"https://github.com/org/./sub/", "https://github.com/org/x/../sub", "https://github.com/%6Frg/sub/", "https://GITHUB.com:443/org/sub"} {
			e := entries("http.https://github.com/org/sub/.extraheader", hdr, "http."+reset+".extraheader", "")
			if got := EffectiveHeaders(e, "https://github.com/org/sub/r"); len(got) != 0 {
				t.Errorf("reset %q: headers %v", reset, got)
			}
		}
	})
	t.Run("a pattern with a control byte in the path is applied", func(t *testing.T) {
		for _, b := range []string{"\t", "\x7f", " ", "\x01"} {
			key := "http.https://github.com/org/a" + b + "b/.extraheader"
			e := entries(key, hdr)
			if got := EffectiveHeaders(e, "https://github.com/org/a"+fmt.Sprintf("%%%02X", b[0])+"b/r"); !slices.Equal(got, []string{"X-L"}) {
				t.Errorf("byte %#x: headers %v", b, got)
			}
			if names := scopedNames(e, "github.com"); !slices.Equal(names, []string{"X-L"}) {
				t.Errorf("byte %#x: scoped headers %v", b, names)
			}
		}
	})
	t.Run("a pattern with a space in the user is applied", func(t *testing.T) {
		e := entries("http.https://us er@github.com/.extraheader", hdr)
		if got := EffectiveHeaders(e, "https://us%20er@github.com/org/r"); !slices.Equal(got, []string{"X-L"}) {
			t.Errorf("headers %v", got)
		}
		if names := scopedNames(e, "github.com"); !slices.Equal(names, []string{"X-L"}) {
			t.Errorf("scoped headers %v", names)
		}
	})
}

func TestUnparsableHeaderScope(t *testing.T) {
	bad := []string{
		"github.com", "https:/github.com/", "1http://github.com/", "https://github.com/a%zz/", "https://github.com/a%/",
		"https://github.com:0443/", "https://github.com:99999/", "https://github.com:x/", "https://a@b@github.com/",
		"https://git%68ub.com/", "https://git hub.com/", "https:///", "https://github.com/..", "https://github.com/a/..",
		"https://github.com/a/.", "https://[::1/", "https://github.com/../x",
	}
	for _, pat := range bad {
		if _, got := UnparsableHeaderScope(entries("http."+pat+".extraheader", "X: 1")); !got {
			t.Errorf("%q parses", pat)
		}
		if _, got := UnparsableHeaderScope(entries("http."+pat+".extraheader", "")); got {
			t.Errorf("%q: an empty reset is refused", pat)
		}
	}
	good := []string{"https://github.com/", "https://*.github.com/x", "HTTPS://GitHub.Com:443/o/", "https://u:p@github.com", "https://github.com/a b/", "https://[::1]:8080/", "https://github.com/x?y#z"}
	for _, pat := range good {
		if e, got := UnparsableHeaderScope(entries("http."+pat+".extraheader", "X: 1")); got {
			t.Errorf("%q does not parse (%s)", pat, e.source())
		}
	}
	if _, got := UnparsableHeaderScope(entries("http.extraheader", "X: 1", "http.sslcainfo", "x")); got {
		t.Error("a generic header has no pattern")
	}
}

func TestUnparsableHeaderIsRefusedByTheCheck(t *testing.T) {
	a := providertest.NewFakeActivator()
	a.Hosts = []string{"github.com", "gist.github.com", "ssh.github.com"}
	c := &remoteChecker{a: a}
	for _, pat := range []string{"https://github.com/a%zz/", "https://github.com:0443/", "https://git%68ub.com/", "github.com"} {
		err := c.checkHostHeaders(entries("http."+pat+".extraheader", "X-L: 1"))
		if err == nil || !strings.Contains(err.Error(), "agent-id cannot parse") || strings.Contains(err.Error(), "github.com") || strings.Contains(err.Error(), "X-L") {
			t.Errorf("%q: error %v", pat, err)
		}
	}
	for _, pat := range []string{"https://github.com/org/a\tb/", "https://us er@github.com/", "https://github.com/org/a\x7fb/"} {
		if err := c.checkHostHeaders(entries("http."+pat+".extraheader", "X-L: 1")); err == nil {
			t.Errorf("%q passes", pat)
		}
	}
	if err := c.checkHostHeaders(entries("http.https://github.com/a%zz/.extraheader", "")); err != nil {
		t.Errorf("an unparsable reset is refused: %v", err)
	}
}

// gitMatchesAll writes the entry http.<pattern>.extraheader to a new config
// file and asks real Git which of targets it applies to.
func gitMatchesAll(cfg, pattern string, targets []string) (map[string]bool, bool) {
	_ = os.Remove(cfg)
	if err := exec.Command("git", "config", "--file", cfg, "http."+pattern+".extraheader", "X: 1").Run(); err != nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, target := range targets {
		var buf bytes.Buffer
		cmd := exec.Command("git", "config", "--file", cfg, "--get-urlmatch", "http.extraheader", target)
		cmd.Stdout = &buf
		_ = cmd.Run()
		out[target] = strings.TrimSpace(buf.String()) == "X: 1"
	}
	return out, true
}

func ourMatch(pattern, target string) (match, comparable bool) {
	t, ok := parseGitURL(target, false)
	if !ok {
		return false, false
	}
	if _, ok := parseGitURL(pattern, true); !ok {
		return false, false
	}
	_, match = headerScope("http."+pattern+".extraheader", t)
	return match, true
}

// TestUrlmatchAgreesWithGit compares the matcher with git config
// --get-urlmatch. A pattern or URL that agent-id declines to parse is left
// out: it is refused or ignored by design.
func TestUrlmatchAgreesWithGit(t *testing.T) {
	testenv.Isolate(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	patterns := []string{
		"https://github.com/org/sub/", "https://github.com/org%2Fsub/", "https://github.com/org/sub?", "https://github.com/org/sub#f",
		"https://github.com/org/sub/?x", "https://github.com/org/./sub", "https://github.com/org/x/../sub/", "https://github.com/%6Frg/sub",
		"https://github.com/org/%2e/sub", "https://github.com/org/%2e%2e/org/sub", "https://github.com/org/..%2fsub", "https://github.com/org//sub",
		"https://github.com/org/s%75b", "https://github.com/ORG/sub", "https://github.com/org/sub%2F", "https://github.com/org/sub%2f",
		"https://github.com", "https://github.com/", "https://github.com?x", "https://github.com#x", "https://github.com/?x", "https://GitHub.COM/",
		"https://github.com./", "https://github.com:443/", "https://github.com:/", "https://github.com:80/", "http://github.com:80/", "http://github.com/",
		"https://u@github.com/", "https://u:p@github.com/", "https://us er@github.com/", "https://u%41@github.com/", "https://u%40x@github.com/",
		"https://uA@github.com/", "https://github.com/a@b", "https://*.github.com/", "https://*.com/", "https://*.*/", "https://github.*/", "https://*/",
		"https://git*ub.com/", "https://*.*.*/", "https://gist.*.com/", "https://*.github.*/x", "https://[::1]/", "https://[::1]:8443/",
		"HTTPS://github.com/org/", "ftp://github.com/", "https://github.com/x?a=1", "https://github.com/x?", "https://github.com/x/", "https://github.com/x",
		"https://github.com/a%7Eb", "https://github.com/a~b", "https://github.com/a%5Bb", "https://github.com/a[b", "https://github.com/a%C3%A9",
		"https://github.com/a%c3%a9", "https://github.com/é",
		"https://@github.com/", "https://:p@github.com/", "https://github.com../", "https://github.com/x?/../", "https://github.com/x#/../", "https://github.com/x/../y?/./z",
	}
	targets := []string{
		"https://github.com/org/sub/r", "https://github.com/org/sub", "https://github.com/org/sub/", "https://github.com/org/sub?", "https://github.com/org/sub?/r",
		"https://github.com/org/sub#f", "https://github.com/org/sub/?x", "https://github.com/org%2Fsub/r", "https://github.com/org%2fsub/r", "https://github.com/org/sub%2F/r",
		"https://github.com/org/sub%2f/r", "https://github.com/ORG/sub/r", "https://github.com/org//sub/r", "https://github.com/", "https://github.com", "https://github.com?x",
		"https://github.com/?x", "https://github.com#x", "https://github.com/x", "https://github.com/x/y", "https://github.com/x?a=1", "https://github.com/x?a=1/y",
		"https://github.com/x?/y", "https://github.com/x#frag", "https://github.com/x?q", "https://github.com./r", "https://GITHUB.com:443/r", "https://github.com:80/r",
		"http://github.com/r", "https://u@github.com/r", "https://u:p@github.com/r", "https://us%20er@github.com/org/r", "https://uA@github.com/r", "https://u%40x@github.com/r",
		"https://other@github.com/r", "https://gist.github.com/x/y", "https://gist.github.com/r", "https://ssh.github.com/r", "https://sub.gist.github.com/r",
		"https://a.b.c/r", "https://example.com/r", "https://[::1]/r", "https://[::1]:8443/r", "ftp://github.com/r", "https://github.com/a@b/r",
		"https://github.com/a~b/r", "https://github.com/a%7Eb/r", "https://github.com/a[b/r", "https://github.com/a%5Bb/r", "https://github.com/a%C3%A9/r", "https://github.com/%C3%A9/r", "https://@github.com/r", "https://:p@github.com/r", "https://github.com../r", "https://github.com/x?/../",
	}
	groups := map[string][]string{}
	for _, p := range patterns {
		groups[p] = targets
	}
	for c := 1; c < 0x80; c++ {
		if c == '\n' || c == '%' {
			continue
		}
		b := string(rune(c))
		enc := fmt.Sprintf("%%%02X", c)
		groups["https://github.com/o/a"+b+"z/"] = []string{"https://github.com/o/a" + b + "z/r", "https://github.com/o/a" + enc + "z/r", "https://github.com/o/a/z/r"}
		groups["https://github.com/o/a"+enc+"z/"] = []string{"https://github.com/o/a" + b + "z/r", "https://github.com/o/a" + enc + "z/r", "https://github.com/o/a/z/r"}
		groups["https://u"+b+"v@github.com/"] = []string{"https://u" + enc + "v@github.com/r", "https://u" + b + "v@github.com/r", "https://github.com/r"}
	}
	var (
		mu                sync.Mutex
		wg                sync.WaitGroup
		compared, skipped int
		sem               = make(chan struct{}, 8)
	)
	dir := t.TempDir()
	n := 0
	for pattern, tgs := range groups {
		if strings.ContainsAny(pattern, "\x00\r") {
			continue
		}
		n++
		cfg := filepath.Join(dir, fmt.Sprintf("gitconfig-%d", n))
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			var want []string
			for _, tg := range tgs {
				if _, ok := ourMatch(pattern, tg); ok {
					want = append(want, tg)
				}
			}
			git, ok := gitMatchesAll(cfg, pattern, want)
			mu.Lock()
			defer mu.Unlock()
			skipped += len(tgs) - len(want)
			if !ok {
				return
			}
			for _, tg := range want {
				compared++
				if ours, _ := ourMatch(pattern, tg); git[tg] != ours {
					t.Errorf("pattern %q, URL %q: agent-id %v, git %v", pattern, tg, ours, git[tg])
				}
			}
		}()
	}
	wg.Wait()
	t.Logf("compared %d pairs with git, left out %d", compared, skipped)
	if compared < 3000 {
		t.Errorf("only %d pairs compared", compared)
	}
}

// TestRealGitSendsWhatTheCheckPredicts runs real git against a local listener
// and compares the header that git sends with EffectiveHeaders, for the
// shapes of the #99 review moved to the listener's host.
func TestRealGitSendsWhatTheCheckPredicts(t *testing.T) {
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
	base := server.URL
	host := strings.TrimPrefix(base, "http://")
	userHost := "us er@" + host
	cases := []struct {
		name    string
		entries []ConfigEntry
	}{
		{"reset with encoded slash", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/org%2Fsub/.extraheader", "")},
		{"reset with a query", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/org/sub?.extraheader", "")},
		{"reset with a query after a slash", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/org/sub/?x.extraheader", "")},
		{"reset with a fragment", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/org/sub#f.extraheader", "")},
		{"reset that Git normalizes to the path", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/org/./sub.extraheader", "")},
		{"reset with an upper-case escape of a letter", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http."+base+"/%6Frg/sub.extraheader", "")},
		{"tab in the path", entries("http."+base+"/org/a\tb/.extraheader", "X-L: 1")},
		{"DEL in the path", entries("http."+base+"/org/a\x7fb/.extraheader", "X-L: 1")},
		{"space in the user", entries("http://"+userHost+"/.extraheader", "X-L: 1")},
		{"reset with an empty user", entries("http."+base+"/org/sub/.extraheader", "X-L: 1", "http.http://@"+host+"/org/sub/.extraheader", "")},
		{"reset with dot segments in a query", entries("http."+base+"/org/.extraheader", "X-L: 1", "http."+base+"/org/x?/../.extraheader", "")},
		{"plain header at the host", entries("http."+base+"/.extraheader", "X-L: 1")},
		{"header then host reset", entries("http."+base+"/.extraheader", "X-L: 1", "http."+base+"/.extraheader", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, rawURL := range []string{base + "/org/sub/r.git", base + "/org/r.git", base + "/org/a%09b/r.git", base + "/org/a%7Fb/r.git", "http://us%20er@" + host + "/org/r.git"} {
				mu.Lock()
				sent = nil
				mu.Unlock()
				args := []string{}
				for _, e := range tc.entries {
					args = append(args, "-c", e.Key+"="+e.Value)
				}
				args = append(args, "ls-remote", rawURL)
				cmd := exec.Command("git", args...)
				_ = cmd.Run()
				mu.Lock()
				got := slices.Contains(sent, "1")
				mu.Unlock()
				want := len(EffectiveHeaders(tc.entries, rawURL)) > 0
				if got != want {
					t.Errorf("URL %q: git sent the header = %v, EffectiveHeaders predicts %v", rawURL, got, want)
				}
			}
		})
	}
}

// The three fail-open cases that the review of PR #101 found in the first
// version of the matcher. Each reset must not remove the header, because Git
// ranks or matches the pattern differently.
func TestReviewExamplesOfPR101(t *testing.T) {
	hdr := "X-L: 1"
	type tc struct{ name, header, reset, url string }
	cases := []tc{
		{"trailing dot ranks above the baseline", "https://github.com./", "https://github.com/", "https://github.com/org/r"},
		{"two trailing dots never match", "https://github.com/org/", "https://github.com../", "https://github.com/org/r"},
		{"two trailing dots, wildcard", "https://*.github.com/", "https://*.github.com../", "https://gist.github.com/org/r"},
		{"empty user", "https://github.com/org/", "https://@github.com/org/", "https://github.com/org/r"},
		{"empty user with a password", "https://github.com/org/", "https://:pw@github.com/org/", "https://github.com/org/r"},
		{"empty user, wildcard", "https://*.github.com/", "https://@*.github.com/", "https://gist.github.com/r"},
		{"dot segments in a query", "https://github.com/org/", "https://github.com/org/x?/../", "https://github.com/org/r"},
		{"dot segments in a fragment", "https://github.com/org/", "https://github.com/org/x#/../", "https://github.com/org/r"},
		{"dot segments in a query, wildcard", "https://*.github.com/", "https://*.github.com/x?/../", "https://gist.github.com/r"},
	}
	for _, c := range cases {
		e := entries("http."+c.header+".extraheader", hdr, "http."+c.reset+".extraheader", "")
		if got := EffectiveHeaders(e, c.url); !slices.Equal(got, []string{"X-L"}) {
			t.Errorf("%s: headers %v", c.name, got)
		}
	}
	e := entries("http.https://github.com/org/.extraheader", hdr, "http.https://github.com/org/x?/../.extraheader", "")
	if got := scopedNames(e, "github.com"); !slices.Equal(got, []string{"X-L"}) {
		t.Errorf("scoped headers %v", got)
	}
}

// TestUrlmatchRankAgreesWithGit compares which of two entries wins. Git
// prints the value of the best match, so with the header "X-L: 1" first and a
// reset (here "X-L: 2") second, the header survives exactly when Git prints
// "X-L: 1".
func TestUrlmatchRankAgreesWithGit(t *testing.T) {
	testenv.Isolate(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	headers := []string{
		"https://github.com/", "https://github.com./", "https://github.com/org/", "https://*.github.com/", "https://*.github.com/x/", "https://github.com/org/x/",
		"https://u@github.com/", "https://@github.com/", "https://github.com/org/x?", "https://*.com/", "https://github.*/",
	}
	resets := []string{
		"https://github.com/", "https://github.com./", "https://github.com../", "https://github.com/org/", "https://github.com/org/x/", "https://github.com/org/x?/../",
		"https://github.com/org/x#/../", "https://github.com/org/x?", "https://@github.com/", "https://:pw@github.com/", "https://u@github.com/", "https://u:p@github.com/",
		"https://*.github.com/", "https://*.github.com../", "https://@*.github.com/", "https://*.github.com/x?/../", "https://*.github.com/x/", "https://*.com/", "https://*.com./",
		"https://github.*/", "https://*.*/", "https://GITHUB.com:443/", "https://github.com/org/./x", "https://github.com/org/%2e/x/",
	}
	targets := []string{
		"https://github.com/org/r", "https://github.com/org/x/r", "https://github.com./org/r", "https://github.com../org/r", "https://u@github.com/org/r",
		"https://@github.com/org/r", "https://gist.github.com/x/y", "https://gist.github.com/r", "https://ssh.github.com/r", "https://gist.github.com./r", "https://github.com/org/x?",
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		sem = make(chan struct{}, 8)
		n   int
	)
	dir := t.TempDir()
	compared := 0
	for _, h := range headers {
		for _, r := range resets {
			n++
			cfg := filepath.Join(dir, fmt.Sprintf("gitconfig-%d", n))
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer func() { <-sem; wg.Done() }()
				if _, ok := parseGitURL(h, true); !ok {
					return
				}
				if _, ok := parseGitURL(r, true); !ok {
					return
				}
				_ = os.Remove(cfg)
				for _, kv := range [][2]string{{"http." + h + ".extraheader", "X-L: 1"}, {"http." + r + ".extraheader", "X-L: 2"}} {
					if exec.Command("git", "config", "--file", cfg, "--add", kv[0], kv[1]).Run() != nil {
						return
					}
				}
				for _, target := range targets {
					if _, ok := parseGitURL(target, false); !ok {
						continue
					}
					var buf bytes.Buffer
					cmd := exec.Command("git", "config", "--file", cfg, "--get-urlmatch", "http.extraheader", target)
					cmd.Stdout = &buf
					_ = cmd.Run()
					gitKeeps := strings.TrimSpace(buf.String()) == "X-L: 1"
					ours := len(EffectiveHeaders(entries("http."+h+".extraheader", "X-L: 1", "http."+r+".extraheader", ""), target)) > 0
					mu.Lock()
					compared++
					if gitKeeps != ours {
						t.Errorf("header %q, reset %q, URL %q: agent-id keeps the header = %v, git = %v", h, r, target, ours, gitKeeps)
					}
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	t.Logf("compared %d rankings with git", compared)
	if compared < 2000 {
		t.Errorf("only %d rankings compared", compared)
	}
}
