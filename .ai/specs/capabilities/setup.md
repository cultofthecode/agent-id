# Capability: setup

`agent-id setup <provider>` turns an existing provider app into a verified agent profile. In the MVP the only provider is GitHub (`agent-id setup github`).

Implemented by the setup slice `internal/setup`, the onboarding port `provider.Onboarder`, and the GitHub onboarder `github.Onboarder` (#19). The fake GitHub App of the tests uses its own slug and bot id (`githubtest`), so the GitHub-layer test checks those values in place of `claude-dev` and 12345678.

## Requirement: Setup writes a verified profile
The system SHALL write a profile only after the provider has minted a token, resolved the bot identity, and listed the installation's repositories with the given values, and SHALL store the resolved identity in the profile.

### Scenario: GitHub layer — valid inputs against the fake GitHub
- WHEN, in `internal/provider/github`'s tests, `github.Onboarder.Onboard` runs with the fake App's ID, its installation, and a `0600` key file of that App, and the result is written through a real temporary `profile.Store` with the GitHub validator registered
- THEN `config.json` holds profile `claude` with `provider: github`, `host: github.com`, the two IDs, `private_key: file:<absolute key path>`, and the bot's `identity.login` and `identity.user_id`; the result's account is the bot login, its email is `<id>+<login>@users.noreply.github.com`, and its repositories are `o/a` and `o/b`

### Scenario: Setup layer — output of a verified profile
- WHEN `run` executes `setup github --profile claude …` in-process, with a `FakeOnboarder` scripted with those values, against a temporary store
- THEN the exit status is 0, the profile is in `config.json`, and stdout shows `claude-dev[bot]`, `12345678+claude-dev[bot]@users.noreply.github.com`, `2` repositories, `o/a` and `o/b`

### Scenario: Re-running setup updates the profile
- WHEN profile `claude` exists for App 1 and setup runs for `claude` with App 2
- THEN the profile holds App 2 and the identity of App 2's bot, stdout says `updated`, and every other profile keeps the same members and values (compared after parsing)

Source: .ai/specs/2026-11-13-setup-github.md

## Requirement: A failed setup writes nothing
The system SHALL exit with a non-zero status and an actionable message, and SHALL leave the configuration file unchanged, when any input, the key, the provider, or the write fails.

### Scenario: Wrong installation
- WHEN the fake GitHub answers 404 to the token request
- THEN the exit status is 1, stderr names `--installation-id` and the App ID, and `config.json` is unchanged (or still absent)

### Scenario: Key of another App
- WHEN the key file holds a valid RSA key that the fake App does not know
- THEN the exit status is 1, stderr says that GitHub rejected the App credentials and names `--app-id` and `--private-key`, and nothing is written

### Scenario: Key file too open
- WHEN the key file has mode `0644`
- THEN the exit status is 1, stderr contains the credential source's `chmod 600` fix, and the fake GitHub received no request

### Scenario: Profile of another provider
- WHEN profile `claude` exists with `provider: gitlab`
- THEN the exit status is 1, the message names both providers, and the fake GitHub received no request

Source: .ai/specs/2026-11-13-setup-github.md

## Requirement: Setup changes nothing but the profile file
The system SHALL NOT create or modify any file other than the profile configuration, its lock file, and its temporary file, and SHALL NOT print a token, a JWT, or key material.

### Scenario: Personal configuration untouched
- WHEN setup succeeds in-process in an isolated environment with a personal `~/.gitconfig` and a `GH_CONFIG_DIR` holding `hosts.yml`, and when the built binary fails before the network (missing flags, a `0644` key)
- THEN both are byte-identical afterwards, and the only changed paths under `HOME` are `~/.config/agent-id/config.json` and `~/.config/agent-id/config.json.lock`

### Scenario: Secret pasted as the key path
- WHEN `--private-key` or `--profile` is a fake `ghs_` token, a JWT-shaped string, or the one-line `-----BEGIN RSA PRIVATE KEY-----`
- THEN the exit status is 1, the message names the flag but not the value, and the key loader and the fake GitHub were not called

### Scenario: No secret in output
- WHEN setup succeeds or fails in any scenario of this capability
- THEN neither stdout nor stderr contains the minted fake token, a `ghs_` prefix, `-----BEGIN`, or a JWT shape

Source: .ai/specs/2026-11-13-setup-github.md
