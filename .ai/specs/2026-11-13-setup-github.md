# agent-id setup github — onboarding an existing GitHub App

Issue: #19 (E02-S05). Builds on #15 (GitHub adapter, merged), #16 (credential source, merged), #17 (profile store, merged), and #58 (the `github` profile block, open). ADR #35 (provider abstraction) applies throughout.

Extends: `.ai/specs/2026-11-12-profile-config.md` (setup is the writer it names; the file format does not change)
Extends: `.ai/specs/2026-11-13-github-provider-adapter.md` (setup is the first command to construct the adapter and owns the first check against real GitHub)

## 📝 TLDR

A developer who already has a GitHub App, its installation, and the App's private key cannot use agent-id yet: no command writes a profile. This spec proposes `agent-id setup github --profile <name> --app-id <id> --installation-id <id> --private-key <path>`. It loads the key from the protected file, mints one installation token, resolves the bot (`claude-dev[bot]`, its id and noreply email), and lists the repositories the installation can reach. Only when all of that succeeds does it write the profile, with the resolved identity cached, in one atomic write. It prints the profile, the bot, the commit email, and the repositories, never the token. No other file changes. Everything GitHub-specific stays in the GitHub adapter behind a new, provider-neutral `provider.Onboarder` port, so a later `setup gitlab` adds an adapter, not a new command.

## 📝 Problem Statement

