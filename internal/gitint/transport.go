// The transport half of package gitint (spec .ai/specs/2026-11-13-git-credential-helper.md, #22) parses a
// Git credential request, decides whether the profile's host may answer it,
// renders the answer, and lists the process-scoped configuration entries that
// install the helper and the SSH→HTTPS rewrites. Every function is pure,
// names no forge, and never echoes a request value.

package gitint

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Limits of a credential request.
const (
	maxRequestBytes = 64 << 10
	maxRequestLines = 64
)

// Errors of the transport half. Their messages hold no request value.
var (
	// ErrRequestInvalid is a credential request that Git would not send.
	ErrRequestInvalid = errors.New("gitint: malformed credential request")
	// ErrCredentialInvalid is a provider answer that must not reach Git.
	ErrCredentialInvalid = errors.New("gitint: the provider's credential is not usable")
	// ErrEntryInvalid is a value that cannot go into the helper entries.
	ErrEntryInvalid = errors.New("gitint: cannot build the transport entries")
)

// Request is the part of a Git credential request that the decision reads.
type Request struct {
	Protocol, Host, Path, Username, Password string
	hasProtocol, hasHost, hasPath            bool
	hasUsername, hasPassword                 bool
}

// ParseRequest reads Git's credential protocol: key=value lines up to a blank
// line or the end of r, at most 64 KiB and 64 lines. It refuses a NUL byte, a
// line without "=", an empty key, an input over the limits, and a repeated
// protocol, host, path, username, or password. Other keys, including the
// url key, are ignored.
func ParseRequest(r io.Reader) (Request, error) {
	var req Request
	reader := bufio.NewReader(io.LimitReader(r, maxRequestBytes+1))
	total := 0
	for lines := 1; ; lines++ {
		line, err := reader.ReadString('\n')
		total += len(line)
		if total > maxRequestBytes || lines > maxRequestLines+1 {
			return Request{}, fmt.Errorf("%w (too long)", ErrRequestInvalid)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return Request{}, fmt.Errorf("%w (unreadable)", ErrRequestInvalid)
		}
		atEOF := err != nil
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return req, nil
		}
		if setErr := req.set(line); setErr != nil {
			return Request{}, setErr
		}
		if atEOF {
			return req, nil
		}
	}
}

func (r *Request) set(line string) error {
	if strings.ContainsAny(line, "\x00\r") {
		return fmt.Errorf("%w (NUL or CR byte)", ErrRequestInvalid)
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok || key == "" {
		return fmt.Errorf("%w (line without key=value)", ErrRequestInvalid)
	}
	var field *string
	var seen *bool
	switch key {
	case "protocol":
		field, seen = &r.Protocol, &r.hasProtocol
	case "host":
		field, seen = &r.Host, &r.hasHost
	case "path":
		field, seen = &r.Path, &r.hasPath
	case "username":
		field, seen = &r.Username, &r.hasUsername
	case "password":
		field, seen = &r.Password, &r.hasPassword
	default:
		return nil
	}
	if *seen {
		return fmt.Errorf("%w (repeated attribute)", ErrRequestInvalid)
	}
	*field, *seen = value, true
	return nil
}

// Verdict is the answer of Decide.
type Verdict int

// Verdicts.
const (
	// Skip means the request is for another host: print nothing.
	Skip Verdict = iota
	// Refuse means the request is for the profile's host but must not get a
	// credential.
	Refuse
	// Allow means the provider's credential may answer the request.
	Allow
)

var repositoryPath = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}/?$`)

// Decide says whether the profile's host may answer req. host is the
// provider's host, for example "github.com". A look-alike host, a host with a
// port, and a host with user-info are Skip, because they are not host. For
// host itself the protocol must be exactly https, the path must be
// owner/name or owner/name.git, and the request must carry no password. The
// reason of a Refuse is a fixed string.
func Decide(req Request, host string) (Verdict, string) {
	if !sameHost(req.Host, host) {
		if sameHost(hostname(req.Host), hostname(host)) {
			return Refuse, "the host has a port or user-info that the profile's host has not"
		}
		return Skip, ""
	}
	switch {
	case req.Protocol != "https":
		return Refuse, "only https requests get a credential"
	case req.Path == "":
		return Refuse, "the request has no repository path"
	case !validPath(req.Path):
		return Refuse, "the repository path is not owner/name"
	case req.hasPassword:
		return Refuse, "the request already carries a password"
	}
	return Allow, ""
}

func validPath(path string) bool {
	if !repositoryPath.MatchString(path) {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(path, "/"), "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

// hostname strips user-info and a port from a host, so that Git's
// normalized forms of the profile host (github.com:443, user@github.com) are
// recognized and refused instead of ignored.
func hostname(h string) string {
	if i := strings.LastIndexByte(h, '@'); i >= 0 {
		h = h[i+1:]
	}
	if i := strings.LastIndexByte(h, ':'); i >= 0 {
		h = h[:i]
	}
	return h
}

// sameHost compares an ASCII host without regard to case. An empty, non-ASCII,
// or different host does not match.
func sameHost(got, want string) bool {
	if got == "" || want == "" || len(got) != len(want) {
		return false
	}
	for i := 0; i < len(got); i++ {
		if got[i] >= 0x80 || lower(got[i]) != lower(want[i]) {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// Render returns the helper's answer for cred, which the provider built for
// host: the username and password lines. It fails closed with
// ErrCredentialInvalid when the protocol is not https, the host differs, or
// the username or password is empty or holds a CR, LF, or NUL.
func Render(cred provider.GitCredential, host string) (string, error) {
	switch {
	case cred.Protocol != "https", !sameHost(cred.Host, host):
		return "", fmt.Errorf("%w (protocol or host)", ErrCredentialInvalid)
	case !plain(cred.Username), !plain(cred.Password):
		return "", fmt.Errorf("%w (username or password)", ErrCredentialInvalid)
	}
	return "username=" + cred.Username + "\npassword=" + cred.Password + "\n", nil
}

func plain(s string) bool {
	return s != "" && !strings.ContainsAny(s, "\r\n\x00")
}

// QuitLine tells Git to stop asking helpers and prompts.
const QuitLine = "quit=1\n"

var profilePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// TransportEntries returns, in order, the entries that install the helper for
// https://host and rewrite its SSH remotes to HTTPS: the scoped helper
// command, useHttpPath, and the insteadOf rules for ssh://git@host/ and, when
// host has no port, git@host:. binary must be an absolute path without a
// control character; profile must be a valid profile name.
func TransportEntries(binary, profile, host string) ([]Entry, error) {
	switch {
	case !strings.HasPrefix(binary, "/") || strings.ContainsFunc(binary, isControl):
		return nil, fmt.Errorf("%w (binary path)", ErrEntryInvalid)
	case !profilePattern.MatchString(profile):
		return nil, fmt.Errorf("%w (profile name)", ErrEntryInvalid)
	case !validHost(host):
		return nil, fmt.Errorf("%w (host)", ErrEntryInvalid)
	}
	base := "https://" + host
	entries := []Entry{
		{"credential." + base + ".helper", "!" + shellQuote(binary) + " git-credential --profile " + profile},
		{"credential." + base + ".useHttpPath", "true"},
		{"url." + base + "/.insteadOf", "ssh://git@" + host + "/"},
	}
	if !strings.Contains(host, ":") {
		entries = append(entries, Entry{"url." + base + "/.insteadOf", "git@" + host + ":"})
	}
	return entries, nil
}

var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?(:[0-9]{1,5})?$`)

func validHost(host string) bool { return hostPattern.MatchString(host) }

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
