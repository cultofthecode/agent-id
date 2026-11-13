# Capability: diagnostics-redaction

Every byte of `whoami` and `doctor` output passes three layers (#24, `internal/diagnose`): collection (values that can hold credentials are not collected), `Redact` on every string, and `Guard` on every sanitized string and on the rendered bytes.

## Requirement: No secret in any output
The system SHALL NOT write a token, private key, JWT, URL credential, or `Authorization` value to stdout or stderr in any mode of `whoami` or `doctor`.

### Scenario: Decoys everywhere
- WHEN the isolated environment holds a fake token in `GH_TOKEN`, a GitHub-shaped token in `HOMEBREW_GITHUB_API_TOKEN`, a JWT in another variable, a remote URL with user-info, and a key error that carries a PEM marker, and `doctor` runs in text and JSON, and `whoami` and the usage errors run in the process test
- THEN no output contains a token prefix, `-----BEGIN`, a JWT shape, or any decoy value, `Guard` accepts the output, and the variables are still reported by name

### Scenario: Guard
- WHEN a test replaces `Redact` with the identity function and a result holds a fake token
- THEN the output is withheld (`ErrWithheld`, exit status 3, nothing on stdout) for text and JSON, `whoami` and `doctor`

## Requirement: Redact rules
The system SHALL redact, in this order: PEM blocks (to the end marker or the end of the string), JWTs, the token prefixes that the provider adapters registered with the profile registry, URL user-info (the user of an `ssh://` URL without a password and `git@host:` stay), the values of `Authorization`, `password`, and `oauth_token` labels; then control characters become `?` and values are cut to 200 characters. Paths under `$HOME` are printed as `~/…`, free text included, and only at a whole path prefix.

### Scenario: No report field can hold a secret
- WHEN a reflection test walks `Snapshot`, `DoctorReport`, `WhoamiReport`, and `Result`
- THEN no field has the type `provider.Token`, `provider.GitCredential`, or `[]byte`, and every static check and fix string passes `Guard`

Source: .ai/specs/2026-11-13-whoami-doctor-output.md
