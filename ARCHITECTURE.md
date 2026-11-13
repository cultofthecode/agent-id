# Architecture — agent-id

The code so far is the composition root in `cmd/agent-id` (root command and `--version`) and the test isolation helpers in `internal/testenv`. Every other module below is **planned**. This file records the target structure so that new code follows it. Rows marked **OPEN** are undecided: they mirror `PRODUCT_BRIEF.md` §14 and the design questions of §9.4, and code must not assume an answer. Keep this file current as the system changes; agents read it (through `AGENTS.md`) before structural changes.

## Purpose and scope

agent-id binds an agent profile to Git identity, Git transport credentials, and GitHub API credentials for one process and its descendants (brief §6). The developer's terminal keeps its personal Git and `gh` configuration; a process started through `agent-id exec` (or a shell that loaded `agent-id env`) uses a GitHub App bot identity with installation tokens that are minted, cached, and refreshed automatically.

In scope for the MVP (brief §8, Phase 1): one GitHub App profile, process-scoped activation, Git author and committer, Git transport credentials, `gh` token injection, token cache and refresh, fail-closed behavior, `whoami` and `doctor`, and a Claude Code `SessionStart` integration. GitHub.com, macOS and Linux, Bash and Zsh. Deferred (brief §8, MVP boundaries): Windows, GitHub Enterprise Server, custom Git hosts, managed team deployment, other providers. Not a goal (brief §6): an AI agent or orchestrator, a replacement for Git or `gh`, a password manager or IAM platform, a security sandbox in local mode, a continuation of GHAPP.

## System context

```mermaid
flowchart TD
    Human[Developer terminal] --> Personal[Personal Git and gh configuration, unchanged]
    Human --> Runtime[agent-id exec / agent-id env]
    Runtime --> Agent[Claude Code / Codex / any process]
    Runtime --> Profile[Profile store]
    Profile --> Provider[GitHub App provider]
    Provider --> Tokens[Token manager: cache and refresh]
    Agent --> Git[git]
    Agent --> GH[gh]
    Git --> Identity[Process-scoped author and committer]
    Git --> Helper[agent-id credential helper]
    GH --> Wrapper[agent-id gh injection]
    Helper --> Tokens
    Wrapper --> Tokens
    Tokens --> GitHub[api.github.com]
```

The child environment is the only activation mechanism: agent-id never writes to `~/.gitconfig`, the system Git configuration, `~/.config/gh`, the ghapp configuration, shell startup files, or the repository's `.git/config`.

## Components

The layout follows orbit: a vertical slice architecture adjusted to Go. A **module** is a bounded context under `internal/`. A **slice** is one user-visible operation: one Go package that holds its contract, handler, CLI command, and tests.

```text
cmd/agent-id/              composition root: build adapters, wire slices, run            (exists)
internal/
  testenv/                 test isolation: temporary HOME/XDG/Git/gh config, fake GitHub (exists)
  profile/                 module: profile config (non-secret metadata, key references) (planned)
  credential/              module: private key sources: protected file; keyring OPEN     (planned)
  github/                  adapter: GitHub App JWT, installation tokens, bot identity     (planned)
    githubtest/            deterministic fake responses built on testenv.FakeGitHub
  token/                   module: token cache per host/app/installation/scope, refresh  (planned)
  activation/              module: child environment of exec and env (pure), activation  (planned)
    exec/  env/            slices: agent-id exec, agent-id env
  gitint/                  module: Git identity, process-scoped Git config, credential helper (planned)
    credentialhelper/      slice: agent-id git-credential (the helper protocol)
  ghint/                   module: gh token injection, recursion guard, host selection   (planned)
  diagnose/                module: whoami, doctor (no secrets in output)                 (planned)
  setup/                   slice module: agent-id setup github                           (planned)
  integration/claude/      Claude Code SessionStart integration                          (planned)
  kernel/                  shared kernel: only what three or more modules need
```

