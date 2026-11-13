# Git credential helper and SSH→HTTPS rewrites (E02-S08)

Issue: #22. Builds on the exec spec (#20, #68), the token manager (#18), and the provider interface (ADR #35, #15). It also touches `internal/gitint` and `Activate`, which the open PR #21 touches; the two merge in either order (new files here, one added call in `Activate`), and the plan keeps #21's identity group before this transport group. Constrained by the O1–O4 decisions (#22 comment of 2026-11-13). Always `risk-high` and `security`: this story puts a token where Git can read it.

## Goal

In an agent context, `git fetch`, `git clone`, and `git push` over HTTPS to the profile's host authenticate as the App through `agent-id git-credential`. Common SSH remotes of that host become HTTPS remotes. Every other host, protocol, and path gets nothing. When the token cannot be obtained, Git gets no credential and fails.

## Non-goals

- Hosts other than the profile's `Host()` (GHES, `gist.github.com`, `ssh.github.com`). `exec` still refuses what stays on SSH (O1).
- Repository-specific policy beyond what the installation can access (Phase 2/3).
- The `gh` token (#23). The `GIT_SSH_COMMAND` shim (O3, Phase 3).
- A security boundary. A process in the context can run `agent-id git-credential` itself (brief §10).
- Git LFS (`path` ends in `/info/lfs`) and repository paths with more than two segments. They get `quit=1`. A later story can extend the path rule through the provider.
- Dropping a cached token after Git rejects it. `erase` persists and drops nothing, so a revoked token stays in the cache until it expires (≤ 1 h). `agent-id doctor` and a new token after expiry recover it.

## Design

### Where the code lives

| Piece | Package | Why |
|---|---|---|
| Pure decision, request parser, renderer, config entries | `internal/gitint` (root) | Architecture rule 5: a security decision is a pure function in a module. `activation` reads the entries through the root package (rule 3). |
| The `git-credential` operation | `internal/gitint/credentialhelper` (slice: `contract.go`, `handler.go`, `command.go`, `handler_test.go`) | Slice anatomy. |
| Wiring: profile store, adapter, token manager | `cmd/agent-id` | Only the composition root names the adapter. |

`gitint` and `credentialhelper` import `internal/provider` and the standard library. They name no forge. `credentialhelper` does not import `internal/token`: the root injects a function that mints through `token.Manager`.

### Config entries (`gitint.TransportEntries(binary, profile, host)`)

`activation.PlanFor` already emits `credential.helper=` (reset of every inherited helper, any host), `http.https://<host>/.extraHeader=`, and `core.askPass=`. #22 appends, in this order, for host `H`:

1. `credential.https://H.helper` = `!'<binary>' git-credential --profile <profile>` (Git appends the operation).
2. `credential.https://H.useHttpPath` = `true` (Git then sends `path`).
3. `url.https://H/.insteadOf` = `ssh://git@H/`.
4. `url.https://H/.insteadOf` = `git@H:` — only when `H` has no `:` (the scp form has no port).

The binary path is single-quoted for `sh`, with `'` written as `'\''`; a path with a control character, or that is not absolute, is an error. `activation` maps it to `ErrPlanUnsafe` (exit 125). `AGENT_ID_BIN` is already absolute; `gitint` checks again as defense in depth. The host must match `^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`. The profile name must match the profile pattern. The helper is scoped to `https://H`, so Git does not start it for any other URL. The helper also decides for itself (below), because a config scope is not a security check.

### Decision (`gitint.Decide(req, host)`)

`ParseRequest` reads Git's credential protocol: `key=value` lines, split on LF only, up to a blank line or EOF, at most 64 KiB and 64 lines. Keys match exactly (`HOST=` is an unknown key). It refuses: a NUL or CR byte anywhere (a CR in a URL has passed through Git to helpers before; the protocol never needs one), a line without `=`, an empty key, and a repeated `protocol`, `host`, `path`, `username`, or `password`. `capability[]`, `wwwauth[]`, and unknown keys are ignored. `url` is never read.

`Decide` returns one of three results and never reads the environment, a file, or the network:

| Result | When |
|---|---|
| `Skip` | `host` is another host: it differs from `H` even after its user-info and port are removed, or it is empty or not ASCII. The helper prints nothing and exits 0. |
| `Refuse(reason)` | `host` is `H` (compared as ASCII, case-insensitive), or `H` with a port or user-info (Git sends these forms for URLs that match the helper's scope, such as `https://github.com:443/…`), and, for `H` itself: `protocol` is not exactly `https`; `path` is empty; `path` is not `owner/name` or `owner/name.git` (one slash, each part `[A-Za-z0-9._-]{1,100}`, no part `.` or `..`, optional final `/`); `password` is present (a URL carried a secret). The reason is a fixed string; it never repeats input. |
| `Allow` | `host` is `H`, `protocol` is `https`, `path` is valid, and no `password`. A `username` in the request is ignored; the answer carries the adapter's username (`x-access-token`). |

Look-alike hosts (`github.com.evil.example`, `github.com.`, `github.com@evil.example`, non-ASCII letters) are `Skip`, because they are not `H`. For `H` = `github.com`, `github.com:443` and `user@github.com` are `Refuse` with `quit=1`, so Git never falls through to a prompt for them.

### Operations

`agent-id git-credential --profile <name> get|store|erase` (hidden command).

- `store` and `erase`: read and discard at most 64 KiB, print nothing, write nothing, exit 0. Nothing is ever persisted by the helper.
- `get`: parse; resolve the profile's host (no key read, no network); `Decide`. `Skip` → no output, exit 0. `Refuse` or a parse error → stdout `quit=1`, a one-line message on stderr, exit 1. `Allow` → obtain the credential (below), validate it, print `username=…` and `password=…`, exit 0.
- Any other operation: message on stderr, exit 1, no output.

`quit=1` makes Git stop at once (it dies with "credential helper … told us to quit", exit 128) and skip the next helpers and the prompt. Git ignores the helper's exit status; status 1 is for scripts. The answer of a successful `get` has exactly two lines, `username=` and `password=`; the helper never echoes `host=`, `protocol=`, or `path=`.

### Getting the credential

The root builds the provider for the profile (it loads the key through the credential source), and `token.Manager.Token` returns a token that is valid for longer than the margin, from the cache or newly minted. `Provider.GitCredential(token)` gives the answer. The key is read only after `Allow`.

`Render` is pure and fails closed: the credential must have protocol `https`, host equal to `H`, a non-empty username and password, and no CR, LF, or NUL in either. A failure is an internal error with no value in the message.

### Failure (fail closed)

When the profile cannot be resolved, the key cannot be loaded, the mint fails, the cache is unusable, or `Render` refuses: stdout is `quit=1` only, exit 1, and stderr has one line:
`agent-id: git-credential: cannot get a token for profile "<name>" (<class>); run agent-id doctor --profile <name>`.
`<class>` is a fixed word from the error class: `unauthorized`, `forbidden`, `not found`, `rate limited`, `unavailable`, `cache not private`, `cache write failed`, `lock timeout`, `unusable token`, `private key`, `profile`, `canceled`, or `error`. The message never holds `err.Error()`. No personal helper (osxkeychain, `gh`) runs, because `credential.helper=` reset every inherited helper before ours, and `core.askPass`, `GIT_ASKPASS`, and `GIT_TERMINAL_PROMPT=0` close the prompt.

### Secrets

The token appears only on stdout of a successful `get`. No log, no error, no message names it. Tests use fake tokens (`ghs_aidtest_…`) and assert that stderr and every error text are free of them.

### Compatibility

Git 2.43 minimum (O4). `useHttpPath` and the `quit` attribute are older than that. The entries add four `GIT_CONFIG_*` slots to the plan; `AGENT_ID_GIT_CONFIG_BASE/_END` already carry the count. #21 adds its identity group to the same plan; the order is baseline, identity group, transport group.

## Requirements and scenarios

| ID | Requirement |
|---|---|
| R1 | An HTTPS push, fetch, or clone to `H` sends the installation token as `x-access-token`. |
| R2 | An SSH-style remote (`ssh://git@H/o/r.git`, and `git@H:o/r.git` when `H` has no port) resolves to HTTPS and authenticates as in R1. |
| R3 | A request for any other host, `http`, a malformed path, or a request that holds a password gets no credential. |
| R4 | When the token cannot be obtained, Git gets no credential, no personal helper or prompt runs, and the message names the profile and class, never a secret. |
| R5 | `store` and `erase` persist nothing. |
| R6 | The token never appears in an error, a message, a log, or `--help` output. |

- GIVEN a fake Git server on `H` that accepts only fake tokens, WHEN the agent pushes over HTTPS, THEN the server records `x-access-token` and the fake installation token (R1).
- GIVEN the same server, WHEN the remote is `ssh://git@H/o/r.git`, THEN the push reaches it as HTTPS with the same credential (R2).
- GIVEN requests `host=github.com.evil.example`, `http` on `H`, `host=H:444`, `path=../x`, `path=o/r/extra`, and `password=x`, WHEN the helper runs, THEN none gets a credential; the `H` cases print `quit=1` (R3).
- GIVEN a mint that fails with 401, 403, 404, 429, or 5xx, an unusable cache, or a key that cannot load, WHEN the agent pushes, THEN the server records no credential, the push fails, and a decoy personal helper and askpass record zero uses (R4; the #25 scenarios for Git).
- GIVEN `store` or `erase` with a full request, THEN stdout is empty, exit 0, and no file appears in the isolated `HOME` or cache directory (R5).
- GIVEN every test above, THEN no stdout or stderr outside a successful `get` holds a `ghs_` token (R6).

## Tests

Pure tables with hostile input for `ParseRequest`, `Decide`, `Render`, `TransportEntries`, and the shell quoting. Handler tests use `providertest.Fake` and a fake token function; they cover each failure class. Process tests use real `git` against `testenv.NewGitServer`: the test binary re-executes itself as the helper (`TestMain` child mode), so a real Git process runs a real helper process with the fake provider. A built `agent-id` binary covers the profile-resolution failures and, in the #25 suite, `git credential fill` inside `agent-id exec` for each key state. The production GitHub adapter always talks to `api.github.com`, so no test mints a token through the built binary.

## Open questions

None for the owner. Defaults recorded here, reversible before the first release:

| ID | Question | Default |
|---|---|---|
| Q1 | Require a path? | Yes. `useHttpPath` makes Git send it. A request without a path (for example `git credential fill` with `host` only) gets `Refuse`. |
| Q2 | Accept only `owner/name`? | Yes. GitHub repository paths have this shape. A different forge may need another rule through the provider later. |
| Q3 | `quit=1` on a refusal at `H`? | Yes: fail closed, skip later helpers and the prompt. |
| Q4 | Rewrite `ssh.github.com:443` and `git+ssh://`? | No. `exec` refuses them (O1). |
| Q5 | The ADR (#35) gives the SSH→HTTPS mapping and the repository-path rule to the provider. Do they move there now? | No. The MVP derives the SSH forms from `Host()` (they are the same on every forge that has scp-style URLs) and keeps the `owner/name` rule in `gitint`. A second adapter (GitLab subgroups) moves both onto `provider.Activator`. Recorded in Key decisions of `ARCHITECTURE.md`. |
