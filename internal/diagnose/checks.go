package diagnose

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Failure is the outcome of one gatherer or provider call that did not work:
// a stable class and the error's own message, which names the path and the
// fix and never holds a secret.
type Failure struct {
	Class   string
	Message string
}

// failed reports whether f describes a failure.
func (f Failure) failed() bool { return f.Class != "" || f.Message != "" }

// text is the failure as one line.
func (f Failure) text() string {
	switch {
	case f.Class == "":
		return f.Message
	case f.Message == "":
		return f.Class
	}
	return f.Class + ": " + f.Message
}

// Snapshot holds what the gatherers collected. It has no field for a token,
// a key, or a credential value; only names, classes, flags and expiry.
type Snapshot struct {
	Profile  string
	Provider string
	Host     string

	Config   ConfigState
	Cache    CacheState
	Key      KeyState
	Token    TokenState
	Identity IdentityState
	Repo     RepoState
	Git      GitState
	Env      EnvState
}

// ConfigState is the outcome of loading the configuration and resolving the
// profile.
type ConfigState struct {
	Path string
	// Load is the failure to load the file, Profile the failure to resolve
	// the profile (not found, unsupported provider).
	Load    Failure
	Profile Failure
	// Unsupported names the profiles of providers this agent-id does not
	// support.
	Unsupported []string
}

// CacheState is the outcome of the read-only check of the token cache
// directory.
type CacheState struct {
	Exists bool
	Err    Failure
}

// KeyState is the outcome of loading the private key.
type KeyState struct {
	Skip string
	Err  Failure
}

// TokenState is the outcome of minting a token. ExpiresInMinutes is rounded
// down.
type TokenState struct {
	Skip             string
	Err              Failure
	ExpiresInMinutes int
}

// IdentityState is the outcome of comparing the cached identity with the
// host's bot.
type IdentityState struct {
	Skip       string
	Err        Failure
	RateLimit  bool
	Bot        string
	Match      bool
	Refreshed  bool
	RefreshErr string
}

// RepoState is the outcome of the repository access check.
type RepoState struct {
	Skip string
	Err  Failure
	// Missing names the repositories of the remotes that the installation
	// cannot read; Checked how many were looked at.
	Checked int
	Missing []string
}

// GitState is the outcome of running git --version.
type GitState struct {
	Err     string
	Version string
}

// EnvState holds the names and flags of the environment. A value is
// collected only for GH_HOST and GH_REPO.
type EnvState struct {
	// TokenLike names the variables whose value matches a token prefix, a
	// PEM block or a JWT shape.
	TokenLike []string
	GHHost    string
	GHRepo    string
}

// DeferredChecks are the catalog ids that wait for the exec, Git integration
// and gh shim work (#20, #22, #23). Run reports each as skip with the reason,
// in catalog order, so that a report never looks complete when it is not.
var DeferredChecks = map[string]string{
	"git.remote.ssh":       "needs the remote classification of the Git integration (#22)",
	"git.remote.userinfo":  "needs the remote classification of the Git integration (#22)",
	"git.extraheader":      "needs the Git integration (#22)",
	"git.parameters":       "needs agent-id exec (#20)",
	"git.helper.effective": "needs the Git integration (#22)",
	"git.helper.personal":  "needs the Git integration (#22)",
	"git.signing":          "needs the Git integration (#22)",
	"git.ssh":              "needs the Git integration (#22)",
	"git.askpass":          "needs the Git integration (#22)",
	"env.credential":       "needs the provider's credential variables from agent-id exec (#20)",
	"gh.binary":            "needs the gh shim (#23)",
	"context.state":        "needs agent-id exec (#20)",
	"context.git":          "needs agent-id exec (#20)",
}

// catalog is the output order of the check ids; ids in DeferredChecks are
// skip results until their work lands.
var catalog = []string{
	"config.load", "config.profile", "config.unsupported", "token.cache", "key.file",
	"provider.token", "provider.identity", "provider.repo", "git.version",
	"git.remote.ssh", "git.remote.userinfo", "git.extraheader", "git.parameters",
	"git.helper.effective", "git.helper.personal", "git.signing", "git.ssh", "git.askpass",
	"env.credential", "env.tokenlike", "gh.binary", "gh.host", "context.state", "context.git",
	"context.shell",
}

// Run evaluates every check over s and returns the results in catalog order.
// It is pure: it reads only s. A check that panics becomes a fail result with
// the id internal.
func Run(s Snapshot) []Result {
	checks := map[string]func(Snapshot) []Result{
		"config.load": checkConfigLoad, "config.profile": checkConfigProfile, "config.unsupported": checkUnsupported,
		"token.cache": checkTokenCache, "key.file": checkKey, "provider.token": checkToken,
		"provider.identity": checkIdentity, "provider.repo": checkRepo, "git.version": checkGitVersion,
		"env.tokenlike": checkTokenLike, "gh.host": checkGHHost, "context.shell": checkShell,
	}
	var results []Result
	for _, id := range catalog {
		if reason, ok := DeferredChecks[id]; ok {
			results = append(results, Result{ID: id, Status: StatusSkip, Title: "not checked in this version", Detail: reason})
			continue
		}
		results = append(results, safely(id, checks[id], s)...)
	}
	return results
}

