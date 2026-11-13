package activation

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cultofthecode/agent-id/internal/profile"
)

// BlockVersion is the contract version of the env block.
const BlockVersion = "1"

// ErrUnrenderable is returned when the activation cannot be printed as shell
// text without risk: an invalid name or a value that looks like a secret. The
// message names the variable, never the value.
var ErrUnrenderable = errors.New("activation: the environment cannot be rendered as shell text")

var commandName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Render returns the activation block: one chain of statements, joined with
// && so that it succeeds or fails as a whole, that turns the environment
// parent into a.Env when evaluated in bash or zsh. It is pure, quotes every
// value literally, and contains no token.
//
// The order is the comment, one unset, the unshadow group, one export per
// changed variable other than PATH, the PATH group, and the cleanup.
func Render(parent []string, a Activation) (string, error) {
	pm, cm := parseEnv(parent), parseEnv(a.Env)
	profileOf := cm[VarProfile]
	if !profileName.MatchString(profileOf) {
		return "", unrenderable("an invalid profile name")
	}
	stmts, err := renderStatements(pm, cm, a)
	if err != nil {
		return "", err
	}
	head := "# agent-id env " + BlockVersion + " profile=" + profileOf + "\n"
	return head + strings.Join(stmts, " &&\n") + "\n", nil
}

func renderStatements(pm, cm map[string]string, a Activation) ([]string, error) {
	unsets, err := unsetNames(pm, cm, a.Drop)
	if err != nil {
		return nil, err
	}
	stmts := []string{"unset " + strings.Join(unsets, " ")}
	group, err := unshadowGroup(a.Unshadow)
	if err != nil {
		return nil, err
	}
	stmts = append(stmts, group)
	exports, err := exportStatements(pm, cm, a.TokenPrefixes)
	if err != nil {
		return nil, err
	}
	stmts = append(stmts, exports...)
	if len(a.PathPrepend) > 0 {
		path, err := pathStatements(a.PathPrepend, a.TokenPrefixes)
		if err != nil {
			return nil, err
		}
		stmts = append(stmts, path...)
	}
	return stmts, nil
}

func unsetNames(pm, cm map[string]string, drop []string) ([]string, error) {
	set := map[string]bool{}
	for _, n := range drop {
		set[n] = true
	}
	for n := range pm {
		if _, ok := cm[n]; !ok {
			set[n] = true
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		if !varName.MatchString(n) {
			return nil, unrenderable("an invalid variable name")
		}
		names = append(names, n)
	}
	slices.Sort(names)
	if len(names) == 0 {
		return nil, unrenderable("nothing to unset")
	}
	return names, nil
}

func unshadowGroup(names []string) (string, error) {
	for _, n := range names {
		if !commandName.MatchString(n) {
			return "", unrenderable("an invalid command name")
		}
	}
	list := strings.Join(names, " ")
	if len(names) == 0 {
		return "true", nil
	}
	return "{ unalias " + list + " 2>/dev/null; unset -f " + list + " 2>/dev/null; true; }", nil
}

func exportStatements(pm, cm map[string]string, prefixes []string) ([]string, error) {
	names := make([]string, 0, len(cm))
	for n, v := range cm {
		if n == "PATH" {
			continue
		}
		if old, ok := pm[n]; ok && old == v {
			continue
		}
		names = append(names, n)
	}
	slices.Sort(names)
	out := make([]string, 0, len(names))
	for _, n := range names {
		if !varName.MatchString(n) {
			return nil, unrenderable("an invalid variable name")
		}
		if profile.LooksSecret(cm[n], prefixes) {
			return nil, unrenderable("a forbidden value in " + n)
		}
		out = append(out, "export "+n+"="+shellQuote(cm[n]))
	}
	return out, nil
}

func pathStatements(dirs, prefixes []string) ([]string, error) {
	out := []string{}
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if !strings.HasPrefix(d, "/") || strings.Contains(d, ":") || profile.LooksSecret(d, prefixes) {
			return nil, unrenderable("an invalid PATH directory")
		}
		out = append(out, pathGroup(d)...)
	}
	return out, nil
}

func pathGroup(dir string) []string {
	return []string{
		"__agent_id_dir=" + shellQuote(dir),
		`__agent_id_path=":$PATH:"`,
		`while case $__agent_id_path in *":$__agent_id_dir:"*) true ;; *) false ;; esac; do __agent_id_path="${__agent_id_path%%":$__agent_id_dir:"*}:${__agent_id_path#*":$__agent_id_dir:"}"; done`,
		`__agent_id_path="${__agent_id_path#:}" && __agent_id_path="${__agent_id_path%:}"`,
		`export PATH="$__agent_id_dir${__agent_id_path:+:$__agent_id_path}"`,
		"unset __agent_id_path __agent_id_dir",
	}
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func unrenderable(what string) error {
	return fmt.Errorf("%w: %s", ErrUnrenderable, what)
}
