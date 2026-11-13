# Capability: git-credential

`agent-id git-credential --profile <name> get|store|erase` is the Git credential helper that an agent context installs (#22). Spec: `.ai/specs/2026-11-13-git-credential-helper.md`. Implemented by `internal/gitint` (pure decision) and `internal/gitint/credentialhelper` (slice).

## Requirement: HTTPS Git operations authenticate as the App
- A push, fetch, or clone over HTTPS to the profile's host sends the installation token with the user name `x-access-token`.
- `ssh://git@<host>/`, `ssh://<host>/`, `git+ssh://git@<host>/`, and `ssh+git://git@<host>/` remotes, and `git@<host>:` and `<host>:` remotes when the host has no port, resolve to HTTPS in the context.

## Requirement: Nothing else gets a token
- A request for another host gets no output.
- A request for the profile host with another protocol, a port, user-info, a path that is not `owner/name`, a password, a CR, a NUL, or a repeated attribute gets `quit=1` and exit status 1.

## Requirement: Fail closed
- When the token cannot be obtained, Git gets `quit=1`, one fixed message with a class word, and no credential. No personal helper or prompt runs.
- `store` and `erase` persist nothing.

## Requirement: No secret in output
- The token appears only on stdout of a successful `get`, as two lines. No error, message, log, or help text holds it.
