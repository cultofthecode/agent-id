# Profile configuration v1

Issue: #9 (E01-T04). Implemented by #17 (E02-S03, profile store) and #16 (E02-S02, credential source); written by #19 (E02-S05, setup).

## 📝 TLDR

aidentity needs one file that says which GitHub App each agent profile uses, where its private key is, and which bot identity the profile resolved to. Today only the illustrative YAML of `PRODUCT_BRIEF.md` §9.4 exists, so the profile store (#17) cannot start. This spec proposes the v1 contract: the file location, the format, every field with its type, validation rule and error message, the private-key reference syntax, the identity cache, explicit profile selection, and the rule for future versions.

The format is **JSON read with the Go standard library `encoding/json`**, with no YAML or TOML dependency: the owner's decision of 2026-11-12 (Jacob, on #9 and #33).

## 📝 Problem Statement

- The profile config is a shared contract (`SDLC.md`, risk inference): every later command reads it, and a change after the first release needs a migration. The brief §9.4 schema is marked illustrative, and `ARCHITECTURE.md` lists "Profile config format and schema; keyring support" as **OPEN**.
- The file decides which key signs tokens and which identity commits carry. A loose reader is a security problem: a misspelled key that is silently ignored, a duplicate key whose last value wins, a host that is "almost" `github.com`, or a token pasted into the file all lead to the wrong credential or to a secret on disk.
- Three backlog items wait on this contract: the profile store (#17), the credential source (#16), and setup (#19).

## 📝 Proposed Solution

One file per user, read strictly, written only by aidentity:

1. **Location.** `$XDG_CONFIG_HOME/aidentity/config.json`; when `XDG_CONFIG_HOME` is unset, empty, or not an absolute path, `$HOME/.config/aidentity/config.json`. The same rule applies on macOS: aidentity does not use `os.UserConfigDir` (which returns `~/Library/Application Support` there), so the path is the same on both platforms and `testenv.Isolate` covers it. The directory name `aidentity` follows the product-name decision of #6.
2. **Format.** JSON (RFC 8259), UTF-8, at most 64 KiB. Decoded with `encoding/json` and a strict token walk: unknown members, duplicate member names, and wrong JSON types are errors. aidentity writes the file with two-space indentation and a trailing newline.
3. **Schema.** `version: 1`, a `profiles` object keyed by profile name, and nothing else at the top level. Each profile has `provider`, `host`, `app_id`, `installation_id`, `private_key`, and an optional `identity` cache.
4. **No secrets in the file.** `private_key` is a reference (`file:` now, `keyring:` reserved). Every string value in the document is scanned for token and key material, and a match rejects the whole file without echoing the value.
5. **Explicit selection.** A command that needs a profile takes `--profile <name>`. The file has no default profile, and aidentity does not detect a profile from the directory, the repository, or the environment (brief §14).
6. **Validation is a pure function** (`ARCHITECTURE.md`, dependency rule 5): `Validate([]byte) (Config, error)` takes the bytes and touches no file system and no network. Loading, permission checks, and the existence of the key file are separate steps.

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **JSON, `encoding/json`** | No new dependency (`AGENTS.md`, Ask first). Strict types: `"123"` is not a number, `no` is not `false`. Unknown and duplicate members are detectable with the standard decoder. The file is written by `setup`, so hand editing is rare. | No comments. Less pleasant to edit by hand than YAML. Differs from the brief's example. | **Chosen** (owner's decision, 2026-11-12) |
| YAML (`go.yaml.in/yaml/v3`, the maintained successor of `gopkg.in/yaml.v3`) | Matches brief §9.4. Comments. Familiar from CI files. | New dependency (owner's yes needed). Implicit typing (`on`, `0700`, `1e3`) and anchors/aliases need extra guards. The original `gopkg.in/yaml.v3` is unmaintained. | Rejected by the owner (2026-11-12): no YAML dependency. |
| TOML (`github.com/pelletier/go-toml/v2` or `BurntSushi/toml`) | Comments, strict types, made for config. | New dependency. Nested `profiles.<name>.identity` tables read less naturally. Nobody asked for it. | Rejected. |
| One file per profile (`profiles/<name>.json`) | Concurrent `setup` of two profiles never conflicts. | Two places to look, harder `doctor` output, a directory listing becomes part of the contract. | Rejected for v1; a lock on the single file solves the conflict. |
| Identity cache in `$XDG_CACHE_HOME` | Keeps resolved data out of config. | Clearing the cache would make `exec` fail or call GitHub. `ARCHITECTURE.md` (Data) places it with the profile. | Rejected. |

Comparable open-source tools point the same way. The AWS CLI selects named profiles explicitly (`--profile`) and keeps secrets in a separate `credentials` file. `gh` writes `hosts.yml` itself, but falls back to a plaintext token in that file when no keyring is available, which is exactly what v1 forbids. Both tolerate unknown keys; aidentity does not, because a silently ignored key here changes which credential an agent gets.

## 📝 Data Model

### Example (v1)

```json
{
  "version": 1,
  "profiles": {
    "claude": {
      "provider": "github",
      "host": "github.com",
      "app_id": 123456,
      "installation_id": 789012,
      "private_key": "file:~/.config/aidentity/keys/claude.pem",
      "identity": {
        "login": "claude-dev[bot]",
        "user_id": 12345678,
        "resolved_at": "2026-11-12T10:00:00Z"
      }
    },
    "reviewer": {
      "provider": "github",
      "host": "github.com",
      "app_id": 456789,
      "installation_id": 987654,
      "private_key": "file:/Users/dev/keys/reviewer.pem"
    }
  }
}
```

### Difference from brief §9.4

The brief's example does **not** parse under this spec. The differences, each deliberate:

| Brief §9.4 | v1 | Why |
|---|---|---|
| YAML | JSON | Owner's decision (2026-11-12): JSON with `encoding/json`, no new dependency; strict typing. |
| `private_key: keyring:claude-github-app` | Rejected with `ErrUnsupportedKeyReference`; use `file:` | The keyring backend is OPEN (`ARCHITECTURE.md`) and out of scope of #9 and #16. The `keyring:` prefix is reserved so that a later version can add it without a new syntax. |
| `identity.name`, `identity.email` | `identity.login`, `identity.user_id`, `identity.resolved_at` | Name and email are derived from login and id (`ARCHITECTURE.md`, Git integration): name `<login>`, email `<user_id>+<login>@users.noreply.github.com`. Storing them separately would allow a mismatch. The brief asks for resolved, cached identity rather than manual entry. |
| `integrations.claude-code` | Not in v1; unknown top-level member | Explicit selection only (brief §14). The Claude Code hook (#26) passes `--profile` on its command line. |
| `reviewer` without `identity` | Allowed | `identity` is optional until `setup` or `doctor` resolves it. |

### Fields

All members are required unless marked optional. Paths in error messages use dots: `profiles.claude.host`. Every error message starts with `config <path>: ` (the absolute file path), shown below as `…: `.

| Field | Type | Validation rule | Error (sentinel → message) |
|---|---|---|---|
| (document) | JSON object | Valid UTF-8 JSON, at most 64 KiB, one top-level object, no trailing data. | `ErrConfigInvalid` → `…: not valid JSON at line L, column C` / `…: larger than 64 KiB` |
| (any object) | — | No member name appears twice in the same object. | `ErrConfigInvalid` → `…: <path>: duplicate member "<name>"` |
| (any object) | — | Only the members listed here. | `ErrConfigInvalid` → `…: <path>: unknown member "<name>"` |
| (any string) | — | Must not contain token or key material (see Inline secrets). Checked before every other field rule. | `ErrInlineSecret` → `…: <path>: contains what looks like a token or private key; the config holds references only — remove the value and revoke it if it was real` |
| `version` | integer | Present; exactly `1`. A higher integer means a newer aidentity wrote the file. | `ErrUnsupportedVersion` → `…: version is missing` / `…: version <n> is newer than this aidentity supports (1); upgrade aidentity` / `…: version must be the integer 1` |
| `profiles` | object | Present. May be empty. Keys are profile names. | `ErrConfigInvalid` → `…: profiles is missing` / `…: profiles must be an object` |
| profile name (key) | string | `^[a-z][a-z0-9-]{0,31}$`: lowercase letter first, then lowercase letters, digits, or `-`; 1–32 characters. The name ends up in cache file names and in diagnostics. | `ErrConfigInvalid` → `…: profiles: invalid profile name "<name>": use 1–32 lowercase letters, digits or "-", starting with a letter` |
| `profiles.<n>.provider` | string | Exactly `github`. | `ErrConfigInvalid` → `…: profiles.<n>.provider: "<v>" is not supported; use "github"` |
| `profiles.<n>.host` | string | Exactly `github.com`: lowercase, no scheme, port, path, trailing dot, or whitespace. GitHub Enterprise Server and custom hosts are deferred (brief §8). | `ErrUnsupportedHost` → `…: profiles.<n>.host: "<v>" is not supported; this version accepts only github.com` |
| `profiles.<n>.app_id` | integer | JSON number without fraction or exponent, 1 ≤ value ≤ 2⁵³−1. A string such as `"123456"` is rejected. | `ErrConfigInvalid` → `…: profiles.<n>.app_id: must be a positive integer` |
| `profiles.<n>.installation_id` | integer | Same rule as `app_id`. | `ErrConfigInvalid` → `…: profiles.<n>.installation_id: must be a positive integer` |
| `profiles.<n>.private_key` | string | A key reference (see below). The value is never echoed. | `ErrUnsupportedKeyReference` → see Key references |
| `profiles.<n>.identity` | object, optional | When present, all three members below are present. When absent, the profile is valid but its identity is unresolved. | `ErrConfigInvalid` → `…: profiles.<n>.identity: <member> is missing` |
| `profiles.<n>.identity.login` | string | `^[a-z0-9][a-z0-9-]{0,62}\[bot\]$`: an App slug followed by `[bot]`. | `ErrConfigInvalid` → `…: profiles.<n>.identity.login: "<v>" is not a GitHub App bot login (expected "<slug>[bot]")` |
| `profiles.<n>.identity.user_id` | integer | Same rule as `app_id`. | `ErrConfigInvalid` → `…: profiles.<n>.identity.user_id: must be a positive integer` |
| `profiles.<n>.identity.resolved_at` | string | RFC 3339 timestamp in UTC (`Z`). | `ErrConfigInvalid` → `…: profiles.<n>.identity.resolved_at: must be an RFC 3339 UTC timestamp` |

Values are echoed in messages only for `provider`, `host`, `login`, and profile names: these are short identifiers, never secret, and the user needs to see the wrong value. Any echoed value is cut to 64 characters and has control characters replaced with `?`, so that a hostile file cannot inject terminal escapes into diagnostics.

When a document has several problems, `Validate` returns the first in this order: document syntax and size, inline secrets, duplicate members, unknown members, `version`, then the fields in document order. `ErrInlineSecret` comes first so that the user's first action is to remove and revoke the secret.

### Key references

`private_key` is `<scheme>:<value>`:

| Form | v1 behavior | Error |
|---|---|---|
| `file:/abs/path.pem` | Accepted. The path is cleaned with `filepath.Clean`. | — |
| `file:~/path.pem` | Accepted. `~/` expands to `$HOME` at load time, not at validation time. | — |
| `file:relative/path.pem`, `file:`, `file:~user/x` | Rejected: the meaning would depend on the working directory or on another user. | `ErrUnsupportedKeyReference` → `…: profiles.<n>.private_key: a file reference needs an absolute path or ~/…` |
| `keyring:<name>` | Rejected; the scheme is reserved for a later version. | `ErrUnsupportedKeyReference` → `…: profiles.<n>.private_key: keyring references are reserved and not supported yet; use file:<path>` |
| `/abs/path.pem` (no scheme), any other scheme, an empty string | Rejected. | `ErrUnsupportedKeyReference` → `…: profiles.<n>.private_key: unknown key reference; use file:<path>` |
| Contains a newline, a NUL byte, or key material | Rejected by the inline-secret scan before this rule. | `ErrInlineSecret` |

Validation checks the syntax only. Whether the file exists, is a regular file or a symlink to one, has mode `0600` or stricter, and holds a parseable RSA key is the credential source's job (#16) at use time, and `doctor` reports it.

### Inline secrets

The scan runs over every string value and every member name in the document. It rejects a string that contains any of:

- a GitHub token prefix: `ghs_`, `ghp_`, `gho_`, `ghu_`, `ghr_`, `github_pat_`;
- a PEM block marker: `-----BEGIN`;
- a JWT shape: three base64url segments separated by dots, the first starting with `eyJ`;
- a newline or a NUL byte (no v1 field holds multi-line text).

The scan is a guard against pasting a credential into the file, not a secret detector for arbitrary content: a false positive on an identifier is acceptable, because no v1 field legitimately holds these patterns. The error names the field path and never the value, and the loader never logs the raw document.

### Identity cache

`identity` is non-secret data that `setup` (#19) writes after it resolves the bot from GitHub, and `doctor` (#24) refreshes. It belongs to the profile's `host` and `app_id`: a write that changes either removes `identity` in the same write. Consumers derive the Git identity from it:

- name: `<login>` (for example `claude-dev[bot]`);
- email: `<user_id>+<login>@users.noreply.github.com`.

A profile without `identity` is valid in the file. The proposed consumer rule for `exec` and `env` (#20) is to refuse such a profile with an error that points to `aidentity setup github --profile <n>`, rather than resolve the identity on every start; #20 owns that decision. `doctor` compares the cache with GitHub and reports a mismatch; it never trusts the cache as proof of anything.

### File system rules

- The directory is created with mode `0700` and the file with `0600`. The file holds no secret, but it decides which key file signs tokens and which identity commits carry, so it is treated like credentials configuration.
- Load refuses a file (after following symlinks) or directory that another user can write, or that the current user does not own: `ErrInsecurePermissions` → `config <path>: writable by group or others (mode 0664); run chmod 600 <path>`. A readable-by-others file is accepted.
- A missing file: `ErrConfigNotFound` → `no aidentity configuration at <path>; run aidentity setup github --profile <name>`.
- Writes take an exclusive `flock` on `config.json.lock` in the same directory (standard library `syscall.Flock`, macOS and Linux), read the current file, apply the change, write a temporary file in the same directory with mode `0600`, `fsync` it, and `rename` it over `config.json`. A crash leaves either the old or the new file, never a partial one.
- aidentity never writes the file during a read, a migration, `exec`, or `env`.

### Profile selection

A command that needs a profile takes `--profile <name>`. The store's `Resolve(name)` returns the profile or `ErrProfileNotFound` → `profile "<name>" not found in <path>; available: claude, reviewer` (or `no profiles configured` when empty). There is no default profile, no `AIDENTITY_PROFILE` variable for selection, and no detection from the working directory or Git remote: those are Phase 2 decisions (#29).

### Versioning and migration

- `version` is an integer. A reader knows the set of versions 1…N it supports.
- Every schema change, including a new optional member, increases `version`, because readers reject unknown members. This keeps "an older aidentity ignored a field it did not know" impossible.
- A reader that meets an older version migrates it **in memory** through a chain of pure functions (`v1→v2`, `v2→v3`, …), each with its own table test. It does not rewrite the file.
- The next write (for example `setup`) writes the newest version and first copies the old file to `config.json.v<old>.bak` with mode `0600`.
- A reader that meets a newer version fails with `ErrUnsupportedVersion` and asks the user to upgrade. Downgrading aidentity after a write of a newer version needs the `.bak` file; this is documented in the release notes of the version that bumps the schema.

## 📝 Delta

### Capability: profile-config

#### ADDED Requirement: Config location
The system SHALL read the profile configuration from `$XDG_CONFIG_HOME/aidentity/config.json`, or from `$HOME/.config/aidentity/config.json` when `XDG_CONFIG_HOME` is unset, empty, or relative, on both macOS and Linux.

##### Scenario: XDG directory set
- WHEN `XDG_CONFIG_HOME=/tmp/x` and a command resolves the config path
- THEN the path is `/tmp/x/aidentity/config.json`

##### Scenario: Relative XDG directory
- WHEN `XDG_CONFIG_HOME=rel` and `HOME=/home/dev`
- THEN the path is `/home/dev/.config/aidentity/config.json`

#### ADDED Requirement: Strict v1 schema
The system SHALL accept a config only when it is a JSON object with `version: 1` and a `profiles` object whose entries match the field table, and SHALL reject unknown members, duplicate members, and wrong types with an error that names the field path.

##### Scenario: Valid config resolves
- WHEN the v1 example of this spec is validated and profile `claude` is resolved
- THEN every field equals the example, and the derived email is `12345678+claude-dev[bot]@users.noreply.github.com`

##### Scenario: Duplicate member
- WHEN a profile contains `"host": "github.com"` twice
- THEN validation fails with `ErrConfigInvalid` naming `profiles.claude` and `host`

##### Scenario: Misspelled member
- WHEN a profile contains `privat_key`
- THEN validation fails with `ErrConfigInvalid` naming the unknown member

##### Scenario: Newer version
- WHEN the config has `"version": 2`
- THEN validation fails with `ErrUnsupportedVersion` and the message asks to upgrade aidentity

#### ADDED Requirement: github.com only
The system SHALL accept `provider: github` and `host: github.com` only, and SHALL reject any other host, including look-alikes.

##### Scenario: Enterprise host rejected
- WHEN a profile has `host: github.example.com`
- THEN validation fails with `ErrUnsupportedHost` and the message says that only github.com is accepted

##### Scenario: Look-alike host rejected
- WHEN a profile has `host` set to `GitHub.com`, `github.com.`, `https://github.com`, `github.com:443`, or `api.github.com`
- THEN validation fails with `ErrUnsupportedHost`

#### ADDED Requirement: No inline secrets
The system SHALL reject a config in which any string contains token or key material, and SHALL NOT include that value in any error, log, or output.

##### Scenario: Token pasted into private_key
- WHEN `private_key` is `ghs_` followed by token characters
- THEN validation fails with `ErrInlineSecret` naming `profiles.claude.private_key`, and the error text does not contain the value

##### Scenario: PEM pasted into an unknown member
- WHEN a profile contains a member `key` whose value starts with `-----BEGIN RSA PRIVATE KEY-----`
- THEN validation fails with `ErrInlineSecret` (not with the unknown-member error), and the error text does not contain the value

#### ADDED Requirement: Key reference syntax
The system SHALL accept `private_key` values of the form `file:<absolute path>` or `file:~/<path>`, and SHALL reject `keyring:` references as reserved and every other form as unknown.

##### Scenario: Keyring reserved
- WHEN `private_key` is `keyring:claude-github-app`
- THEN validation fails with `ErrUnsupportedKeyReference` and the message says that keyring references are not supported yet

##### Scenario: Relative path
- WHEN `private_key` is `file:keys/claude.pem`
- THEN validation fails with `ErrUnsupportedKeyReference`

#### ADDED Requirement: Explicit profile selection
The system SHALL resolve a profile only by the name the user passes, and SHALL fail with an error listing the configured names when that name does not exist.

##### Scenario: Unknown profile
- WHEN profile `writer` is resolved in a config with `claude` and `reviewer`
- THEN the result is `ErrProfileNotFound` and the message lists `claude, reviewer`

#### ADDED Requirement: Safe config file
The system SHALL refuse to load a config file or directory that is writable by group or others or not owned by the current user, and SHALL write the file atomically with mode `0600` in a `0700` directory.

##### Scenario: Group-writable file
- WHEN the config file has mode `0664`
- THEN loading fails with `ErrInsecurePermissions` and the message names the `chmod` fix

##### Scenario: Atomic write
- WHEN a profile is written
- THEN the file has mode `0600`, its directory `0700`, and no temporary file remains

## 📝 Architecture

- **`internal/profile`** (new, #17) owns this contract: the `Config` and `Profile` types, `KeyRef` (scheme plus value), the sentinel errors, the pure `Validate`, the pure `Path(env)` resolver, `Load`, `Resolve`, and `Write`. It depends only on `kernel` and the standard library (`ARCHITECTURE.md`, Components).
- **`internal/credential`** (#16) receives a `profile.KeyRef` and loads the key; it does not parse the config. The `keyring` scheme reaches it only after a later version enables it.
- **`setup`** (#19) is the only writer in Phase 1; `doctor` (#24) writes only the refreshed `identity`.
- **`activation`** (#20) reads a resolved `Profile` and the derived Git identity; it never reads the file itself.
- The spec adds no Go module dependency, and no cross-module dependency beyond the rows already in `ARCHITECTURE.md`.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| No config yet | `ErrConfigNotFound` with the `setup` command to run. |
| Empty file | `ErrConfigInvalid`: not valid JSON. |
| `{}` | `ErrUnsupportedVersion`: version is missing. |
| File edited by hand with a trailing comma or a comment | `ErrConfigInvalid` with line and column. JSON has no comments; `setup` is the supported editor. |
| `"app_id": "123456"` (string) | `ErrConfigInvalid`: must be a positive integer. |
| `"app_id": 1e6` or `123456.0` | `ErrConfigInvalid`: a number with exponent or fraction is not an integer here. |
| Profile name `Claude` or `my_bot` | `ErrConfigInvalid`: invalid profile name with the allowed pattern. |
| Two `setup` runs at once for different profiles | The lock serializes them; both profiles are kept. |
| Config is a symlink into a dotfiles repository | Followed; the target's owner and mode are checked. |
| `identity` present but `app_id` changed by hand | Valid file; `doctor` reports that the cached login does not belong to the App. `setup` drops `identity` when it changes `app_id`. |
| Token pasted anywhere | `ErrInlineSecret` naming the path; the value is never printed; the message tells the user to revoke it. |
| Terminal escape sequences in `host` or a profile name | Rejected by the field rule; the echoed value has control characters replaced. |
| Very large file (for example a pasted log) | Rejected at 64 KiB before parsing. |

## 📝 Risks & Impact Review

- **Contract.** After the first release, changing the format or a field name needs a version bump, the in-memory migration, and a `.bak`. The format is decided (JSON) before #17 starts, so no migration is needed; changing the format later would mean supporting two decoders for at least one release.
- **Name.** The directory name follows #6. If the owner renames the product to `agent-id`, the path becomes `$XDG_CONFIG_HOME/agent-id/config.json`; no release exists yet, so no migration is needed as long as #6 is decided before the first release.
- **Strictness costs convenience.** No comments and no unknown members make hand editing less friendly. The trade-off is deliberate: a silently ignored member can change which credential or identity an agent gets.
- **Not a security boundary.** Permission checks stop accidents and other local users, not the agent itself, which runs as the same OS user and can rewrite the file (brief §10). The spec does not claim otherwise.
- **Product direction vs. rule.** The format was the owner's direction call and is decided (JSON). The rules for unknown members, duplicates, inline secrets, and fail-closed errors follow `AGENTS.md` and `CODE_REVIEW.md` and are not optional.

## 📝 Decisions in play

- Brief §9.4: the illustrative schema, replaced by this spec once the owner accepts it.
- Brief §14, *Profile selection*: explicit `--profile` only, no auto-detection.
- Brief §8, MVP boundaries: GitHub.com only.
- Brief §10, *Credential handling*: keyring or protected file; v1 implements the file, reserves the keyring.
- `ARCHITECTURE.md` OPEN rows: "Profile config format and schema; keyring support" (format decided by the owner on 2026-11-12: JSON with `encoding/json`; the implementation PR #17 flips the row to active and records that decision; keyring stays OPEN) and "Product and binary name" (#6, followed here).

## Resolved assumptions (autonomous defaults)

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | File format: YAML (new dependency), TOML (new dependency), or JSON (standard library)? | JSON with `encoding/json` and a strict token walk; file `config.json`. | No new dependency without the owner (`AGENTS.md`, Ask first); strict types and duplicate detection without extra guards; the file is written by `setup`. | Confirmed by the owner (Jacob, 2026-11-12): JSON with `encoding/json`; no YAML or TOML dependency. |
| A2 | Keyring references in v1? | `keyring:` is reserved and rejected with a clear error. | Keyring backend is OPEN and out of scope (#9, #16); reserving the prefix keeps the syntax stable. | reversible |
| A3 | Identity cache fields | `login`, `user_id`, `resolved_at`; name and email are derived. | One source of truth; matches `ARCHITECTURE.md` (Git integration). | reversible |
| A4 | Keep the brief's `integrations` section? | No; not in v1. | Explicit selection only (brief §14); the hook passes `--profile`. | reversible |
| A5 | Can a new optional member be added without a version bump? | No; every schema change bumps `version`. | Readers reject unknown members; a bump makes an older reader fail loudly instead of ignoring a field. | reversible before the first release |
| A6 | Config directory name | `aidentity`, following #6. | Working name in the brief and `ARCHITECTURE.md`; #6 owns the decision. | reversible before the first release |

## 📋 Phasing

- **Phase 1 — profile store (#17).** Contract, pure validation, path resolution, load, resolve, write. Independently shippable: nothing reads it until #19 and #20, but it is fully tested.
- **Phase 2 — credential source hook-up (#16).** `internal/credential` consumes `profile.KeyRef`; tracked in #16's own plan.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first and uses only files under the isolated `HOME`. All of Phase 1 is `risk-high` (profile selection and a shared contract, `SDLC.md`): hostile-input tables are required in each step.

### Phase 1 — `internal/profile` (#17)

1. **Contract.** Add `internal/profile` with its `AGENTS.md` (invariants: pure `Validate`, no value echo for secrets, strict members), `Config`, `Profile`, `Identity`, `KeyRef`, the sentinel errors (`ErrConfigNotFound`, `ErrConfigInvalid`, `ErrUnsupportedVersion`, `ErrUnsupportedHost`, `ErrUnsupportedKeyReference`, `ErrInlineSecret`, `ErrInsecurePermissions`, `ErrProfileNotFound`), and a row in `AGENTS.md` task routing. Test: the package builds; sentinels are distinct.
2. **Strict document decode.** Token walk with `encoding/json`: size limit, UTF-8, one top-level object, duplicate members, unknown members, JSON types. Table tests: trailing data, duplicate at every level, unknown at every level, `"123"` vs `123`, `1e6`, `1.0`, `2^53`.
3. **Inline-secret scan.** Pure function over all strings and member names. Table tests: each token prefix, PEM, JWT shape, newline, NUL, in a known member, an unknown member and a member name; assert the error text never contains the value.
4. **Field rules and `Validate`.** Version, profile names, provider, host (look-alikes from the Delta), integers, key references (every row of the Key references table), identity. Error order as specified. Golden test: the v1 example parses to the expected `Config`.
5. **Path resolution.** Pure `Path(getenv)`: XDG set, unset, empty, relative; macOS and Linux give the same result.
6. **Load and Resolve.** Read with the size limit, permission and owner checks (file, symlink target, directory), then `Validate`; `Resolve(name)` with `ErrProfileNotFound` listing names. Tests: missing file, `0664`, `0777` directory, symlink to a group-writable target, unknown profile, empty profiles.
7. **Write.** `flock` on `config.json.lock`, read-modify-write, temp file `0600`, `fsync`, `rename`; directory `0700`; drop `identity` when `host` or `app_id` changes. The `.bak` copy and the migration chain arrive with the first v2 change, not in this phase. Tests: modes, no temp file left, two concurrent writers keep both profiles.
8. **Identity derivation.** Pure `GitIdentity(Identity) (name, email)`. Test against the example.
9. **Record the decision.** Update `ARCHITECTURE.md` (Data and Key decisions: format row from OPEN to active: JSON with `encoding/json`, owner Jacob, 2026-11-12; keyring stays OPEN) and archive the Delta into `.ai/specs/capabilities/profile-config.md`.

Run the validation gate in an isolated environment after each step.
