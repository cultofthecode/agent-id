# Architecture — agent-id (aidentity)

The code so far is the composition root in `cmd/aidentity` (root command and `--version`) and the test isolation helpers in `internal/testenv`. Every other module below is **planned**. This file records the target structure so that new code follows it. Rows marked **OPEN** are undecided: they mirror `PRODUCT_BRIEF.md` §14 and the design questions of §9.4, and code must not assume an answer. Keep this file current as the system changes; agents read it (through `AGENTS.md`) before structural changes.

## Purpose and scope

aidentity binds an agent profile to Git identity, Git transport credentials, and GitHub API credentials for one process and its descendants (brief §6). The developer's terminal keeps its personal Git and `gh` configuration; a process started through `aidentity exec` (or a shell that loaded `aidentity env`) uses a GitHub App bot identity with installation tokens that are minted, cached, and refreshed automatically.

In scope for the MVP (brief §8, Phase 1): one GitHub App profile, process-scoped activation, Git author and committer, Git transport credentials, `gh` token injection, token cache and refresh, fail-closed behavior, `whoami` and `doctor`, and a Claude Code `SessionStart` integration. GitHub.com, macOS and Linux, Bash and Zsh. Deferred (brief §8, MVP boundaries): Windows, GitHub Enterprise Server, custom Git hosts, managed team deployment, other providers. The code still reaches GitHub only through a provider interface, so that GitLab and Bitbucket can be added later as adapters (see Provider abstraction). Not a goal (brief §6): an AI agent or orchestrator, a replacement for Git or `gh`, a password manager or IAM platform, a security sandbox in local mode, a continuation of GHAPP.

## System context

```mermaid
flowchart TD
    Human[Developer terminal] --> Personal[Personal Git and gh configuration, unchanged]
    Human --> Runtime[aidentity exec / aidentity env]
    Runtime --> Agent[Claude Code / Codex / any process]
    Runtime --> Profile[Profile store]
    Profile --> Provider[Provider interface: GitHub adapter in the MVP]
    Provider --> Tokens[Token manager: cache and refresh]
    Agent --> Git[git]
    Agent --> GH[gh]
    Git --> Identity[Process-scoped author and committer]
    Git --> Helper[aidentity credential helper]
    GH --> Wrapper[aidentity gh injection]
    Helper --> Tokens
    Wrapper --> Tokens
    Tokens --> GitHub[api.github.com]
```

The child environment is the only activation mechanism: aidentity never writes to `~/.gitconfig`, the system Git configuration, `~/.config/gh`, the ghapp configuration, shell startup files, or the repository's `.git/config`.

## Components

The layout follows orbit: a vertical slice architecture adjusted to Go. A **module** is a bounded context under `internal/`. A **slice** is one user-visible operation: one Go package that holds its contract, handler, CLI command, and tests.

```text
cmd/aidentity/             composition root: build adapters, wire slices, run            (exists)
internal/
  testenv/                 test isolation: temporary HOME/XDG/Git/gh config, fake GitHub (exists)
  profile/                 module: profile config (non-secret metadata, key references) (planned)
  credential/              module: private key sources: protected file; keyring OPEN     (planned)
  provider/                port: the forge provider interface and its contract tests      (planned)
    providertest/          fake provider and the contract suite every adapter must pass
    github/                adapter: GitHub App JWT, installation tokens, bot identity, gh (planned)
      githubtest/          deterministic fake responses built on testenv.FakeGitHub
  token/                   module: token cache per host/app/installation/scope, refresh  (planned)
  activation/              module: child environment of exec and env (pure), activation  (planned)
    exec/  env/            slices: aidentity exec, aidentity env
  gitint/                  module: Git identity, process-scoped Git config, credential helper (planned)
    credentialhelper/      slice: aidentity git-credential (the helper protocol)
  cliint/                  module: forge CLI token injection (gh in the MVP), recursion guard (planned)
  diagnose/                module: whoami, doctor (no secrets in output)                 (planned)
  setup/                   slice module: aidentity setup <provider> (github in the MVP)   (planned)
  integration/claude/      Claude Code SessionStart integration                          (planned)
  kernel/                  shared kernel: only what three or more modules need
```

