# Profile configuration v1

Issue: #9 (E01-T04). Implemented by #17 (E02-S03, profile store), #16 (E02-S02, credential source) and the GitHub adapter (#15, under ADR #35); written by #19 (E02-S05, setup).

## 📝 TLDR

agent-id needs one file that says, for each agent profile, which provider and host it talks to and which provider credentials and cached bot identity it uses. Today only the illustrative YAML of `PRODUCT_BRIEF.md` §9.4 exists, so the profile store (#17) cannot start. This spec proposes the v1 contract: the file location, the format, a provider-agnostic core schema with a required `provider` field, the GitHub provider block (the only provider in the MVP), every field with its type, validation rule and error message, the private-key reference syntax, the identity cache, explicit profile selection, and the rules for future versions and future providers.

Owner decisions this spec follows (Jacob, 2026-11-12):

- **Format:** JSON read with the Go standard library `encoding/json`, with no YAML or TOML dependency (#9, #33).
- **Name:** `agent-id` everywhere: binary, config directory `$XDG_CONFIG_HOME/agent-id`, environment prefix `AGENT_ID_` (#6, #33).
- **Providers:** GitHub sits behind a provider abstraction (`internal/provider`, adapter `internal/provider/github`), so that GitLab and Bitbucket can be added later; core code imports nothing GitHub-specific (ADR #35).

## 📝 Problem Statement

- The profile config is a shared contract (`SDLC.md`, risk inference): every later command reads it, and a change after the first release needs a migration. The brief §9.4 schema is marked illustrative, and `ARCHITECTURE.md` lists "Profile config format and schema; keyring support" as **OPEN**.
- The file decides which key signs tokens and which identity commits carry. A loose reader is a security problem: a misspelled key that is silently ignored, a duplicate key whose last value wins, a host that is "almost" `github.com`, or a token pasted into the file all lead to the wrong credential or to a secret on disk.
- ADR #35 requires that GitLab and Bitbucket can be added later without rewriting core. A schema that puts `app_id` and `installation_id` next to core fields, or core Go types that carry them, would force every future provider to import GitHub concepts, and a profile of a provider that an older agent-id does not know would break every GitHub profile in the same file.
- Backlog items waiting on this contract: the profile store (#17), the credential source (#16), the GitHub adapter (#15), and setup (#19).

## 📝 Proposed Solution

One file per user, read strictly, written only by agent-id, split into a provider-agnostic core and one block per provider:

1. **Location.** `$XDG_CONFIG_HOME/agent-id/config.json`; when `XDG_CONFIG_HOME` is unset, empty, or not an absolute path, `$HOME/.config/agent-id/config.json`. The same rule applies on macOS: agent-id does not use `os.UserConfigDir` (which returns `~/Library/Application Support` there), so the path is the same on both platforms and `testenv.Isolate` covers it.
2. **Format.** JSON (RFC 8259), UTF-8, at most 64 KiB. Decoded with `encoding/json` and a strict token walk: unknown members, duplicate member names, and wrong JSON types are errors. agent-id writes the file with two-space indentation and a trailing newline.
3. **Core schema** (owned by `internal/profile`, provider-agnostic): `version: 1`, a `profiles` object keyed by profile name, and nothing else at the top level. Each profile has exactly three members: `provider` (required), `host` (required), and a **provider block** whose member name equals the value of `provider`.
4. **Provider blocks** (owned by the provider adapter). The core never interprets a provider block; it hands the raw block and the host to the adapter registered for that provider, which validates it strictly. In the MVP the only adapter is GitHub (`internal/provider/github`); its block holds `app_id`, `installation_id`, `private_key`, and the optional `identity` cache.
5. **Future providers do not break v1 profiles.** A profile whose `provider` has no registered adapter is kept, still checked by every document-level rule, but not validated further; resolving it fails with `ErrUnsupportedProvider`. Every other profile in the file loads normally, and a write preserves the unknown profile unchanged.
6. **No secrets in the file.** Key material is referenced (`file:` now, `keyring:` reserved), never inlined. Every string in the whole document, provider blocks included, is scanned for token and key material (generic patterns in core plus the token prefixes of every registered adapter), and a match rejects the whole file without echoing the value.
7. **Explicit selection.** A command that needs a profile takes `--profile <name>`. The file has no default profile, and agent-id does not detect a profile from the directory, the repository, or the environment (brief §14). There is no `AGENT_ID_PROFILE` selection variable.
8. **Validation is a pure function** (`ARCHITECTURE.md`, dependency rule 5): validation takes the bytes and the registered provider validators and touches no file system and no network. Loading, permission checks, and the existence of the key file are separate steps.

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **JSON, `encoding/json`** | No new dependency (`AGENTS.md`, Ask first). Strict types: `"123"` is not a number, `no` is not `false`. Unknown and duplicate members are detectable with the standard decoder. The file is written by `setup`, so hand editing is rare. | No comments. Less pleasant to edit by hand than YAML. Differs from the brief's example. | **Chosen** (owner's decision, 2026-11-12) |
| YAML (`go.yaml.in/yaml/v3`) | Matches brief §9.4. Comments. | New dependency. Implicit typing (`on`, `0700`, `1e3`) and anchors/aliases need extra guards. | Rejected by the owner (2026-11-12): no YAML dependency. |
| TOML | Comments, strict types, made for config. | New dependency. Nobody asked for it. | Rejected by the owner (2026-11-12): no TOML dependency. |
| **Provider block named after the provider** (`"github": {…}`) | The JSON shows which fields belong to which adapter; error paths read `profiles.claude.github.app_id`. Core and provider member names can never collide, so the core can grow without touching any provider. | One extra nesting level compared with the brief. | **Chosen** |
| Flat profile (provider fields next to `provider` and `host`) | Matches brief §9.4. | Core and adapter share one namespace: core must know which members belong to which provider, and a future core member can collide with a provider member. | Rejected. |
| Generic block name (`"settings": {…}`) | One name for every provider. | Error paths and the file itself no longer show which provider a field belongs to; a `provider` typo is less visible. | Rejected. |
| An unknown provider fails the whole file | Simplest; strictest. | A file that a newer agent-id extended with a GitLab profile would make an older agent-id unusable for its GitHub profiles: exactly what ADR #35 asks to avoid. | Rejected; the unknown profile alone fails, at resolve time. |
| One file per profile (`profiles/<name>.json`) | Concurrent `setup` of two profiles never conflicts. | Two places to look, harder `doctor` output, a directory listing becomes part of the contract. | Rejected for v1; a lock on the single file solves the conflict. |
| Identity cache in `$XDG_CACHE_HOME` | Keeps resolved data out of config. | Clearing the cache would make `exec` fail or call the provider. `ARCHITECTURE.md` (Data) places it with the profile. | Rejected. |

Comparable open-source tools point the same way. The AWS CLI selects named profiles explicitly (`--profile`) and keeps secrets in a separate `credentials` file. `gh` writes `hosts.yml` itself, but falls back to a plaintext token in that file when no keyring is available, which is exactly what v1 forbids. Both tolerate unknown keys; agent-id does not, because a silently ignored key here changes which credential an agent gets.

## 📝 Data Model

### Example (v1)

```json
{
  "version": 1,
  "profiles": {
    "claude": {
      "provider": "github",
      "host": "github.com",
      "github": {
        "app_id": 123456,
        "installation_id": 789012,
        "private_key": "file:~/.config/agent-id/keys/claude.pem",
        "identity": {
          "login": "claude-dev[bot]",
          "user_id": 12345678,
          "resolved_at": "2026-11-12T10:00:00Z"
        }
      }
    },
    "reviewer": {
      "provider": "github",
      "host": "github.com",
      "github": {
        "app_id": 456789,
        "installation_id": 987654,
        "private_key": "file:/Users/dev/keys/reviewer.pem"
      }
    }
  }
}
```

A later agent-id with a GitLab adapter could add a profile such as the one below to the same file without a version bump. A v1 agent-id that has no GitLab adapter keeps loading `claude` and `reviewer`, and fails only when asked to resolve `gl-bot` (the `gitlab` block's members here are illustrative, not part of this spec):

```json
    "gl-bot": {
      "provider": "gitlab",
      "host": "gitlab.com",
      "gitlab": { "…": "defined by the GitLab adapter's own spec" }
    }
```

### Difference from brief §9.4

The brief's example does **not** parse under this spec. The differences, each deliberate:

| Brief §9.4 | v1 | Why |
|---|---|---|
| YAML | JSON | Owner's decision (2026-11-12): JSON with `encoding/json`, no new dependency; strict typing. |
| `app_id`, `installation_id`, `private_key`, `identity` directly in the profile | Inside the `github` provider block | ADR #35: GitHub-specific fields belong to the GitHub adapter, not to core. |
| `private_key: keyring:claude-github-app` | Rejected with `ErrUnsupportedKeyReference`; use `file:` | The keyring backend is OPEN (`ARCHITECTURE.md`) and out of scope of #9 and #16. The `keyring:` prefix is reserved so that a later version can add it without a new syntax. |
| `identity.name`, `identity.email` | `github.identity.login`, `.user_id`, `.resolved_at` | The GitHub adapter derives name and email from login and id (`ARCHITECTURE.md`, Git integration): name `<login>`, email `<user_id>+<login>@users.noreply.github.com`. Storing them separately would allow a mismatch. The brief asks for resolved, cached identity rather than manual entry. |
| `integrations.claude-code` | Not in v1; unknown top-level member | Explicit selection only (brief §14). The Claude Code hook (#26) passes `--profile` on its command line. |
| `reviewer` without `identity` | Allowed | `identity` is optional until `setup` or `doctor` resolves it. |

### Core fields (`internal/profile`, provider-agnostic)

All members are required unless marked optional. Paths in error messages use dots: `profiles.claude.host`. Every error message starts with `config <path>: ` (the absolute file path), shown below as `…: `.

| Field | Type | Validation rule | Error (sentinel → message) |
|---|---|---|---|
| (document) | JSON object | Valid UTF-8 JSON, at most 64 KiB, one top-level object, no trailing data. | `ErrConfigInvalid` → `…: not valid JSON at line L, column C` / `…: larger than 64 KiB` |
| (any object, provider blocks included) | — | No member name appears twice in the same object. | `ErrConfigInvalid` → `…: <path>: duplicate member "<name>"` |
| (any string or member name, provider blocks included) | — | Must not contain token or key material (see Inline secrets). Checked before every other field rule. | `ErrInlineSecret` → `…: <path>: contains what looks like a token or private key; the config holds references only — remove the value and revoke it if it was real` |
| (top level, each profile) | — | Only the members listed here. A provider block's own members are checked by its adapter. | `ErrConfigInvalid` → `…: <path>: unknown member "<name>"` |
| `version` | integer | Present; exactly `1`. A higher integer means a newer agent-id wrote the file. | `ErrUnsupportedVersion` → `…: version is missing` / `…: version <n> is newer than this agent-id supports (1); upgrade agent-id` / `…: version must be the integer 1` |
| `profiles` | object | Present. May be empty. Keys are profile names. | `ErrConfigInvalid` → `…: profiles is missing` / `…: profiles must be an object` |
| profile name (key) | string | `^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`: lowercase letter first, then lowercase letters, digits, or `-`, never `-` last; 1–32 characters. The name ends up in cache file names and in diagnostics. | `ErrConfigInvalid` → `…: profiles: invalid profile name "<name>": use 1–32 lowercase letters, digits or "-", starting with a letter and not ending with "-"` |
| `profiles.<n>.provider` | string | Present; `^[a-z][a-z0-9-]{0,31}$`. Whether the value is supported is decided at resolve time (see Provider registry). | `ErrConfigInvalid` → `…: profiles.<n>.provider is missing` / `…: profiles.<n>.provider: invalid provider name` |
| `profiles.<n>.host` | string | Present; a lowercase DNS host name: labels of `[a-z0-9-]` not starting or ending with `-`, at least two labels, at most 253 characters, no scheme, port, path, trailing dot, or whitespace. Which hosts a provider accepts is the adapter's rule. | `ErrUnsupportedHost` → `…: profiles.<n>.host: "<v>" is not a valid host name (lowercase, no scheme, port or path)` |
| `profiles.<n>.<provider>` | object | Present, and named exactly as the value of `provider` (for `provider: github`, the member `github`). Its contents are opaque to core. | `ErrConfigInvalid` → `…: profiles.<n>: provider block "<provider>" is missing` |

### GitHub provider block (`internal/provider/github`)

Validated by the GitHub adapter, which receives the host and the raw `github` block. The adapter wraps the core sentinels, so `errors.Is` works the same for every provider.

| Field | Type | Validation rule | Error (sentinel → message) |
|---|---|---|---|
| `profiles.<n>.host` (GitHub rule) | string | Exactly `github.com`. GitHub Enterprise Server and custom hosts are deferred (brief §8). | `ErrUnsupportedHost` → `…: profiles.<n>.host: "<v>" is not supported by the github provider; this version accepts only github.com` |
| `profiles.<n>.github` | object | Only the members below. | `ErrConfigInvalid` → `…: profiles.<n>.github: unknown member "<name>"` |
| `….github.app_id` | integer | JSON number without fraction or exponent, 1 ≤ value ≤ 2⁵³−1. A string such as `"123456"` is rejected. | `ErrConfigInvalid` → `…: profiles.<n>.github.app_id: must be a positive integer` |
| `….github.installation_id` | integer | Same rule as `app_id`. | `ErrConfigInvalid` → `…: profiles.<n>.github.installation_id: must be a positive integer` |
| `….github.private_key` | string | A key reference, parsed with the core `ParseKeyRef` (see Key references). The value is never echoed. | `ErrUnsupportedKeyReference` → see Key references |
| `….github.identity` | object, optional | When present, all three members below are present. When absent, the profile is valid but its identity is unresolved. | `ErrConfigInvalid` → `…: profiles.<n>.github.identity: <member> is missing` |
| `….github.identity.login` | string | `^[a-z0-9][a-z0-9-]{0,62}\[bot\]$`: an App slug followed by `[bot]`. | `ErrConfigInvalid` → `…: profiles.<n>.github.identity.login: "<v>" is not a GitHub App bot login (expected "<slug>[bot]")` |
| `….github.identity.user_id` | integer | Same rule as `app_id`. | `ErrConfigInvalid` → `…: profiles.<n>.github.identity.user_id: must be a positive integer` |
| `….github.identity.resolved_at` | string | RFC 3339 timestamp in UTC (`Z`). | `ErrConfigInvalid` → `…: profiles.<n>.github.identity.resolved_at: must be an RFC 3339 UTC timestamp` |

Values are echoed in messages only for `provider`, `host`, `login`, and profile names: these are short identifiers, never secret, and the user needs to see the wrong value. Any echoed value is cut to 64 characters and has control characters replaced with `?`, so that a hostile file cannot inject terminal escapes into diagnostics.

When a document has several problems, validation returns the first in this order: document syntax and size, inline secrets, duplicate members, `version`, then profiles in document order: core members of the profile, then its provider block through the adapter. `ErrInlineSecret` comes first so that the user's first action is to remove and revoke the secret. An error in a profile of a **registered** provider fails the whole file, as before: a known profile that does not validate is a broken config, not a future one.

### Provider registry

- The core holds no list of providers. The composition root (`cmd/agent-id`) registers one validator per provider name; in the MVP that is `github` → the GitHub adapter.
- A profile whose `provider` is registered is validated by that adapter during load.
- A profile whose `provider` is not registered is **unsupported**: it must still pass every core rule above (syntax, inline secrets, duplicates, profile name, `provider` and `host` syntax, provider block present), but its provider block is not interpreted. Loading succeeds; `Resolve` on that profile fails with `ErrUnsupportedProvider` → `profile "<n>": provider "<v>" is not supported by this agent-id (supported: github); upgrade agent-id or fix the provider name`. `doctor` lists unsupported profiles.
- This is fail closed for the unsupported profile (it can never be activated) and keeps every other profile usable. The trade-off: a typo such as `"provider": "githb"` with a block named `githb` is reported when the profile is resolved, not when the file is loaded. A typo in only one of the two names is caught at load time, because the provider block would be missing.

### Key references

Key references are a core concept (`profile.KeyRef`, parsed by `profile.ParseKeyRef`), because every provider that signs with a local key needs the same syntax and the credential source (#16) loads keys without knowing the provider. In v1 only the GitHub block uses it, as `private_key`. Form `<scheme>:<value>`:

| Form | v1 behavior | Error |
|---|---|---|
| `file:/abs/path.pem` | Accepted. The path is cleaned with `filepath.Clean`. | — |
| `file:~/path.pem` | Accepted. `~/` expands to `$HOME` at load time, not at validation time. | — |
| `file:relative/path.pem`, `file:`, `file:~user/x` | Rejected: the meaning would depend on the working directory or on another user. | `ErrUnsupportedKeyReference` → `…: <path>: a file reference needs an absolute path or ~/…` |
| `keyring:<name>` | Rejected; the scheme is reserved for a later version. | `ErrUnsupportedKeyReference` → `…: <path>: keyring references are reserved and not supported yet; use file:<path>` |
| `/abs/path.pem` (no scheme), any other scheme, an empty string | Rejected. | `ErrUnsupportedKeyReference` → `…: <path>: unknown key reference; use file:<path>` |
| Contains a newline, a NUL byte, or key material | Rejected by the inline-secret scan before this rule. | `ErrInlineSecret` |

Validation checks the syntax only. Whether the file exists, is a regular file or a symlink to one, has mode `0600` or stricter, and holds a parseable key is the credential source's job (#16) at use time, and `doctor` reports it.

### Inline secrets

The scan runs in core over every string value and every member name in the document, including provider blocks of unregistered providers. It rejects a string that contains any of:

- a token prefix of a registered provider. Each adapter registers its prefixes together with its validator (ADR #35, "Authentication and tokens"). The GitHub adapter registers `ghs_`, `ghp_`, `gho_`, `ghu_`, `ghr_`, `github_pat_`, and the composition root always registers it, so the production scan always includes them;
- a PEM block marker: `-----BEGIN`;
- a JWT shape: three base64url segments separated by dots, the first starting with `eyJ` at a token boundary (the start of the string, a character that is not a letter or digit, or a percent-escape such as `%20`), with at least 15 characters in the first segment and 2 in the second; the third may be empty (an unsecured JWT). A word that merely contains `eyJ`, such as `MoneyJar.prod.pem`, is not a JWT (#63);
- a newline or a NUL byte (no v1 field holds multi-line text).

The scan itself is a core concern because the rule protects the file, not one provider. The token prefixes come from the adapters, because they are installation-token details and dependency rule 7 keeps those in the adapter. A new adapter brings its own prefixes (for example GitLab's `glpat-`); core keeps only the generic patterns. A false positive on an identifier is acceptable, because no v1 field legitimately holds these patterns. The error names the field path and never the value, and the loader never logs the raw document.

### Identity cache (GitHub provider)

`github.identity` is non-secret data that `setup` (#19) writes after it resolves the bot from GitHub, and `doctor` (#24) refreshes. It belongs to the profile's `host` and `github.app_id`: the GitHub adapter removes `identity` in any update that changes either. The adapter derives the Git identity from it, as the provider interface's "commit author / bot identity" responsibility (ADR #35):

- name: `<login>` (for example `claude-dev[bot]`);
- email: `<user_id>+<login>@users.noreply.github.com`.

Core never reads `identity`; the Git integration (`internal/gitint`) asks the provider for the Git author (ADR #35). Another provider defines its own identity cache in its own block.

A profile without `identity` is valid in the file. The proposed consumer rule for `exec` and `env` (#20) is to refuse such a profile with an error that points to `agent-id setup github --profile <n>`, rather than resolve the identity on every start; #20 owns that decision. `doctor` compares the cache with GitHub and reports a mismatch; it never trusts the cache as proof of anything.

### File system rules

- The directory is created with mode `0700` and the file with `0600`. The file holds no secret, but it decides which key file signs tokens and which identity commits carry, so it is treated like credentials configuration.
- Load refuses a file (after following symlinks) or directory that another user can write, or that the current user does not own: `ErrInsecurePermissions` → `config <path>: writable by group or others (mode 0664); run chmod 600 <path>`. A readable-by-others file is accepted.
- A missing file: `ErrConfigNotFound` → `no agent-id configuration at <path>; run agent-id setup github --profile <name>`.
- Writes take an exclusive `flock` on `config.json.lock` in the same directory (when `config.json` is a symlink, next to its target, so that every path to one file takes one lock; #63) (standard library `syscall.Flock`, macOS and Linux), read the current file, apply the change, write a temporary file in the same directory with mode `0600`, `fsync` it, and `rename` it over `config.json`. A crash leaves either the old or the new file, never a partial one.
- A write keeps the profiles of unregistered providers exactly as read (same members and values), so an older agent-id never drops a profile that a newer one created.
- agent-id never writes the file during a read, a migration, `exec`, or `env`.

### Profile selection

A command that needs a profile takes `--profile <name>`. The store's `Resolve(name)` returns the profile, `ErrProfileNotFound` → `profile "<name>" not found in <path>; available: claude, reviewer` (or `no profiles configured` when empty), or `ErrUnsupportedProvider` for a profile of an unregistered provider. There is no default profile, no `AGENT_ID_PROFILE` variable for selection, and no detection from the working directory or Git remote: those are Phase 2 decisions (#29).

### Versioning and migration

- `version` is an integer. A reader knows the set of versions 1…N it supports.
- `version` covers the core schema and the block schema of every provider that exists at that version. Every change to them, including a new optional member, increases `version`, because readers reject unknown members. This keeps "an older agent-id ignored a field it did not know" impossible.
- **Adding a new provider is not a schema change** and does not increase `version`: older readers treat the new provider's profiles as unsupported (see Provider registry) and keep working for every provider they know. A v1 GitHub profile therefore stays valid, unchanged, when GitLab or Bitbucket support arrives.
- A reader that meets an older version migrates it **in memory** through a chain of pure functions (`v1→v2`, `v2→v3`, …), each with its own table test. A migration of a provider block lives in that provider's adapter. The reader does not rewrite the file.
- The next write (for example `setup`) writes the newest version and first copies the old file to `config.json.v<old>.bak` with mode `0600`.
- A reader that meets a newer version fails with `ErrUnsupportedVersion` and asks the user to upgrade. Downgrading agent-id after a write of a newer version needs the `.bak` file; this is documented in the release notes of the version that bumps the schema.

## 📝 Delta

### Capability: profile-config

#### ADDED Requirement: Config location
The system SHALL read the profile configuration from `$XDG_CONFIG_HOME/agent-id/config.json`, or from `$HOME/.config/agent-id/config.json` when `XDG_CONFIG_HOME` is unset, empty, or relative, on both macOS and Linux.

##### Scenario: XDG directory set
- WHEN `XDG_CONFIG_HOME=/tmp/x` and a command resolves the config path
- THEN the path is `/tmp/x/agent-id/config.json`

##### Scenario: Relative XDG directory
- WHEN `XDG_CONFIG_HOME=rel` and `HOME=/home/dev`
- THEN the path is `/home/dev/.config/agent-id/config.json`

#### ADDED Requirement: Strict v1 schema
The system SHALL accept a config only when it is a JSON object with `version: 1` and a `profiles` object whose entries match the core field table and, for registered providers, the provider's block table, and SHALL reject unknown members, duplicate members, and wrong types with an error that names the field path.

##### Scenario: Valid config resolves
- WHEN the v1 example of this spec is validated and profile `claude` is resolved
- THEN every field equals the example, and the GitHub adapter derives the author email `12345678+claude-dev[bot]@users.noreply.github.com`

##### Scenario: Duplicate member
- WHEN the `github` block of a profile contains `"app_id"` twice
- THEN validation fails with `ErrConfigInvalid` naming `profiles.claude.github` and `app_id`

##### Scenario: Misspelled member
- WHEN the `github` block contains `privat_key`
- THEN validation fails with `ErrConfigInvalid` naming the unknown member

##### Scenario: Newer version
- WHEN the config has `"version": 2`
- THEN validation fails with `ErrUnsupportedVersion` and the message asks to upgrade agent-id

#### ADDED Requirement: Provider field
The system SHALL require a `provider` member and a provider block of the same name in every profile, and in the MVP SHALL support the provider `github`.

##### Scenario: Missing provider
- WHEN a profile has `host` and a `github` block but no `provider`
- THEN validation fails with `ErrConfigInvalid` naming `profiles.claude.provider`

##### Scenario: Block does not match provider
- WHEN a profile has `"provider": "github"` and a `gitlab` block but no `github` block
- THEN validation fails with `ErrConfigInvalid` saying that provider block `github` is missing

#### ADDED Requirement: Unknown providers are isolated
The system SHALL load a config that contains profiles of providers it does not support, SHALL keep every other profile usable, SHALL fail to resolve the unsupported profile, and SHALL preserve it unchanged on write.

##### Scenario: GitHub profile next to a future provider
- WHEN a v1 config contains the GitHub profile `claude` and a profile `gl-bot` with `"provider": "gitlab"` and a `gitlab` block, and this agent-id supports only `github`
- THEN resolving `claude` succeeds, and resolving `gl-bot` fails with `ErrUnsupportedProvider` naming `gitlab` and the supported `github`

##### Scenario: Write keeps the unknown profile
- WHEN `setup` writes profile `claude` into that file
- THEN `gl-bot` is still present with the same members and values

##### Scenario: Secrets are scanned in unknown blocks
- WHEN the `gitlab` block of `gl-bot` contains a value starting with `-----BEGIN`
- THEN loading fails with `ErrInlineSecret` for the whole file

#### ADDED Requirement: GitHub provider accepts github.com only
The system SHALL accept `host: github.com` only for profiles with `provider: github`, and SHALL reject any other host, including look-alikes.

##### Scenario: Enterprise host rejected
- WHEN a GitHub profile has `host: github.example.com`
- THEN validation fails with `ErrUnsupportedHost` and the message says that only github.com is accepted

##### Scenario: Look-alike host rejected
- WHEN a GitHub profile has `host` set to `GitHub.com`, `github.com.`, `https://github.com`, `github.com:443`, or `api.github.com`
- THEN validation fails with `ErrUnsupportedHost`

#### ADDED Requirement: No inline secrets
The system SHALL reject a config in which any string contains token or key material, and SHALL NOT include that value in any error, log, or output.

##### Scenario: Token pasted into private_key
- WHEN `github.private_key` is `ghs_` followed by token characters
- THEN validation fails with `ErrInlineSecret` naming `profiles.claude.github.private_key`, and the error text does not contain the value

##### Scenario: PEM pasted into an unknown member
- WHEN a `github` block contains a member `key` whose value starts with `-----BEGIN RSA PRIVATE KEY-----`
- THEN validation fails with `ErrInlineSecret` (not with the unknown-member error), and the error text does not contain the value

#### ADDED Requirement: Key reference syntax
The system SHALL accept key references of the form `file:<absolute path>` or `file:~/<path>`, and SHALL reject `keyring:` references as reserved and every other form as unknown.

##### Scenario: Keyring reserved
- WHEN `github.private_key` is `keyring:claude-github-app`
- THEN validation fails with `ErrUnsupportedKeyReference` and the message says that keyring references are not supported yet

##### Scenario: Relative path
- WHEN `github.private_key` is `file:keys/claude.pem`
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

```mermaid
flowchart LR
    Root["cmd/agent-id (composition root)"] -->|registers github validator| Profile["internal/profile (core, new)"]
    Root --> GH["internal/provider/github (adapter, new)"]
    GH -->|KeyRef, ParseKeyRef, sentinels| Profile
    Cred["internal/credential (new)"] -->|KeyRef| Profile
    Act["internal/activation (planned)"] -->|Profile: name, provider, host| Profile
    Act -->|adapter for the profile| Prov["internal/provider (interface, ADR #35)"]
    GH -.implements.-> Prov
```

Core packages depend on `internal/profile` and on the provider interface; only the adapter and the composition root know GitHub.

- **`internal/profile`** (core, #17) owns the provider-agnostic contract. Go types (indicative):
  ```go
  type Config struct {
      Version  int
      Profiles map[string]Profile
  }
  type Profile struct {
      Name     string
      Provider string          // "github" in the MVP; checked against the registry
      Host     string          // syntax only; the adapter decides which hosts it accepts
      Settings json.RawMessage // the provider block, opaque to core
  }
  type KeyRef struct{ Scheme, Value string }
  // SettingsValidator is the narrow port a provider adapter satisfies.
  type SettingsValidator interface {
      ValidateSettings(host string, settings json.RawMessage) error
  }
  ```
  It also owns `ParseKeyRef`, the sentinel errors (`ErrConfigNotFound`, `ErrConfigInvalid`, `ErrUnsupportedVersion`, `ErrUnsupportedProvider`, `ErrUnsupportedHost`, `ErrUnsupportedKeyReference`, `ErrInlineSecret`, `ErrInsecurePermissions`, `ErrProfileNotFound`), the pure validation, the pure `Path(getenv)` resolver, `Load`, `Resolve`, and `Write`. It imports no provider package, not even `internal/provider`, so that adapters can import it without a cycle. Nothing in it names GitHub: the token prefixes of the inline-secret scan come from the registered adapters.
- **`internal/provider/github`** (adapter, ADR #35, #15) owns the GitHub block: `ProfileSettings{AppID, InstallationID int64; PrivateKey profile.KeyRef; Identity *BotIdentity}`, `BotIdentity{Login string; UserID int64; ResolvedAt time.Time}`, the strict decode of the `github` block, the `github.com` host rule, the login pattern, the identity-drop rule on `app_id` or host change, and the derivation of the Git author. It satisfies `profile.SettingsValidator`, registers the GitHub token prefixes, and imports `internal/profile` (adapter → core). `ARCHITECTURE.md` records this dependency, and the dependencies of `internal/credential` and `internal/activation` below, in Key decisions (2026-11-13, PR #38).
- **`internal/provider`** (interface, ADR #35) exposes the provider responsibilities to core (tokens, commit author, credential helper, CLI integration, host mapping). This spec adds nothing to it. The adapter satisfies `profile.SettingsValidator` directly, and the composition root registers it with its token prefixes.
- **`internal/credential`** (#16) receives a `profile.KeyRef` and loads the key; it does not parse the config and does not know the provider. The `keyring` scheme reaches it only after a later version enables it.
- **`setup`** (#19, GitHub adapter) is the only writer in Phase 1; `doctor` (#24) writes only the refreshed `github.identity`.
- **`activation`** (#20) reads a resolved `Profile`, takes the adapter registered for its `provider`, and hands it to `gitint` and `cliint`. Those ask the adapter for the Git author, hosts, and CLI data, and the token manager supplies the tokens. Activation never reads a provider block or the file itself.
- The spec adds no Go module dependency.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| No config yet | `ErrConfigNotFound` with the `setup` command to run. |
| Empty file | `ErrConfigInvalid`: not valid JSON. |
| `{}` | `ErrUnsupportedVersion`: version is missing. |
| File edited by hand with a trailing comma or a comment | `ErrConfigInvalid` with line and column. JSON has no comments; `setup` is the supported editor. |
| `"app_id": "123456"` (string) | `ErrConfigInvalid`: must be a positive integer. |
| `"app_id": 1e6` or `123456.0` | `ErrConfigInvalid`: a number with exponent or fraction is not an integer here. |
| Brief-style flat profile (`app_id` next to `provider`) | `ErrConfigInvalid`: unknown member `app_id` in the profile, and the provider block is missing. |
| Profile name `Claude` or `my_bot` | `ErrConfigInvalid`: invalid profile name with the allowed pattern. |
| Profile name `claude-` (trailing `-`), `-a` (leading `-`), or 33 characters long | `ErrConfigInvalid`: invalid profile name with the allowed pattern. |
| Profile name `a--b` | Accepted: an inner `--` is allowed (#44). |
| Profile of a provider this agent-id does not know | The file loads; that profile fails to resolve with `ErrUnsupportedProvider`; `doctor` lists it; writes keep it. |
| Typo `"provider": "githb"` with a `github` block | `ErrConfigInvalid`: provider block `githb` is missing (and `github` is an unknown member). |
| Two `setup` runs at once for different profiles | The lock serializes them; both profiles are kept. |
| Config is a symlink into a dotfiles repository | Followed; the target's owner and mode are checked. |
| `identity` present but `app_id` changed by hand | Valid file; `doctor` reports that the cached login does not belong to the App. `setup` drops `identity` when it changes `app_id`. |
| Token pasted anywhere, including an unknown provider's block | `ErrInlineSecret` naming the path; the value is never printed; the message tells the user to revoke it. |
| Terminal escape sequences in `host` or a profile name | Rejected by the field rule; the echoed value has control characters replaced. |
| Very large file (for example a pasted log) | Rejected at 64 KiB before parsing. |

## 📝 Risks & Impact Review

- **Contract.** After the first release, changing the format or a field name needs a version bump, the in-memory migration, and a `.bak`. The format (JSON), the name (`agent-id`), and the provider split (ADR #35) are all decided before #17 starts, so no migration is needed for them.
- **Adding providers.** The provider block and the unsupported-profile rule let GitLab or Bitbucket arrive without a version bump and without breaking any v1 GitHub profile. The cost is that a profile of an unregistered provider is only reported when resolved or in `doctor`, not as a load error.
- **Strictness costs convenience.** No comments and no unknown members make hand editing less friendly. The trade-off is deliberate: a silently ignored member can change which credential or identity an agent gets.
- **Not a security boundary.** Permission checks stop accidents and other local users, not the agent itself, which runs as the same OS user and can rewrite the file (brief §10). The spec does not claim otherwise.
- **Product direction vs. rule.** The format, name and provider split are the owner's decisions. The provider block naming and the unsupported-profile rule (A7, A8) implement ADR #35 and were confirmed by the manager on 2026-11-13. The rules for unknown members, duplicates, inline secrets, and fail-closed errors follow `AGENTS.md` and `CODE_REVIEW.md` and are not optional.

## 📝 Decisions in play

- Brief §9.4: the illustrative schema, replaced by this spec.
- Brief §14, *Profile selection*: explicit `--profile` only, no auto-detection.
- Brief §8, MVP boundaries: GitHub.com only.
- Brief §10, *Credential handling*: keyring or protected file; v1 implements the file, reserves the keyring.
- Owner decisions of 2026-11-12: JSON with `encoding/json` (#9, #33); the name `agent-id` with `AGENT_ID_` (#6, #33); the provider abstraction (ADR #35), which supersedes the planned `internal/github` and brief §13's deferral of a cross-provider abstraction.
- `ARCHITECTURE.md` OPEN row "Profile config format and schema; keyring support": the format and schema are decided here; the implementation PR #17 flips the row to active (keyring stays OPEN).

## Resolved assumptions (autonomous defaults)

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | File format | JSON with `encoding/json` and a strict token walk; file `config.json`. | Owner's decision. | Confirmed by the owner (Jacob, 2026-11-12): no YAML or TOML dependency. |
| A2 | Keyring references in v1? | `keyring:` is reserved and rejected with a clear error. | Keyring backend is OPEN and out of scope (#9, #16); reserving the prefix keeps the syntax stable. | reversible |
| A3 | Identity cache fields | GitHub block: `login`, `user_id`, `resolved_at`; name and email are derived by the adapter. | One source of truth; matches `ARCHITECTURE.md` (Git integration); identity belongs to the provider (ADR #35). | reversible |
| A4 | Keep the brief's `integrations` section? | No; not in v1. | Explicit selection only (brief §14); the hook passes `--profile`. | reversible |
| A5 | When does `version` increase? | On every change to the core schema or to an existing provider's block, including new optional members; never for adding a new provider. | Readers reject unknown members, so a change must fail loudly in older readers; new providers are isolated instead (A8). | reversible before the first release |
| A6 | Config directory name | `agent-id` (`$XDG_CONFIG_HOME/agent-id`), environment prefix `AGENT_ID_`. | Owner's decision. | Confirmed by the owner (Jacob, 2026-11-12). |
| A7 | Where do provider-specific fields go? | In a provider block named after the provider (`"github": {…}`); core members are only `provider`, `host` and that block. | ADR #35: provider fields belong to the adapter; no namespace collision between core and providers; clear error paths. | Confirmed (manager, 2026-11-13). |
| A8 | What does an older agent-id do with a profile of a provider it does not know? | Loads the file, keeps the profile, fails only when that profile is resolved (`ErrUnsupportedProvider`), and preserves it on write. | ADR #35: later providers must not break v1 GitHub profiles; the unknown profile still fails closed. | Confirmed (manager, 2026-11-13). |

## 📋 Phasing

- **Phase 1 — core profile store (#17).** Provider-agnostic contract, pure validation with a validator registry, path resolution, load, resolve, write. Tested with a fake provider validator; independently shippable.
- **Phase 2 — GitHub profile block (#15, after ADR #35's interface exists).** `internal/provider/github` validates and decodes the `github` block and derives the Git author; the composition root registers it.
- **Phase 3 — credential source hook-up (#16).** `internal/credential` consumes `profile.KeyRef`; tracked in #16's own plan.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first and uses only files under the isolated `HOME`. Phases 1 and 2 are `risk-high` (profile selection and a shared contract, `SDLC.md`): hostile-input tables are required in each step. Run the validation gate in an isolated environment after each step.

### Phase 1 — `internal/profile` (#17)

1. **Contract.** Add `internal/profile` with its `AGENTS.md` (invariants: provider-agnostic, imports no provider package; pure validation; no value echo for secrets; strict members), the types and sentinel errors of the Architecture section, the `SettingsValidator` port, and a row in `AGENTS.md` task routing. Test: the package builds; sentinels are distinct.
2. **Strict document decode.** Token walk with `encoding/json`: size limit, UTF-8, one top-level object, duplicate members at every depth (provider blocks included), JSON types; provider blocks are captured as raw JSON. Table tests: trailing data, duplicate at every level, `"123"` vs `123`, `1e6`, `1.0`, `2^53`.
3. **Inline-secret scan.** Pure function over all strings and member names, provider blocks included, with the generic patterns and the prefixes of the registered validators. Table tests (with a fake validator that registers its own prefixes): each registered prefix, PEM, JWT shape, newline, NUL, in a core member, a provider block, an unknown provider's block, and a member name; assert the error text never contains the value.
4. **Core rules and validation.** Version, profile names, `provider` presence and syntax, `host` syntax, provider block present and matching, unknown core members, `ParseKeyRef` (every row of the Key references table), the validator registry, and unsupported-provider profiles. Tests with a fake validator: a registered provider's error fails the file; an unregistered provider's profile loads and is unsupported; error order as specified.
5. **Path resolution.** Pure `Path(getenv)`: XDG set, unset, empty, relative; macOS and Linux give the same `agent-id` path.
6. **Load and Resolve.** Read with the size limit, permission and owner checks (file, symlink target, directory), then validation; `Resolve(name)` with `ErrProfileNotFound` listing names and `ErrUnsupportedProvider` for unregistered providers. Tests: missing file, `0664`, `0777` directory, symlink to a group-writable target, unknown profile, empty profiles, unsupported provider.
7. **Write.** `flock` on `config.json.lock`, read-modify-write, temp file `0600`, `fsync`, `rename`; directory `0700`; profiles of unregistered providers preserved unchanged. The `.bak` copy and the migration chain arrive with the first v2 change, not in this phase. Tests: modes, no temp file left, two concurrent writers keep both profiles, an unknown-provider profile survives a write.
8. **Record the decision.** Update `ARCHITECTURE.md` (Data and Key decisions: format and schema row from OPEN to active with JSON, the provider block, owner Jacob, 2026-11-12; keyring stays OPEN) and archive the core requirements of the Delta into `.ai/specs/capabilities/profile-config.md`.

### Phase 2 — GitHub block in `internal/provider/github` (#15, after ADR #35)

9. **GitHub settings.** `ProfileSettings`, `BotIdentity`, strict decode of the `github` block, `github.com` host rule (look-alikes from the Delta), integers, `private_key` through `profile.ParseKeyRef`, identity. Satisfies `profile.SettingsValidator` and registers the GitHub token prefixes; errors wrap the core sentinels. Test: with the production registry, a `ghs_` value anywhere in the file fails with `ErrInlineSecret`. Golden test: the v1 example parses to the expected settings.
10. **Git author and identity updates.** Pure derivation of name and email from `BotIdentity`; an update that changes `host` or `app_id` drops `identity`. Tests against the example.
11. **Wire and archive.** `cmd/agent-id` registers the GitHub validator; a test proves no package outside `internal/provider/github` and `cmd/agent-id` imports it (the ADR #35 import rule); archive the GitHub requirements of the Delta into the capability file.
