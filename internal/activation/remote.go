package activation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// commandTimeout bounds every git and ssh call of the remote checks.
const commandTimeout = 10 * time.Second

// Check is a refusal that runs with the child environment, in dir, before the
// command starts.
type Check func(ctx context.Context, dir string, child []string) error

// Runner runs git and ssh for the remote checks.
type Runner interface {
	// Run runs name with args in dir with env and returns its standard
	// output. A command that cannot start returns an error that wraps
	// exec.ErrNotFound; one that exits non-zero returns *exec.ExitError.
	Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error)
}

// ExecRunner is the Runner of the operating system.
type ExecRunner struct{}

// Run implements Runner with a 10 second timeout.
func (ExecRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// NewChecks returns the remote checks for the provider, in their fixed order:
// netrc, rewrites to SSH, then the remotes of the working directory (O1, D2,
// D3).
func NewChecks(a provider.Activator, r Runner) []Check {
	c := &remoteChecker{a: a, run: r}
	return []Check{c.netrc, c.rewrites, c.remotes}
}

type remoteChecker struct {
	a   provider.Activator
	run Runner
}

func (c *remoteChecker) netrc(_ context.Context, _ string, child []string) error {
	return checkNetrc(child, c.a.GitHosts())
}

func checkFailed(why string) error {
	return failf(ErrRemoteCheck, "cannot check the Git configuration (%s); refusing to start", why)
}

func (c *remoteChecker) git(ctx context.Context, dir string, child []string, args ...string) ([]byte, error) {
	out, err := c.run.Run(ctx, dir, child, "git", args...)
	if err != nil {
		var exit *exec.ExitError
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return nil, err
		case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
			return nil, checkFailed("timeout")
		case errors.As(err, &exit):
			return nil, err
		}
		return nil, checkFailed("git failed")
	}
	return out, nil
}

// configList returns the Git configuration seen from dir with the child
// environment. git being absent means there is no Git path at all.
func (c *remoteChecker) configList(ctx context.Context, dir string, child []string) ([]ConfigEntry, bool, error) {
	out, err := c.git(ctx, dir, child, "config", "--list", "-z", "--show-origin", "--show-scope")
	if errors.Is(err, exec.ErrNotFound) {
		return nil, false, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, false, checkFailed("git failed")
	}
	if err != nil {
		return nil, false, err
	}
	return ParseConfigList(out), true, nil
}

// rewrites evaluates the rewrite rules and the extra headers outside any
// repository and in the working directory.
func (c *remoteChecker) rewrites(ctx context.Context, dir string, child []string) error {
	for _, where := range []string{emptyDir(), dir} {
		env := child
		if where == emptyDir() {
			env = withoutRepoVars(child)
		}
		entries, ok, err := c.configList(ctx, where, env)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		for _, host := range c.a.GitHosts() {
			if e, bad := RewritesToSSH(entries, "https://"+host+"/"); bad {
				return failf(ErrSSHRewrite, "url.<...>.insteadOf from %s rewrites https://%s/ to SSH; the agent would use your SSH key for every clone; remove the rule or scope it to a repository", e.source(), host)
			}
		}
		if err := c.checkHostHeaders(entries); err != nil {
			return err
		}
	}
	return nil
}

// checkHostHeaders refuses an extra header that survives for any Git host of
// the provider, not only the host that the baseline resets: the reset is
// scoped to one host, and a wildcard or generic header reaches the others. It
// also refuses a header whose URL pattern does not parse (fail closed).
func (c *remoteChecker) checkHostHeaders(entries []ConfigEntry) error {
	if e, bad := UnparsableHeaderScope(entries); bad {
		return failf(ErrRemoteHeader, "an http.<url>.extraheader entry from %s has a URL pattern that agent-id cannot parse as Git does, so Git may send its header to a Git host; remove that entry", e.source())
	}
	for _, h := range c.a.GitHosts() {
		for _, name := range HostRootHeaders(entries, h) {
			return failf(ErrRemoteHeader, "Git host %s gets an extra HTTP header (%s) from an http.extraheader entry that agent-id cannot reset; remove that entry", h, name)
		}
	}
	for _, hh := range ScopedHeaders(entries, c.a.GitHosts()) {
		return failf(ErrRemoteHeader, "Git host %s gets an extra HTTP header (%s) from a path-, user- or host-pattern-scoped http.extraheader entry that agent-id cannot reset; remove that entry", hh.Host, hh.Name)
	}
	return nil
}

func emptyDir() string { return "/" }