| Component | Location (planned) | Responsibility (brief §9.2) | Depends on |
|---|---|---|---|
| Composition root | `cmd/aidentity` | Builds the adapters, adds the cobra command of every slice, prints errors, sets the exit code. No behavior. | every module |
| Test isolation | `internal/testenv` | Isolated environment, fake GitHub, port allocation in 18000–18999. Test files only. | — |
| Profile store | `internal/profile` | Persist non-secret metadata and references to securely stored credentials; resolve the active profile. | `kernel` |
| Credential source | `internal/credential` | Load the App private key from its reference; check file permissions; keyring backend behind an interface. | `kernel` |
| Provider interface | `internal/provider` | The port through which the core reaches a Git forge: authentication and tokens, bot identity, per-host credential helper data, CLI integration, host and URL mapping (see Provider abstraction). Defines no forge-specific type. | `kernel` |
| GitHub adapter | `internal/provider/github` | Implements the provider interface for GitHub.com: sign the App JWT, mint installation tokens, resolve the bot login, numeric id and noreply email, validate repository access, describe `gh`. The only package with GitHub-specific code. | `provider`, `kernel` |
| Token manager | `internal/token` | Cache tokens with restrictive permissions, atomic writes and a cross-process lock; refresh ahead of expiry; never expose a token. | `provider`, `kernel` |
| Context runtime | `internal/activation` | Build the child environment as a pure function, start the child, preserve exit status and signals, detect nesting. | `profile`, `gitint`, `cliint`, `kernel` |
| Git integration | `internal/gitint` | Author and committer variables, `GIT_CONFIG_COUNT` entries (credential helper ordering, `useHttpPath`, SSH→HTTPS rewrites, signing off), and the credential helper protocol with host, protocol and path validation. Hosts, URL rewrites and identity values come from the provider. | `provider`, `token`, `kernel` |
| Forge CLI integration | `internal/cliint` | Run the provider's known CLI (`gh` in the MVP) with a fresh token per invocation; drop conflicting inherited variables; pin the host; prevent recursion. The binary, variable names and host pinning come from the provider. | `provider`, `token`, `kernel` |
| Diagnostics | `internal/diagnose` | `whoami` and `doctor`: active context, profile, identity, credential source, token status, configuration conflicts. Redacts every secret. | `profile`, `provider`, `token`, `gitint`, `cliint`, `kernel` |

### Slice anatomy

- `contract.go` — the `Input` and `Output` structs and the sentinel errors of the operation. Write it first.
- `handler.go` — `Handler` with its dependencies as fields, and `Handle(ctx, Input) (Output, error)`. A handler declares the narrow interface it needs from the provider, the token cache, or the filesystem; the adapter satisfies it.
- `command.go` — a constructor that takes the handler and returns a `*cobra.Command`. The command binds flags to `Input`, calls the handler, and renders `Output`. No decisions, no package-level state, no `init()`.
- `handler_test.go` — table tests for the happy path, the invalid input, and the provider failure, in an isolated environment.

### Dependency rules

1. Nothing imports `cmd/aidentity`. Only `cmd/aidentity` imports an adapter (`internal/provider/github`); it constructs the adapter and injects it as a `provider` interface value.
2. A slice imports the root package of its own module, the `internal/` of its own module, port packages, and `kernel`. A slice never imports another slice.
3. A module uses another module only through that module's root package.
4. `kernel` imports nothing from this repository.
5. A security decision is a pure function in a module, not logic in a handler or a command: the child environment, the credential helper's answer for a request, the token cache key, the conflict checks of `doctor`, redaction. Handlers load, call the function, and act on the result.
6. Only test files import `internal/testenv`.
7. No GitHub-specific import outside `internal/provider/github`: GitHub types, API clients and libraries, hostnames such as `github.com` and `api.github.com`, App JWT and installation-token details, and `gh` wiring live in the adapter. Every other package uses the `internal/provider` interface. Exceptions: `cmd/aidentity` constructs the adapter (rule 1), and test files may use `internal/testenv`'s fake GitHub.

