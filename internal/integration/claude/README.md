# Claude Code integration

`agent-id claude-code session-start` lets a plain `claude` start run its Bash commands as the agent's bot. It is the convenience layer. The canonical activation stays explicit: `agent-id exec --profile claude -- claude`. Spec: `.ai/specs/2026-11-13-env-claude-session-start.md` (#26).

## How it works

Claude Code runs the contents of `$CLAUDE_ENV_FILE` before every Bash command. A `SessionStart` hook runs `agent-id claude-code session-start --profile <name>`, which appends one guarded block to that file:

- the block is the output of `agent-id env --profile <name> --shell bash` (the child environment of `agent-id exec`, as shell text, with no token in it), chained so that it succeeds or fails as a whole;
- if any statement fails, the guard ends the command's shell with **exit status 125** and a message, so the command does not run;
- if activation fails at session start (unknown profile, no resolved identity, a nested context, an SSH remote), the hook appends a **refusal line** instead and exits 3: every Bash command of the session stops with 125 and the reason;
- running the hook again (`resume`, `clear`, `compact`, `fork`) appends nothing when the identical block is already there.

The block removes any shell function and, best effort, any alias named `git` or `gh`, and puts agent-id's `gh` stub first on `PATH`. In this version the bot has no App token for `gh`, and Git has no bot credential yet: both arrive with the credential helper and the `gh` shim (#22, #23). Until then the agent can neither use your personal login nor authenticate as the bot.

## The hook

agent-id never writes your Claude settings. Add this to `.claude/settings.local.json` of the repository (personal, not committed), and change `claude` to your profile name:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "agent-id claude-code session-start --profile claude; s=$?; case $s in 0|3|4) exit \"$s\" ;; esac; printf '%s\\n' \"printf '%s\\\\n' 'agent-id: the SessionStart hook could not run agent-id; the command was not run' >&2; exit 125 # agent-id refusal 1\" >> \"$CLAUDE_ENV_FILE\"; exit 1"
          }
        ]
      }
    ]
  }
}
```

- Status 0, 3 or 4 comes from agent-id itself: success, a refusal line it already wrote, or no safe or parseable file to write. Any other status (a usage error, an older agent-id without the command, not found, a crash) appends a fallback refusal line and exits 1, so Claude Code shows the error and every command stops with 125.
- There is no `matcher`: the hook runs for every `source`.
- **Location.** `.claude/settings.local.json` is personal. A team that standardizes on agent-id may commit the hook to `.claude/settings.json`; then every collaborator's Bash commands require agent-id and the same profile name, and fail closed without them.
- **Workspace trust.** In interactive use Claude Code runs no hook until you accept the folder's trust dialog. Until then the session is personal and unmanaged. With `-p` or the SDK, hooks from a repository's `.claude/settings.json` run without that dialog.
- There is no `agent-id claude-code install` in this version: you edit the file yourself.

## What is covered

- **Bash commands of the session only.** MCP servers and subagents are not covered; start Claude Code with `agent-id exec --profile <name> -- claude` for those.
- A Bash command that runs `agent-id exec --profile <name> -- …` with the same profile re-enters without duplicates; with another profile it is refused with an error that names both.
- A function or alias under another name that calls `gh`, another hook that appends `export GH_TOKEN=…` after agent-id's block, `env -i`, and `unset AGENT_ID_CONTEXT` are bypasses. **Local mode is not a sandbox or a security boundary**: it controls attribution and credentials on the supported paths.
- `agent-id doctor` and `agent-id whoami` (planned) run inside the session and report what the session really uses. `bash -n "$CLAUDE_ENV_FILE"` (or `zsh -n`) checks the env file from a hook when in doubt.

## Exit statuses of `session-start`

| Status | Meaning |
|---|---|
| 0 | the block was appended, or was already there |
| 3 | activation failed; a refusal line was appended |
| 4 | no safe or parseable file to write: `CLAUDE_ENV_FILE` is missing, not absolute, a symbolic link, writable by others, group-writable by a shared group, owned by someone else, or does not parse. Nothing is written; the session stays personal |

## Assumptions about Claude Code (verified by #27)

The documentation says the file runs before each Bash command and that `SessionStart` provides `CLAUDE_ENV_FILE`. This integration also assumes that the file runs in the command's own shell, so `exit 125` stops the command, and it does not know in which order Claude Code applies its shell snapshot. #27 verifies both on a real session.

## Contract

The block, the guarded block, the refusal line, their markers, the command name, and the hook snippet are contract version 1. A change needs a new version in the marker and a migration note here.
