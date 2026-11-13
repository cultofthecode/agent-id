# Capability: the `gh` shim

Spec: `.ai/specs/2026-11-13-gh-shim.md` (#23, E02-S09). Package: `internal/cliint`. Rules: `internal/provider/github/cli.go`.

### Requirement: Fresh bot token for each `gh` call

Inside an agent context, the system SHALL start the real `gh` with a token that the token manager returns for the profile, in `GH_TOKEN`, for that call only. The token SHALL NOT be in the environment of the agent, in output, in errors, or in `argv`.

##### Scenario: Two calls across a refresh
- WHEN the agent runs `gh pr create` and later `gh pr view`, and the cached token was replaced in between
- THEN the first call got the first token and the second call got the second token

##### Scenario: Personal tokens in the parent
- WHEN the parent has `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST` for another host, and a `GH_CONFIG_DIR` with a personal `hosts.yml`
- THEN `gh` sees only the bot token in `GH_TOKEN`, `GH_HOST=github.com`, no `GITHUB_TOKEN`, the decoy in `GH_ENTERPRISE_TOKEN`, and agent-id's empty `GH_CONFIG_DIR`

### Requirement: Fail closed

The system SHALL NOT start `gh` when no valid token can be obtained, and SHALL NOT run it with an empty token, the decoy, or the developer's login. It SHALL exit with 125 and one line that starts with a code and names no secret.

##### Scenario: The mint fails
- WHEN the provider answers 401, 403, 404, 429, or is unavailable, or returns an unusable token, or the token cache is not private, or the key cannot be loaded
- THEN `gh` does not start, the exit code is 125, stderr holds `AID-GH-TOKEN`, and a personal `gh` on a later `PATH` entry, a credential helper, and a `hosts.yml` record no use

##### Scenario: No agent-id binary
- WHEN `AGENT_ID_BIN` is unset or not executable and the launcher runs
- THEN it exits 125 with `AID-GH-TOKEN` and does not start another `gh`

### Requirement: Deny-list and host check

The system SHALL refuse, before it reads the key, `gh auth *`, the commands that need a user account, `@me`, repository names without an owner, and any host other than the profile's host in `--hostname`, `-R`, `--repo`, `GH_REPO`, or a URL argument.

##### Scenario: Denied commands
- WHEN the agent runs `gh auth token`, `gh api user`, `gh status`, `gh pr list --assignee @me`, `gh api --hostname ghe.example.com x`, or `gh pr view https://ghe.example.com/o/r/pull/1`
- THEN exit 125, one of `AID-GH-AUTH`, `AID-GH-USER`, `AID-GH-ME`, `AID-GH-HOST` on stderr, and `gh` does not start

### Requirement: No recursion

The system SHALL refuse when the real `gh` that it finds is the launcher (a link or hard link), and when a `gh` that the shim started starts `gh` through `PATH`.

##### Scenario: Link to the launcher
- WHEN the only `gh` besides the launcher directory is a symlink to the launcher
- THEN exit 125 with `AID-GH-LOOP`

##### Scenario: Nested call
- WHEN the real `gh` runs `gh` again through `PATH`
- THEN the inner call exits 125 with `AID-GH-LOOP` and the outer `gh` is unaffected