| Component | Location (planned) | Responsibility (brief §9.2) | Depends on |
|---|---|---|---|
| Composition root | `cmd/agent-id` | Builds the adapters, adds the cobra command of every slice, prints errors, sets the exit code. No behavior. | every module |
| Test isolation | `internal/testenv` | Isolated environment, fake GitHub, port allocation in 18000–18999. Test files only. | — |
| Profile store | `internal/profile` | Persist non-secret metadata and references to securely stored credentials; resolve the active profile. | `kernel` |
| Credential source | `internal/credential` | Load the App private key from its reference; check file permissions; keyring backend behind an interface. | `kernel` |
| GitHub provider | `internal/github` | Sign the App JWT, mint installation tokens, resolve the bot login, numeric id and noreply email, validate repository access. | `kernel` |
| Token manager | `internal/token` | Cache tokens with restrictive permissions, atomic writes and a cross-process lock; refresh ahead of expiry; never expose a token. | `github` (through a port), `kernel` |
| Context runtime | `internal/activation` | Build the child environment as a pure function, start the child, preserve exit status and signals, detect nesting. | `profile`, `gitint`, `ghint`, `kernel` |
| Git integration | `internal/gitint` | Author and committer variables, `GIT_CONFIG_COUNT` entries (credential helper ordering, `useHttpPath`, SSH→HTTPS rewrites, signing off), and the credential helper protocol with host, protocol and path validation. | `token`, `kernel` |
| GitHub CLI integration | `internal/ghint` | Run a known `gh` with a fresh `GH_TOKEN` per invocation; drop conflicting inherited variables; pin the host; prevent recursion. | `token`, `kernel` |
| Diagnostics | `internal/diagnose` | `whoami` and `doctor`: active context, profile, identity, credential source, token status, configuration conflicts. Redacts every secret. | `profile`, `token`, `gitint`, `ghint`, `kernel` |

### Slice anatomy

- `contract.go` — the `Input` and `Output` structs and the sentinel errors of the operation. Write it first.
- `handler.go` — `Handler` with its dependencies as fields, and `Handle(ctx, Input) (Output, error)`. A handler declares the narrow interface it needs from GitHub, the token cache, or the filesystem; the adapter satisfies it.
- `command.go` — a constructor that takes the handler and returns a `*cobra.Command`. The command binds flags to `Input`, calls the handler, and renders `Output`. No decisions, no package-level state, no `init()`.
- `handler_test.go` — table tests for the happy path, the invalid input, and the GitHub failure, in an isolated environment.

### Dependency rules

1. Nothing imports `cmd/agent-id`. Only `cmd/agent-id` imports an adapter (`internal/github`).
2. A slice imports the root package of its own module, the `internal/` of its own module, port packages, and `kernel`. A slice never imports another slice.
3. A module uses another module only through that module's root package.
4. `kernel` imports nothing from this repository.
5. A security decision is a pure function in a module, not logic in a handler or a command: the child environment, the credential helper's answer for a request, the token cache key, the conflict checks of `doctor`, redaction. Handlers load, call the function, and act on the result.
6. Only test files import `internal/testenv`.

depguard (`.golangci.yml`) enforces rule 6 today. Rules 1, 2, and 4 get depguard rules when the packages they name exist; the compiler enforces rule 3.

## Git integration (brief §9.3)

- Identity: `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` from the resolved bot (`<slug>[bot]`, `<id>+<slug>[bot]@users.noreply.github.com`).
- Configuration: only `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n`, appended after any inherited entries so that inherited process-scoped configuration is preserved or handled explicitly, never replaced blindly.
- Credentials: an empty `credential.helper` entry resets inherited helpers for `https://github.com`, then the agent-id helper; `credential.useHttpPath` where repository scope matters. The helper answers only for `protocol=https` and the profile's host, and refuses any other host, look-alike host, or protocol (fail closed).
- Transport: GitHub SSH remotes (`git@github.com:`, `ssh://git@github.com/`) are rewritten to HTTPS inside the context; push URLs and `http.extraHeader` are checked by `doctor`.
- Signing: inherited personal commit and tag signing is turned off in agent mode unless an explicit agent signing configuration exists. Commit metadata is attribution, not proof of origin.

## GitHub CLI integration (brief §9.3)

