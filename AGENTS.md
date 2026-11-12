# AGENTS.md — agent-id (aidentity)

## Project overview

aidentity (working name, `PRODUCT_BRIEF.md` §14; repository `cultofthecode/agent-id`) is a Go CLI and local identity runtime that gives an AI coding agent its own GitHub App identity for one process and its descendants. A developer keeps their personal Git and `gh` identity in their own terminal; Claude Code, Codex, or another agent runs in the same repository as a dedicated bot, with installation tokens that are minted and refreshed automatically. The identity belongs to the execution context, not to the machine, the repository, or the developer's account (§2). The MVP targets GitHub.com, macOS and Linux, Bash and Zsh (§8). In the MVP local mode, aidentity controls attribution and credentials on its supported paths only; it is **not** a security boundary (§10). It is a standalone product: it may reuse audited GHAPP code with the required notices, but it never depends on GHAPP at run time and never requires its installation (§1, §13 Phase 0).

The code so far is the composition root in `cmd/aidentity` (the root command and `--version`) and the test helpers in `internal/testenv` and `internal/github/githubtest`. Everything else in `ARCHITECTURE.md` is planned. The backlog is on GitHub: https://github.com/cultofthecode/agent-id/issues, mapped in `.ai/specs/backlog.md`.

## Task routing

Before you work on a task of a given kind, read the files in **Read first**. When a module under `internal/` comes into existence, give it its own row here and a short `AGENTS.md` with its invariants.

| When the task involves… | Read first | Key rules |
|---|---|---|
| Product scope, requirements, non-goals | `PRODUCT_BRIEF.md` | The brief is the design authority. Open decisions (§14 and the OPEN rows of `ARCHITECTURE.md`) are not decided: do not implement a guess; the ticket carries `needs-decision` until the owner answers. |
| Delivery process, labels, QA gate, claims, Definition of Ready | `SDLC.md` | Follow the ticket lifecycle and the label state machine. Never apply `qa-approved`, `automerge`, or `do-not-merge` from an authoring task. |
| Code review | `CODE_REVIEW.md` | `om-code-review` and `om-auto-review-pr` apply it automatically. |
| System structure, module boundaries | `ARCHITECTURE.md` | Follow the dependency rules. A new module or a new cross-module dependency needs a row in Key decisions. |
| Credentials: private key source, installation tokens, the token cache, the Git credential helper, the `gh` token injection | `ARCHITECTURE.md` (Security model), brief §9.3, §10 | Always `risk-high`. Fail closed. Keep the decision in a pure function with hostile-input tests. A token never appears in output, errors, logs, or fixtures. |
| Git identity and process-scoped Git configuration | `ARCHITECTURE.md` (Git integration), brief §9.3 | Always `risk-high`. Only `GIT_CONFIG_COUNT`/`GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` and the author and committer variables; never `git config --global`, never a file in the developer's home. |
| A new CLI operation | `ARCHITECTURE.md` (Slice anatomy) | One slice is one package: `contract.go`, `handler.go`, `command.go`, `handler_test.go`. Write the contract first. |
| Tests | `ARCHITECTURE.md` (Testing strategy), `internal/testenv/AGENTS.md`, `internal/github/githubtest/AGENTS.md`, the Test isolation section below | Every test that runs aidentity code, `git`, or `gh` calls `testenv.Isolate(t)` first. GitHub is always the fake. Do not weaken a test to make it pass. |
| Pipeline configuration | `.ai/agentic.config.json`, `.ai/trackers/github.md` | Change the config and `SDLC.md` together. Run `om-setup-agent-pipeline` again when the toolchain or the labels change. |
| Specs and design docs | `.ai/specs/` | Filenames: `{YYYY-MM-DD}-{kebab-case-title}.md`. Analyses and spikes go under `.ai/analysis/`. |
| CI | `.github/workflows/ci.yml`, `.github/workflows/darwin.yml` | `ci.yml` runs the gate on GitHub-hosted `ubuntu-24.04` as the check `gate`; `darwin.yml` runs the unit tests on `macos-26` by hand and weekly. Pin every action by full commit SHA with the version in a comment; keep `contents: read`; no secrets; no self-hosted runner. |

## Code conventions

- Standard Go style: `gofmt`, short lowercase package names, errors wrapped with `%w`.
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

aidentity exists to change which identity Git and `gh` use. A test that leaks into the developer's real configuration is the worst bug this repository can have. Every test, script, and manual check follows these rules:

