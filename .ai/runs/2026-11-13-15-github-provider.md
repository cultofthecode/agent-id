# Execution plan — GitHub provider adapter (E02-S01, #15)

Source issue: https://github.com/cultofthecode/agent-id/issues/15
Source doc: `.ai/specs/2026-11-13-github-provider-adapter.md` (spec PR #54, merged)
Route: `om-auto-fix-issue` feature route: spec through `om-auto-write-spec`, implementation on its own PR.
Claim-Run: aid15-43e20a3a-73ba-44c9-8288-f088f07726f7

## Tasks

- [x] 1. Rate limiting in the interface: `provider.ErrRateLimited` (wraps `ErrUnavailable`), the contract with a one-class-per-failure check, `Fake`, `githubtest` 429 and `FailTokensWith`.
- [x] 2. Key and config: `Config`, `New`, test-only `newProvider`/`api`, `parsePrivateKey`, `checkBaseURL`, redacting `Format`.
- [x] 3. JWT: `signJWT`, exact claims, accepted by the fake within the clock skew, rejected for another App.
- [x] 4. HTTP and classification: GitHub headers, no redirects, 1 MiB cap, `classify`.
- [x] 5. Token: AC scenarios, denied paths without secrets, hostile 201 answers, redirect, deadline, no server.
- [x] 6. Identity: `Bot`, `CommitIdentity`, `Identity`; hostile slugs and users; no credential on `/users`.
- [x] 7. Repositories: pagination without `Link`, expired token, hostile names, page cap.
- [x] 8. Contract and Git/CLI: `RunContract` against the adapter.
- [x] 9. Docs: package `AGENTS.md`, task routing, `ARCHITECTURE.md`, capability file.

Follow-up: the `github` profile block (profile-config spec steps 9–11) after #17 merges.