A known `gh` executable runs with a fresh installation token in `GH_TOKEN` on each invocation; `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, and conflicting host variables are removed; the host is pinned to the profile's host. The injection never writes `hosts.yml`. A recursion guard stops the wrapper from calling itself. Commands that need a user token (for example `gh auth`, user-scoped endpoints) fail with a clear error.

## Data

agent-id has no database.

- **Profile config:** `$XDG_CONFIG_HOME/agent-id/` (default `~/.config/agent-id/`). Non-secret metadata and key references only. Format and schema: **OPEN** (brief §9.4 is illustrative YAML).
- **Token cache:** `$XDG_CACHE_HOME/agent-id/` (or the state directory), mode `0600` files in a `0700` directory, keyed by host, App, installation, and scope; atomic writes and a lock. Installation tokens are bearer credentials, valid about one hour.
- **Private key:** a protected file (`0600`, refused otherwise) or an OS keyring entry (**OPEN**). The key can mint tokens for every installation of the App: its blast radius is larger than a token's; rotation and revocation are documented.
- **Identity cache:** resolved bot login, numeric id, and email, cached with the profile, refreshed by `setup` and `doctor`.

## Security model (brief §10)

- Guarantees of local mode: identity and credentials are controlled on the supported execution paths; personal Git and `gh` configuration is never modified; agent-mode authentication fails closed; GitHub App permissions and installation repository access are the authoritative controls.
- Limitation, stated everywhere it matters: a local agent running as the developer's OS user is not isolated from the developer's files, SSH agent, tokens, or executables, and can bypass wrappers or unset variables. Local mode is attribution and convenience, not containment.
- Secrets never appear in output, errors, logs, shell tracing (`set -x` of generated scripts), configuration dumps, or fixtures. `doctor` and `whoami` print the token status and expiry, never the token.
- Hardened mode (separate broker, separate OS identity or sandbox, policy, audit) is Phase 3 and a separate milestone.

## Cross-cutting concerns

- **Errors:** wrapped with `%w`, matched with `errors.Is`. Only `cmd/agent-id` prints errors and sets exit codes; `exec` returns the child's exit status unchanged. No `panic` for expected failures. Output goes to stdout, diagnostics to stderr.
- **Testing strategy:**
  - Unit tests for every pure security function, with hostile input: look-alike hosts, other protocols, unrelated repository paths, inherited personal tokens and helpers, `GIT_CONFIG_COUNT` already set, expired tokens, unreadable or world-readable keys.
  - Handler tests against `testenv.NewFakeGitHub` in an isolated environment (`testenv.Isolate`). `go test ./...` never reaches the network and never needs a GitHub account.
  - Process tests that run real `git` and `gh` binaries do so only in an isolated environment with a fake GitHub and a local bare repository.
  - End-to-end tests against a disposable GitHub repository and a dedicated test App carry the `e2e` build tag and run by hand (`SDLC.md`, Validation gate).
  - CI runs the gate on `ubuntu-24.04` and the darwin lint, vet, and build; `darwin.yml` runs the unit tests on `macos-26`.

## Key decisions

Derived from `PRODUCT_BRIEF.md`; the brief is the record, except where a row cites a later decision of the owner on the tracker. Rows with status **OPEN** wait for the owner.

| Date | Decision | Source | Status |
|---|---|---|---|
| 2026-11-12 | Go CLI, module `github.com/cultofthecode/agent-id`, cobra for the command tree (as in orbit) | owner's starter task; orbit D02/D23 | active |
| 2026-11-12 | GitHub first; MVP targets GitHub.com, macOS/Linux, Bash/Zsh | brief §3 principle 6, §8 | active |
| 2026-11-12 | Explicit `agent-id exec` is the canonical activation; `env` + Claude Code `SessionStart` is a convenience layer | brief §7.2–7.3, §14 profile selection | active |
| 2026-11-12 | Process-scoped Git configuration through `GIT_CONFIG_COUNT` and author/committer variables; no global changes | brief §9.3, §10 | active |
| 2026-11-12 | Fail closed in agent mode; no silent fallback to personal credentials | brief §5, §8 | active |
| 2026-11-12 | Local mode is attribution, not containment; hardened isolation is Phase 3 | brief §10, §14 credential leakage | active |
| 2026-11-12 | Turn off inherited personal signing in agent mode; never claim verified commits | brief §9.3 | active |
| 2026-11-12 | Start with existing GitHub Apps; guided or manifest registration is evaluated later | brief §14 onboarding | active |
| 2026-11-12 | No runtime dependency on GHAPP and no requirement to install it | brief §1, §13 Phase 0 | active |
| 2026-11-12 | The product name is `agent-id` everywhere: binary `agent-id` (`cmd/agent-id`), config and cache directory `agent-id` (`$XDG_CONFIG_HOME/agent-id`, `$XDG_CACHE_HOME/agent-id`), environment prefix `AGENT_ID_`. The repository and module path stay. The package, domain and trademark availability check is a pre-launch item. | owner's decision (Jacob, 2026-11-12 22:40 Europe/Warsaw) on [#6](https://github.com/cultofthecode/agent-id/issues/6); supersedes the working name of brief §14 | active |
| 2026-11-12 | Rewrite GHAPP-related code from scratch and reuse none of it; sign the App JWT with the Go standard library, not `go-githubauth`; GHAPP code copied without its license notice is a review blocker | owner, PR #31; `.ai/analysis/2026-11-12-ghapp-reuse-audit.md` | active |
| — | License of agent-id | brief §14, #8 | **OPEN** |
| — | Distribution and commercial model (license, hosting, release channel) | brief §14 | **OPEN** |
| — | Agent-specific commit signing as a later feature | brief §14 | **OPEN** |
| — | Profile auto-detection beyond explicit `exec`, and its precedence | brief §14 | **OPEN** (MVP: explicit only) |
| — | Profile config format and schema; keyring support | brief §9.4 | **OPEN** |
| — | Supported Git paths and documented bypasses; installation-token-compatible `gh` commands | brief §14 | **OPEN** (researched in Phase 1 issues) |