- Brief §7.1 and §8 (*GitHub App profile setup*, P0): configure an existing App and installation, resolve the bot identity, validate the credentials, and make no global auth change. Success metric (§12): first agent operation under 5 minutes once an App and key exist.
- `exec` (#20) refuses a profile without a cached identity and points to `agent-id setup github --profile <name>` (exec spec, OQ2). `ErrConfigNotFound` of the profile store already prints the same hint (`internal/profile/store.go`). Until setup exists, both hints point to nothing and the only way to make a profile is to write JSON by hand.
- Setup is the first command that constructs the GitHub adapter and the first that touches real GitHub. The adapter spec leaves the first real-GitHub check, and the key rotation and revocation documentation, to #19.
- ADR #35 (owner comment on #19): `setup github` is the GitHub adapter's onboarding and must go through the provider interface. Today that interface has no onboarding responsibility, so a naive `internal/setup` would import `internal/provider/github` and break dependency rule 7.

## 📝 Proposed Solution

1. **Command.** `agent-id setup github --profile <name> --app-id <n> --installation-id <n> --private-key <path>`. All four flags are required, and there are no others. Setup never prompts, so it behaves the same in a terminal, a script, and CI. The `github` subcommand and its three provider flags come from the GitHub adapter's onboarder; `--profile` belongs to `setup` itself.
2. **Check the input (no I/O).** The profile name uses the profile store's name rule (`profile.CheckName`). The onboarder's pure `Check` tests its own values. Both IDs must be positive integers. `--private-key` must be a path or a `file:` reference, and it must not look like a secret: a PEM marker, a JWT shape, one of the provider's token prefixes, or a control character are each refused **before** the value is normalized, and the value is never echoed. Without this check, a pasted token would become `file:<cwd>/<token>` and come back in the credential source's "not found" error, which names the path.
3. **Check for a conflict.** Load the current configuration. Only a configuration file that does not exist counts as empty. Every other store error, including a non-absolute configuration path, stops setup. A profile with this name and another provider is refused before any network call.
4. **Verify (network, through the onboarder).** Load the key through the credential source (owner, `0600`, one RSA key), construct the adapter, mint an installation token, resolve the bot, and list the installation's repositories with that token. The token stays in memory, is used once, and is dropped. Setup does not cache it (the token manager is #18).
5. **Write.** Call `profile.Store.Write` once, under its lock. The change function checks the conflict again (another `setup` may have run meanwhile) and sets the profile: `provider`, `host`, and the `github` block with `app_id`, `installation_id`, `private_key`, and `identity` (`login`, `user_id`, `resolved_at`). Write validates the whole document with the production registry and replaces the file atomically (`0600` file, `0700` directory). Every other profile, those of unknown providers included, keeps the same members and values. The store re-serializes the whole file, so whitespace of a hand-edited file can change.
6. **Print** the summary (API Contracts). Nothing else is printed on success.

Nothing is written before step 5. A failure in steps 2–5 leaves the configuration file exactly as it was, or absent if it was absent.

### Provider boundary

`internal/setup` names no forge. It talks to an onboarder through a new port in `internal/provider`, next to `Provider`:

```go
// Onboarder is one provider's onboarding: the setup flags it needs and the
// verification that turns them into a profile block. One per provider, not per profile.
type Onboarder interface {
    Name() string                 // "github": the setup subcommand and the profile's provider
    SetupFields() []SetupField    // provider flags, in display order; all required
    Check(r SetupRequest) error   // pure: the values' syntax and the secret-shape refusal; no I/O, never echoes a refused value
    Onboard(ctx context.Context, r SetupRequest) (SetupResult, error) // calls Check first, then verifies over the network
}
type SetupField struct{ Name, Usage string }            // "app-id", "The GitHub App ID"
type SetupRequest struct {
    Values map[string]string // one entry per SetupField, exactly as typed
    Dir    string            // working directory, for a relative key path
}
type SetupResult struct {
    Host         string          // "github.com"
    Settings     json.RawMessage // the provider block to store, identity cache included
    Account      string          // the bot account, "claude-dev[bot]"
    Identity     Identity        // commit name and email
    Details      []SetupDetail   // provider lines for the summary: App, installation, key reference
    Repositories []string        // "owner/name", sorted
}
type SetupDetail struct{ Label, Value string }
```

The GitHub adapter implements it as `github.Onboarder`, built by `cmd/agent-id` with one injected function: `LoadKey func(profile.KeyRef) ([]byte, error)`. The composition root wires it to `internal/credential` (`KeyRef.Path(home)`, then `credential.File{Path}.PrivateKeyPEM()`). The adapter therefore does not import `internal/credential`, and the credential source still does not know the provider. Everything that names GitHub stays in the adapter: the flag names, the `github.com` host, the help text, the error messages that tell the user where to find an App ID, the block encoding (the `ProfileSettings` of #58), and the repository listing (an adapter-only method, A6 of the adapter spec).

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **Onboarder port in `internal/provider`; generic `internal/setup` slice** | Setup is core; every forge brings its own flags and verification; core tests use a fake onboarder; no new import edge for the adapter. | One more interface in the port package. | **Chosen** |
| `internal/setup/github` slice that imports the adapter | Direct. | Breaks dependency rule 7 (GitHub-specific code outside the adapter) and rule 1 (only `cmd/agent-id` imports an adapter); depguard rejects it. | Rejected |
| Port types in `internal/setup`, imported by the adapter | Keeps `internal/provider` small. | New edge adapter → CLI module; a second forge would import a slice module. | Rejected |
| Interactive prompts for missing values (brief §7.1 "guides the user") | Friendlier the first time. | Needs a TTY library or hand-rolled prompts, behaves differently in scripts and agents, and is harder to test. The flags plus clear errors meet the 5-minute target. | Deferred (OQ1) |
| Write first, verify later (`doctor`) | Works offline. | A profile that cannot mint a token would look configured; the issue asks for validation at setup. | Rejected |
| Seed the token cache with the minted token | Saves one mint on first use. | Couples setup to #18, which is not a dependency, and puts a token on disk from a command that does not need it. | Rejected |

Comparable tools: `gh auth login --with-token` and `aws configure set` are flag-driven and non-interactive when the values are given; `gh` validates the token against the API before it saves anything, as setup does here. Unlike `gh`, agent-id stores no secret: the profile references the key file.

## 📝 Delta

### Capability: setup

#### ADDED Requirement: Setup writes a verified profile
The system SHALL write a profile only after the provider has minted a token, resolved the bot identity, and listed the installation's repositories with the given values, and SHALL store the resolved identity in the profile.

The binary cannot reach the fake GitHub: the adapter's API base URL is an unexported test hook (adapter spec, A8). So the end-to-end AC of #19 is covered in two layers. Each is a real test. The GitHub layer runs in the adapter's own package against the fake. The setup layer runs the slice and the composition root with a scripted onboarder.

##### Scenario: GitHub layer — valid inputs against the fake GitHub
- WHEN, in `internal/provider/github`'s tests, `github.Onboarder.Onboard` runs with the fake App's ID, its installation and a `0600` key file of that App. The fake App's slug is `claude-dev`, its bot id is 12345678, and its installation holds `o/a` and `o/b`. The result is then written through a real temporary `profile.Store` that has the GitHub validator registered.
- THEN `config.json` holds profile `claude` with `provider: github`, `host: github.com`, the two IDs, `private_key: file:<absolute key path>`, `identity.login: claude-dev[bot]` and `identity.user_id: 12345678`. The result's `Account` is `claude-dev[bot]`, its email is `12345678+claude-dev[bot]@users.noreply.github.com`, and its repositories are `o/a` and `o/b`.

##### Scenario: Setup layer — output of a verified profile
- WHEN `run` executes `setup github --profile claude …` in-process, with a `FakeOnboarder` scripted with those values, against a temporary store
- THEN the exit status is 0, the profile is in `config.json`, and stdout shows `claude-dev[bot]`, `12345678+claude-dev[bot]@users.noreply.github.com`, `2` repositories, `o/a` and `o/b`

##### Scenario: Re-running setup updates the profile
- WHEN profile `claude` exists for App 1 and setup runs for `claude` with App 2
- THEN the profile holds App 2 and the identity of App 2's bot, stdout says `updated`, and every other profile keeps the same members and values (compared after parsing)

#### ADDED Requirement: A failed setup writes nothing
The system SHALL exit with a non-zero status and an actionable message, and SHALL leave the configuration file unchanged, when any input, the key, the provider, or the write fails.

##### Scenario: Wrong installation
- WHEN the fake GitHub answers 404 to the token request
- THEN the exit status is 1, stderr names `--installation-id` and the App ID, and `config.json` is unchanged (or still absent)

##### Scenario: Key of another App
- WHEN the key file holds a valid RSA key that the fake App does not know
- THEN the exit status is 1, stderr says that GitHub rejected the App credentials and names `--app-id` and `--private-key`, and nothing is written

##### Scenario: Key file too open
- WHEN the key file has mode `0644`
- THEN the exit status is 1, stderr contains the credential source's `chmod 600` fix, and the fake GitHub received no request

##### Scenario: Profile of another provider
- WHEN profile `claude` exists with `provider: gitlab`
- THEN the exit status is 1, the message names both providers, and the fake GitHub received no request

#### ADDED Requirement: Setup changes nothing but the profile file
The system SHALL NOT create or modify any file other than the profile configuration, its lock file, and its temporary file, and SHALL NOT print a token, a JWT, or key material.

##### Scenario: Personal configuration untouched
- WHEN setup succeeds (the setup layer above, run in-process) in an isolated environment with a personal `~/.gitconfig` and a `GH_CONFIG_DIR` holding `hosts.yml`, and when the built binary fails before the network (missing flags, a `0644` key)
- THEN both are byte-identical afterwards, and the only changed paths under `HOME` are `~/.config/agent-id/config.json` and `~/.config/agent-id/config.json.lock`

##### Scenario: Secret pasted as the key path
- WHEN `--private-key` is a fake `ghs_` token, a JWT-shaped string, or the one-line `-----BEGIN RSA PRIVATE KEY-----`
- THEN the exit status is 1, the message names `--private-key` but not the value, and the key loader and the fake GitHub were not called

##### Scenario: No secret in output
- WHEN setup succeeds or fails in any scenario of this capability
- THEN neither stdout nor stderr contains the minted fake token, a `ghs_` prefix, `-----BEGIN`, or a JWT shape

## 📝 Architecture

```mermaid
flowchart LR
    Root["cmd/agent-id (exists)"] --> Setup["internal/setup (new slice)"]
    Root -->|builds, injects LoadKey| Onb["github.Onboarder (new, in internal/provider/github)"]
    Root -->|LoadKey wiring| Cred["internal/credential (exists)"]
    Setup -->|Onboarder port| Port["internal/provider (+ Onboarder)"]
    Setup -->|Load, Write| Store["internal/profile (exists, + CheckName)"]
    Onb -.implements.-> Port
    Onb -->|ProfileSettings, ParseKeyRef| Store
    Onb --> API["GitHub API via the adapter (exists)"]
```

Setup knows the port and the profile store only; the GitHub adapter supplies the flags, the verification, and the block.

- **`internal/provider`** gains `Onboarder`, `SetupField`, `SetupRequest`, `SetupResult`, `SetupDetail`. `providertest` gains `FakeOnboarder` (fields, a scripted result or error, a request recorder) for core tests, and a small contract, `RunOnboarderContract`: fields are non-empty, unique and match `^[a-z][a-z0-9-]*$`; `Onboard` with a missing field fails without calling anything; a successful result has a valid host, an `Account`, a non-empty `Settings` object, and no value that contains a token prefix passed to the contract by the caller (the helper is provider-neutral, so `RunOnboarderContract(t, o, tokenPrefixes)` takes the adapter's prefixes as a parameter), `-----BEGIN`, or a JWT shape; `Check` refuses each of those shapes as a `--private-key`-style value without echoing it.
- **`internal/profile`** gains two exported members: `CheckName(name string) error` (the existing name rule, so setup can reject a bad name before any I/O) and `Store.Path() string` (the configuration path, for the summary line). Nothing else changes.
- **`internal/setup`** (slice, new): `contract.go` (`Input{Profile, Provider string; Values map[string]string; Dir string}`, `Output{Profile, Provider, Host, Account string; Identity provider.Identity; Details []provider.SetupDetail; Repositories []string; Created bool; ConfigPath string}`, sentinels), `handler.go` (dependencies: the onboarders by name and a narrow store port `Load() (profile.Config, error)`, `Write(func(*profile.Config) error) error`, `Path() string`), `command.go` (one `setup` command with one subcommand per registered onboarder, its flags from `SetupFields`), `handler_test.go`.
- **`internal/provider/github`** gains `Onboarder` (`onboard.go`): it parses the values, normalizes the key reference, calls `LoadKey`, `New`, `Token`, `Bot`, `Repositories`, and encodes `ProfileSettings` with `identity`. It reuses #58's `ProfileSettings` and `BotIdentity`; if #58 has not added an encoder, this step adds `ProfileSettings.MarshalJSON` there, with a round-trip test against #58's strict decoder.
- **`cmd/agent-id`** registers the `setup` command with the GitHub onboarder and builds the production `profile.Store` with the registry that #58 wires. `run` keeps exit status 1 for every setup error.
- **Dependencies on other work.** #58 is required (`Store.Write` validates the `github` block with the registered validator; without it, Write rejects the new profile as an unsupported provider, so setup fails closed). The issue lists #15, #16, #17; this spec adds #58.
- No new Go module dependency. `ARCHITECTURE.md` gets the `Onboarder` responsibility in the provider table and a Key decisions row.

## 📝 API Contracts

### Command

```text
agent-id setup github --profile <name> --app-id <id> --installation-id <id> --private-key <path>
```

| Flag | Rule | Error (after `agent-id: setup github: `) |
|---|---|---|
| `--profile` | required; the profile name rule (`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`) | `--profile is required` / `invalid profile name "<v>": use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"` |
| `--app-id` | required; decimal integer, 1 ≤ n ≤ 2⁵³−1, no sign, no leading zero | `--app-id must be a positive integer (the App ID on the App's settings page)` |
| `--installation-id` | same rule | `--installation-id must be a positive integer (the number at the end of the installation's settings URL)` |
| `--private-key` | required; `file:<ref>` is parsed with `profile.ParseKeyRef`; otherwise a path: `~/…` becomes `file:~/…`, an absolute path `file:<path>`, a relative path is joined to the working directory and cleaned; `keyring:` gives the store's reserved-scheme error | `--private-key: <ParseKeyRef message>` |
| missing flags | all missing required flags in one message | `missing required flags: --app-id (the App ID on the App's settings page), --private-key (the .pem file downloaded from the App's settings)` — the hints are the onboarder's `SetupField.Usage` |

Cobra flag errors (unknown flag, missing value) keep cobra's text; the exit status is 1.

### Output (stdout, success)

```text
Profile:       claude (created)
Provider:      github (github.com)
App:           123456
Installation:  789012
Private key:   file:/Users/dev/keys/claude.pem
Identity:      claude-dev[bot]
Commit email:  12345678+claude-dev[bot]@users.noreply.github.com
Repositories:  2
               octo/api
               octo/web
Config:        /Users/dev/.config/agent-id/config.json

Next: agent-id exec --profile claude -- <command>
```

- `created` or `updated`.
- The provider lines (`App`, `Installation`, `Private key`) are the onboarder's `Details`, in its order. Setup prints them without quotes or truncation, after a pure setup function replaces control and Unicode format characters with `?` (the store's `Quote` would add quotes and cut long key paths at 64 characters).
- Repositories are sorted. Up to 20 are listed; above that, the 20 first and `… and <n> more`. With zero repositories the count line is `0 (the installation can access no repository; add repositories in the installation's settings)` and the exit status stays 0, because the credentials are valid.
- The output is for people. It is not a parsed contract: no `--json` in this spec.

### Errors (stderr, one line, exit 1)

| Sentinel | Cause | Message after `agent-id: setup github: ` |
|---|---|---|
| `setup.ErrProfileRequired`, flag errors | input | see the flag table |
| `setup.ErrProviderConflict` | the name belongs to another provider's profile | `profile "claude" already exists for provider "gitlab"; choose another name or remove it from <path>` |
| profile store sentinels (`ErrConfigInvalid`, `ErrInsecurePermissions`, `ErrUnsupportedVersion`, `ErrInlineSecret`, …) | the existing file cannot be read or the result does not validate | the store's message, unchanged |
| credential sentinels | the key file | the credential source's message, unchanged (it names the path and the fix, never key bytes) |
| `github.ErrInvalidKey` | not a GitHub App key | `--private-key <path>: not a GitHub App private key (one unencrypted RSA key of at least 2048 bits, PEM)` |
| `provider.ErrUnauthorized` on the token | wrong App ID or key, revoked key, clock skew | `GitHub rejected the App credentials: check that --app-id 123456 is the App's ID and that --private-key belongs to that App and is not revoked; a clock more than a minute off also causes this` |
| `provider.ErrNotFound` on the token | wrong installation or App uninstalled | `installation 789012 of App 123456 not found: check --installation-id and that the App is installed` |
| `provider.ErrForbidden` | App suspended or blocked | `GitHub denied the App (suspended or blocked): check the App's installation` |
| `provider.ErrRateLimited` | primary or secondary limit | `GitHub is rate limiting this machine; try again later` |
| `provider.ErrUnavailable`, context canceled | network or GitHub | `cannot reach GitHub: <adapter's message>` (the adapter's message names the operation and status, never a body or a token) |

The GitHub-specific texts are the GitHub onboarder's; setup prints the error it gets. Every error wraps its sentinel (`%w`), so tests use `errors.Is`.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| No configuration file yet | Setup creates the directory (`0700`) and the file (`0600`); `created`. |
| Existing file invalid or group-writable | The store's error; nothing written; no network call (step 3 loads first). |
| Existing file has a profile of an unknown provider under another name | Kept with the same members and values. |
| Same values run twice | `updated`; identity refreshed (`resolved_at` changes); nothing else changes. |
| Two setups of different profiles at the same time | Both network phases run; the store's lock serializes the writes; both profiles end up in the file. |
| Two setups of the same profile at the same time | The last write wins; each was fully verified, so the file is valid either way. |
| Another process creates the name for another provider during the network phase | The change function refuses (`ErrProviderConflict`); nothing written. |
| `--private-key ~/Downloads/app.2026-11-13.private-key.pem` with mode `0644` (GitHub's download) | Credential error with `chmod 600 <path>`; no network. Setup never changes the key file's mode. |
| `--private-key` is a symlink to a `0600` key | Accepted (credential source rules); the reference stores the path as given, not the target. |
| The key path contains a newline or control character | Refused by `ParseKeyRef` / the inline-secret scan; the value is not echoed. |
| Key or token pasted instead of a path (`--private-key "-----BEGIN…"`, a `ghs_…` token, a JWT) | Refused by the onboarder's `Check` before normalization and before any I/O; the message names `--private-key`, never the value. |
| An App ID typed as `0123` or `1e6` | Integer error. |
| Installation reaches 5000 repositories | The adapter's page cap (100 pages × 100) covers it; above that, `ErrUnavailable` from the adapter, and nothing is written. |
| GitHub answers a bot login or repository name with escape characters | Refused by the adapter's patterns (`ErrUnavailable`); nothing printed from the answer. |
| Ctrl-C during the network phase | Context canceled; exit 1; nothing written. |
| `HOME` unset or relative | The store's path error (`Path`), before any network call. |
| Setup runs inside an agent context (`AGENT_ID_CONTEXT=1`) | Allowed: setup writes no environment and starts nothing; the next `exec` re-reads the profile. |

## 📝 Risks & Impact Review

- **First contact with real GitHub.** The fake follows GitHub's documentation, but CI never talks to GitHub (no secrets in CI). The adapter spec assigns the first real check to #19. The implementation PR records a manual check on the owner's machine, in an isolated environment (`AGENTS.md`, manual run variables), with a dedicated test App: one successful setup, one wrong installation ID, one key of another App. The PR records each result and the exit status, but never the App's key, the token, or private repository names. The end-to-end suite (#27) automates this later under the `e2e` tag.
- **Key blast radius (brief §10).** Setup reads the key once, signs one JWT, and drops the parsed key when it exits. The profile holds a reference, not the key. The implementation adds a short "Rotate or revoke the App key" section to the README or docs: generate a new key in the App settings, run setup again with the new path, delete the old key in GitHub. Setup never copies, moves, or changes the mode of the key file (OQ2).
- **Contract.** The flag names and the provider block it writes are shared contracts (`SDLC.md`, risk inference: CLI flags). The block is the profile-config v1 format, unchanged. The human output is not a parsed contract.
- **Provider port.** `Onboarder` is a new responsibility of the provider abstraction (ADR #35 names five; onboarding was implicit in "profile setup"). It is additive. A future adapter must implement it to have a `setup` subcommand.
- **Not a security boundary.** Setup's checks stop mistakes and hostile answers, not a local agent that can rewrite `config.json` (brief §10).
- **Product direction vs. rule.** OQ1–OQ3 below are product choices with the recommendation applied. The fail-closed and no-write rules follow brief §8 and §10 and are not optional.

## 📝 Decisions in play

- Brief §7.1, §8 (GitHub App profile setup, P0), §10 (key handling), §12 (5-minute target), §14 (*GitHub App onboarding friction*: existing Apps first).
- ADR #35 and the owner's comment on #19: setup goes through the provider interface.
- Profile-config spec: the file, the `github` block, the identity cache, and `setup` as its writer. Adapter spec: A2 (no downscoping), A4 (unauthenticated bot lookup), A6 (repository listing stays in the adapter).

## Open Questions (owner) — recommendations applied

These are product choices. The spec applies the recommendation; each is reversible and does not block the spec. The owner's answer is needed before #19 is implemented only if it differs.

| # | Question | Recommendation (applied) | Why | Confirm? |
|---|---|---|---|---|
| OQ1 | Should setup prompt interactively for missing values (brief §7.1 "guides the user"), or be flags-only in the MVP? | **Flags only.** All missing flags are named in one error with where to find each value. | Same behavior for people, scripts, and agents; no TTY dependency; the 5-minute target is met; prompts can be added later without breaking the flags. | owner, non-blocking |
| OQ2 | Should setup copy the private key into agent-id's directory (`~/.config/agent-id/keys/<profile>.pem`, `0600`), or reference the file where the user keeps it? | **Reference in place; never copy, move, or chmod it.** | A copy duplicates the most sensitive secret (brief §10: it can mint tokens for every installation) and makes rotation harder; the credential source already refuses an unprotected file with the fix. The profile-config spec's example path stays valid when the user chooses to keep keys there. | owner, non-blocking (security model: keys) |
| OQ3 | Should setup discover the installation ID (list the App's installations with the JWT and pick the only one), or require `--installation-id`? Should it also take `--repo owner/name` and fail when the installation cannot reach it? | **Neither in the MVP.** Require the ID; list the reachable repositories in the output; `doctor` (#24) checks the current repository's access. | Smallest surface; discovery needs a new adapter call and a rule for several installations; a repository check belongs where the repository is known. Both are additive later. | owner, non-blocking |

## Resolved assumptions (autonomous defaults)

Technical decisions taken by this spec, recorded for override:

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | Where does GitHub-specific onboarding live? | `github.Onboarder` behind a new `provider.Onboarder` port; `internal/setup` is generic. | ADR #35 and dependency rule 7; core tests use a fake onboarder. | reversible |
| A2 | How does the adapter get the key without importing `internal/credential`? | `cmd/agent-id` injects `LoadKey func(profile.KeyRef) ([]byte, error)`. | No new module edge; the credential source stays provider-blind. | reversible |
| A3 | Existing profile with the same name | Same provider: update in place (`updated`). Another provider: refuse. | Re-running setup is the natural way to rotate a key or change an installation; replacing another forge's profile silently would be surprising. | reversible |
| A4 | When is the existing configuration read? | Before the network phase (fail fast) and again inside `Store.Write` (authoritative). | No network call for a broken file; no lost update under concurrency. | reversible |
| A5 | How is a key path stored? | `file:` + absolute cleaned path; `~/…` kept as `file:~/…`; symlinks not resolved. | Meaning independent of the working directory; follows the user's own link. | reversible |
| A6 | Zero reachable repositories | Write the profile, exit 0, say how to add repositories. | The credentials are valid; repository access is changed in GitHub, not here. | reversible |
| A7 | Exit status of setup failures | 1. | No caller needs to tell failure classes apart by code; `exec`'s 125–127 are for wrapping a command. | reversible |
| A8 | Does setup cache the minted token? | No; the token is dropped. | #18 is not a dependency; no token on disk from setup. | reversible |

## 📋 Phasing

One implementation PR (`Closes #19`), `risk-high`, after #58 merges.

- **Phase 1 — ports.** `provider.Onboarder` and its types, `providertest.FakeOnboarder` and `RunOnboarderContract`, `profile.CheckName`.
- **Phase 2 — GitHub onboarder.** `github.Onboarder` against `githubtest`.
- **Phase 3 — the slice and wiring.** `internal/setup`, `cmd/agent-id`, process tests.
- **Phase 4 — docs and record.** Key rotation docs, `ARCHITECTURE.md`, `AGENTS.md` routing, capability archive, manual real-GitHub check.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first. GitHub is always `githubtest.NewApp` on `testenv.NewFakeAPI`, keys are generated per test, and tokens are fakes. `risk-high` evidence (`SDLC.md`): hostile-input tables, the denied paths against the fake, the scan for secrets in all output, and a second-person review.

### Phase 1 — ports

1. **Onboarder port.** Add the types to `internal/provider`; `providertest.FakeOnboarder`; `RunOnboarderContract` run against the fake. Update `internal/provider/AGENTS.md`. Tests: the contract passes for the fake; a fake whose result carries a `testenv.FakeToken` value fails the contract.
2. **Name check and path.** `profile.CheckName` reusing the existing rule; `Store.Path`. Tests: the profile spec's name table (`Claude`, `my_bot`, `claude-`, `-a`, 33 characters, `a--b` accepted).

### Phase 2 — `github.Onboarder`

3. **Values.** `Check`, built on a pure `parseSetupValues(values, dir) (appID, installationID int64, key profile.KeyRef, err)`. Table: missing each field, `0`, `-1`, `0123`, `1e6`, `2^53`, whitespace, `~/k.pem`, `/abs/k.pem`, `rel/k.pem` with a `dir`, `file:rel` (refused), `keyring:x` (reserved), a path with a newline, and as the value: `-----BEGIN RSA PRIVATE KEY-----`, a `testenv.FakeToken`, `ghp_x`, `github_pat_x`, and a JWT shape. Each is refused by `Check`, the error does not contain the value, and `LoadKey` is not called.
4. **Onboard.** `LoadKey` → `New` → `Token` → `Bot` → `Repositories` → `ProfileSettings` with `identity` (`resolved_at` from an injected clock, UTC, seconds). Tests against `githubtest`: success with two repositories (the Delta's values); 401, 403, 404, 429, 503 on the token, each with its message and the right sentinel; a key of another App; `LoadKey` failing (no request reaches the fake); a hostile bot login; more than 20 repositories; zero repositories. Every error and the result are scanned for the fake token, the JWT, and `-----BEGIN`. `RunOnboarderContract` passes for `github.Onboarder`.
5. **Block round trip.** The `Settings` that `Onboard` returns pass #58's strict validator and decode to the same values.

### Phase 3 — `internal/setup` and wiring

6. **Slice.** `contract.go`, `handler.go`, `command.go`, `handler_test.go` with `FakeOnboarder` and a temporary store: created, updated (other profiles keep members and values, compared after parsing; an unknown-provider profile kept), provider conflict before the network (the fake's recorder is empty), conflict appearing during the network phase (the change function refuses), store errors before the network, onboarder errors (nothing written: file hash and directory listing unchanged), missing flags listed together, output rendering (20-repository cut-off, zero repositories, a detail value with an escape sequence rendered with `?`).
7. **Composition root** (`cmd/agent-id/main.go`, the only file depguard lets import the adapter, test files included; the builder of the production root lives there and tests call it). Register `setup` with the GitHub onboarder and the production store; inject `LoadKey` through `internal/credential`. Test: with the production registry, `setup github --help` lists the four flags; an unknown provider subcommand fails.
8. **Process test.** Build the binary with `testenv.BuildBinary`; `TestMain` calls `testenv.Main`. The adapter's API base URL is an unexported test-only hook (adapter spec, A8), so neither the binary nor a test outside `internal/provider/github` can point the GitHub onboarder at the fake. The split is therefore: step 4 tests the GitHub onboarder against the fake in its own package; `cmd/agent-id` tests run `run` in-process with a root built from `providertest.FakeOnboarder` (wiring, rendering, exit status); and the binary test covers flag parsing and the failures that happen before any network call (missing flags, `0644` key, conflicting provider, invalid config). Assertions: the isolated `~/.gitconfig` and `GH_CONFIG_DIR` hash the same before and after; only `config.json` and its lock appear under `HOME`; stdout and stderr contain no `ghs_`, `-----BEGIN`, or JWT shape.

### Phase 4 — docs and record

9. **Docs.** A "Set up an agent profile" section and the "Rotate or revoke the App key" section (README or `docs/`), with the honest limits of brief §10. `internal/setup/AGENTS.md` (invariants: names no forge; nothing written before verification; no secret printed), the `AGENTS.md` routing row, `ARCHITECTURE.md` (components: setup exists; provider table: the onboarding responsibility; Key decisions: A1–A8 and the applied OQ1–OQ3). Archive the Delta into `.ai/specs/capabilities/setup.md`.
10. **Manual real-GitHub check** (owner's machine, isolated environment, dedicated test App, not in CI). Record in the PR: success, wrong installation ID, key of another App, each with exit status and redacted output.
