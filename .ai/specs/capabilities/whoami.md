# Capability: whoami

`agent-id whoami [--json]` prints which context, profile, bot, installation, credential and token this shell uses, without a secret. Implemented by `internal/diagnose/whoami` and `internal/diagnose` (#24).

The context is read by `readContext` in `cmd/agent-id/diagnose.go`, which applies the rules of the exec spec to the context variables. It is the extension point for `activation.ReadContext`: when the exec slice (#86) merges, `whoami` switches to it. The text labels and their order, the JSON members (schema 1), and the exit codes are a contract.

## Requirement: Agent context report
The system SHALL print, inside an agent context, the fields Context, Profile, Provider, Identity, Installation, Credentials, Token status and Activation, in this order. The token status SHALL come from the token manager.

### Scenario: Exec context
- WHEN `whoami` runs in an exec context against the fake provider with a token that expires in 42 minutes 30 seconds
- THEN the output is the eight labeled lines with `Token status:  valid (expires in 42m)` (rounded down), and the exit status is 0

Source: .ai/specs/2026-11-13-whoami-doctor-output.md

## Requirement: No guess outside a context
The system SHALL print `Context:       personal/unmanaged` and nothing else outside an agent context, and SHALL NOT read Git, `gh`, or agent-id configuration to do so.

### Scenario: Personal terminal with tokens in the environment
- WHEN the process test runs `whoami` and `whoami --json` without `AGENT_ID_CONTEXT`, with a `GH_TOKEN` in the environment and fake `git`, `gh`, and `ssh` first on `PATH`
- THEN the output is exactly the one line (or `{"context":"personal/unmanaged","schema":1}`), the exit status is 0, and the fakes recorded no call

## Requirement: Token failure is visible
The system SHALL report a token that cannot be obtained as `unavailable (<class>: <hint>)` with the provider's failure class and no secret, and SHALL exit with 1.

### Scenario: Installation deleted
- WHEN the provider answers `ErrNotFound`
- THEN `Token status:  unavailable (not found: the installation does not exist or the App lost access)` and the exit status is 1

### Scenario: Every class
- WHEN the token source fails with each provider error, each credential-source error, each token-manager error, and an unknown error
- THEN the class is one of `unauthorized`, `forbidden`, `not found`, `rate limited`, `unavailable`, `key`, `cache`, `config`, with `ErrRateLimited` matched before `ErrUnavailable`

## Requirement: Damaged contexts and missing profiles
The system SHALL report `agent (damaged: <reason>)` or `agent (incompatible context version <v>)` with `Token status: not checked`, and a profile that the configuration lacks as `<name> (not found in the configuration)`, each with exit status 1 and without resolving a profile or minting a token for a damaged context.

Not delivered yet: the `Describe`-based identity line shows the cached identity; a refresh happens only in `doctor`.
