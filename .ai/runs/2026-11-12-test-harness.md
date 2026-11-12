# Execution plan — extend the test harness (E01-T05, #10)

Source issue: https://github.com/cultofthecode/agent-id/issues/10
Route: `om-auto-fix-issue` feature route, implemented through `om-auto-create-pr`. The issue carries the scope and acceptance criteria and meets the Definition of Ready, so it is the source document; no separate spec PR (the owner asked for implementation and one PR).
Claim-Run: 055cfc2d-c12e-43a0-a1e8-664848316428

## Goal

Phase 1 can test token minting, identity resolution, Git pushes, and `gh` calls end to end on one machine, with no network, no real token, and no write outside `$TMPDIR/agent-id/`.

## Scope

On top of `internal/testenv` (`Isolate`, `NewFakeGitHub`, `Listen`, `FakeToken`):

- `internal/github/githubtest` (new): a fake GitHub App on `testenv.FakeGitHub`. RS256 JWT check against a key generated per test; `POST /app/installations/{id}/access_tokens` with controllable lifetime and failure status (401, 403, 404, 5xx); `GET /app`; `GET /users/{slug}[bot]`; `GET /installation/repositories` with installation-token check.
- `internal/testenv`:
  - a smart-HTTP Git server (`git http-backend` behind `net/http/cgi`) over HTTPS on 127.0.0.1:18000–18999 that challenges for Basic auth and records the username and password of every request;
  - helpers for a local bare repository with one commit and a clone under the isolated HOME, plus a `git` runner;
  - `BuildBinary`, which builds a package into the test's temporary directory with `GOPROXY=off` (no `go install`, no network);
  - a network guard: `Isolate` points the proxy variables at a guard proxy that fails the test on any request that leaves 127.0.0.1 (covers `git` and `gh` child processes), and `NewHTTPClient` gives in-process code a client that fails the test for any host other than 127.0.0.1;
  - `CheckFakeToken`, which fails the test when a real-looking GitHub token (`ghs_`, `ghp_`, … without `ghs_aidtest_`) reaches a fake; the message never contains the token;
  - `Main`, for `TestMain`: hashes `~/.gitconfig`, `~/.config/git`, `~/.config/gh`, and `~/.config/ghapp` before and after the run and fails the run when they changed.
- `.golangci.yml`: depguard lets `githubtest` import `testenv` and lets only tests import `githubtest`. `ARCHITECTURE.md` rule 6, `AGENTS.md`, `internal/testenv/AGENTS.md`, and a new `internal/github/githubtest/AGENTS.md` follow.

## Non-goals

- Real GitHub, real Apps, Docker, or OrbStack.
- The production GitHub provider (`internal/github`, #15), the token cache, the credential helper, or `gh` injection.
- No new Go module dependency: JWT verification uses the standard library.

## Implementation Plan

### Phase 1: testenv network and token guards

- 1.1 Guard proxy started by `Isolate`, `NewHTTPClient`, `CheckFakeToken`, more inherited variables; tests including the negative cases.
- 1.2 `Main` real-configuration guard with a test of the comparison.

### Phase 2: Git process helpers

- 2.1 Bare repository, clone, and `git` runner helpers.
- 2.2 HTTPS smart-HTTP Git server that records credentials; push test with a fake token, negative test with a real-looking token, guard test for a push to github.com.
- 2.3 `BuildBinary` and a test in `cmd/aidentity` that builds and runs the binary; `gh` process test against the network guard.

### Phase 3: fake GitHub App

- 3.1 `internal/github/githubtest` with the four endpoints, failure modes, and table tests (valid, expired, wrong-key, and wrong-issuer JWT; each failure status; expired installation token).

### Phase 4: rules and docs

- 4.1 depguard, `ARCHITECTURE.md`, `AGENTS.md`, package `AGENTS.md` files.

## Risks

- `git http-backend` location differs between macOS and Linux: resolve it with `git --exec-path`.
- `BuildBinary` needs the module cache of the developer: it reads `GOMODCACHE` and `GOCACHE` from `go env` with the environment captured before `Isolate`, and builds with `GOPROXY=off`, so a missing module fails instead of downloading.
- `Main` reads (hashes) the developer's real Git and gh configuration; it never prints contents.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: testenv network and token guards

- [x] 1.1 Guard proxy, NewHTTPClient, CheckFakeToken — d684364
- [x] 1.2 Main real-configuration guard — e3dd0ee

### Phase 2: Git process helpers

- [x] 2.1 Bare repository, clone, and git runner helpers — f0cd079
- [x] 2.2 HTTPS Git server recording credentials — f0cd079
- [x] 2.3 BuildBinary and gh process test — afa1c6f (gh process test in d684364)

### Phase 3: fake GitHub App

- [x] 3.1 githubtest fake App endpoints and tests — 42b5607

### Phase 4: rules and docs

- [x] 4.1 depguard, ARCHITECTURE.md, AGENTS.md files — 7ddcebe