// safely runs check and turns a panic into a fail result.
func safely(id string, check func(Snapshot) []Result, s Snapshot) (results []Result) {
	defer func() {
		if recover() != nil {
			results = []Result{{ID: "internal", Status: StatusFail, Title: "check " + id + " failed internally", Fix: "this is a bug; please report it without the output"}}
		}
	}()
	return check(s)
}

func checkConfigLoad(s Snapshot) []Result {
	if s.Config.Load.failed() {
		return []Result{{ID: "config.load", Status: StatusFail, Title: "configuration cannot be loaded", Detail: s.Config.Load.text(), Fix: "fix the file as the message says, or run agent-id setup <provider> --profile " + s.Profile}}
	}
	return []Result{{ID: "config.load", Status: StatusOK, Title: "configuration " + s.Config.Path + " loaded"}}
}

func checkConfigProfile(s Snapshot) []Result {
	switch {
	case s.Config.Load.failed():
		return []Result{skip("config.profile", "the configuration did not load")}
	case s.Config.Profile.failed():
		return []Result{{ID: "config.profile", Status: StatusFail, Title: "profile cannot be used", Detail: s.Config.Profile.text(), Fix: "create it with agent-id setup <provider> --profile " + s.Profile + ", or pick an existing profile"}}
	}
	return []Result{{ID: "config.profile", Status: StatusOK, Title: "profile " + s.Profile + " resolves to " + s.Provider + " on " + s.Host}}
}

func checkUnsupported(s Snapshot) []Result {
	if s.Config.Load.failed() {
		return []Result{skip("config.unsupported", "the configuration did not load")}
	}
	if len(s.Config.Unsupported) == 0 {
		return []Result{{ID: "config.unsupported", Status: StatusOK, Title: "every profile belongs to a supported provider"}}
	}
	return []Result{{ID: "config.unsupported", Status: StatusInfo, Title: "profiles of providers this agent-id does not support", Detail: strings.Join(s.Config.Unsupported, ", ")}}
}

func checkTokenCache(s Snapshot) []Result {
	switch {
	case s.Cache.Err.failed():
		return []Result{{ID: "token.cache", Status: StatusFail, Title: "the token cache is not private", Detail: s.Cache.Err.text(), Fix: "agent-id refuses to use it; fix the directory as the message says"}}
	case !s.Cache.Exists:
		return []Result{skip("token.cache", "the token cache does not exist yet")}
	}
	return []Result{{ID: "token.cache", Status: StatusOK, Title: "token cache directory is private"}}
}

func checkKey(s Snapshot) []Result {
	switch {
	case s.Key.Skip != "":
		return []Result{skip("key.file", s.Key.Skip)}
	case s.Key.Err.failed():
		return []Result{{ID: "key.file", Status: StatusFail, Title: "the private key cannot be used", Detail: s.Key.Err.text(), Fix: "fix the key file as the message says; the key must be a regular file of yours with mode 0600 or stricter"}}
	}
	return []Result{{ID: "key.file", Status: StatusOK, Title: "private key: regular file, mode 0600 or stricter, parses"}}
}

func checkToken(s Snapshot) []Result {
	switch {
	case s.Token.Skip != "":
		return []Result{skip("provider.token", s.Token.Skip)}
	case s.Token.Err.failed():
		return []Result{{ID: "provider.token", Status: StatusFail, Title: "no installation token can be minted", Detail: s.Token.Err.text(), Fix: tokenFix(s.Token.Err.Class)}}
	}
	return []Result{{ID: "provider.token", Status: StatusOK, Title: fmt.Sprintf("installation token minted (expires in %dm)", s.Token.ExpiresInMinutes)}}
}

// tokenFix is the advice for a failed mint by its class.
func tokenFix(class string) string {
	switch class {
	case ClassUnauthorized:
		return "the host rejected the App's credentials; check the App ID and that the private key is the App's current key"
	case ClassForbidden:
		return "the host denied access; check the App's permissions and the installation"
	case ClassNotFound:
		return "the installation does not exist or the App lost access; reinstall the App and run agent-id setup again"
	case ClassRateLimited:
		return "the host is rate limiting this App; wait and try again"
	case ClassKey:
		return "fix the private key as the key.file result says"
	}
	return "check the network and the host's status, then try again"
}

