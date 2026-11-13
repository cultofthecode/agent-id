# agent-id whoami and doctor — output contract, checks, and redaction

Issue: #24 (E02-S10). Implemented by #24 after #22 and #23.

Extends: `.ai/specs/2026-11-13-exec-child-environment.md` (reads its context variables and re-checks its Git markers).
Extends: `.ai/specs/2026-11-12-profile-config.md` (`doctor` lists unsupported profiles and refreshes the identity cache, as that spec and `ARCHITECTURE.md` (Data) already assign to it).

## 📝 TLDR

A developer who debugs authentication needs to see which context, profile, bot, installation, credential and token a shell uses, and which configuration around it conflicts with agent mode, without any secret on the screen. Today neither command exists. This spec proposes:

- `agent-id whoami` prints the fields of brief §7.4. Outside a context it prints only `personal/unmanaged`.
- `agent-id doctor --profile <name>` runs a fixed catalog of checks with stable ids. The checks cover configuration, key file, token mint, identity, repository access, Git version, the Git conflicts D1–D7 of the precedence analysis, inherited tokens, `gh`, and the agent context.
- Both commands have a human text format and a versioned `--json` format for scripts.
- Every byte of output passes three redaction layers: no secret is collected, every string is redacted, and the rendered output is scanned and withheld if anything still matches.

## 📝 Problem Statement

