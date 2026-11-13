# Capability: profile-config

agent-id reads its agent profiles from one strictly validated configuration file and resolves a profile only by the name the user passes.

Implemented by the provider-agnostic profile store `internal/profile` (#17). Scenarios that name the `github` block hold once the GitHub adapter (#15) registers its validator and token prefixes; the core rules behind them are tested with a fake provider. The requirement "GitHub provider accepts github.com only" is archived with the adapter (#15).

## Requirement: Config location
The system SHALL read the profile configuration from `$XDG_CONFIG_HOME/agent-id/config.json`, or from `$HOME/.config/agent-id/config.json` when `XDG_CONFIG_HOME` is unset, empty, or relative, on both macOS and Linux.

### Scenario: XDG directory set
- WHEN `XDG_CONFIG_HOME=/tmp/x` and a command resolves the config path
- THEN the path is `/tmp/x/agent-id/config.json`

### Scenario: Relative XDG directory
- WHEN `XDG_CONFIG_HOME=rel` and `HOME=/home/dev`
- THEN the path is `/home/dev/.config/agent-id/config.json`

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Strict v1 schema
The system SHALL accept a config only when it is a JSON object with `version: 1` and a `profiles` object whose entries match the core field table and, for registered providers, the provider's block table, and SHALL reject unknown members, duplicate members, and wrong types with an error that names the field path.

### Scenario: Valid config resolves
- WHEN the v1 example of this spec is validated and profile `claude` is resolved
- THEN every field equals the example, and the GitHub adapter derives the author email `12345678+claude-dev[bot]@users.noreply.github.com`

### Scenario: Duplicate member
- WHEN the `github` block of a profile contains `"app_id"` twice
- THEN validation fails with `ErrConfigInvalid` naming `profiles.claude.github` and `app_id`

### Scenario: Misspelled member
- WHEN the `github` block contains `privat_key`
- THEN validation fails with `ErrConfigInvalid` naming the unknown member

### Scenario: Newer version
- WHEN the config has `"version": 2`
- THEN validation fails with `ErrUnsupportedVersion` and the message asks to upgrade agent-id

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Provider field
The system SHALL require a `provider` member and a provider block of the same name in every profile, and in the MVP SHALL support the provider `github`.

### Scenario: Missing provider
- WHEN a profile has `host` and a `github` block but no `provider`
- THEN validation fails with `ErrConfigInvalid` naming `profiles.claude.provider`

### Scenario: Block does not match provider
- WHEN a profile has `"provider": "github"` and a `gitlab` block but no `github` block
- THEN validation fails with `ErrConfigInvalid` saying that provider block `github` is missing

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Unknown providers are isolated
The system SHALL load a config that contains profiles of providers it does not support, SHALL keep every other profile usable, SHALL fail to resolve the unsupported profile, and SHALL preserve it unchanged on write.

### Scenario: GitHub profile next to a future provider
- WHEN a v1 config contains the GitHub profile `claude` and a profile `gl-bot` with `"provider": "gitlab"` and a `gitlab` block, and this agent-id supports only `github`
- THEN resolving `claude` succeeds, and resolving `gl-bot` fails with `ErrUnsupportedProvider` naming `gitlab` and the supported `github`

### Scenario: Write keeps the unknown profile
- WHEN `setup` writes profile `claude` into that file
- THEN `gl-bot` is still present with the same members and values

### Scenario: Secrets are scanned in unknown blocks
- WHEN the `gitlab` block of `gl-bot` contains a value starting with `-----BEGIN`
- THEN loading fails with `ErrInlineSecret` for the whole file

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: No inline secrets
The system SHALL reject a config in which any string contains token or key material, and SHALL NOT include that value in any error, log, or output.

### Scenario: Token pasted into private_key
- WHEN `github.private_key` is `ghs_` followed by token characters
- THEN validation fails with `ErrInlineSecret` naming `profiles.claude.github.private_key`, and the error text does not contain the value

### Scenario: PEM pasted into an unknown member
- WHEN a `github` block contains a member `key` whose value starts with `-----BEGIN RSA PRIVATE KEY-----`
- THEN validation fails with `ErrInlineSecret` (not with the unknown-member error), and the error text does not contain the value

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Key reference syntax
The system SHALL accept key references of the form `file:<absolute path>` or `file:~/<path>`, and SHALL reject `keyring:` references as reserved and every other form as unknown.

### Scenario: Keyring reserved
- WHEN `github.private_key` is `keyring:claude-github-app`
- THEN validation fails with `ErrUnsupportedKeyReference` and the message says that keyring references are not supported yet

### Scenario: Relative path
- WHEN `github.private_key` is `file:keys/claude.pem`
- THEN validation fails with `ErrUnsupportedKeyReference`

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Explicit profile selection
The system SHALL resolve a profile only by the name the user passes, and SHALL fail with an error listing the configured names when that name does not exist.

### Scenario: Unknown profile
- WHEN profile `writer` is resolved in a config with `claude` and `reviewer`
- THEN the result is `ErrProfileNotFound` and the message lists `claude, reviewer`

Source: .ai/specs/2026-11-12-profile-config.md

## Requirement: Safe config file
The system SHALL refuse to load a config file or directory that is writable by group or others or not owned by the current user, and SHALL write the file atomically with mode `0600` in a `0700` directory.

### Scenario: Group-writable file
- WHEN the config file has mode `0664`
- THEN loading fails with `ErrInsecurePermissions` and the message names the `chmod` fix

### Scenario: Atomic write
- WHEN a profile is written
- THEN the file has mode `0600`, its directory `0700`, and no temporary file remains

Source: .ai/specs/2026-11-12-profile-config.md
