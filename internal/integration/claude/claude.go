// Package claude holds the pure functions of the Claude Code integration: the
// guarded block that a SessionStart hook appends to CLAUDE_ENV_FILE, and the
// refusal line that makes every Bash command of a session stop when
// activation failed (spec .ai/specs/2026-11-13-env-claude-session-start.md,
// #26). It names no forge, and it never edits Claude settings: agent-id
// writes only a guarded block or a refusal line, and only to the file that
// Claude Code names.
package claude

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Markers of the shared text contract, version 1.
const (
	BlockStart  = "# agent-id session-start 1 profile="
	BlockEnd    = "# agent-id session-start end"
	RefusalMark = "# agent-id refusal 1"
)

// MaxReason is the length, in characters, to which a refusal reason is cut.
const MaxReason = 200

var profileName = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`)

// ValidProfile reports whether name matches the profile name rule, which is
// shell-safe.
func ValidProfile(name string) bool { return profileName.MatchString(name) }

var blockProfile = regexp.MustCompile(`(?m)^# agent-id env 1 profile=([a-z][a-z0-9-]*)$`)

// GuardedBlock wraps block, the output of activation.Render, so that the
// shell running it stops with exit status 125 when any statement fails.
// refusal is the shell text that reports the failure; use GuardRefusal.
//
// The block runs inside eval. In zsh, a read-only variable error in a sourced
// file aborts only that file, and the command then runs unactivated; inside
// eval the error is a failed status that the guard catches (tested on zsh
// 5.9: the same error in a { } group or a function does not reach the guard
// when the file is sourced).
func GuardedBlock(block, refusal string) string {
	profile := ""
	if m := blockProfile.FindStringSubmatch(block); m != nil {
		profile = m[1]
	}
	if !strings.HasSuffix(block, "\n") {
		block += "\n"
	}
	return BlockStart + profile + "\neval " + quote(block) + " || { " + refusal + "; }\n" + BlockEnd + "\n"
}

// GuardRefusal is the failure text inside a guarded block.
func GuardRefusal(profile string) string {
	return "printf '%s\\n' 'agent-id: agent activation failed in this shell; the command was not run" + doctor(profile) + "' >&2; exit 125"
}

// RefusalLine returns the line that stops every later command of the session
// with exit status 125 and reason. The profile is named only when it matches
// the profile name rule; the reason is cut, cleaned of control characters,
// and single-quoted.
func RefusalLine(reason, profile string) string {
	return "printf '%s\\n' " + quote("agent-id: "+clean(reason)+"; the command was not run. Fix it and start a new session"+doctor(profile)) +
		" >&2; exit 125 " + RefusalMark + "\n"
}

func doctor(profile string) string {
	if !ValidProfile(profile) {
		return " (agent-id doctor)"
	}
	return " (agent-id doctor --profile " + profile + ")"
}

func clean(reason string) string {
	var b strings.Builder
	n := 0
	for i := 0; i < len(reason) && n < MaxReason; n++ {
		r, size := utf8.DecodeRuneInString(reason[i:])
		i += size
		if r == utf8.RuneError || unicode.IsControl(r) {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ProfilesIn returns the profile of every guarded block in content, in
// order. A block whose header names an invalid profile is reported as "?".
func ProfilesIn(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if p, ok := strings.CutPrefix(line, BlockStart); ok {
			if !ValidProfile(p) {
				p = "?"
			}
			out = append(out, p)
		}
	}
	return out
}

// HasBlock reports whether content already holds block, a GuardedBlock
// result, on whole lines.
func HasBlock(content, block string) bool {
	return strings.HasPrefix(content, block) || strings.Contains(content, "\n"+block)
}