- Brief §5 (last job), §7.4 and §8 *Diagnostics* (P0): explain the active profile, the resolved identity, configuration conflicts and access failures without exposing tokens.
- The Git precedence analysis shows that some leaks cannot be neutralized through Git configuration, only detected: a personal `insteadOf` to SSH (D1), credentials in a URL (D2), a path-scoped `extraHeader` (D3), an inherited `GIT_CONFIG_PARAMETERS` (D4), and shell startup files that undo the context (D6). The decisions of 13.11 (O1–O4, `ARCHITECTURE.md` Key decisions) make `exec` refuse some of these and leave the rest for `doctor` to report.
- The exec spec leaves to `doctor` the report of inherited token-shaped values that `exec` does not remove (its OQ3).
- Scripts parse this output (triage of #24: raised to `risk-high`). A diagnostic command reads the very things that hold secrets: Git configuration, remote URLs, the environment, the key file, the token. It is the most likely place for a token to reach a terminal, a CI log, or an issue comment in a public repository.

## 📝 Proposed Solution

1. **One report model, two renderers.** Each command builds a typed report: no field of it can hold a secret value. Then it renders the report as text (default) or as JSON (`--json`). The JSON schema is versioned and is the contract for scripts. In the text format, the `whoami` labels and order are stable. The `doctor` text is for humans and may change.
2. **`whoami` reads the context, not the configuration of the developer.** The context variables of #20 decide the context. Outside a context, `whoami` never reads Git or `gh` configuration and never guesses a person.
3. **`doctor` is a list of pure checks over a snapshot.** Gatherers collect a `Snapshot` through narrow ports: the profile store, the credential source (#16), the provider, `git`, `ssh -G`, and `gh` on `PATH`. Each check is a pure function `Snapshot → Result`, with table tests that include hostile input (`ARCHITECTURE.md`, dependency rule 5).
4. **Redaction in three layers.**
   - Collection: values that can hold credentials are not collected.
   - Rendering: a pure `Redact` runs on every string.
   - A final guard scans the rendered bytes, and withholds the output with exit 3 if anything still matches.
5. **Network only where the brief asks for it.** `doctor` mints a token, resolves the identity, and checks repository access (brief §7.1). `whoami` obtains the token through the token manager, as the credential helper does (OQ1). Every network call goes through the provider interface and reaches only the fake GitHub in tests.
6. **`doctor` fixes nothing except the identity cache.** It refreshes `identity` in the profile's provider block when the provider reports a different or missing bot. `ARCHITECTURE.md` (Data) and the profile spec assign this to it. Every other finding gets a `fix:` line for the developer to act on. Fixing conflicts automatically is out of scope (#24).

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **Typed report + JSON contract + three redaction layers** | Scripts get a stable schema. A secret has no field to live in. A redaction bug in one layer is caught by the next. | More code than printing strings. | **Chosen** |
| Text only, stable columns | Smaller. | Scripts would parse aligned text, and every wording fix would become a breaking change. | Rejected |
| Redaction only at the end (output filter) | One place. | It cannot know which free text came from a credential. A filter miss prints the secret. | Rejected as the only layer; kept as layer 3 |
| `doctor` read-only, identity refresh only through `setup` | Diagnostics never write. | Contradicts `ARCHITECTURE.md` (Data: "refreshed by `setup` and `doctor`") and the profile spec. A stale cache would then need a second command. | Rejected; the write is limited to `identity` and reported |
| `whoami` reads the token cache only | Offline, instant. | In a fresh session the cache is empty, so the brief's `Token status: valid (expires in Nm)` would never appear before the first Git operation. It also hides broken credentials. | Rejected (OQ1) |

Comparable tools: `gh auth status` prints a masked token prefix (`ghs_****`). agent-id prints no part of a token, because a prefix alone fails the scan that #24 requires. `aws sts get-caller-identity` returns a JSON identity for scripts, and `brew doctor` and `flutter doctor` print a check list with a fix per finding, which is the shape adopted here.

## 📝 Delta

### Capability: whoami

#### ADDED Requirement: Agent context report
The system SHALL print, inside an agent context, the fields Context, Profile, Provider, Identity, Installation, Credentials, Token status and Activation, in this order. The token status SHALL come from the token manager.

##### Scenario: Exec session
- WHEN `agent-id exec --profile claude -- agent-id whoami` runs against the fake GitHub
- THEN the output is the example of API Contracts with `Token status:  valid (expires in 59m)` (rounded down) and exit status 0

#### ADDED Requirement: No guess outside a context
The system SHALL print `Context:       personal/unmanaged` and nothing else outside an agent context, and SHALL NOT read Git, `gh`, or agent-id configuration to do so.

##### Scenario: Personal terminal with a Git identity
- WHEN `whoami` runs without `AGENT_ID_CONTEXT`, with a global `user.name`, a `GH_TOKEN` and a `hosts.yml` in the isolated environment
- THEN the output is exactly `Context:       personal/unmanaged`, the exit status is 0, and no child process was started (the test's fake `git` and `gh` on `PATH` record zero calls)

#### ADDED Requirement: Token failure is visible
The system SHALL report a token that cannot be obtained as `unavailable (<class>)`, with the provider's failure class and no secret, and SHALL exit with 1.

##### Scenario: Installation deleted
- WHEN the fake GitHub answers 404 for the installation
- THEN `Token status:  unavailable (not found: the installation does not exist or the App lost access)` and the exit status is 1

### Capability: doctor

#### ADDED Requirement: Conflicts are reported with a fix
The system SHALL report each conflict of the check catalog with its check id, status, source, and a `fix:` line.

##### Scenario: Personal helper and inherited token
- WHEN `doctor --profile claude` runs with a global `credential.https://github.com.helper` and an inherited `GH_TOKEN`
- THEN `git.helper.personal` is `warn` with the source `~/.gitconfig:<line>`, `env.credential` is `warn` naming `GH_TOKEN`, each has a `fix:` line, and the exit status is 0

##### Scenario: SSH remote after the rewrites
- WHEN the repository's `origin` fetch URL resolves to `git@github.com:o/r.git` because of a personal `url.git@github.com:.insteadOf=https://github.com/`
- THEN `git.remote.ssh` is `fail`, it names the rule and its source file, and the exit status is 1

#### ADDED Requirement: Exit status
The system SHALL exit with 0 when no check failed, 1 when at least one check failed, 2 on a usage error, and 3 when the output was withheld by the redaction guard.

### Capability: diagnostics-redaction

#### ADDED Requirement: No secret in any output
The system SHALL NOT write a token, private key, JWT, URL credential, or `Authorization` value to stdout or stderr in any mode of `whoami` or `doctor`.

##### Scenario: Decoys everywhere
- WHEN the isolated environment holds a fake token in `GH_TOKEN`, in `HOMEBREW_GITHUB_API_TOKEN`, in a remote URL's user-info, in a path-scoped `http.extraHeader`, in a credential helper command line, in `GIT_CONFIG_PARAMETERS`, in the token cache, and a PEM key in the key file, and `whoami`, `doctor`, `whoami --json` and `doctor --json` run inside and outside a context
- THEN no output contains `ghs_`, `ghp_`, `gho_`, `ghu_`, `ghr_`, `github_pat_`, `-----BEGIN`, a JWT shape, or any decoy value, and every decoy is still reported by its variable name or source location

##### Scenario: Guard
- WHEN a test injects a check result whose detail contains a fake token
- THEN `Redact` removes it, and with `Redact` replaced by the identity function the guard withholds the output and exits with 3

## 📝 Architecture

```mermaid
flowchart LR
    Root["cmd/agent-id"] --> W["internal/diagnose/whoami (new slice)"]
    Root --> D["internal/diagnose/doctor (new slice)"]
    W --> Diag["internal/diagnose (new module): Report, checks, Redact, Guard, renderers"]
    D --> Diag
    Diag --> Act["internal/activation (#20): ReadContext, CheckInheritedGit, ParseGitConfigParameters"]
    Diag --> Prov["internal/provider (port)"]
    Diag -.-> Tok["internal/token (#18)"]
    Diag -.-> Git["internal/gitint (#21, #22): expected entries, remote classification"]
    Diag -.-> Cli["internal/cliint (#23): shim location"]
    Diag -.-> Cred["internal/credential (#16): key file inspection"]
```

Dashed edges are modules planned by the named issues. `internal/diagnose` depends on them through their root packages, as the `ARCHITECTURE.md` component table already lists.

- **`internal/diagnose`** (module root, new). It holds:
  - `Report`, `Field`, `Result{ID, Status, Title, Detail, Source, Fix string}` and `Status` (`ok`, `info`, `warn`, `fail`, `skip`).
  - `Snapshot`, with the gathered inputs.
  - The checks, as pure functions `func(Snapshot) []Result`.
  - The pure `Redact(s string, p Patterns) string`, and `Guard(out []byte, p Patterns) error`.
  - `RenderText` and `RenderJSON`, and `AbbreviateHome(path, home string) string`.

  Its `AGENTS.md` records the invariants: no report field holds a secret; every string passes `Redact`; output passes `Guard`; checks are pure; no forge names (labels and hosts come from the provider).
- **Slices** `internal/diagnose/whoami` and `internal/diagnose/doctor`. Each has `contract.go`, `handler.go` (gather through narrow ports, then call the root's pure functions), `command.go`, and `handler_test.go`. `doctor` requires `--profile` (explicit selection, profile spec). Both take `--json`. Flag errors exit 2 (`SetFlagErrorFunc`), and the default of `run` stays 1 for other commands.
- **Provider interface.** The provisional `internal/provider.Provider` (PR #32), plus the two answers that the exec spec adds (`CredentialVariables`, `Ready`). This spec adds the following, settled with #15:
  - `TokenPrefixes() []string`: already required by the profile spec for its secret scan, and reused here for `Patterns`.
  - `Describe() []Field`: the provider-specific `whoami` lines and labels. For GitHub: `Installation` = the installation id, `Credentials` = `GitHub App installation token`.
  - `ResolveIdentity(ctx, token) (CachedIdentity, error)`: the bot as the host reports it, used by `setup` (#19) as well.
  - `RepositoryAccess(ctx, token, repo string) error`: whether the installation can read `owner/name`. It returns the provider failure classes.

  The core never names `installation` or `GitHub`. The GitHub adapter supplies the labels.
- **Token manager (#18).** `whoami` calls the same `Get(ctx, profile)` that the credential helper uses, which serves the cache or mints a token. `doctor` calls `provider.Token` directly, so the check proves that a mint works now, and does not store the result. Only `ExpiresAt` leaves the call. `provider.Token` already redacts its value under every `fmt` verb.
- **Credential source (#16).** It exposes `Inspect(KeyRef) KeyStatus{Exists, Regular, OwnedByUser, Mode, Parses bool…}` without returning the key material.
- **Git integration (#21, #22).** It exposes, as pure functions, the expected `GIT_CONFIG_*` entries for a profile and `ClassifyRemote(url, hostMapping)`. `ClassifyRemote` reports whether a URL is the provider's host over HTTPS, over SSH, through an SSH alias, or another host. `doctor` uses the same rewrite set that `exec` applies, so the two can never disagree.
- **`internal/activation` (#20).** `ReadContext`, `CheckInheritedGit` and `ParseGitConfigParameters` are reused: `doctor` reports with the very functions that make `exec` refuse.

## 📝 API Contracts

### `agent-id whoami [--json]`

Text, inside an agent context. Labels are padded to 15 columns, and the labels and their order are stable:

```text
Context:       agent
Profile:       claude
Provider:      github (github.com)
Identity:      claude-dev[bot]
Installation:  12345678
Credentials:   GitHub App installation token
Token status:  valid (expires in 42m)
Activation:    exec
```

- `Context` is `agent`, `agent (damaged: <reason>)` or `agent (incompatible context version <v>)`, from `activation.ReadContext`. The profile then comes from `AGENT_ID_ACTIVE_PROFILE`. This is a report of the active context, not a profile selection (profile spec).
- `Provider` is `<provider name> (<host>)`. The brief prints only the host, and the provider name is added because the config can hold several providers (ADR #35).
- `Identity` is the cached bot login. When it is missing, `Identity:      not resolved (run agent-id doctor --profile <name>)`.
- `Installation` and `Credentials` are the provider's `Describe()` lines. Another provider brings its own labels.
- `Token status` is one of:
  - `valid (expires in <N>m)`, where N is rounded down, and `<1m` below one minute;
  - `unavailable (<class>: <hint>)`, where the class is one of `unauthorized`, `forbidden`, `not found`, `unavailable`, `key` or `config`;
  - `not checked (<reason>)`, when the profile cannot be resolved.
- `Activation` is `exec` or `env` (`AGENT_ID_MECHANISM`).

Outside a context the output is exactly one line, `Context:       personal/unmanaged`, with exit 0. `whoami` reads no configuration and makes no network call then.

| Exit | When |
|---|---|
| 0 | outside a context, or inside with `Token status: valid` |
| 1 | inside a context that is damaged or incompatible, whose profile cannot be resolved, or whose token is unavailable |
| 2 | usage error |
| 3 | output withheld by the redaction guard |

JSON (`--json`), one object, schema version 1:

```json
{
  "schema": 1,
  "context": "agent",
  "context_problem": null,
  "profile": "claude",
  "provider": "github",
  "host": "github.com",
  "identity": "claude-dev[bot]",
  "details": [
    { "label": "Installation", "value": "12345678" },
    { "label": "Credentials", "value": "GitHub App installation token" }
  ],
  "token": { "status": "valid", "expires_at": "2026-11-13T10:42:00Z", "class": null },
  "activation": "exec"
}
```

Outside a context: `{"schema":1,"context":"personal/unmanaged"}`, with every other member absent. Members are never removed or retyped within schema 1. A new member is allowed, and consumers must ignore members they do not know.

### `agent-id doctor --profile <name> [--json]`

Text example. Paths under `$HOME` are printed as `~/…`, so that a pasted report does not carry the user's home path:

```text
agent-id doctor — profile claude (github, github.com)

ok    config.load          configuration ~/.config/agent-id/config.json loaded
ok    key.file             private key ~/.config/agent-id/keys/claude.pem: regular file, mode 0600, parses
ok    provider.token       installation token minted (expires in 60m)
ok    provider.identity    claude-dev[bot] matches the cached identity
ok    provider.repo        installation can read cultofthecode/agent-id
warn  git.helper.personal  credential helper "osxkeychain" for https://github.com (~/.gitconfig:12)
      fix: none needed in agent mode: agent-id resets helpers for github.com; remove the line only if you do not use it yourself
FAIL  git.remote.ssh       remote origin resolves to SSH (git@github.com:o/r.git) through url.git@github.com:.insteadOf (~/.gitconfig:20)
      fix: remove that rule, or run the agent in a clone whose remote is HTTPS; agent-id exec refuses to start here
warn  env.credential       GH_TOKEN is set in this shell
      fix: agent-id exec and env remove it from the agent; unset it in your shell startup files if you do not need it

1 failure, 2 warnings, 5 ok
```

JSON: `{"schema":1,"profile":"claude","provider":"github","host":"github.com","results":[{"id":"…","status":"ok|info|warn|fail|skip","title":"…","detail":"…","source":"…","fix":"…"}],"summary":{"ok":5,"info":0,"warn":2,"fail":1,"skip":0}}`. Check ids and statuses are part of the contract. Titles, details and fixes are human text.

| Exit | When |
|---|---|
| 0 | no `fail` |
| 1 | at least one `fail` |
| 2 | usage error (missing `--profile`, unknown flag) |
| 3 | output withheld by the redaction guard |

### Check catalog

The order below is the output order. `fail` means `exec` refuses, or agent mode would fail open or not work. `warn` means a conflict that agent mode neutralizes, or a documented bypass risk. `info` is context. `skip` means a precondition is missing (for example, no Git repository in the working directory).

| Id | Checks | Status | Source of the rule |
|---|---|---|---|
| `config.load` | config file found, permissions, valid (profile spec) | fail on any store error | profile spec |
| `config.profile` | `--profile` resolves; the provider is supported | fail | profile spec |
| `config.unsupported` | profiles of unregistered providers in the file | info, listing names | profile spec, Provider registry |
| `key.file` | the key reference's file exists, is regular, is owned by the user, has mode `0600` or stricter, and parses | fail | brief §10; #16 |
| `provider.token` | a fresh token can be minted | fail, with the class and the provider's hint | brief §7.1 |
| `provider.identity` | the host's bot equals the cached identity; refresh the cache when it is missing or different | ok / warn "refreshed" / fail when it cannot be resolved | profile spec, Identity cache |
| `provider.repo` | the installation can read each repository of a remote on the provider's host | fail per repository; skip outside a repository | brief §7.1 |
| `git.version` | `git --version` ≥ 2.43 | fail below, skip when Git is missing | O4 |
| `git.remote.ssh` | D1: effective fetch and push URL of each remote, including `ssh -G` for an alias | fail (O1: `exec` refuses) | analysis §6 D1; O1 |
| `git.remote.userinfo` | D2: user-info in a remote URL or `pushurl` | fail, URL printed with user-info replaced by `[redacted]` | D2 |
| `git.extraheader` | D3: an `Authorization` header that survives agent-id's reset for a remote URL | fail; only the presence and the source location are printed | D3 |
| `git.parameters` | D4: an inherited `GIT_CONFIG_PARAMETERS`, through `activation.ParseGitConfigParameters` | fail for a forbidden key (O2: `exec` refuses), warn otherwise; keys only | D4; O2 |
| `git.helper.personal` | personal credential helpers that match the provider's host, from every scope | warn (neutralized in agent mode); helper name only (see redaction) | C1–C18 |
| `git.signing` | personal commit or tag signing is configured | info (turned off in agent mode) | S1–S8 |
| `git.ssh` | D5: `GIT_SSH_COMMAND` or `core.sshCommand` while a remote on the host is not HTTPS | info | D5; O3 |
| `git.askpass` | D7: `GIT_ASKPASS`, `SSH_ASKPASS`, `core.askPass` | info | D7 |
| `env.credential` | the provider's `CredentialVariables` present in doctor's own environment | warn outside a context; fail inside one (a startup file re-exported it) | #20; analysis R |
| `env.tokenlike` | any other environment variable whose value matches a registered token prefix, a PEM block or a JWT shape | warn, variable name only | exec spec OQ3 |
| `gh.binary` | `gh` on `PATH`; inside a context, the first `gh` is the agent-id shim (#23) | warn when missing; fail when, inside a context, another `gh` comes first | gh analysis |
| `gh.host` | `GH_HOST` or `GH_REPO` names a host other than the profile's | warn | gh analysis, host rules |
| `context.state` | inside a context: `activation.ReadContext` succeeds, and its profile equals `--profile` | fail when damaged; warn when the profile differs | exec spec |
| `context.git` | D6: inside a context, `GIT_CONFIG_COUNT` equals `AGENT_ID_GIT_CONFIG_END`, and the entries from BASE to END equal the expected set for the profile | fail | D6; exec spec |
| `context.shell` | always | info: shell functions and aliases named `git` or `gh` are invisible to a child process; run `type git gh` in the agent's shell | analysis R9–R10 |

Checks that need a repository run in the working directory. Git runs with doctor's own environment, so that inside a context the effective configuration is the one the agent gets. Every gatherer has a timeout of 10 s. A gatherer that fails turns its checks into `skip`, with the reason, and never into `ok`.

### Redaction

**Layer 1: collection.** The `Snapshot` never holds these values:

| Source | What is collected | What is never collected |
|---|---|---|
| Environment | variable names; for the context variables of #20 and `GH_HOST`/`GH_REPO`, their values; for every other variable, the result of a pattern match | the value of any other variable |
| `git config --list --show-scope --show-origin -z` | key, scope, origin (file and line); the values of `url.*`, `remote.*.url`, `remote.*.pushurl`, `core.sshCommand` and `credential.*.helper` | the values of `http.*.extraHeader`, `credential.*` other than `helper`, `user.signingKey` (presence only) |
| `GIT_CONFIG_PARAMETERS` | keys, through `ParseGitConfigParameters` | values |
| Token | `ExpiresAt` | `Value` |
| Key file | `KeyStatus` | content |
| Credential helper value | the first word, without a leading `!`, cut to 40 characters (for example `osxkeychain`, `gh`, `/usr/local/bin/git-credential-manager`) | the rest of the command line, which may hold inline credentials |

**Layer 2: `Redact`.** It is applied to every string of a `Report` before rendering, in this order:

1. A PEM block, from `-----BEGIN` to the matching `-----END …-----` or to the end of the string, becomes `[redacted]`.
2. A JWT shape (`eyJ…` with three base64url segments) becomes `[redacted]`.
3. Each registered token prefix followed by `[A-Za-z0-9_]*` becomes `[redacted]`. The prefixes are those of every registered provider (`TokenPrefixes`).
4. The user-info of any `scheme://user[:password]@host` becomes `scheme://[redacted]@host`. SSH `user@host:` forms keep `git@`, because that user is not a secret.
5. `Authorization: …`, `password=…`, and `oauth_token: …` up to the end of the line keep the label and get the value `[redacted]`.
6. Control characters become `?`. Every echoed value is cut to 200 characters.

**Layer 3: `Guard`.** It scans the final rendered bytes, text or JSON, with patterns 1–3 and the user-info pattern. On a match, the command writes nothing to stdout, writes `agent-id: output withheld: it matched a secret pattern (this is a bug; please report it without the output)` to stderr, and exits 3.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| `whoami` in a personal terminal with `GH_TOKEN` set | `personal/unmanaged` only. `doctor` reports the variable. |
| `whoami` where the context names a profile that was deleted | `Profile: claude (not found in the configuration)`, `Token status: not checked (profile not found)`, exit 1. |
| `whoami` with no network | `Token status: unavailable (unavailable: cannot reach the host)` unless the cache still holds a valid token, exit 1 in that case. |
| `doctor` outside any Git repository | Repository checks are `skip` ("not in a Git repository"). |
| `doctor` in a repository with remotes on other hosts | Only remotes that `ClassifyRemote` assigns to the provider's host are checked for access and SSH. Others are ignored. |
| `doctor --profile reviewer` inside a `claude` context | `context.state` is `warn` ("this shell is in the context of claude"). The other checks run for `reviewer`. |
| A helper value `!f() { echo password=abc; }; f` | Shown as `f()`, the first word without `!`, and source `~/.gitconfig:7`. The rest is never collected. |
| A remote URL `https://user:ghp_x@github.com/o/r` | `git.remote.userinfo` fail with `https://[redacted]@github.com/o/r`. |
| `doctor` while another `setup` writes the config | The identity refresh goes through the profile store's locked write. A concurrent change is preserved. |
| The identity refresh fails to write (read-only file system) | `provider.identity` is `warn` with the write error. Other checks continue. |
| The fake GitHub answers 403 on the repository | `provider.repo` fail: "the installation cannot read o/r; add the repository to the App installation". |
| `--json` with a check that panics | Checks are pure and table-tested. A recovered panic becomes a `fail` result with id `internal`, and the output is still guarded. |
| Output piped to a file or CI log | Same bytes. No color codes are used in v1. |

## 📝 Risks & Impact Review

- **Contract.** The `whoami` text labels and order, both JSON schemas (version 1), the `doctor` check ids and statuses, and the exit codes 0–3 are consumed by scripts and by #27's end-to-end checks. A change to an existing member needs `schema: 2`.
- **Network from diagnostics.** `doctor` mints a token and calls the API on every run, and `whoami` may mint one. Both stay inside the App's own permissions (brief §10: the App's permissions are the authoritative control). In tests they reach only the fake GitHub.
- **A write from `doctor`.** Limited to `identity`, through the profile store's atomic, locked write, and reported as `warn` "refreshed". It is never done for an unsupported profile.
- **Redaction is best effort beyond known patterns.** A secret with no known shape in a value that layer 1 collects (for example, a custom helper name that is itself a token) can only be caught if it matches a pattern. Layer 1 keeps such values to a minimum. The spec does not claim more.
- **Not a security boundary.** `doctor` can only see what a child process can see: shell functions, aliases and a later `.zshrc` are invisible (R9–R10), which `context.shell` states.
- **Product direction vs. rule.** The check catalog follows the decisions of 13.11 (O1–O4) and the analyses. OQ1 and OQ2 are product choices recorded with a recommendation.

## 📝 Decisions in play

- Brief §5, §7.1, §7.4, §8 *Diagnostics*, §10.
- `ARCHITECTURE.md`: Diagnostics row, dependency rule 5 (conflict checks and redaction are pure functions), Data (identity cache "refreshed by `setup` and `doctor`"), Security model ("`doctor` and `whoami` print the token status and expiry, never the token"), Key decisions row "Supported Git paths and documented bypasses" (O1–O4).
- Exec spec: context variables, `ReadContext`, BASE/END markers, OQ3 (inherited token-shaped values reported by `doctor`).
- Profile spec: explicit `--profile`, unsupported profiles listed by `doctor`, identity cache.
- Triage of #24: `risk-high`.

## Open Questions (owner) — recommendations applied

| # | Question | Recommendation (applied) | Why | Confirm? |
|---|---|---|---|---|
| OQ1 | Should `whoami` obtain a token (cache or mint, over the network on a miss), or only read the token cache? | **Obtain it through the token manager**, the same call the credential helper makes. | The brief's example shows a valid token. A cache-only answer is empty in a fresh session and hides broken credentials. | owner, non-blocking |
| OQ2 | Should `doctor` fail (exit 1) on conflicts that agent mode neutralizes (a personal helper, an inherited `GH_TOKEN` outside a context), or only warn? | **Warn.** Fail only where `exec` refuses or agent mode would fail open or not work. | A developer's normal setup (osxkeychain, `gh auth setup-git`) would otherwise make `doctor` always fail, and scripts could no longer use its exit status. | owner, non-blocking |

## Resolved assumptions (autonomous defaults)

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | Output formats | Text by default, `--json` with `schema: 1`. Stable: the `whoami` labels and order, the JSON members, the check ids, the exit codes. | Scripts parse this output (#24 triage). | reversible before the first release |
| A2 | Exit codes | `whoami`: 0/1/2/3. `doctor`: 0/1/2/3, with 3 = output withheld. | Scripts can tell "problem found" from "command misused" and from "redaction bug". | reversible before the first release |
| A3 | Redaction design | Three layers: collection, `Redact`, `Guard`. | One missed pattern does not leak. | reversible |
| A4 | Home path in output | Abbreviated to `~`. | A pasted report does not carry the user's account name into an issue of a public repository. | reversible |
| A5 | Identity refresh | Only `identity`, through the store's locked write, reported as `warn`. | `ARCHITECTURE.md` and the profile spec assign it to `doctor`. | reversible |
| A6 | New provider answers | `TokenPrefixes`, `Describe`, `ResolveIdentity`, `RepositoryAccess` on the provisional interface. | Keeps every GitHub term in the adapter (ADR #35). | settled with #15 |

## 📋 Phasing

- **Phase 1 — `internal/diagnose` core.** The report model, `Redact`, `Guard`, the renderers, and the pure checks over a hand-built `Snapshot`. Needs only #20 (merged spec; `internal/activation` code for the reused functions).
- **Phase 2 — `whoami`.** Needs #17 and #18 and the provider additions (#15).
- **Phase 3 — `doctor`.** Its gatherers need #16 (key inspection), #22 (`ClassifyRemote`, the expected entries) and #23 (the shim location).
- **Phase 4 — record.** `ARCHITECTURE.md` and the capability archive.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first. Packages that run Git or `gh` use `testenv.Main`. `risk-high` evidence per `SDLC.md`:
- hostile-input tables for every check and for `Redact`;
- fake-GitHub tests for the denied paths (401, 403, 404, unavailable);
- the decoy scan of the Delta on every output mode.

### Phase 1 — `internal/diagnose`

1. **Contract.** Package, `AGENTS.md` (the invariants above), report types, statuses, a row in the root `AGENTS.md` task routing. Test: the package builds, and no exported report type has a field of type `provider.Token` or `[]byte`. A reflection test walks the types.
2. **`Redact` and `Guard`.** Table tests for every rule:
   - a PEM block in the middle of text, and one without an end marker;
   - a JWT inside a URL;
   - each prefix of the fake provider and of `githubtest` at the start, in the middle, and next to punctuation;
   - user-info with and without a password;
   - `ssh://git@` (kept) and `git@github.com:` (kept);
   - `Authorization: Bearer x`, `password=x`, `oauth_token: x`;
   - control characters, and a 10 KiB value.

   `Guard` is tested on text and on JSON, where escaped forms such as `-` in `-----BEGIN` are decoded before scanning.
3. **Renderers.** Golden files for the text and JSON examples of this spec, for `personal/unmanaged`, and for every `Context` variant. `AbbreviateHome` covers a path inside `$HOME`, `$HOME` itself, a prefix collision (`/home/devx` vs `/home/dev`), and a relative path.
4. **Checks.** One table per catalog id over a constructed `Snapshot`, with the matrix rows as cases. D1 uses U2–U8 and U12. D2 uses C21 and U11. D3 uses H7. D4 uses C19b, O2 keys, and `include.path`. D6 uses R8, an appended foreign entry, and a modified agent entry. `env.tokenlike` covers each pattern, and the value never appears in the result. Every check gets a `skip` row for a missing input.

### Phase 2 — `whoami`

5. **Slice.** Ports for the profile resolver, the provider source and the token manager. Handler tests:
   - outside a context, with zero calls to the ports;
   - an exec context;
   - an env context;
   - damaged and incompatible contexts;
   - profile not found;
   - each provider failure class from `providertest.Fake`;
   - `--json` for each case;
   - exit codes.
6. **Process test.** `testenv.BuildBinary`. `agent-id exec --profile claude -- agent-id whoami` against `githubtest` gives the brief's fields and exit 0. Plain `agent-id whoami` with fake `git` and `gh` that record calls gives `personal/unmanaged` and zero calls. The decoy scan runs on both.

### Phase 3 — `doctor`

7. **Gatherers.** Git (`config --list --show-scope --show-origin -z`, `ls-remote --get-url`, `remote get-url --push`, `config --get-urlmatch`), `ssh -G` for aliases, `gh` lookup on `PATH`, the environment, `KeyStatus`, and the provider. Each has a timeout, and a failure leads to `skip`. Tests with real `git` in `testenv.Isolate`, with the matrix's hostile global configuration and a local bare repository from `testenv.NewBareRepository`.
8. **Slice.** `--profile`, `--json`, the exit codes, and the identity refresh through the store (a test proves only `identity` changed, byte for byte elsewhere).
9. **Process tests.** The Delta scenarios end to end:
   - a personal helper plus an inherited `GH_TOKEN` gives two `warn` lines with fixes and exit 0;
   - an SSH remote gives `fail` and exit 1;
   - a deleted installation on the fake GitHub gives `provider.token` fail;
   - 403 on the repository gives `provider.repo` fail;
   - inside a context with `GIT_CONFIG_COUNT` unset by a fake startup step gives `context.git` fail;
   - the decoy scan runs on every output.

### Phase 4 — record

10. **Architecture and archive.** `ARCHITECTURE.md`:
    - in Cross-cutting concerns, the exit codes of `whoami` and `doctor` and the JSON schema rule;
    - the Diagnostics row's dependencies (`activation`, `credential`) in the component table;
    - a Key decisions row for the output contract.

    Archive the Delta into `.ai/specs/capabilities/whoami.md`, `doctor.md`, and `diagnostics-redaction.md`.