func checkIdentity(s Snapshot) []Result {
	id := s.Identity
	switch {
	case id.Skip != "":
		return []Result{skip("provider.identity", id.Skip)}
	case id.RateLimit:
		return []Result{{ID: "provider.identity", Status: StatusWarn, Title: "the host rate-limits the identity lookup", Detail: id.Err.text(), Fix: "try again later; the cached identity is unchanged"}}
	case id.Err.failed():
		return []Result{{ID: "provider.identity", Status: StatusFail, Title: "the bot identity cannot be resolved", Detail: id.Err.text(), Fix: tokenFix(id.Err.Class)}}
	case id.RefreshErr != "":
		return []Result{{ID: "provider.identity", Status: StatusWarn, Title: id.Bot + " differs from the cached identity, and the cache could not be refreshed", Detail: id.RefreshErr, Fix: "make the configuration file writable, or run agent-id setup again"}}
	case id.Refreshed:
		return []Result{{ID: "provider.identity", Status: StatusWarn, Title: id.Bot + " refreshed in the cached identity", Fix: "none needed: the cache was missing or stale and doctor updated it"}}
	case id.Match:
		return []Result{{ID: "provider.identity", Status: StatusOK, Title: id.Bot + " matches the cached identity"}}
	}
	return []Result{skip("provider.identity", "no identity answer")}
}

func checkRepo(s Snapshot) []Result {
	r := s.Repo
	switch {
	case r.Skip != "":
		return []Result{skip("provider.repo", r.Skip)}
	case r.Err.failed():
		return []Result{{ID: "provider.repo", Status: StatusFail, Title: "the installation's repositories cannot be listed", Detail: r.Err.text(), Fix: tokenFix(r.Err.Class)}}
	}
	var results []Result
	for _, name := range r.Missing {
		results = append(results, Result{ID: "provider.repo", Status: StatusFail, Title: "the installation cannot read " + name, Fix: "add the repository to the App installation"})
	}
	if len(results) == 0 {
		return []Result{{ID: "provider.repo", Status: StatusOK, Title: fmt.Sprintf("installation can read %d of %d repositories of the remotes", r.Checked, r.Checked)}}
	}
	return results
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)`)

// The oldest Git that agent-id supports (decision O4).
const minGitMajor, minGitMinor = 2, 43

func checkGitVersion(s Snapshot) []Result {
	if s.Git.Err != "" {
		return []Result{{ID: "gather.git", Status: StatusFail, Title: "git cannot be run", Detail: s.Git.Err, Fix: "install Git 2.43 or newer and put it on PATH"}}
	}
	m := versionPattern.FindStringSubmatch(s.Git.Version)
	if m == nil {
		return []Result{{ID: "git.version", Status: StatusFail, Title: "the Git version is not recognized", Fix: "install Git 2.43 or newer"}}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if major < minGitMajor || major == minGitMajor && minor < minGitMinor {
		return []Result{{ID: "git.version", Status: StatusFail, Title: "git " + m[0] + " is older than 2.43", Fix: "upgrade Git to 2.43 or newer"}}
	}
	return []Result{{ID: "git.version", Status: StatusOK, Title: "git " + m[0]}}
}

func checkTokenLike(s Snapshot) []Result {
	if len(s.Env.TokenLike) == 0 {
		return []Result{{ID: "env.tokenlike", Status: StatusOK, Title: "no environment variable holds a token-shaped value"}}
	}
	var results []Result
	for _, name := range s.Env.TokenLike {
		results = append(results, Result{ID: "env.tokenlike", Status: StatusWarn, Title: name + " holds a token-shaped value", Source: "environment", Fix: "the agent inherits it unless agent-id exec drops it, and exec drops only the provider's credential variables (#20); unset it in the shell that starts the agent if the agent must not see it"})
	}
	return results
}

func checkGHHost(s Snapshot) []Result {
	var results []Result
	for _, v := range []struct{ name, value string }{{"GH_HOST", s.Env.GHHost}, {"GH_REPO", hostOfRepo(s.Env.GHRepo)}} {
		if v.value != "" && !strings.EqualFold(v.value, s.Host) {
			results = append(results, Result{ID: "gh.host", Status: StatusWarn, Title: v.name + " names the host " + v.value + ", not " + s.Host, Source: "environment", Fix: "unset " + v.name + " or use the profile's host; gh would talk to another host"})
		}
	}
	if len(results) == 0 {
		return []Result{{ID: "gh.host", Status: StatusOK, Title: "GH_HOST and GH_REPO do not name another host"}}
	}
	return results
}

// hostOfRepo returns the host of a GH_REPO value [HOST/]OWNER/REPO, or "".
func hostOfRepo(repo string) string {
	parts := strings.Split(repo, "/")
	if len(parts) == 3 {
		return parts[0]
	}
	return ""
}

func checkShell(Snapshot) []Result {
	return []Result{{ID: "context.shell", Status: StatusInfo, Title: "shell functions and aliases named git or gh are invisible to a child process", Fix: "run `type git gh` in the agent's shell to see which ones it gets"}}
}

// skip returns a skip result for id with its reason.
func skip(id, reason string) Result {
	return Result{ID: id, Status: StatusSkip, Title: "not checked", Detail: reason}
}