depguard (`.golangci.yml`) enforces rule 6 today. Rules 1, 2, 4, and 7 get depguard rules when the packages they name exist; the compiler enforces rule 3. Until then, review enforces rule 7 (`CODE_REVIEW.md`).

## Provider abstraction (ADR, 2026-11-12)

- **Status:** accepted. **Owner:** Jacob. **Date:** 12 November 2026, 22:41 Europe/Warsaw. **Ticket:** [#35](https://github.com/cultofthecode/agent-id/issues/35).
- **Context:** brief §3 principle 6 and §13 Phase 4 defer a cross-provider abstraction until demand is proven. If Phase 1 hard-wires GitHub into the core, a later GitLab or Bitbucket adapter means a rewrite of the token manager, the Git integration, the CLI integration, and the profile store.
- **Decision:** the core reaches a Git forge only through a provider interface in `internal/provider`. GitHub is the first and, in the MVP, the only adapter, in `internal/provider/github`; it replaces the planned `internal/github`. GitLab and Bitbucket come later as further adapters of the same interface (`internal/provider/gitlab`, `internal/provider/bitbucket`), without changes to the core. This changes package boundaries, not the MVP feature set: the MVP still speaks only to GitHub.com and still uses `gh`.

The interface covers five responsibilities. The ADR fixes the responsibilities and the boundary; the method signatures are shaped by the first adapter (#15) and recorded in `internal/provider`.

| Responsibility | What the provider supplies | GitHub adapter (MVP) | Later adapters |
|---|---|---|---|
| Authentication and tokens | Mint a short-lived credential for the profile, with its expiry, from the profile's credential reference. The token manager caches and refreshes it; the core never parses it. | App JWT signed with the standard library, installation access token | GitLab: project, group, or service-account token; Bitbucket: workspace or repository access token |
| Commit author and bot identity | Resolve the bot's display name, login, numeric id, and commit email for the author and committer variables. | `<slug>[bot]`, `<id>+<slug>[bot]@users.noreply.github.com` | the forge's bot or service-account identity |
| Per-host credential helper | The hosts and protocol the credential helper answers for, the username to return, and the repository-path scope. The helper protocol and its fail-closed validation stay in `internal/gitint`. | `https` on `github.com`, `x-access-token` | the forge's host and token username |
| CLI integration | The known CLI binary, the variable that carries the token, the variables to drop, and the host pinning. Injection and the recursion guard stay in `internal/cliint`. | `gh`, `GH_TOKEN`; drop `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` | `glab` for GitLab; Bitbucket: no official CLI, so none |
| Host and URL mapping | The canonical host, the API base URL, and the SSH→HTTPS rewrites; recognize which remotes belong to the provider. | `github.com`, `api.github.com`, `git@github.com:` and `ssh://git@github.com/` | the forge's hosts and remote formats |

Consequences:

- **Profile config v1** has a required `provider` field. The MVP accepts `github` and refuses any other value (fail closed). The profile-config spec (#9) carries the field; its format stays the OPEN row below.
- **Token cache key** includes the provider next to host, App or account, installation, and scope.
- **Contract tests.** `internal/provider` owns a contract suite that every adapter must pass. It runs against a fake provider in `internal/provider/providertest`, with no network and no real credentials. The fake GitHub of `internal/testenv` serves the GitHub adapter's tests behind that interface; core packages never import GitHub-only test types.
- **Import rule.** Dependency rule 7: no GitHub-specific import outside `internal/provider/github`.
- **Out of scope** for this decision: implementing GitLab or Bitbucket adapters (follow-ups under #5), Azure DevOps (still an optional Phase 4 evaluation), replacing `gh` in the MVP, and any second runtime provider in Phase 1.
- **Work it constrains:** #9 (`provider` field), #10 (fake GitHub behind the interface), #15 (GitHub adapter), #17 (profile store), #19 (setup for the GitHub adapter), #22 (per-host credential helper), #23 (CLI integration). #15 and #19 build on the interface recorded here.

## Git integration (brief §9.3)

The GitHub values below come from the GitHub adapter's identity, credential helper, and host mapping data; `internal/gitint` itself names no forge.

- Identity: `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` from the resolved bot (`<slug>[bot]`, `<id>+<slug>[bot]@users.noreply.github.com`).
- Configuration: only `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n`, appended after any inherited entries so that inherited process-scoped configuration is preserved or handled explicitly, never replaced blindly.
- Credentials: an empty `credential.helper` entry resets inherited helpers for `https://github.com`, then the aidentity helper; `credential.useHttpPath` where repository scope matters. The helper answers only for `protocol=https` and the profile's host, and refuses any other host, look-alike host, or protocol (fail closed).
- Transport: GitHub SSH remotes (`git@github.com:`, `ssh://git@github.com/`) are rewritten to HTTPS inside the context; push URLs and `http.extraHeader` are checked by `doctor`.
- Signing: inherited personal commit and tag signing is turned off in agent mode unless an explicit agent signing configuration exists. Commit metadata is attribution, not proof of origin.

## GitHub CLI integration (brief §9.3)

The GitHub adapter describes `gh`; `internal/cliint` runs it. A known `gh` executable runs with a fresh installation token in `GH_TOKEN` on each invocation; `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, and conflicting host variables are removed; the host is pinned to the profile's host. The injection never writes `hosts.yml`. A recursion guard stops the wrapper from calling itself. Commands that need a user token (for example `gh auth`, user-scoped endpoints) fail with a clear error.

## Data

aidentity has no database.

- **Profile config:** `$XDG_CONFIG_HOME/aidentity/` (default `~/.config/aidentity/`). Non-secret metadata and key references only. v1 has a required `provider` field; the MVP accepts `github` (Provider abstraction). Format and the rest of the schema: **OPEN** (brief §9.4 is illustrative YAML).
- **Token cache:** `$XDG_CACHE_HOME/aidentity/` (or the state directory), mode `0600` files in a `0700` directory, keyed by provider, host, App, installation, and scope; atomic writes and a lock. Installation tokens are bearer credentials, valid about one hour.
- **Private key:** a protected file (`0600`, refused otherwise) or an OS keyring entry (**OPEN**). The key can mint tokens for every installation of the App: its blast radius is larger than a token's; rotation and revocation are documented.
- **Identity cache:** resolved bot login, numeric id, and email, cached with the profile, refreshed by `setup` and `doctor`.

## Security model (brief §10)

- Guarantees of local mode: identity and credentials are controlled on the supported execution paths; personal Git and `gh` configuration is never modified; agent-mode authentication fails closed; GitHub App permissions and installation repository access are the authoritative controls.
- Limitation, stated everywhere it matters: a local agent running as the developer's OS user is not isolated from the developer's files, SSH agent, tokens, or executables, and can bypass wrappers or unset variables. Local mode is attribution and convenience, not containment.
- Secrets never appear in output, errors, logs, shell tracing (`set -x` of generated scripts), configuration dumps, or fixtures. `doctor` and `whoami` print the token status and expiry, never the token.
- Hardened mode (separate broker, separate OS identity or sandbox, policy, audit) is Phase 3 and a separate milestone.

## Cross-cutting concerns

- **Errors:** wrapped with `%w`, matched with `errors.Is`. Only `cmd/aidentity` prints errors and sets exit codes; `exec` returns the child's exit status unchanged. No `panic` for expected failures. Output goes to stdout, diagnostics to stderr.
- **Testing strategy:**
  - Unit tests for every pure security function, with hostile input: look-alike hosts, other protocols, unrelated repository paths, inherited personal tokens and helpers, `GIT_CONFIG_COUNT` already set, expired tokens, unreadable or world-readable keys.
  - Handler tests against `testenv.NewFakeGitHub` in an isolated environment (`testenv.Isolate`). `go test ./...` never reaches the network and never needs a GitHub account.
  - Process tests that run real `git` and `gh` binaries do so only in an isolated environment with a fake GitHub and a local bare repository.
  - End-to-end tests against a disposable GitHub repository and a dedicated test App carry the `e2e` build tag and run by hand (`SDLC.md`, Validation gate).
  - CI runs the gate on `ubuntu-24.04` and the darwin lint, vet, and build; `darwin.yml` runs the unit tests on `macos-26`.

## Key decisions

Derived from `PRODUCT_BRIEF.md`; the brief is the record, except where a row cites a later owner decision. Rows with status **OPEN** wait for the owner.

| Date | Decision | Source | Status |
|---|---|---|---|
| 2026-11-12 | Go CLI, module `github.com/cultofthecode/agent-id`, cobra for the command tree (as in orbit) | owner's starter task; orbit D02/D23 | active |
| 2026-11-12 | GitHub first; MVP targets GitHub.com, macOS/Linux, Bash/Zsh | brief §3 principle 6, §8 | active |
| 2026-11-12 | Explicit `aidentity exec` is the canonical activation; `env` + Claude Code `SessionStart` is a convenience layer | brief §7.2–7.3, §14 profile selection | active |
| 2026-11-12 | Process-scoped Git configuration through `GIT_CONFIG_COUNT` and author/committer variables; no global changes | brief §9.3, §10 | active |
| 2026-11-12 | Fail closed in agent mode; no silent fallback to personal credentials | brief §5, §8 | active |
| 2026-11-12 | Local mode is attribution, not containment; hardened isolation is Phase 3 | brief §10, §14 credential leakage | active |
| 2026-11-12 | Turn off inherited personal signing in agent mode; never claim verified commits | brief §9.3 | active |
| 2026-11-12 | Start with existing GitHub Apps; guided or manifest registration is evaluated later | brief §14 onboarding | active |
| 2026-11-12 | No runtime dependency on GHAPP and no requirement to install it | brief §1, §13 Phase 0 | active |
| 2026-11-12 | Git forges behind a provider interface (`internal/provider`); GitHub is the first adapter (`internal/provider/github`, replacing `internal/github`); GitLab and Bitbucket later as adapters of the same interface; no GitHub-specific import outside the adapter; profile config v1 has a required `provider` field | owner decision (Jacob, 12 November 2026, 22:41 Europe/Warsaw), #35; supersedes the deferral in brief §3 principle 6 and §13 Phase 4 for package boundaries only | active |
| — | Product and binary name: `aidentity` (working) vs `agent-id`; config directory and env prefix follow it | brief §14 | **OPEN** |
| — | Code provenance and license of agent-id; which GHAPP code is reused and with which notices | brief §14 | **OPEN** |
| — | Distribution and commercial model (license, hosting, release channel) | brief §14 | **OPEN** |
| — | Agent-specific commit signing as a later feature | brief §14 | **OPEN** |
| — | Profile auto-detection beyond explicit `exec`, and its precedence | brief §14 | **OPEN** (MVP: explicit only) |
| — | Profile config format and schema; keyring support | brief §9.4 | **OPEN** |
| — | Supported Git paths and documented bypasses; installation-token-compatible `gh` commands | brief §14 | **OPEN** (researched in Phase 1 issues) |
