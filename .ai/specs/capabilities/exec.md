# Capability: exec

`agent-id exec --profile <name> [--] <command> [args…]` starts one command, and only its process tree, in the agent context of a profile. Implemented by `internal/activation` and `internal/activation/exec` (#20, #68). In this version the child holds no App token: it is closed to the developer's credentials (fail closed), and the bot's Git and `gh` credentials come with #22 and #23.

## Requirement: Only the command's process tree is in agent mode
The system SHALL start the command with the child environment of the resolved profile and SHALL NOT change the environment, the files, or the configuration of the invoking process.

### Scenario: Parent unchanged
- WHEN `agent-id exec --profile claude -- sh -c 'exit 0'` runs in an isolated environment
- THEN the invoking environment is unchanged and no file under the isolated `HOME` is created except agent-id's own directories under `<cache>/agent-id/<provider>/`

### Scenario: Marker in the child only
- WHEN the command prints `$AGENT_ID_ACTIVE_PROFILE`
- THEN it prints `claude`, and the invoking process has no such variable

## Requirement: Exit status and signals belong to the command
The system SHALL replace its own process with the command (`execve`).

### Scenario: Exit status, SIGTERM, SIGKILL
- WHEN the command exits with 7, traps SIGTERM with exit 42, or is killed by SIGKILL
- THEN the observed status is 7, 42, or termination by SIGKILL

## Requirement: agent-id's own failures use 125–127
The system SHALL exit with 125 before the command starts, 126 when it cannot be executed, 127 when it is not found, with one line `agent-id: exec: <message>` on stderr.

### Scenario: Not found, missing profile flag, unknown flag
- WHEN the command is `does-not-exist`, `--profile` is missing, or a flag is unknown
- THEN the status is 127, 125, and 125

## Requirement: Nested exec
The system SHALL rebuild the environment idempotently for the same profile and SHALL refuse a different profile, a damaged context, and an unknown contract version (125, naming both profiles).

### Scenario: Same profile, different profile, unknown version
- WHEN `exec --profile claude` runs inside a `claude` context, inside another profile's context, or with `AGENT_ID_CONTEXT=2`
- THEN the environment is identical without duplicate `GIT_CONFIG_*` entries, 125 naming both profiles, and 125 `incompatible agent-id`

## Requirement: Inherited Git process configuration
The system SHALL refuse a malformed `GIT_CONFIG_COUNT` set and a `GIT_CONFIG_PARAMETERS` that sets a `credential`, `url`, `http`, `gpg`, `include`, `includeIf`, signing key or `core.askPass`, and keep any other.

## Requirement: Fail closed from the first merge (#68)
The system SHALL start the command only with the neutral credential baseline.

### Scenario: The child does not see the developer's credentials
- WHEN the isolated global and repository Git configuration set a recording credential helper and an askpass, the parent's `GH_CONFIG_DIR` holds a fake `hosts.yml`, and the parent has fake `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`
- THEN `git credential fill` prints nothing and fails without a prompt, `git ls-remote` sends no credentials, `gh` by name is the refusing stub, the real `gh auth token` prints the decoy, no helper, askpass or `gh` records a call, and no developer value is in the environment

### Scenario: Refusals
- WHEN a remote resolves to SSH (scp form, `ssh://`, `pushurl`, an `insteadOf` rewrite), a remote URL has a password, an extra header survives for the provider's host, or `~/.netrc` has credentials for it
- THEN the status is 125 and the message names the remote or file and the configuration source, never a secret

### Scenario: Neutral directories are checked
- WHEN `cli-config` or `neutral-bin` is a symlink, is group-writable, holds `hosts.yml`, or the stub is a symlink
- THEN the status is 125 and nothing is changed

## Requirement: Unresolved identity
The system SHALL refuse a profile that its provider reports as not ready, with the provider's hint and without any network call.

Source: .ai/specs/2026-11-13-exec-child-environment.md
