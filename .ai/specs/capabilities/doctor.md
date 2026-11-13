# Capability: doctor

`agent-id doctor --profile <name> [--json]` runs a fixed catalog of checks with stable ids and prints each result with a `fix:` line. Implemented by `internal/diagnose/doctor` and `internal/diagnose` (#24), in part.

**Delivered:** `config.load`, `config.profile`, `config.unsupported`, `token.cache`, `key.file`, `provider.token`, `provider.identity` (with the identity refresh), `provider.repo`, `git.version`, `env.tokenlike`, `gh.host`, `context.shell`, and the exit codes.

**Deferred** (reported as `skip` with the reason, `diagnose.DeferredChecks`): `git.remote.ssh`, `git.remote.userinfo`, `git.extraheader`, `git.helper.effective`, `git.helper.personal`, `git.signing`, `git.ssh`, `git.askpass` (the Git integration, #22), `git.parameters`, `context.state`, `context.git` (exec, #20), `env.credential` (the provider's `CredentialVariables`, #20), and `gh.binary` (the `gh` shim, #23). Each is a table row to add with its test when the dependency merges.

## Requirement: Checks report with a fix
The system SHALL report each check of the catalog with its id, status, and a `fix:` line for every `warn` and `fail`.

### Scenario: Inherited token-shaped variable
- WHEN `doctor --profile claude` runs with `GH_TOKEN` holding a token and `GH_HOST` naming another host
- THEN `env.tokenlike` and `gh.host` are `warn` with fix lines, the variable is named and its value is not printed, and the exit status is 0

### Scenario: Repository access
- WHEN a remote on the provider's host names a repository that the installation cannot read
- THEN `provider.repo` is `fail` for that repository with the fix "add the repository to the App installation", and the exit status is 1; outside a repository, or with no remote on the host, it is `skip`

## Requirement: A failed gatherer is a failure, not a pass
The system SHALL report an unusable key, a failed mint, an identity that cannot be resolved, a Git that cannot run, and an insecure token cache as `fail`, and SHALL turn the checks that depend on them into `skip`.

### Scenario: Key file with a loose mode
- WHEN the credential source refuses the key
- THEN `key.file` is `fail` with the error's own message, `provider.token` and `provider.identity` are `skip`, and the exit status is 1

## Requirement: Identity refresh writes only the identity
The system SHALL refresh the cached `identity` of the profile when the host reports a different or missing bot, through the profile store's locked write, and SHALL report it as `warn`.

### Scenario: Missing cache
- WHEN `VerifyIdentity` returns a refresh
- THEN only the `identity` member of that profile changes and every other profile is unchanged (compared after parsing); a write error is `warn` and the other checks continue; a rate-limited lookup is `warn` with exit 0

## Requirement: Exit status
The system SHALL exit with 0 when no check failed, 1 when at least one failed, 2 on a usage error (missing `--profile`, unknown flag, extra argument), and 3 when the output was withheld.

Source: .ai/specs/2026-11-13-whoami-doctor-output.md
