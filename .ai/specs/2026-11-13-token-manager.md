# Token manager: cache installation tokens safely and refresh them before expiry

Extends: `.ai/specs/2026-11-13-github-provider-adapter.md` (adds one method to the provider interface that #15 settled; everything else there stays valid)

Issue: #18 (E02-S04). Builds on ADR #35 (`ARCHITECTURE.md`, Provider abstraction), the provider interface in `internal/provider`, and the GitHub adapter (#15, PR #57).

## 📝 TLDR

Nothing in agent-id caches a token yet: every consumer that asks the provider for a token mints a new one, and every mint is a signed App request against GitHub's rate limits. This spec proposes a new module, `internal/token`, that gives the credential helper (#22), the `gh` injection (#23), and diagnostics (#24) a currently valid token for a profile. The token comes from a per-key cache file (`0600`, in a `0700` directory under `$XDG_CACHE_HOME/agent-id/tokens/`), written atomically, refreshed under a cross-process `flock` five minutes before expiry, and never printed. When a token cannot be obtained, it fails closed. To key the cache without knowing GitHub, the provider interface gains one method, `TokenSubject()`.

## 📝 Problem Statement

- Brief §8 *Token lifecycle* (P0): "Cache installation tokens by appropriate profile/installation/host and scope; refresh before expiry; never emit secrets in diagnostics." Brief §10: installation tokens are bearer credentials valid about one hour; "test cache locking and atomic writes."
- A Git push runs the credential helper once per request, and an agent runs `gh` many times a minute. Without a cache, each call signs a JWT and mints a token: a slow call each time, and a quick way to hit GitHub's rate limit. Without a shared cache, concurrent processes (a `git fetch` and several `gh` calls at once) each mint their own token.
- The cache holds bearer credentials on disk. A loose implementation leaks them through file modes, a symlink, a partial write read by another process, an error message, or a cache entry of one installation that is served for another.
- `internal/token` is planned in `ARCHITECTURE.md` (Components, Data) and is on the critical path of Phase 1: #22, #23, and #24 depend on it.

## 📝 Proposed Solution

A new module `internal/token`, standard library only (no new module dependency). Core code; it knows no forge and imports only `internal/provider`.

1. **Cache key** (`Key`, built by the pure function `KeyFor(src)`): provider name, host, token subject, and scope. The subject comes from the new `provider.Provider.TokenSubject()` (A1): an opaque, non-secret string that names whose token `Token` mints. The GitHub adapter returns `app-<appID>-installation-<installationID>`. The scope is `full` in the MVP (the token carries the installation's whole grant, adapter spec A2); scoped tokens are Phase 2. Every field must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`; anything else fails with `ErrInvalidKey` before the filesystem is touched.
2. **Location** (`Dir(getenv)`, pure): `$XDG_CACHE_HOME/agent-id/tokens` when `XDG_CACHE_HOME` is an absolute path, otherwise `$HOME/.cache/agent-id/tokens` (the XDG rule: a relative value is ignored), on macOS and Linux alike, matching the config directory and `ARCHITECTURE.md` (Data). An empty or relative `HOME` with no usable `XDG_CACHE_HOME` fails with `ErrNoCacheDir`.
3. **Files.** One entry per key: `<name>.json` and its lock file `<name>.lock`, where `<name>` is the lowercase hex SHA-256 of the canonical key encoding (`token-cache-v1`, then the four fields, each followed by a NUL byte). Hashing keeps hostile key bytes out of paths; the entry also stores the key in clear, and the reader compares it, so a copied or colliding file is never served for another key. Entry content (JSON):

   ```json
   {"version":1,"provider":"github","host":"github.com","subject":"app-1-installation-2","scope":"full","token":"ghs_…","expires_at":"2026-11-13T08:00:00Z"}
   ```

4. **Directory checks** (`checkDir`, pure over `fs.FileInfo` and the user id). `Manager` creates the directory chain with mode `0700` (`os.MkdirAll` does not change existing directories). Then it `Lstat`s the `tokens` directory: it must be a real directory (not a symlink), owned by the current user, with no group or other bits. Otherwise every call fails with `ErrInsecureCache`, whose message names the path and the fix (`chmod 700 <dir>` or remove it) (A6). agent-id never `chmod`s a directory it did not just create.
5. **Reading an entry** (`readEntry`, then the pure `decodeEntry(data, key)`): open with `O_RDONLY|O_NOFOLLOW`; `fstat` the descriptor: a regular file, owned by the current user, mode exactly `0600` (no other bits); at most 64 KiB; strict JSON (unknown members, duplicates, and wrong types rejected); `version` 1; the stored key equals the requested key; a valid token value (item 8) and a non-zero `expires_at`. Any failure, including a missing file, is a **cache miss**, never an error and never a partial read: the entry is replaced by a fresh mint (corrupt cache → refetch). Decoding errors are replaced by fixed messages and never wrap the parser error, which can repeat the input.
6. **Freshness** (`fresh(entry, now, margin)`, pure): an entry is served only while `now + margin < expires_at`. `Manager.Margin` defaults to 5 minutes (`DefaultMargin`) and is a field, set in code, not a flag or config key.
7. **Token(ctx, src)** — the one public operation:
   1. Build the key, resolve and check the directory.
   2. Read the entry without a lock. Fresh → return it. A reader never sees a partial file, because writers replace the file with `rename`.
   3. Otherwise take an exclusive `flock` on the lock file (`syscall.Flock`, macOS and Linux, as in the profile-config spec). The wait polls with `LOCK_NB` every 20 ms and ends with the context, or after `Manager.LockTimeout` (default 30 s) with `ErrLockTimeout`. The kernel releases the lock when its process dies, so a crashed process never leaves a stale lock.
   4. Re-read the entry under the lock. Fresh → another process has just refreshed it; return it (this is what turns 20 concurrent misses into one mint).
   5. Mint with `src.Token(ctx)`. A failure returns an error that wraps the provider error (`errors.Is(err, provider.ErrUnauthorized)` and the other classes still hold) and the zero `Token`. No retry, no backoff, and no stale fallback: an entry inside the margin is not served even if it has not expired yet (A5).
   6. Check the minted token (item 8). It must also be fresh at `now`: a token whose lifetime is not longer than the margin (an expired `expires_at`, or a clock that is far off) fails with `ErrUnusableToken`, because serving it would make every later call mint again.
   7. Write the entry atomically: `os.CreateTemp` in the same directory, `Chmod 0600` before any byte is written, write, `fsync`, close, `rename` over `<name>.json`, `fsync` the directory. On any failure, the temporary file is removed and `Token` fails with `ErrCacheWrite`; the minted token is discarded, not returned (A7).
   8. Release the lock, return the token.
8. **Token value check** (`checkValue`, pure): 1–4096 visible ASCII characters (`0x21`–`0x7e`), the same rule as the GitHub adapter, so neither the provider nor a tampered file can inject a newline into the credential helper protocol or the child environment. The check is provider-neutral; it does not test a prefix.
9. **No secret leaves the package.** `Token` returns `provider.Token`, which already redacts its value under every `fmt` verb. `Manager` has no logger. Errors name the operation, the directory or file path (paths contain only the hash), and the fix; never the token, the file content, or a parser error. `Key` holds no secret.
10. **Clock.** `Manager.Now` (default `time.Now`) is the only clock; tests inject it.

```go
// Package token caches provider tokens per key on disk and refreshes them
// before they expire.
package token

type Source interface { // satisfied by every provider.Provider
	Name() string
	Host() string
	TokenSubject() string
	Token(ctx context.Context) (provider.Token, error)
}

type Key struct{ Provider, Host, Subject, Scope string }

const DefaultMargin = 5 * time.Minute
const DefaultLockTimeout = 30 * time.Second

var (
	ErrInvalidKey    = errors.New("token: invalid cache key")
	ErrNoCacheDir    = errors.New("token: no cache directory")
	ErrInsecureCache = errors.New("token: the token cache directory is not private")
	ErrLockTimeout   = errors.New("token: timed out waiting for the token cache lock")
	ErrUnusableToken = errors.New("token: the provider returned an unusable token")
	ErrCacheWrite    = errors.New("token: cannot write the token cache")
)

type Manager struct {
	Dir         string           // from Dir(os.Getenv)
	Margin      time.Duration    // zero means DefaultMargin
	LockTimeout time.Duration    // zero means DefaultLockTimeout
	Now         func() time.Time // nil means time.Now
}

func Dir(getenv func(string) string) (string, error)
func KeyFor(src Source) (Key, error)
func (m *Manager) Token(ctx context.Context, src Source) (provider.Token, error)
```

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| Key the cache by profile name | The profile config already restricts names to `[a-z0-9-]` and its spec says "the name ends up in cache file names" | Editing a profile's `installation_id` or `app_id` would serve the old installation's token under the same name. The issue's negative criterion forbids it. | Rejected; the profile name is not part of the key |
| Callers pass the key (built from the profile's provider block) | No interface change | Core would have to read GitHub fields (`app_id`, `installation_id`), against dependency rule 7, or hash the opaque block, which also changes when the identity cache is refreshed. | Rejected |
| `TokenSubject()` on `provider.Provider` | Each adapter names its own grant (GitHub: App + installation; GitLab later: project or group token); core stays forge-neutral | One more interface method, the contract, and both fakes change | **Chosen** (A1) |
| One cache file with all entries | One lock | Every refresh of one profile rewrites and locks the others; one corrupt file loses every entry | Rejected |
| OS keyring for tokens | Not on disk in clear | Keyring support is OPEN in `ARCHITECTURE.md`; a new dependency; brief §10 and the issue name a `0600` file | Rejected for the MVP |
| Serve a still-valid token inside the margin when the refresh fails | Fewer failures during a short GitHub outage | Hides a revoked App or installation for up to five minutes; the request was to fail closed after an error | Rejected (A5); reversible |
| Lock library (`gofrs/flock`) | Portable | A new dependency ("Ask first"); `syscall.Flock` covers macOS and Linux, the only targets (`.goreleaser.yaml`) | Rejected |

Comparable tools: the AWS CLI caches SSO and assumed-role credentials as JSON files under `~/.aws/cli/cache` and `~/.aws/sso/cache`, named by a SHA-1 of the request, refreshed before expiry; this spec takes the same shape with a stricter mode check and a lock. `gh` does not cache installation tokens at all.

## 📝 Delta

### Capability: token-cache

#### ADDED Requirement: Serve a fresh cached token without a request
The token manager SHALL return a cached token without calling the provider while the token stays valid for longer than the refresh margin.

##### Scenario: Token valid for 30 minutes
- WHEN the cache holds a token for the key that expires in 30 minutes, and the margin is 5 minutes
- THEN `Token` returns that token and the provider sees no request

#### ADDED Requirement: Refresh ahead of expiry
The token manager SHALL mint, cache, and return a new token when the cached token expires within the margin, has expired, or is missing or unreadable.

##### Scenario: Token expiring in 2 minutes
- WHEN the cache holds a token that expires in 2 minutes, and the margin is 5 minutes
- THEN `Token` mints one new token, writes it to the cache, and returns it

##### Scenario: Corrupt entry
- WHEN the cache file of the key is truncated JSON, has an unknown member, stores another key, holds a token with a newline, or has mode `0644`
- THEN `Token` mints a new token, replaces the file with a `0600` file, and returns the new token

#### ADDED Requirement: One mint across concurrent processes
The token manager SHALL serialize refreshes of one key across processes, so that concurrent requests on a missing or stale entry mint one token.

##### Scenario: 20 processes on an empty cache
- WHEN 20 separate processes request a token for the same key at the same time on an empty cache
- THEN the provider sees exactly one mint, and every process gets the same token

#### ADDED Requirement: Entries never cross keys
The token manager SHALL return a cached token only for the key it was cached under.

##### Scenario: Two installations
- WHEN tokens are requested for two sources that differ only in their token subject (two installations of one App)
- THEN each gets its own token, and neither cache entry is returned for the other

#### ADDED Requirement: Fail closed
The token manager SHALL return an error and no token when a valid token cannot be obtained, and SHALL NOT include a token in any error.

##### Scenario: Refresh denied
- WHEN the cached token expires within the margin and the provider fails with `ErrUnauthorized`
- THEN `Token` returns the zero token and an error that matches `provider.ErrUnauthorized` and contains neither the old nor any other token

##### Scenario: Insecure cache directory
- WHEN the cache directory has mode `0755`, is a symlink, or belongs to another user
- THEN `Token` fails with `ErrInsecureCache` without reading the entry or calling the provider

#### ADDED Requirement: Private, atomic cache files
The token manager SHALL store each entry in a `0600` file inside a `0700` directory and SHALL replace it atomically.

##### Scenario: Fresh cache
- WHEN `Token` mints on an empty cache
- THEN the directory has mode `0700`, the entry has mode `0600`, and no temporary file remains

## 📝 Architecture

- **New module** `internal/token` (planned in `ARCHITECTURE.md` with the dependencies `provider`, `kernel`; it uses only `provider`). Its consumers, `internal/gitint` (#22), `internal/cliint` (#23), and `internal/diagnose` (#24), are planned; nothing calls it in this PR, and the composition root wires it with its first consumer.
- **Provider interface** gains `TokenSubject() string` (A1). The change touches `internal/provider` (interface and doc), `providertest` (contract check and `Fake`, which gains `SetTokenSubject` for the two-installation test), `githubtest` (`App.Provider`), and the GitHub adapter. The contract adds: the subject matches the key pattern above, is stable across calls, and two fake installations give two subjects.
- **No GitHub knowledge in core.** `internal/token` never sees an App ID, a host constant, or a token prefix. depguard's `github-only-in-its-adapter` rule already forbids the import; review checks the rest.

## 📝 Edge Cases & Failure Scenarios

| Situation | Behavior |
|---|---|
| `XDG_CACHE_HOME` relative or empty, `HOME` set | `$HOME/.cache/agent-id/tokens`. |
| Neither variable usable | `ErrNoCacheDir`; no mint. |
| Cache directory (or `tokens`) is a symlink, has group/other bits, or another owner | `ErrInsecureCache` naming the path and the fix; no read, no mint. |
| Entry is a symlink, FIFO, directory, another owner's file, or not `0600` | Miss; replaced by `rename` (the symlink itself is replaced, its target is never written). |
| Entry larger than 64 KiB, truncated, wrong `version`, unknown or duplicate member, stored key differs, token empty or with control characters, `expires_at` zero | Miss; replaced. |
| Entry expired or inside the margin | Refresh under the lock. |
| Another process holds the lock for longer than `LockTimeout` | `ErrLockTimeout`; no mint. |
| Context canceled while waiting or minting | The context's error, wrapped; the lock is released; no write. |
| Provider fails (`ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrUnavailable`, `ErrRateLimited`) | Wrapped error, zero token; the old entry stays on disk and is not served. |
| Provider returns an expired token, a token valid for less than the margin, or an invalid value | `ErrUnusableToken`; nothing written. |
| Disk full or read-only during the write | `ErrCacheWrite`; temporary file removed; minted token discarded. |
| Process killed between `rename` and directory `fsync` | Either the old or the new entry survives; both are complete files. |
| Clock jumps backward after caching | An entry is served while `now + margin < expires_at` by the local clock; GitHub rejects an actually expired token, and the next call after real expiry refreshes. |
| Two profiles with the same App and installation | Same key, same entry: correct, because the token is the same grant. |

## 📝 Risks & Impact Review

- **Tokens on disk (brief §10).** The cache is the one place a token is persisted. Mode `0600` in a `0700` directory keeps other OS users out; it does not keep out the developer's own processes, including the agent. That is the documented limit of local mode (`ARCHITECTURE.md`, Security model), not a new risk.
- **Interface change.** `TokenSubject()` is additive to `provider.Provider`, but every implementation must add it; today there are three (the GitHub adapter, `githubtest`, `providertest.Fake`). A later forge adapter defines its own subject.
- **Cache format.** `version: 1` in every entry. A later format change bumps the version; an older or newer entry is a miss and is replaced, so no migration is ever needed. Removing the directory is always safe.
- **Rate limits.** One mint per key per ~55 minutes under normal use. No retry loop is added, so a rate-limited host is not hammered; callers see `ErrRateLimited`.
- **Not reviewed against real GitHub.** Tests use fakes only; real tokens appear first with #22/#23 on the owner's machine.

## 📝 Decisions in play

- Brief §8 (*Token lifecycle*, *Fail-closed default*), §9.2 (*Token manager*), §10 (*Credential handling*); ADR #35 (cache key includes the provider; dependency rule 7); `ARCHITECTURE.md` Data (token cache location and modes).

## Resolved assumptions (autonomous defaults)

These are security-model details beyond the brief. Each is the most reversible, fail-closed choice; the owner can override any of them before merge.

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | How does core key the cache without knowing GitHub's App and installation IDs? | New `provider.Provider.TokenSubject()`; GitHub returns `app-<appID>-installation-<installationID>`. | Keeps forge fields in the adapter (rule 7) and satisfies the two-installation criterion. | reversible |
| A2 | Cache location on macOS | `$XDG_CACHE_HOME/agent-id/tokens`, default `~/.cache/agent-id/tokens`, not `~/Library/Caches`. | Matches the config directory and `ARCHITECTURE.md` (Data); one rule for both platforms. | reversible |
| A3 | Refresh margin | 5 minutes, a `Manager` field. | The issue's example; a long `git push` fits inside it. | reversible |
| A4 | Lock wait | `flock` polled until the context ends or 30 s. | Refreshing is one HTTP request; 30 s matches the adapter's HTTP timeout. | reversible |
| A5 | Serve a still-valid token inside the margin when the refresh fails? | No: fail closed with the provider error. | A revoked App or installation shows at once; the request was to fail closed after an error. | reversible; recommend keeping |
| A6 | Cache directory not private or not the user's | Fail with `ErrInsecureCache`; never `chmod` an existing directory. | Writing a token into a readable directory leaks it; silently fixing modes hides a misconfiguration. | reversible; recommend keeping |
| A7 | Cache write fails after a successful mint | Fail with `ErrCacheWrite`; discard the token. | One code path; a cache that silently stops working would mint on every call and hit rate limits unnoticed. | reversible |
| A8 | Public surface | `Token` only; no `Status`, `Forget`, or `Erase` yet. | No consumer exists; `doctor` (#24) and the credential helper's `erase` (#22) add what they need. | reversible |

## 📋 Phasing

One implementation PR (`Closes #18`), `risk-high`. Each phase leaves the gate green.

- **Phase 1 — interface.** `TokenSubject()` in the interface, the contract, both fakes, and the GitHub adapter.
- **Phase 2 — module.** `internal/token`: key, directory, entry codec, lock, `Manager.Token`, process test.
- **Phase 3 — docs.** Module `AGENTS.md`, task routing, `ARCHITECTURE.md`, archived capability.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first (it points `XDG_CACHE_HOME` and `HOME` into the test root), uses `providertest.Fake` or a test-local source that issues `testenv.FakeToken` values, and injects the clock. No test reads a real token or reaches the network. Run the gate in an isolated environment after each step.

### Phase 1 — provider interface

1. **TokenSubject.** Add `TokenSubject() string` to `provider.Provider` and its doc. `providertest`: a contract case "names its token subject" (pattern, stable across two calls, no newline); `Fake` returns `fake-installation` by default and gains `SetTokenSubject`. `githubtest.App.Provider` and the GitHub adapter return `app-<appID>-installation-<installationID>`; adapter test with two installation IDs. Update `internal/provider/AGENTS.md`.

### Phase 2 — `internal/token`

2. **Key and directory.** `Key`, `KeyFor`, `keyName` (canonical encoding + SHA-256), `Dir(getenv)`, `checkDir`. Hostile tables: key fields with `/`, `..`, NUL, newline, spaces, empty, 129 characters; `XDG_CACHE_HOME` relative/empty, `HOME` empty/relative; directories with modes `0750`/`0705`/`0777`, a symlink, a file in place of the directory (other owner covered by a pure test of `checkDir` with a fake `fs.FileInfo` uid).
3. **Entry codec.** `decodeEntry` and `encodeEntry` (pure), `checkValue`, `fresh`. Table: truncated, unknown member, duplicate member, wrong types, version 2, other key, empty or 4097-character token, token with `\n`/`\x00`/space/non-ASCII, zero or unparsable `expires_at`; freshness at exactly the margin boundary. Assert that no error string contains the token.
4. **File I/O.** `readEntry` (`O_NOFOLLOW`, `fstat` checks, 64 KiB cap) and `writeEntry` (temp file, `0600`, `fsync`, `rename`, directory `fsync`). Tests: modes after a write, no leftover temp file, symlinked entry replaced without touching its target, a `0644` entry ignored, a read-only directory gives `ErrCacheWrite` and leaves no temp file.
5. **Lock and Manager.** `lockKey` (`flock` with `LOCK_NB` polling, context, `LockTimeout`) and `Manager.Token`. Tests with an injected clock and `providertest.Fake`: the AC scenarios (30 min → no mint; 2 min → one mint, cached; two subjects → two tokens, no crossing); expired entry; each provider error wrapped and fail-closed with the old entry not served; a token valid for less than the margin → `ErrUnusableToken`; a held lock (second descriptor in the test) → `ErrLockTimeout` with a short timeout, and context cancellation; every error checked for every issued token value.
6. **Process test.** `TestMain` calls `testenv.Main(m)` and dispatches a child mode when its environment variable is set. The parent starts 20 copies of the test binary (`os.Args[0]`) with the isolated environment; each child runs `Manager.Token` with a test source that appends one line to a shared mint log (`O_APPEND`) and sleeps 100 ms in the mint, then prints the SHA-256 of its token (never the token). Assert one mint line and 20 identical hashes; scan all output for the `testenv.FakeTokenPrefix`.

### Phase 3 — documentation

7. **Docs and archive.** `internal/token/AGENTS.md` (invariants: core only, fail closed, `0600`/`0700`, atomic rename, `flock`, no logger, no secret in errors, pure decisions). `AGENTS.md`: the overview sentence and a task-routing row for `internal/token`. `ARCHITECTURE.md`: components tree and table (token exists), the interface line in the provider row, a Key decisions row for A1–A8. Archive the Delta into `.ai/specs/capabilities/token-cache.md`.