var remoteName = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// remotes checks every remote of the repository that dir is in: O1 (SSH),
// D2 (password in the URL), and D3 (an extra header that survives).
func (c *remoteChecker) remotes(ctx context.Context, dir string, child []string) error {
	out, err := c.git(ctx, dir, child, "rev-parse", "--is-inside-work-tree")
	var exit *exec.ExitError
	if errors.Is(err, exec.ErrNotFound) || errors.As(err, &exit) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != "true" {
		return nil
	}
	entries, ok, err := c.configList(ctx, dir, child)
	if err != nil || !ok {
		return err
	}
	names, err := c.git(ctx, dir, child, "remote")
	if err != nil {
		return checkFailedOr(err)
	}
	for _, name := range strings.Fields(string(names)) {
		if err := c.checkRemote(ctx, dir, child, entries, name); err != nil {
			return err
		}
	}
	return nil
}

func checkFailedOr(err error) error {
	if errors.Is(err, ErrRemoteCheck) {
		return err
	}
	return checkFailed("git failed")
}

type remoteURL struct {
	raw  string
	push bool
}

func (c *remoteChecker) checkRemote(ctx context.Context, dir string, child []string, entries []ConfigEntry, name string) error {
	if !remoteName.MatchString(name) || strings.HasPrefix(name, "-") {
		return checkFailed("a remote has an unusual name")
	}
	fetch, err := c.git(ctx, dir, child, "ls-remote", "--get-url", name)
	if err != nil {
		return checkFailedOr(err)
	}
	push, err := c.git(ctx, dir, child, "remote", "get-url", "--push", "--all", name)
	if err != nil {
		return checkFailedOr(err)
	}
	urls := []remoteURL{{strings.TrimSpace(string(fetch)), false}}
	for _, line := range strings.Split(strings.TrimSpace(string(push)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, remoteURL{line, true})
		}
	}
	for _, u := range urls {
		if err := c.checkURL(ctx, dir, child, entries, name, u); err != nil {
			return err
		}
	}
	return nil
}

func (c *remoteChecker) checkURL(ctx context.Context, dir string, child []string, entries []ConfigEntry, name string, u remoteURL) error {
	class := ClassifyRemoteURL(u.raw, c.a.GitHosts())
	switch class.Kind {
	case KindHTTP:
		if !class.Provider {
			return nil
		}
		if HasPassword(u.raw) {
			return failf(ErrRemoteCredentials, "remote %q has a password in its URL; remove it (git remote set-url %s %s)", name, name, Redact(u.raw))
		}
		for _, h := range EffectiveHeaders(entries, u.raw) {
			return failf(ErrRemoteHeader, "remote %q gets an extra HTTP header (%s) that agent-id cannot reset; remove that http.<url>.extraheader entry", name, h)
		}
		return nil
	case KindSCP, KindSSH:
		form := map[URLKind]string{KindSCP: "scp form", KindSSH: "ssh://"}[class.Kind]
		if !class.Provider {
			alias, err := c.resolveAlias(ctx, dir, child, class.Host)
			if err != nil {
				return err
			}
			if alias == "" {
				return nil
			}
			form = fmt.Sprintf("alias %q", clean(class.Host, 40))
		}
		through := ""
		if e, ok := RuleFor(entries, u.raw, u.push); ok {
			through = ", through a url rewrite rule from " + e.source()
		}
		return failf(ErrSSHRemote, "remote %q reaches %s over SSH (%s)%s; the agent would use your SSH key; use an HTTPS remote (agent-id doctor shows details)", name, c.a.Host(), form, through)
	}
	return nil
}

// resolveAlias asks ssh -G for the host name behind an SSH host alias and
// returns it when it is one of the provider's hosts, otherwise "".
func (c *remoteChecker) resolveAlias(ctx context.Context, dir string, child []string, host string) (string, error) {
	if !validAlias(host) {
		return "", checkFailed("an SSH remote has a host that cannot be checked")
	}
	out, err := c.run.Run(ctx, dir, child, "ssh", "-G", "--", host)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", checkFailed("timeout")
		}
		return "", checkFailed("ssh -G failed")
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		if strings.EqualFold(key, "hostname") {
			for _, h := range c.a.GitHosts() {
				if strings.EqualFold(strings.TrimSpace(value), h) {
					return h, nil
				}
			}
		}
	}
	return "", nil
}

// withoutRepoVars returns env without the variables that point Git at a
// repository, so that the listing outside any repository really is one.
func withoutRepoVars(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_WORK_TREE=")
	})
}
