package github

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/cultofthecode/agent-id/internal/provider"
)

// Codes of the refusals of the gh shim (#23, spec
// .ai/specs/2026-11-13-gh-shim.md). The core refusals of the shim (token,
// missing CLI, recursion, context) carry the same prefix.
const (
	codeAuth = "AID-GH-AUTH"
	codeUser = "AID-GH-USER"
	codeMe   = "AID-GH-ME"
	codeHost = "AID-GH-HOST"
)

// ghContext is what the messages of the rules name: the profile and the bot.
type ghContext struct {
	profile string
	bot     string
}

// CheckCLI implements provider.Activator: the deny-list of user-centric gh
// commands and the host check of the arguments and of GH_REPO.
func (a *activator) CheckCLI(args, parent []string) error {
	bot := "the App bot"
	if a.settings.Identity != nil {
		bot = a.settings.Identity.Login
	}
	return checkGH(ghContext{profile: a.name, bot: bot}, args, parent)
}

// checkGH is the pure decision. It returns nil or a *provider.CLIRefusal.
func checkGH(c ghContext, args, parent []string) error {
	if len(args) > 0 && args[0] == "help" {
		return nil
	}
	if err := checkCommand(c, args); err != nil {
		return err
	}
	if err := checkMe(c, args); err != nil {
		return err
	}
	return checkHosts(c, args, parent)
}

var (
	userCommands = map[string]bool{
		"status": true, "gist": true, "org": true, "ssh-key": true, "gpg-key": true,
		"codespace": true, "cs": true, "agent-task": true,
		"extension": true, "extensions": true, "ext": true,
	}
	harmlessLeading = map[string]bool{"--help": true, "-h": true, "--version": true}
	wordPattern     = regexp.MustCompile(`^[a-z][a-z-]{0,31}$`)
	hostPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	schemePattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
)

// valueFlags take a separate value, so that value is no positional argument.
var valueFlags = map[string]bool{
	"--json": true, "-q": true, "--jq": true, "-t": true, "--template": true,
	"-b": true, "--branch": true, "--limit": true, "-L": true, "--visibility": true,
	"--topic": true, "--language": true, "--source": true, "-R": true, "--repo": true,
	"--hostname": true, "-X": true, "--method": true, "-H": true, "--header": true,
	"-f": true, "--raw-field": true, "-F": true, "--field": true, "--input": true,
	"--cache": true, "--preview": true, "-p": true, "--org": true, "--owner": true,
}

func checkCommand(c ghContext, args []string) error {
	if len(args) == 0 {
		return nil
	}
	first := args[0]
	if strings.HasPrefix(first, "-") {
		if harmlessLeading[first] {
			return nil
		}
		return &provider.CLIRefusal{Code: codeUser, Message: fmt.Sprintf("gh takes no option before the command, and agent-id cannot tell which command this is (profile %s). Put the command first, for example \"gh pr list\".", c.profile)}
	}
	switch {
	case first == "auth":
		return refuseAuth(c, args)
	case userCommands[first]:
		return refuseUser(c, quoteCommand(args[:1]))
	case first == "repo":
		return checkRepo(c, args)
	case first == "api":
		return checkAPI(c, args)
	}
	return nil
}

func checkRepo(c ghContext, args []string) error {
	if len(args) < 2 {
		return nil
	}
	rest := positionals(args[2:])
	switch args[1] {
	case "create", "fork":
		return refuseUser(c, quoteCommand(args[:2]))
	case "list":
		if len(rest) == 0 {
			return refuseUser(c, quoteCommand(args[:2])+" without an owner")
		}
	case "view", "clone":
		if len(rest) > 0 && !strings.Contains(rest[0], "/") && !schemePattern.MatchString(rest[0]) {
			return refuseMe(c)
		}
	}
	return nil
}

func checkAPI(c ghContext, args []string) error {
	rest := positionals(args[1:])
	if len(rest) == 0 {
		return nil
	}
	endpoint := rest[0]
	if schemePattern.MatchString(endpoint) {
		u, err := url.Parse(endpoint)
		if err != nil {
			return refuseHost(c, "")
		}
		endpoint = u.Path
	} else if i := strings.IndexAny(endpoint, "?#"); i >= 0 {
		endpoint = endpoint[:i]
	}
	endpoint = strings.ToLower(path.Clean("/" + strings.TrimLeft(endpoint, "/")))
	endpoint = strings.TrimPrefix(endpoint, "/")
	if endpoint == "user" || strings.HasPrefix(endpoint, "user/") || endpoint == "app" || strings.HasPrefix(endpoint, "app/") {
		return refuseUser(c, "\"gh api "+strings.SplitN(endpoint, "/", 2)[0]+"\"")
	}
	return nil
}

// positionals returns the arguments that are no flag and no flag value.
func positionals(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			out = append(out, args[i+1:]...)
			return out
		case valueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
		}
	}
	return out
}

