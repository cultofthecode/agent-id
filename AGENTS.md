# AGENTS.md — agent-id

## Project overview

agent-id (product and binary name decided on #6, `ARCHITECTURE.md` Key decisions; `PRODUCT_BRIEF.md` keeps the earlier working name as history) is a Go CLI and local identity runtime that gives an AI coding agent its own GitHub App identity for one process and its descendants. A developer keeps their personal Git and `gh` identity in their own terminal; Claude Code, Codex, or another agent runs in the same repository as a dedicated bot, with installation tokens that are minted and refreshed automatically. The identity belongs to the execution context, not to the machine, the repository, or the developer's account (§2). The MVP targets GitHub.com, macOS and Linux, Bash and Zsh (§8). In the MVP local mode, agent-id controls attribution and credentials on its supported paths only; it is **not** a security boundary (§10). It is a standalone product: it reuses no GHAPP code (owner decision, PR #31, `ARCHITECTURE.md` Key decisions), never depends on GHAPP at run time, and never requires its installation (§1, §13 Phase 0).

The code so far is the composition root in `cmd/agent-id` (the root command and `--version`), the profile store in `internal/profile` (#17), the setup slice in `internal/setup` (`agent-id setup github`, #19), and the test helpers in `internal/testenv`, `internal/provider/providertest`, and `internal/provider/github/githubtest`, with the provider interface in `internal/provider` (ADR #35; settled by #15) and its GitHub adapter in `internal/provider/github` (#15; the composition root registers its `github` profile block validator, #58, and its onboarder for `setup`, #19). `internal/credential` loads the App private key from a protected file (#16). `internal/token` caches provider tokens on disk and refreshes them (#18; `whoami` is its first consumer). `internal/activation` builds the child environment and its `exec` slice implements `agent-id exec --profile <name> -- <cmd>` (#20): fail closed, with no App token injected yet (#22, #23). `internal/gitint` turns the cached bot identity into the author and committer variables and the signing-off entries (#21). `internal/diagnose` and its slices `whoami` and `doctor` print identity, credential source, token status, and conflicts without secrets (#24; the checks that need exec, the Git integration, and the `gh` shim are still `skip` until they are wired; exec merged in #86, wiring it is follow-up work). `agent-id env` (`internal/activation/env`) prints the child environment as a shell block, and `agent-id claude-code session-start` (`internal/integration/claude`) appends it to `CLAUDE_ENV_FILE` from a Claude Code `SessionStart` hook (#26). Everything else in `ARCHITECTURE.md` is planned. The backlog is on GitHub: https://github.com/cultofthecode/agent-id/issues, mapped in `.ai/specs/backlog.md`.

## Task routing

Before you work on a task of a given kind, read the files in **Read first**. When a module under `internal/` comes into existence, give it its own row here and a short `AGENTS.md` with its invariants.

| When the task involves… | Read first | Key rules |
|---|---|---|
| Product scope, requirements, non-goals | `PRODUCT_BRIEF.md` | The brief is the design authority. Open decisions (§14 and the OPEN rows of `ARCHITECTURE.md`) are not decided: do not implement a guess; the ticket carries `needs-decision` until the owner answers. |
| Delivery process, labels, QA gate, claims, Definition of Ready | `SDLC.md` | Follow the ticket lifecycle and the label state machine. Never apply `qa-approved`, `automerge`, or `do-not-merge` from an authoring task. |
| Code review | `CODE_REVIEW.md` | `om-code-review` and `om-auto-review-pr` apply it automatically. |
| System structure, module boundaries | `ARCHITECTURE.md` | Follow the dependency rules. A new module or a new cross-module dependency needs a row in Key decisions. |
| The profile config: its location, schema, validation, key references, resolution by `--profile`, writes | `.ai/specs/2026-11-12-profile-config.md`, `.ai/specs/capabilities/profile-config.md`, `internal/profile/AGENTS.md` | Always `risk-high`. `internal/profile` stays provider-agnostic; a provider block belongs to its adapter through `profile.SettingsValidator`. Unknown and duplicate members are errors; a schema change bumps `version`. Ask first before changing the format. |
| Credentials: private key source, installation tokens, the token cache, the Git credential helper, the `gh` token injection | `ARCHITECTURE.md` (Security model), brief §9.3, §10 | Always `risk-high`. Fail closed. Keep the decision in a pure function with hostile-input tests. A token never appears in output, errors, logs, or fixtures. |
| The token cache: `internal/token` | `.ai/specs/2026-11-13-token-manager.md`, `.ai/specs/capabilities/token-cache.md`, `internal/token/AGENTS.md` | Always `risk-high`. Core only: imports `internal/provider`, never a forge. Fail closed; `0600` entries in a private `0700` directory, atomic `rename`, per-key `flock`; no token in errors or output. |
| The private key source: `internal/credential` | `internal/credential/AGENTS.md` | Always `risk-high`. Only a protected file (`0600`, owned by the user) loads; a keyring backend is OPEN. No key material in errors; keys are generated at test time, never committed. |
| Git identity and process-scoped Git configuration | `ARCHITECTURE.md` (Git integration), brief §9.3 | Always `risk-high`. Only `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` and the author and committer variables; never `git config --global`, never a file in the developer's home. |
| A Git forge (GitHub now; GitLab, Bitbucket later): its API, tokens, bot identity, hosts and URLs, CLI | `ARCHITECTURE.md` (Provider abstraction, Dependency rules), `internal/provider/AGENTS.md`, `internal/provider/github/AGENTS.md` | Core code talks to `internal/provider` only. GitHub-specific code lives in `internal/provider/github`; only `cmd/agent-id` constructs that adapter (depguard `github-only-in-its-adapter`). A new forge is a new adapter that passes `providertest.RunContract`, never a change to the core. |
| The child environment, `agent-id exec`, nesting, the neutral credential baseline | `.ai/specs/2026-11-13-exec-child-environment.md`, `.ai/specs/capabilities/exec.md`, `internal/activation/AGENTS.md` | Always `risk-high`. Fail closed from the first merge: the child never reaches the developer's credentials through Git or `gh`. `Build` and every decision stay pure with hostile-input tests; no token in the environment, output, or errors; `internal/activation` names no forge (`provider.Activator`). Ask first before changing a variable name or the exit codes. |
| Diagnostics: `agent-id whoami`, `agent-id doctor`, redaction of output | `.ai/specs/2026-11-13-whoami-doctor-output.md`, `.ai/specs/capabilities/whoami.md`, `doctor.md`, `diagnostics-redaction.md`, `internal/diagnose/AGENTS.md` | Always `risk-high`. No report field holds a secret; every string passes `Redact` and the output passes `Guard` (exit 3 on a match). Checks are pure functions over a `Snapshot` with hostile-input tables; a missing dependency is `skip`, never `ok`. The ids, JSON members, labels, and exit codes are a contract. |
| Setup: `agent-id setup <provider>`, the onboarding port, the setup summary | `.ai/specs/2026-11-13-setup-github.md`, `.ai/specs/capabilities/setup.md`, `internal/setup/AGENTS.md` | Always `risk-high`. `internal/setup` names no forge; a provider joins through `provider.Onboarder`. Nothing is written before verification; a failure leaves `config.json` as it was. No token, JWT, or key material in output. |
| Git commit identity in agent mode: author and committer variables, personal signing off | `.ai/specs/2026-11-13-git-commit-identity.md`, `.ai/specs/capabilities/git-identity.md`, `internal/gitint/AGENTS.md` | Always `risk-high`. `gitint` is pure, names no forge, never echoes an identity value; entries reach the environment only through `activation.Build`'s append rule. Never claim bot commits are verified. Ask first before changing a variable or entry name. |
| `agent-id env`, the Claude Code `SessionStart` hook, the guarded block and the refusal line | `.ai/specs/2026-11-13-env-claude-session-start.md`, `.ai/specs/capabilities/env.md`, `.ai/specs/capabilities/claude-code-session-start.md`, `internal/integration/claude/AGENTS.md` | Always `risk-high`. The block carries no token and fails as a whole; a failed activation appends a refusal line (every command stops with 125). Never write `.claude/settings*.json` or `~/.claude` (OQ3: no install command in the MVP); tests use a fake `HOME` and a fake env file under `$TMPDIR/agent-id/`. Ask first before changing a marker, a command name, or an exit code (3, 4, 125). |
| A new CLI operation | `ARCHITECTURE.md` (Slice anatomy) | One slice is one package: `contract.go`, `handler.go`, `command.go`, `handler_test.go`. Write the contract first. |
| Tests | `ARCHITECTURE.md` (Testing strategy), `internal/testenv/AGENTS.md`, `internal/provider/AGENTS.md`, `internal/provider/github/githubtest/AGENTS.md`, the Test isolation section below | Every test that runs agent-id code, `git`, or `gh` calls `testenv.Isolate(t)` first. GitHub is always the fake. Core tests use `providertest.Fake`; only GitHub adapter tests use `githubtest`. Do not weaken a test to make it pass. |
| Pipeline configuration | `.ai/agentic.config.json`, `.ai/trackers/github.md` | Change the config and `SDLC.md` together. Run `om-setup-agent-pipeline` again when the toolchain or the labels change. |
| Specs and design docs | `.ai/specs/` | Filenames: `{YYYY-MM-DD}-{kebab-case-title}.md`. Analyses and spikes go under `.ai/analysis/`. |
| End-to-end validation on real GitHub: `e2e/`, `docs/runbooks/e2e.md` | `docs/runbooks/e2e.md`, `.ai/specs/backlog.md` (#27) | Always `risk-high`. Behind the `e2e` build tag and `AGENT_ID_E2E=1`; only the owner runs it, by hand; CI and agents never run it. Key by path from `AGENT_ID_E2E_KEY`, never read, printed or committed. Only the repository `*-e2e`. No output may match a token or key shape. |
| CI | `.github/workflows/ci.yml`, `.github/workflows/darwin.yml` | `ci.yml` runs the gate on GitHub-hosted `ubuntu-24.04` as the check `gate` and a GoReleaser snapshot as `release-snapshot`; `darwin.yml` runs the unit tests on `macos-26` by hand and weekly. Pin every action by full commit SHA with the version in a comment; keep `contents: read`; no secrets; no self-hosted runner. |
| Releases | `RELEASING.md`, `.goreleaser.yaml`, `.github/workflows/release.yml`, `.github/scripts/` | Only an annotated SemVer tag on `main` publishes. The release job uses the default `GITHUB_TOKEN` only; a new credential (for example for the Homebrew tap) is an owner decision. Test locally with `release-dry-run.sh` under `$TMPDIR/agent-id/`; never create a tag or a release from an agent task. |

## Code conventions

- Standard Go style: `gofmt`, short lowercase package names, errors wrapped with `%w`.
- **Zero GitHub-specific imports outside `internal/provider/github`.** No other package imports a GitHub client or library or GitHub types, hard-codes the hosts `github.com` or `api.github.com`, or holds App JWT, installation-token, or `gh` wiring. A Go import path that merely lives on GitHub, such as this repository's own module path `github.com/cultofthecode/agent-id`, is not GitHub-specific. Core code depends on the `internal/provider` interface. The only exceptions: `cmd/agent-id` constructs and injects the adapter, and the test-only package `internal/testenv` holds the fake GitHub and the GitHub variables that `Isolate` removes, for test files to use (ADR in `ARCHITECTURE.md`, Provider abstraction).
- One level of abstraction per function. Split a complicated function into well-named functions.
- Doc comments on packages and exported identifiers. No comments inside function bodies.
- No abstraction for single-use code, and no option that nobody asked for.

## Validation commands

The gate is active. The same commands are in `validation.commands` in `.ai/agentic.config.json` and in `SDLC.md`. Run them from the repository root, in this order, inside an isolated environment (below):

```sh
test -z "$(gofmt -l .)"
go vet ./...
golangci-lint run
go test ./...
go build ./...
```

`golangci-lint` is already installed on the owner's machine (JARVIS); use it, but never install or upgrade anything with `brew` (no `brew install`, no `brew upgrade`). CI runs the same gate.

## Test isolation (mandatory)

agent-id exists to change which identity Git and `gh` use. A test that leaks into the developer's real configuration is the worst bug this repository can have. Every test, script, and manual check follows these rules:

- **Isolated environment.** Every test that runs agent-id code, `git`, or `gh` calls `testenv.Isolate(t)` first. It sets a temporary `HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME`, `GIT_CONFIG_GLOBAL`, `GH_CONFIG_DIR`, and `TMPDIR` under `$TMPDIR/agent-id/`, sets `GIT_CONFIG_NOSYSTEM=1`, `GIT_TERMINAL_PROMPT=0`, and `GIT_ALLOW_PROTOCOL=file:https`, and removes every inherited token, credential helper hint, SSH agent socket, Git identity, and `AGENT_ID_*`/`GHAPP_*` variable, and points the proxy variables at a guard that fails the test when a child process tries to reach the network. The guard forwards only to 127.0.0.1:18000–18999 and `NO_PROXY` is empty, so a proxy-aware child such as `git` cannot reach another loopback port; Go programs such as `gh` never proxy loopback, which is a residual limit. A package whose tests run `git`, `gh`, or agent-id calls `testenv.Main(m)` from `TestMain`, which fails the run when `~/.gitconfig`, `~/.config/git`, or the `gh` configuration changed, or, by metadata only (never opening a file), when `~/.config/ghapp` or `$XDG_CONFIG_HOME/ghapp` changed. Manual runs of the gate use the same variables (see below).
- **Zero writes to real configuration.** No test, script, or agent writes to `~/.gitconfig`, `~/.config/git`, the system Git configuration, the macOS Keychain or any OS keyring, the ghapp configuration (`~/.config/ghapp`), or the `gh` configuration (`~/.config/gh`). Do not run `ghapp auth configure`, `ghapp auth reset`, `ghapp setup`, `gh auth login`, `gh auth switch`, or `git config --global` outside an isolated environment. A keyring is always a fake behind an interface in tests.
- **No real tokens.** Tests use the fake provider of `internal/provider/providertest` (`NewFake`), the fake GitHub App of `internal/provider/github/githubtest` (`NewApp`, on `testenv.NewFakeAPI`), and fake tokens (`testenv.FakeToken`, prefix `ghs_aidtest_`). No test reads a real private key, a real installation token, `GH_TOKEN`, `GITHUB_TOKEN`, or `ghapp token`. The only tokens an agent uses are for tracker work (`GH_TOKEN=$(ghapp token) gh …`), never inside tests and never in a file of this repository.
- **No global installs.** Tests never install anything globally: no `go install`, `brew`, `npm -g`, `pip install`, no write to `/usr/local`, `/opt/homebrew`, or `~/.local/bin`. A test that needs a binary builds it into its own temporary directory.
- **Temporary files** go under `$TMPDIR/agent-id/` only, and a test removes what it created.
- **Ports.** agent-id listens only on `127.0.0.1` with a port in **18000–18999** (`testenv.Listen`). Ports **19000–19999 belong to orbit**; never bind them.
- **Docker and OrbStack.** A container, network, volume, image tag, or OrbStack machine that agent-id creates carries the prefix **`aid-`**, never `orbit-`. Never restart OrbStack, never stop or delete machines or containers that agent-id did not create, and never delete all machines (`orb delete --all`, `docker system prune`, or similar) without telling the owner first.
- **Worktrees.** Parallel work uses `git worktree add ~/Projects/agent-id-wt/<name>`; never another location, and never a worktree of another repository.
- **Agents on the shared machine.** hc agents of this project are named with the prefix **`aid-`**. Agents of orbit work in `~/Projects/orbit*`: do not touch their directories, branches, worktrees, machines, or processes. Never type into, close, or reuse the terminals **t1** and **t40**; they belong to the owner. Start new agents only with `~/Projects/.agent-id-env/launch-agent.sh` (see below).

A manual run of the gate in an isolated environment, from the repository root:

```sh
iso="${TMPDIR:-/tmp}/agent-id/manual"; mkdir -p "$iso/home"
env -u GH_TOKEN -u GITHUB_TOKEN -u SSH_AUTH_SOCK \
  HOME="$iso/home" XDG_CONFIG_HOME="$iso/home/.config" XDG_CACHE_HOME="$iso/home/.cache" \
  GIT_CONFIG_GLOBAL="$iso/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GH_CONFIG_DIR="$iso/home/.config/gh" \
  GOMODCACHE="$(go env GOMODCACHE)" GOCACHE="$iso/gocache" GOLANGCI_LINT_CACHE="$iso/lintcache" GOTOOLCHAIN=local \
  sh -c 'test -z "$(gofmt -l .)" && go vet ./... && golangci-lint run && go test ./... && go build ./...'
```

## Calling GitHub from an agent (mandatory)

Start new hc agents of this project only with:

```sh
~/Projects/.agent-id-env/launch-agent.sh <nr-slug> <worktree> <issue> [task prompt...]
```

The script sets `GH_CONFIG_DIR` to the empty, write-protected directory `~/Projects/.agent-id-env/gh-empty` and puts `~/Projects/.agent-id-env/bin` first on `PATH`. It does **not** set `GIT_CONFIG_GLOBAL`: the ghapp credential helper must stay visible so `git push` works. It does not export a standing `GH_TOKEN`: installation tokens expire, so mint a fresh one per command.

`GH_CONFIG_DIR` alone is not enough. In gh 2.103.3, `AuthConfig.ActiveToken` still does an unkeyed keychain lookup for service `gh:github.com` even when `hosts.yml` is missing. An empty `GH_TOKEN` is treated as unset and can hit that fallback if the developer later signs in with `gh auth login`. The wrapper `~/Projects/.agent-id-env/bin/gh` therefore refuses to run unless `GH_TOKEN` or `GITHUB_TOKEN` is non-empty. It accepts `GITHUB_TOKEN` as a substitute, and `gh` uses `GITHUB_TOKEN` when `GH_TOKEN` is empty, so never set `GITHUB_TOKEN` in an agent's environment: a personal `GITHUB_TOKEN` there would pass the wrapper. A shell function named `gh` in the developer's startup files (for example ghapp's shell hook) shadows the wrapper by name in the agent's shells. Such a function still reaches the wrapper only when it calls `command gh`, and ghapp's hook does.

Every `gh` command must be:

```sh
GH_TOKEN=$(ghapp token) gh ...
```

Never call `gh` without that token. Never run `gh auth login`, `gh auth logout`, or `gh auth switch`. Do not write to `~/Library/Application Support/gh/`, `~/.config/gh`, or the OS keychain.

An already running agent's process environment cannot be changed in place. Until that agent is relaunched with `launch-agent.sh`, prefix every `gh` command with `GH_CONFIG_DIR="$HOME/Projects/.agent-id-env/gh-empty" GH_TOKEN=$(ghapp token)`. Without the wrapper on `PATH`, nothing stops `gh` when `ghapp token` prints nothing, so such an agent checks that the token is non-empty before it calls `gh`.

## Always / Ask first / Never

- **Always:** work on a branch, never directly on `main`. Run the validation gate in an isolated environment before you open a PR. Label a change to credentials, identity, or the child environment `risk-high`. Fail closed when an agent credential cannot be obtained. Start new `aid-` agents with `~/Projects/.agent-id-env/launch-agent.sh`. Call `gh` only as `GH_TOKEN=$(ghapp token) gh ...`.
- **Ask first:** before you add a Go module dependency (a keyring library, a YAML parser, a file-lock library); before you change the profile config format or the environment that `exec` and `env` emit; before you copy code from GHAPP (the owner decided to reuse none, so copied code needs a new owner decision and its MIT notice, `CODE_REVIEW.md` Provenance); before you resolve an open decision of the brief.
- **Never:** commit secrets, tokens, private keys, or `.env` content. Never print a token, even in debug output or errors. Never fall back to the developer's personal credentials in agent mode. Never call `gh` without a non-empty `GH_TOKEN` from `$(ghapp token)`. Never modify the developer's global Git configuration, `gh` configuration, ghapp configuration, shell startup files, or keychain from agent-id code or from a test. Never claim that local mode isolates the agent, or that App-authenticated pushes produce verified commits. Never delete, rename, or recolor existing tracker labels. Never apply `qa-approved`, `automerge`, or `do-not-merge` from an authoring task.

## Related documents

- `PRODUCT_BRIEF.md` — problem, scope, requirements, roadmap, open decisions
- `SDLC.md` — ticket flow, Definition of Ready, and merge gates
- `CODE_REVIEW.md` — review rules
- `ARCHITECTURE.md` — system structure and decisions
- `.ai/agentic.config.json` — pipeline configuration
- `.ai/specs/backlog.md` — the backlog and its GitHub issues
