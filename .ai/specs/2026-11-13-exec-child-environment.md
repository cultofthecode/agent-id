# agent-id exec — the child environment contract

Issue: #20 (E02-S06). Implemented by #20; extended by #21 (Git identity), #22 (Git transport), #23 (`gh` injection); reused by #26 (`agent-id env`) and read by #24 (`whoami`, `doctor`).

Extends: `.ai/specs/2026-11-12-profile-config.md` (it consumes `Resolve` and decides the consumer rule for an unresolved identity that the profile spec left to #20).

## 📝 TLDR

The developer needs to start one agent process, and only that process tree, under a bot profile, while their own terminal stays personal. Today agent-id has no activation at all. This spec proposes `agent-id exec --profile <name> [--] <command> [args…]`: it resolves the profile, builds the child environment with one pure function, refuses a nested start with a different profile, and then **replaces itself** with the command (`execve`), so the exit status and every signal belong to the command by construction. It also fixes the environment contract (the `AGENT_ID_*` context variables, removal of inherited forge tokens, the append rule for `GIT_CONFIG_*`) that #21–#23 extend and that `agent-id env` (#26) prints.

Decisions this spec follows:

- **Nested exec with a different profile is an explicit error that names both profiles; there is no override in the MVP. The same profile is idempotent** (project manager, 2026-11-13, on #20; owner delegated technical decisions).
- **O1–O4 of the Git precedence analysis** (project manager, 2026-11-13, on #20, #22, #24): O1 — `exec` refuses a repository whose GitHub remote still resolves to SSH after the rewrite set; O2 — refuse an inherited `GIT_CONFIG_PARAMETERS` that sets `credential`, `url`, `http`, or signing keys, otherwise keep it; O3 — SSH fallback is documented and detected (`doctor`), the `GIT_SSH_COMMAND` shim is Phase 3; O4 — Git 2.43 is the minimum.
- **Explicit selection only** (brief §14; profile spec): `--profile` is required. The context variable that `exec` sets is a marker for descendants and diagnostics, never a way to select a profile.
- **Provider abstraction** (ADR #35): `internal/activation` names no forge; the variables to drop and the hint for an unresolved identity come from the provider adapter.

## 📝 Problem Statement

- Brief §8 *Process-scoped execution* is P0: a selected profile must apply to a child and its descendants, and the parent terminal must stay unchanged. Brief §3 principle 1: context over global state.
- The child environment is the only activation mechanism (`ARCHITECTURE.md`, System context). Every later story writes into it: #21 adds the author and committer and the signing entries, #22 the credential helper and SSH rewrites, #23 the `gh` shim. Without one owner and one composition rule, each story would invent its own append and nesting logic, and a nested start would duplicate `GIT_CONFIG_*` entries (#26 acceptance: "nothing is duplicated").
- The environment that `exec` and `env` emit is a shared contract (`SDLC.md`, risk inference; `AGENTS.md`, Ask first). Scripts, `whoami`, and the Claude Code hook depend on its variable names.
- An agent that starts with the developer's inherited `GH_TOKEN` would act as the developer: the fail-open path that brief §5 and §8 forbid.
- Signal and exit-status handling decides whether `exec` is invisible to the developer. A wrapper that forwards `SIGINT` to a child that already got it from the terminal makes Claude Code see Ctrl-C twice, which exits it.

## 📝 Proposed Solution

1. **Command.** `agent-id exec --profile <name> [--] <command> [args…]`. Flag parsing stops at the first non-flag argument, so `agent-id exec --profile claude claude --resume` works without `--`; `--` stays accepted. `--profile` is required. `exec` has no other flag.
2. **Resolve.** Load the config and `Resolve(name)` (profile spec). Take the adapter registered for the profile's `provider`. Ask the adapter whether the profile is ready to activate: in the MVP, the GitHub adapter answers "not ready" when `github.identity` is missing, with its own hint (`run agent-id setup github --profile <name>`). `exec` refuses such a profile and never resolves the identity over the network.
3. **Read the inherited context** (pure). Decide fresh start, idempotent re-entry (same profile), or refusal (different profile, damaged or unknown context).
4. **Check the inherited Git process configuration** (pure). Refuse a malformed `GIT_CONFIG_COUNT` set (G2–G4 of the analysis) and an inherited `GIT_CONFIG_PARAMETERS` that sets a forbidden key (O2).
5. **Preflight checks.** Run the checks that later stories register and that need the file system or Git, in a fixed order. #20 ships the list empty; #22 registers O1 (a GitHub remote of the working directory's repository that resolves to SSH after the rewrite set).
6. **Build** the child environment with one pure function from the parent environment and an activation plan (below). The result never contains a token or key.
7. **Look up** the command with the child's `PATH` (so the `gh` shim of #23 wins, as it would in the agent's shell), then `execve` it with the child environment. agent-id is gone; its process id, open descriptors, controlling terminal, process group, and working directory now belong to the command.

`exec` writes no file, opens no network connection, and prints nothing on success.

### The activation plan

`internal/activation` owns the plan and the builder; the other modules contribute data to it, as in `ARCHITECTURE.md` (Context runtime depends on `gitint` and `cliint`):

| Plan field | Meaning | Contributed by |
|---|---|---|
| `Profile` | the resolved profile name | #20 |
| `Mechanism` | `exec` or `env` | #20, #26 |
| `Binary` | absolute path of the running agent-id binary | #20 |
| `Drop` | variable names removed from the child | the provider adapter (CLI integration: `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN` for GitHub) — #20 wires it |
| `Set` | variables set in the child | #21 (author and committer) |
| `GitConfig` | ordered `key=value` entries appended to the process-scoped Git configuration | #21 (signing), #22 (helper reset, helper, `useHttpPath`, `extraHeader` reset, SSH rewrites, `core.askPass`) |
| `PathPrepend` | directories prepended to `PATH` | #23 (the `gh` shim directory) |

In #20 only `Profile`, `Mechanism`, `Binary`, and `Drop` are filled. The builder treats the other fields generically and is fully tested with them in #20, so #21–#23 add data, not composition logic.

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **`execve`: agent-id replaces itself with the command** | Exit status, signals, job control, and the terminal are the command's own; no double `SIGINT`; no code to forward signals; the command's process id is the one the developer sees. Used by `env(1)`, `direnv exec`, `sudo` without a PTY. | No code runs after the command: no cleanup, no exit message. agent-id needs neither: it creates no temporary state, and tokens are fetched per use by the helper and the shim. | **Chosen** |
| Fork, wait, and forward `SIGINT`/`SIGTERM`/`SIGHUP` (the issue's sketch) | Could clean up or report after the child ends. | Terminal-generated `SIGINT` reaches the whole foreground process group, so forwarding delivers it twice (Claude Code exits on a second Ctrl-C). Needs `SIGTSTP`/`SIGCONT`, `SIGWINCH`, and exit-by-signal emulation (`128+n`) to be invisible. More code on a `risk-high` path. | Rejected |
| Fork with the child in its own process group | Avoids the double signal. | The child loses the terminal's foreground group unless agent-id hands the terminal over (`tcsetpgrp`); interactive agents break in subtle ways. | Rejected |
| Same profile nested: pass the environment through unchanged | Simplest no-op. | A startup file that unset the entries (R8 of the analysis) or a profile whose identity was refreshed would be carried forward silently. | Rejected; a deterministic rebuild gives the same result when the context is intact and repairs it when not. |
| Same profile nested: rebuild without removing the previous contribution | — | Appends the Git entries again (duplicates, #26 acceptance). | Rejected |
| Different profile nested: override (the inner profile wins) | Flexible. | The outer process tree would mix two bots; decided against for the MVP. | Rejected (decision, 2026-11-13) |
| Mint or check a token before the start | Fails before the agent starts. | Makes every start depend on the network; the helper and the shim fail closed at use anyway. | Not in the MVP (OQ1) |

Comparable tools: `aws-vault exec` and `op run` start the command as a child to inject secrets into its environment; agent-id puts no secret in the environment, so it does not need a supervising parent. `direnv exec` and `env(1)` use `execve` and the exit codes 125–127 that this spec adopts.

## 📝 Delta

### Capability: exec

#### ADDED Requirement: Only the command's process tree is in agent mode
The system SHALL start the command with the child environment of the resolved profile and SHALL NOT change the environment, the files, or the configuration of the invoking process or its terminal.

##### Scenario: Parent unchanged
- WHEN a test process runs `agent-id exec --profile claude -- sh -c 'exit 0'` in an isolated environment and then inspects its own environment and the isolated `HOME`
- THEN both are byte-identical to the state before the run, and no file under `HOME`, `XDG_*`, or `GIT_CONFIG_GLOBAL` was created or modified

##### Scenario: Marker in the child only
- WHEN the command is `sh -c 'printf %s "$AGENT_ID_ACTIVE_PROFILE"'`
- THEN it prints `claude`, and the invoking process has no `AGENT_ID_ACTIVE_PROFILE`

#### ADDED Requirement: Exit status and signals belong to the command
The system SHALL replace its own process with the command, so that the command's exit status and every signal sent to the process are the command's.

##### Scenario: Exit status
- WHEN `agent-id exec --profile claude -- sh -c 'exit 7'` finishes
- THEN the exit status is 7

##### Scenario: SIGTERM
- WHEN SIGTERM is sent to the process id that the test started as `agent-id exec --profile claude -- sh -c 'trap "exit 42" TERM; while :; do sleep 1; done'`
- THEN the command's trap runs and the exit status is 42

##### Scenario: Killed by a signal
- WHEN the command is killed by SIGKILL
- THEN the waiting process observes termination by SIGKILL, not an exit code

#### ADDED Requirement: agent-id's own failures use 125–127
The system SHALL exit with 125 when agent-id fails before starting the command, 126 when the command was found but could not be executed, and 127 when the command was not found, and SHALL print one error line to stderr starting with `agent-id: exec: `.

##### Scenario: Command not found
- WHEN the command is `does-not-exist`
- THEN the exit status is 127 and stderr is `agent-id: exec: "does-not-exist": command not found`

##### Scenario: Missing profile flag
- WHEN `agent-id exec -- true` runs
- THEN the exit status is 125 and the message names `--profile`

#### ADDED Requirement: Inherited forge tokens never reach the command
The system SHALL remove from the child environment every variable that the profile's provider names as a credential variable, and SHALL NOT place any token or key in the child environment.

##### Scenario: Personal GH_TOKEN
- WHEN the parent has `GH_TOKEN` and `GITHUB_TOKEN` set to fake personal tokens and the profile's provider is `github`
- THEN the command's environment has neither variable

##### Scenario: No secret in the child environment
- WHEN the child environment is built for any profile of the test fixtures
- THEN no value contains a registered token prefix, `-----BEGIN`, or a JWT shape

#### ADDED Requirement: Nested exec
The system SHALL rebuild the environment idempotently when an exec runs inside a context of the same profile, and SHALL refuse to start when the inherited context belongs to a different profile, is damaged, or has an unknown contract version.

##### Scenario: Same profile is idempotent
- WHEN `agent-id exec --profile claude -- agent-id exec --profile claude -- env` runs
- THEN the inner environment equals the outer one: the same `GIT_CONFIG_COUNT`, the same entries, and no duplicate `PATH` element

##### Scenario: Different profile is an error
- WHEN `agent-id exec --profile reviewer -- true` runs inside a context of profile `claude`
- THEN the exit status is 125, the command does not run, and the message names `claude` and `reviewer`

##### Scenario: Unknown context version
- WHEN the parent has `AGENT_ID_CONTEXT=2`
- THEN the exit status is 125 and the message says that the context comes from an incompatible agent-id

#### ADDED Requirement: Inherited Git process configuration
The system SHALL refuse to start when the inherited `GIT_CONFIG_COUNT` set is malformed, or when an inherited `GIT_CONFIG_PARAMETERS` sets a `credential.*`, `url.*`, `http.*`, or signing key, and SHALL keep every other inherited `GIT_CONFIG_*` entry and `GIT_CONFIG_PARAMETERS` unchanged.

##### Scenario: Malformed count
- WHEN the parent has `GIT_CONFIG_COUNT=2` and no `GIT_CONFIG_KEY_1`
- THEN the exit status is 125 and the message names `GIT_CONFIG_KEY_1` without printing any value

##### Scenario: Forbidden parameter
- WHEN the parent has `GIT_CONFIG_PARAMETERS="'credential.helper'='store'"`
- THEN the exit status is 125 and the message names `credential.helper`

##### Scenario: Harmless parameter kept
- WHEN the parent has `GIT_CONFIG_PARAMETERS="'core.pager'='less'"`
- THEN the command sees the same `GIT_CONFIG_PARAMETERS`

#### ADDED Requirement: Unresolved identity
The system SHALL refuse to activate a profile that its provider reports as not ready, with the provider's hint, and SHALL NOT contact the provider to resolve it.

##### Scenario: Profile without identity
- WHEN profile `reviewer` has no `github.identity`
- THEN the exit status is 125, the message contains `agent-id setup github --profile reviewer`, and the fake GitHub received no request

## 📝 Architecture

```mermaid
flowchart LR
    Root["cmd/agent-id (exists)"] --> Exec["internal/activation/exec (new slice)"]
    Exec --> Act["internal/activation (new module)"]
    Exec --> Store["internal/profile (#17, planned)"]
    Act --> Prov["internal/provider (interface, ADR #35)"]
    Act -.plan data, later.-> Git["internal/gitint (#21, #22)"]
    Act -.plan data, later.-> Cli["internal/cliint (#23)"]
    Env["internal/activation/env (#26, planned)"] --> Act
    Diag["internal/diagnose (#24, planned)"] -->|reads the context| Act
```

`internal/activation` is the only owner of the child environment; `exec` executes it and `env` (#26) prints it. Dashed edges are data that later stories add to the plan.

- **`internal/activation`** (module root, new). Pure functions and types only; no I/O except in the narrow `Execer` port:
  ```go
  type Plan struct {
      Profile     string
      Mechanism   string            // "exec" or "env"
      Binary      string
      Drop        []string
      Set         map[string]string
      GitConfig   []GitConfigEntry
      PathPrepend []string
  }
  type GitConfigEntry struct{ Key, Value string }
  type Context struct{ Version, Profile, Mechanism, Binary string; GitConfigBase int } // the inherited context, if any
  func ReadContext(parent []string) (ctx *Context, err error)              // pure
  func Decide(ctx *Context, profile string) (Start, error)                 // fresh | reenter | ErrNestedProfile
  func CheckInheritedGit(parent []string) error                            // G2–G4, O2; pure
  func Build(parent []string, plan Plan) ([]string, error)                 // pure, idempotent
  func ParseGitConfigParameters(v string) ([]string, error)                // keys only; pure
  ```
  It also owns the sentinel errors below, `ResolveBinary`, and `LookPath(env)` that uses the child's `PATH`. It imports `internal/provider` only for the narrow interface that supplies `Drop` and the readiness check; it imports no adapter.
- **`internal/activation/exec`** (slice, new): `contract.go` (`Input{Profile string; Argv []string}`, the exit-code error type), `handler.go` (dependencies: profile resolver, provider registry, preflight checks, `Execer`), `command.go` (cobra, `SetInterspersed(false)`), `handler_test.go`.
- **`cmd/agent-id`** registers the slice, injects the GitHub adapter as the provider for `github`, and maps the slice's exit-code error to the process exit code. It currently returns 1 for every error; it gains an error type that carries a code (`ExitCode() int`), used by `exec` only.
- **Provider interface** (ADR #35): `exec` needs two things from an adapter, declared as a narrow interface in `internal/activation`: the credential variables to drop (part of the CLI-integration responsibility) and `Ready(profile) error` (a profile-level check; for GitHub, "identity cached"). The method names are shaped with #15.
- **Preflight checks** are values of `type Check func(ctx context.Context, dir string, parent []string) error`, registered by the composition root in a fixed order. #22 registers O1.
- **Dependencies on other work:** #17 (profile store) is required; the provider interface and the GitHub adapter's drop list (#15) are needed to wire the `GH_TOKEN` acceptance criterion in production. Until #21–#23 land, a started command has the context markers but not the bot's Git identity, helper, or `gh` shim (see Risks).

## 📝 API Contracts

### Command

```text
agent-id exec --profile <name> [--] <command> [args…]
```

| Input | Rule | Failure |
|---|---|---|
| `--profile` | required; resolved through the profile store, which validates the name | 125, the store's message (`ErrProfileNotFound`, `ErrConfigNotFound`, `ErrUnsupportedProvider`, …) |
| `<command>` | required; looked up with the child's `PATH` unless it contains `/` | 125 when absent; 127 when not found; 126 when found but not executable |
| other flags | none; anything after the first non-flag argument belongs to the command | — |

### Exit status

| Status | When |
|---|---|
| the command's | always, once the command started (`execve` succeeded) |
| 125 | agent-id failed before starting the command: usage, config, profile, nesting, inherited Git configuration, preflight, binary path |
| 126 | the command was found but `execve` failed (permission denied, not an executable format) |
| 127 | the command was not found in the child's `PATH` |

`exec` writes nothing to stdout. On failure it writes exactly one line to stderr, `agent-id: exec: <message>`, printed by `cmd/agent-id`.

### Child environment (contract version 1)

Starting from the parent environment, in this order:

1. Remove every `AGENT_ID_*` variable, every variable in `Plan.Drop`, and, on re-entry, the previous contribution (step 3 of Build below).
2. Set the context variables:

   | Variable | Value | Consumers |
   |---|---|---|
   | `AGENT_ID_CONTEXT` | `1` (the contract version). Its presence means agent mode. | nested `exec`/`env`, `whoami`, `doctor` |
   | `AGENT_ID_ACTIVE_PROFILE` | the profile name (`^[a-z][a-z0-9-]{0,31}$`, shell-safe) | nested `exec`/`env`, `whoami`, `doctor` |
   | `AGENT_ID_MECHANISM` | `exec` or `env` | `whoami`, `doctor` |
   | `AGENT_ID_BIN` | absolute path of the agent-id binary | the credential helper entry (#22) and the `gh` shim (#23) |
   | `AGENT_ID_GIT_CONFIG_BASE` | the number of inherited `GIT_CONFIG_*` entries before agent-id's (an integer ≥ 0); set only when `Plan.GitConfig` is not empty | re-entry, `doctor` check D6 |

   No variable selects a profile: commands that need a profile still take `--profile` (profile spec). The credential helper and the shim receive their profile on their own command line (#22, #23), not from these variables. `AGENT_ID_ACTIVE_PROFILE` is a deliberately different name from the reserved, non-existent selection variable `AGENT_ID_PROFILE` (profile spec).
3. Set `Plan.Set`.
4. Append `Plan.GitConfig` after the inherited entries: `GIT_CONFIG_KEY_<base+i>`, `GIT_CONFIG_VALUE_<base+i>`, `GIT_CONFIG_COUNT=<base+len>`.
5. Prepend `Plan.PathPrepend` to `PATH`, after removing every existing `PATH` element equal to one of them.

Everything else is inherited unchanged, including `GIT_CONFIG_PARAMETERS` that passed the O2 check, `SSH_AUTH_SOCK`, and `GIT_SSH_COMMAND` (O3: documented, detected by `doctor`). The builder is the security decision (`ARCHITECTURE.md`, dependency rule 5) and has these properties, each a table or property test:

- **Idempotent:** `Build(Build(E, p), p) == Build(E, p)` for every `E` and `p` (the re-entry path).
- **Append-only for Git:** inherited entries `0…base-1` are byte-identical in the result.
- **Order-stable:** the output is sorted by variable name, so equal inputs give equal outputs (golden tests, #26 rendering).
- **Secret-free:** the builder rejects a plan whose `Set`, `GitConfig`, or `PathPrepend` value contains a registered token prefix, `-----BEGIN`, a JWT shape, a newline, or a NUL byte (`ErrPlanUnsafe`). This is a guard against a future contributor's bug, not input validation of user data.

### Reading the inherited context

| Inherited state | Decision |
|---|---|
| no `AGENT_ID_CONTEXT` | fresh start (other stray `AGENT_ID_*` variables are removed and ignored) |
| `AGENT_ID_CONTEXT=1`, `AGENT_ID_ACTIVE_PROFILE` equals `--profile` | re-entry: Build removes the previous contribution, then applies the plan |
| `AGENT_ID_CONTEXT=1`, a different valid profile | `ErrNestedProfile` |
| `AGENT_ID_CONTEXT` any other value | `ErrContextInvalid` (incompatible version) |
| `AGENT_ID_CONTEXT=1`, but `AGENT_ID_ACTIVE_PROFILE` missing or not a valid name, or `AGENT_ID_GIT_CONFIG_BASE` not an integer or greater than `GIT_CONFIG_COUNT` | `ErrContextInvalid` (damaged) |

Removing the previous contribution on re-entry means: drop `GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` for `n ≥ AGENT_ID_GIT_CONFIG_BASE` and set `GIT_CONFIG_COUNT` to the base (unset it when the base is 0), and remove the plan's `PathPrepend` directories from `PATH`. The context variables are a convenience for a cooperating process tree, not a security boundary: an agent can unset them and start another profile (brief §10; a documented bypass).

### Inherited Git process configuration

- **`GIT_CONFIG_COUNT`** (G2–G4): when present, it must be a decimal integer `0 ≤ n ≤ 1000`, and `GIT_CONFIG_KEY_i` and `GIT_CONFIG_VALUE_i` must exist for every `i < n` (an empty value is allowed, as in Git). Otherwise `ErrInheritedGitConfig` naming the variable that is missing or bad. A `GIT_CONFIG_KEY_i`/`VALUE_i` with `i ≥ n` is left alone (Git ignores it), and the append overwrites it.
- **`GIT_CONFIG_PARAMETERS`** (O2): parsed with Git's own quoting (`'key'='value'` pairs and the older `'key=value'` form, separated by spaces, `'\''` escapes). A key is forbidden when, compared case-insensitively, its section is `credential`, `url`, `http`, or `gpg`, or it is `commit.gpgsign`, `tag.gpgsign`, `tag.forcesignannotated`, or `user.signingkey`. A forbidden key or an unparseable value → `ErrInheritedGitConfig`. Anything else is kept unchanged and reported by `doctor` (D4).
- Messages name a variable or the **section and variable** of a key (`credential.<url>.helper`), never a value and never the URL subsection, because both can hold credentials (C21, U11 of the analysis).

### Errors

| Sentinel | Status | Message after `agent-id: exec: ` |
|---|---|---|
| `ErrNoCommand` | 125 | `no command given; usage: agent-id exec --profile <name> [--] <command> [args…]` |
| (cobra) missing `--profile` | 125 | `required flag "profile" not set` |
| profile store sentinels | 125 | the store's message, unchanged |
| `ErrProfileNotReady` (wraps the adapter's error) | 125 | `profile "reviewer": bot identity is not resolved; run agent-id setup github --profile reviewer` (the hint text is the adapter's) |
| `ErrNestedProfile` | 125 | `already in an agent context for profile "claude"; refusing to start profile "reviewer" inside it (nested exec with a different profile is not supported); start it from a terminal outside the agent context` |
| `ErrContextInvalid` | 125 | `the inherited agent context is <damaged: AGENT_ID_ACTIVE_PROFILE is missing \| from an incompatible agent-id (AGENT_ID_CONTEXT=2)>; start from a terminal outside the agent context` |
| `ErrInheritedGitConfig` | 125 | `inherited GIT_CONFIG_COUNT=2 has no GIT_CONFIG_KEY_1; every git command would fail; fix or unset the GIT_CONFIG_* variables` / `inherited GIT_CONFIG_PARAMETERS sets credential.<url>.helper, which would override agent-id's credentials; unset GIT_CONFIG_PARAMETERS or remove that setting (agent-id doctor shows its source)` |
| a preflight check's error (for example O1 from #22) | 125 | the check's message |
| `ErrBinaryPath` | 125 | `cannot determine the path of the agent-id binary: <reason>` |
| `ErrPlanUnsafe` | 125 | `internal error: activation plan contains a forbidden value in <variable>` (never the value) |
| `ErrCommandNotFound` | 127 | `"<command>": command not found` |
| `ErrCommandNotExecutable` | 126 | `"<command>": <permission denied \| exec format error>` |

Echoed values are cut to 64 characters with control characters replaced by `?`, as in the profile spec.

### The binary path (`AGENT_ID_BIN`)

`os.Executable()` gives the running binary. When `os.Args[0]` resolves (absolute, relative, or through `PATH`) to a file that `os.SameFile` reports as the same binary, that path is used instead, made absolute and cleaned, so a Homebrew install records `/opt/homebrew/bin/agent-id` (stable across upgrades) rather than the versioned Cellar path. A path with a control character, a newline, or a non-UTF-8 byte is refused (`ErrBinaryPath`), because #22 embeds it in a Git configuration value.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| `agent-id exec --profile claude` with no command | 125, usage line. |
| `agent-id exec --profile claude -- -v` | `-v` is the command; 127 unless a program named `-v` exists. |
| Command is a shell builtin or alias (`cd`, `ll`) | 127: `exec` runs programs, not shell syntax. Use `sh -c '…'`. |
| Command path contains `/` (`./run.sh`) | Not looked up in `PATH`; 126 or 127 from the file system. |
| Profile name with shell metacharacters | Rejected by the profile store's name rule before anything else. |
| Config file missing or invalid | 125 with the store's message (`run agent-id setup github --profile <name>`). |
| Profile of an unsupported provider | 125, `ErrUnsupportedProvider`. |
| Identity not yet resolved | 125 with the adapter's `setup` hint; no network. |
| Inside an `env`-activated Claude Code session (#26), same profile | Re-entry, identical environment. |
| Inside a session of another profile | 125, both names. |
| A startup file of the agent's shell unset `GIT_CONFIG_COUNT` but not the markers | 125 `ErrContextInvalid` (base greater than count) on the next nested exec; `doctor` D6 explains. |
| Inherited `GIT_CONFIG_COUNT` with a value like `2 ` or `0x2` | 125, malformed. |
| Inherited `GIT_CONFIG_PARAMETERS` with `core.editor` only | Kept. |
| Git older than 2.43 | Not checked by `exec` (no Git call in #20); `doctor` reports it (O4). |
| Ctrl-C in an interactive agent | Delivered once, to the agent only: agent-id is no longer running. |
| The agent-id binary is removed or upgraded during a long session | Later helper or shim invocations fail closed (Git and `gh` report a failed helper); `AGENT_ID_BIN` prefers the stable symlink to make this rare. |
| `exec` on Windows | Not supported (brief §8); the slice builds only on darwin and linux. |

## 📝 Risks & Impact Review

- **Contract.** The variable names, the contract version `1`, the exit codes 125–127, and the append rule are consumed by `env` (#26), `whoami`/`doctor` (#24), the Claude Code hook, and user scripts. A later change needs `AGENT_ID_CONTEXT=2` and a re-entry rule for version 1. This is the "environment that `exec` and `env` emit" of `AGENTS.md` (Ask first): the owner's sign-off on this spec is that confirmation.
- **Intermediate state on `main`.** Between #20 and #21–#23, `exec` starts a command that is marked as agent mode, has no inherited forge tokens, but has no bot identity, no agent-id credential helper, and no `gh` shim: Git would still use the developer's helpers, and `gh` the developer's `hosts.yml`. No release may contain `exec` before #21, #22, and #23 merge; the end-to-end story #27 is the release gate. This is a delivery-order fact, not a design gap.
- **Not a security boundary** (brief §10). The agent can unset the markers, call another binary, or start a shell that rewrites the environment; `exec` controls only what it starts. The spec claims nothing more.
- **`execve` removes the chance to clean up.** Any future feature that needs a supervising parent (a local token broker, audit events) belongs to the Phase 3 hardened runtime and would be a new mechanism, not a change to this one.
- **Refusing on inherited Git configuration may surprise** a developer whose environment carries `GIT_CONFIG_PARAMETERS` from another tool. The message names the key and the fix; refusing is the decided fail-closed behavior (O2).
- **Product direction vs. rule.** Nested-profile refusal and O1–O4 are recorded decisions. `execve`, the exit codes, the variable names, and the re-entry rule are technical decisions of this spec. OQ1 and OQ2 below are product choices recorded for the owner with a recommendation; the spec applies the recommendation.

## 📝 Decisions in play

- Brief §3 principle 1, §7.2, §8 *Process-scoped execution*, §10 (not a boundary), §14 *Profile selection* (explicit only).
- `ARCHITECTURE.md`: Context runtime, dependency rule 5 (pure security decisions), Cross-cutting (exec returns the child's status), Key decisions (explicit `exec` canonical; `AGENT_ID_` prefix; provider abstraction).
- Profile spec: `Resolve`, no selection variable, the consumer rule for an unresolved identity (decided here).
- Project manager, 2026-11-13: nested different profile → error; O1–O4.

## Open Questions (owner) — recommendations applied

These change product behavior, not the design's structure. The spec applies the recommendation; each is reversible before the first release and does not block the spec. The implementation of the affected step waits for an answer only if the owner disagrees.

| # | Question | Recommendation (applied) | Why | Confirm? |
|---|---|---|---|---|
| OQ1 | Should `exec` obtain (or check) an installation token before it starts the command, so that an agent never starts without working credentials? | **No.** Start without network; the credential helper (#22) and the `gh` shim (#23) fail closed at use; `agent-id doctor --profile` is the explicit pre-check. | Keeps `exec` deterministic, fast, and usable offline; fail closed is preserved at the point of use; adding a check later is backward compatible. | owner, non-blocking |
| OQ2 | Should `exec` resolve a missing bot identity over the network on start, instead of refusing? | **No, refuse** with the adapter's `setup` hint. | The profile spec proposed this; no network on start (OQ1); a silent resolution would hide a half-configured profile. | owner, non-blocking |

## Resolved assumptions (autonomous defaults)

Technical decisions taken by this spec, recorded for override:

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | How does `exec` run the command? | `execve`; no supervising parent. | Exit status and signals are the command's by construction; no double `SIGINT`. | reversible |
| A2 | Exit codes of agent-id's own failures? | 125 / 126 / 127 (`env(1)` convention). | Unambiguous next to the command's own codes. | reversible before the first release |
| A3 | Context variable names? | `AGENT_ID_CONTEXT=1`, `AGENT_ID_ACTIVE_PROFILE`, `AGENT_ID_MECHANISM`, `AGENT_ID_BIN`, `AGENT_ID_GIT_CONFIG_BASE`. | `AGENT_ID_` prefix (#6); distinct from the reserved selection name `AGENT_ID_PROFILE`. | reversible before the first release |
| A4 | Same-profile nesting? | Deterministic rebuild after removing the previous contribution. | Idempotent; repairs a damaged context; no duplicates (#26). | reversible |
| A5 | Who appends `GIT_CONFIG_*` entries? | `internal/activation`, from plan data of #21/#22. | One composition rule; enables re-entry and `doctor` D6. | reversible |
| A6 | Where do O2 and G2–G4 live? | `internal/activation` (pure, inherited environment only); O1 is a preflight check of #22. | O2 and G2–G4 need only the environment; O1 needs Git and the provider's host mapping. | reversible |
| A7 | Flag parsing | `--profile` required, interspersed flags off, `--` optional. | Matches `env`, `sudo`, `aws-vault exec`. | reversible |
| A8 | Output on success | None. | The command owns stdout and stderr. | reversible |

## 📋 Phasing

- **Phase 1 — pure core (`internal/activation`).** Context reading, nesting decision, inherited Git checks, builder, binary path, `LookPath`. Independently shippable and fully unit-tested; no command yet. Needs nothing beyond the repository.
- **Phase 2 — the `exec` slice.** Command, handler, exit-code mapping, process tests. Needs #17 (profile store); the production `Drop` list needs the provider interface and the GitHub adapter's CLI data (#15). A fake provider covers the tests until then.
- **Phase 3 — record.** `ARCHITECTURE.md` and the capability archive.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first. `risk-high` evidence per `SDLC.md`: hostile-input tables in each pure step, a process test proving the inherited personal token does not reach the command, a scan proving no token in any output or error.

### Phase 1 — `internal/activation`

1. **Contract.** Package `internal/activation` with its `AGENTS.md` (invariants: pure functions; the only owner of the child environment; never a token in the environment; no forge names), the types and sentinels above, and a row in the root `AGENTS.md` task routing. Test: the package builds; sentinels are distinct.
2. **Read the context and decide.** `ReadContext`, `Decide`. Table: no context; stray `AGENT_ID_*` without `AGENT_ID_CONTEXT`; same profile; different profile; `AGENT_ID_CONTEXT=2`, empty, `1 `; missing or invalid `AGENT_ID_ACTIVE_PROFILE` (uppercase, metacharacters, 33 characters); base not an integer, negative, or greater than the count.
3. **Inherited Git configuration.** `CheckInheritedGit`, `ParseGitConfigParameters`. Tables: G2–G4 and the analysis's malformed counts (`x`, `-1`, `1001`, `2 `, `0x2`); every forbidden section in mixed case; `credential.https://user:tok@github.com.helper` (the message must not contain `user`, `tok`, or the URL); old and new quoting; `'\''` escapes; an unterminated quote; harmless keys kept.
4. **Build.** `Build` with the five steps and four properties. Tables with hostile parents: inherited `GH_TOKEN`/`GITHUB_TOKEN` (fake), an inherited count of 2 with entries, stray `GIT_CONFIG_KEY_5`, `PATH` already containing a prepend directory twice, an empty `PATH`. Property test over generated parents and plans: idempotence and append-only. Secret guard: `testenv.FakeToken`, `-----BEGIN`, a JWT shape, a newline in `Set`, `GitConfig`, `PathPrepend` → `ErrPlanUnsafe` without the value in the error.
5. **Binary path and lookup.** `ResolveBinary` with a symlinked binary in a temporary directory, a relative `os.Args[0]`, a path with a newline; `LookPath(env)` using the child's `PATH`, not the parent's.

### Phase 2 — `internal/activation/exec`

6. **Slice.** `contract.go`, `handler.go` (resolve → ready → read context → decide → inherited Git → preflight → build → look up → `Execer.Exec`), `command.go` (`SetInterspersed(false)`, required `--profile`). Handler tests with a fake profile resolver, a fake provider (drop list, readiness), a fake preflight check, and a recording `Execer`: every row of the Errors table, the order of checks (nesting before inherited Git before preflight), and that the recorded environment equals `Build`'s output.
7. **Composition root.** Register the slice; inject the provider registry; add the exit-code error type and map it in `run`; the default for other commands stays 1. Test: `run` returns 125/126/127 for the slice's errors and 1 for others.
8. **Process tests.** Build `agent-id` into a temporary directory under the isolated `TMPDIR`; write a fixture config with a fake App and a cached identity. Cases: `exit 7` → 7; SIGTERM with a trap → 42; SIGKILL → killed by signal; parent environment and isolated `HOME` tree unchanged (hash before and after); fake `GH_TOKEN`/`GITHUB_TOKEN` absent in the command; nested same profile prints an identical environment; nested different profile → 125; `does-not-exist` → 127; profile without identity → 125 with the fake GitHub recording zero requests. Scan stdout and stderr of every case for `ghs_`, `-----BEGIN`, and a JWT shape. These tests skip on platforms other than darwin and linux.

### Phase 3 — record

9. **Architecture and archive.** Add a Key decisions row to `ARCHITECTURE.md` (the child environment contract v1, `execve`, exit codes 125–127; source this spec and #20) and the `exec` exit codes to Cross-cutting concerns; archive the Delta into `.ai/specs/capabilities/exec.md`.