func checkMe(c ghContext, args []string) error {
	for _, a := range args {
		v := a
		if i := strings.Index(v, "="); i >= 0 && strings.HasPrefix(v, "-") {
			v = v[i+1:]
		}
		for _, item := range strings.Split(v, ",") {
			item = strings.ToLower(item)
			if item == "@me" || strings.HasPrefix(item, "-") && strings.HasSuffix(item, "@me") {
				return refuseMe(c)
			}
		}
	}
	return nil
}

func checkHosts(c ghContext, args, parent []string) error {
	for _, kv := range parent {
		if v, ok := strings.CutPrefix(kv, "GH_REPO="); ok && v != "" {
			if err := checkRepoValue(c, v); err != nil {
				return err
			}
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if flag, v, ok := hostFlagValue(args, i); ok {
			if flag == "--hostname" {
				if err := checkHostName(c, v); err != nil {
					return err
				}
			} else if err := checkRepoValue(c, v); err != nil {
				return err
			}
			continue
		}
		if err := checkPositionalHost(c, a); err != nil {
			return err
		}
	}
	return nil
}

// hostFlagValue returns the flag and its value when args[i] is --hostname,
// -R, or --repo, in the separate, "=", or attached form.
func hostFlagValue(args []string, i int) (flag, value string, ok bool) {
	a := args[i]
	for _, f := range []string{"--hostname", "--repo"} {
		if a == f && i+1 < len(args) {
			return f, args[i+1], true
		}
		if v, found := strings.CutPrefix(a, f+"="); found {
			return f, v, true
		}
	}
	if a == "-R" && i+1 < len(args) {
		return "--repo", args[i+1], true
	}
	if v, found := strings.CutPrefix(a, "-R"); found && v != "" && !strings.HasPrefix(v, "-") {
		return "--repo", strings.TrimPrefix(v, "="), true
	}
	return "", "", false
}

func checkHostName(c ghContext, name string) error {
	if hostAllowed(name) {
		return nil
	}
	return refuseHost(c, name)
}

func checkRepoValue(c ghContext, v string) error {
	if schemePattern.MatchString(v) || strings.HasPrefix(v, "//") {
		return checkURLHost(c, v)
	}
	parts := strings.Split(v, "/")
	if len(parts) == 3 {
		return checkHostName(c, parts[0])
	}
	return nil
}

func checkPositionalHost(c ghContext, a string) error {
	if strings.HasPrefix(a, "//") || schemePattern.MatchString(a) {
		return checkURLHost(c, a)
	}
	if strings.HasPrefix(a, "-") {
		return nil
	}
	parts := strings.Split(a, "/")
	if len(parts) == 3 && hostPattern.MatchString(strings.ToLower(parts[0])) {
		return checkHostName(c, parts[0])
	}
	return nil
}

func checkURLHost(c ghContext, raw string) error {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Port() != "" {
		return refuseHost(c, "")
	}
	return checkHostName(c, u.Hostname())
}

// hostAllowed reports whether name is the profile's host or one of the two
// API hosts that gh maps to it. The comparison is exact, so a port, a
// trailing dot, a user-info, or a look-alike never matches.
func hostAllowed(name string) bool {
	switch strings.ToLower(name) {
	case host, "api." + host, "uploads." + host:
		return true
	}
	return false
}

func refuseAuth(c ghContext, args []string) error {
	what := "gh auth"
	if len(args) > 1 {
		what = quoteCommand(args[:2])
	}
	return &provider.CLIRefusal{Code: codeAuth, Message: fmt.Sprintf("%s is managed by agent-id in agent mode (profile %s, identity %s). Use \"agent-id whoami\" to see the active identity and \"agent-id doctor\" to check credentials.", what, c.profile, c.bot)}
}

func refuseUser(c ghContext, what string) error {
	return &provider.CLIRefusal{Code: codeUser, Message: fmt.Sprintf("%s needs a personal GitHub account and does not work with the GitHub App identity %s (profile %s). Run it in your own terminal, outside the agent context.", what, c.bot, c.profile)}
}

func refuseMe(c ghContext) error {
	return &provider.CLIRefusal{Code: codeMe, Message: fmt.Sprintf("\"@me\" and repository names without an owner resolve to the App identity %s (profile %s), not to a person. Name the user or the OWNER/REPO explicitly.", c.bot, c.profile)}
}

func refuseHost(c ghContext, requested string) error {
	shown := "another host"
	if hostPattern.MatchString(strings.ToLower(requested)) && len(requested) <= 253 {
		shown = strings.ToLower(requested)
	}
	return &provider.CLIRefusal{Code: codeHost, Message: fmt.Sprintf("profile %s is configured for %s, and gh was asked to use %s. agent-id does not send another host's credentials from the agent context.", c.profile, host, shown)}
}

// quoteWord returns word in quotes when it is a plain command word, and a
// neutral name otherwise, so that no argument text reaches a message.
func quoteWord(word string) string {
	if wordPattern.MatchString(word) {
		return word
	}
	return "<command>"
}

func quoteCommand(words []string) string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = quoteWord(w)
	}
	return "\"gh " + strings.Join(out, " ") + "\""
}
