# Capability: claude-code-session-start

`agent-id claude-code session-start --profile <name>` is a Claude Code `SessionStart` command hook that appends the guarded block to `CLAUDE_ENV_FILE`. Implemented by `internal/integration/claude` and its `sessionstart` slice (#26). Scenarios that need the bot's Git and `gh` credentials (a push that the server records with the installation token, `gh` seeing the App token) wait for #22 and #23; in this version the simulated session proves the bot author and committer, no personal token or helper use, and the refusing `gh` stub.

## Requirement: Append the guarded block once
The system SHALL append exactly one guarded activation block for the profile to `$CLAUDE_ENV_FILE`. It SHALL NOT append it again when an identical guarded block is already in the file, and SHALL NOT modify any other line of the file.

### Scenario: Startup then resume
- WHEN `session-start` runs with `{"hook_event_name":"SessionStart","source":"startup"}`, and again with `"source":"resume"`, on the same `CLAUDE_ENV_FILE`, which holds a line written by another hook
- THEN the file holds the other hook's line unchanged and exactly one guarded block

### Scenario: Commands run as the bot
- WHEN a simulated session runs commands as `bash -c '. "$CLAUDE_ENV_FILE"; <command>'`, and the same with `zsh -c`. The commands are `git commit`, `git push` to the local Git server of `internal/testenv`, and `gh api repos/o/r` through the `gh` shim against a fake `gh`
- THEN the commit author and committer are the bot, the server records the fake installation token, and the fake `gh` sees the fake App token and no personal token

## Requirement: Fail closed when activation fails
The system SHALL make every Bash command that runs the env file stop with exit status 125 when activation could not be established at session start.

### Scenario: Hook cannot activate
- WHEN `session-start` runs for a profile without a resolved identity
- THEN it appends a refusal line and exits 3; a simulated command `sh -c '. "$CLAUDE_ENV_FILE"; echo ran'` does not print `ran`, exits 125, and prints the `setup` hint

### Scenario: Hostile profile name
- WHEN the hook runs `session-start --profile "a';x"`
- THEN the refusal line names no profile, evaluating it runs no injected code, and the command exits 125

### Scenario: agent-id cannot run
- WHEN the documented hook command runs with a `PATH` that does not contain `agent-id`
- THEN its fallback appends a refusal line with the refusal marker, the hook exits 1 (any status other than 0, 3 or 4 from agent-id is treated the same, including an older agent-id without the command), and a simulated command exits 125

### Scenario: Not run as a hook
- WHEN `session-start` runs without `CLAUDE_ENV_FILE`
- THEN it exits 4 with a message that it must run as a Claude Code `SessionStart` command hook, and it writes no file

## Requirement: Shadowing aliases and functions are removed
The system SHALL remove, in each command's shell and before the command runs, any function named `git` or named after the provider's CLI, and SHALL attempt the same for aliases. An alias is removed reliably only when the env file and the command are parsed separately; `bash` expands an alias of a line that it has already parsed (assumption verified by #27).

### Scenario: gh function from .zshrc
- WHEN the simulated command's shell defines `gh() { GH_TOKEN=decoy command gh "$@"; }` before running the env file, and then runs `gh api repos/o/r`
- THEN the fake `gh` sees the App token, never `decoy`

## Requirement: Nested exec inside a session
The system SHALL keep the bot identity unchanged, and SHALL NOT duplicate any `GIT_CONFIG_*` entry or `PATH` element, when `agent-id exec` with the same profile runs inside an activated session.

### Scenario: Same profile nested
- WHEN a simulated command runs `agent-id exec --profile claude -- env` after the env file
- THEN `GIT_CONFIG_COUNT` and every `GIT_CONFIG_*` entry equal those that the block set, and the shim directory appears once in `PATH`