- **Isolated environment.** Every test that runs aidentity code, `git`, or `gh` calls `testenv.Isolate(t)` first. It sets a temporary `HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`, `XDG_DATA_HOME`, `GIT_CONFIG_GLOBAL`, `GH_CONFIG_DIR`, and `TMPDIR` under `$TMPDIR/agent-id/`, sets `GIT_CONFIG_NOSYSTEM=1` and `GIT_TERMINAL_PROMPT=0`, and removes every inherited token, credential helper hint, SSH agent socket, Git identity, and `AIDENTITY_*`/`GHAPP_*` variable, and points the proxy variables at a guard that fails the test when a child process tries to reach the network. A package whose tests run `git`, `gh`, or aidentity calls `testenv.Main(m)` from `TestMain`, which fails the run when `~/.gitconfig`, `~/.config/git`, or the `gh` configuration changed. Manual runs of the gate use the same variables (see below).
- **Zero writes to real configuration.** No test, script, or agent writes to `~/.gitconfig`, `~/.config/git`, the system Git configuration, the macOS Keychain or any OS keyring, the ghapp configuration (`~/.config/ghapp`), or the `gh` configuration (`~/.config/gh`). Do not run `ghapp auth configure`, `ghapp auth reset`, `ghapp setup`, `gh auth login`, `gh auth switch`, or `git config --global` outside an isolated environment. A keyring is always a fake behind an interface in tests.
- **No real tokens.** Tests use the fake GitHub API of `internal/testenv` (`NewFakeGitHub`), the fake App of `internal/github/githubtest` (`NewApp`), and fake tokens (`testenv.FakeToken`, prefix `ghs_aidtest_`). No test reads a real private key, a real installation token, `GH_TOKEN`, `GITHUB_TOKEN`, or `ghapp token`. The only tokens an agent uses are for tracker work (`GH_TOKEN=$(ghapp token) gh …`), never inside tests and never in a file of this repository.
- **No global installs.** Tests never install anything globally: no `go install`, `brew`, `npm -g`, `pip install`, no write to `/usr/local`, `/opt/homebrew`, or `~/.local/bin`. A test that needs a binary builds it into its own temporary directory.
- **Temporary files** go under `$TMPDIR/agent-id/` only, and a test removes what it created.
- **Ports.** agent-id listens only on `127.0.0.1` with a port in **18000–18999** (`testenv.Listen`). Ports **19000–19999 belong to orbit**; never bind them.
- **Docker and OrbStack.** A container, network, volume, image tag, or OrbStack machine that agent-id creates carries the prefix **`aid-`**, never `orbit-`. Never restart OrbStack, never stop or delete machines or containers that agent-id did not create, and never delete all machines (`orb delete --all`, `docker system prune`, or similar) without telling the owner first.
- **Worktrees.** Parallel work uses `git worktree add ~/Projects/agent-id-wt/<name>`; never another location, and never a worktree of another repository.
- **Agents on the shared machine.** hc agents of this project are named with the prefix **`aid-`**. Agents of orbit work in `~/Projects/orbit*`: do not touch their directories, branches, worktrees, machines, or processes. Never type into, close, or reuse the terminals **t1** and **t40**; they belong to the owner.

A manual run of the gate in an isolated environment, from the repository root:

```sh
iso="${TMPDIR:-/tmp}/agent-id/manual"; mkdir -p "$iso/home"
env -u GH_TOKEN -u GITHUB_TOKEN -u SSH_AUTH_SOCK \
  HOME="$iso/home" XDG_CONFIG_HOME="$iso/home/.config" XDG_CACHE_HOME="$iso/home/.cache" \
  GIT_CONFIG_GLOBAL="$iso/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GH_CONFIG_DIR="$iso/home/.config/gh" \
  GOMODCACHE="$(go env GOMODCACHE)" GOCACHE="$iso/gocache" GOLANGCI_LINT_CACHE="$iso/lintcache" GOTOOLCHAIN=local \
  sh -c 'test -z "$(gofmt -l .)" && go vet ./... && golangci-lint run && go test ./... && go build ./...'
```

## Always / Ask first / Never

- **Always:** work on a branch, never directly on `main`. Run the validation gate in an isolated environment before you open a PR. Label a change to credentials, identity, or the child environment `risk-high`. Fail closed when an agent credential cannot be obtained.
- **Ask first:** before you add a Go module dependency (a keyring library, a YAML parser, a file-lock library); before you change the profile config format or the environment that `exec` and `env` emit; before you copy code from GHAPP (only after the provenance and license audit, with its notices); before you resolve an open decision of the brief.
- **Never:** commit secrets, tokens, private keys, or `.env` content. Never print a token, even in debug output or errors. Never fall back to the developer's personal credentials in agent mode. Never modify the developer's global Git configuration, `gh` configuration, ghapp configuration, shell startup files, or keychain from aidentity code or from a test. Never claim that local mode isolates the agent, or that App-authenticated pushes produce verified commits. Never delete, rename, or recolor existing tracker labels. Never apply `qa-approved`, `automerge`, or `do-not-merge` from an authoring task.

## Related documents

- `PRODUCT_BRIEF.md` — problem, scope, requirements, roadmap, open decisions
- `SDLC.md` — ticket flow, Definition of Ready, and merge gates
- `CODE_REVIEW.md` — review rules
- `ARCHITECTURE.md` — system structure and decisions
- `.ai/agentic.config.json` — pipeline configuration
- `.ai/specs/backlog.md` — the backlog and its GitHub issues
