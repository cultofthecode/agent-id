# Execution plan — GHAPP provenance, license and reuse audit

Issue: #7 (E01-T02)
Branch: `research/7-ghapp-audit` (named by the owner's task brief, not the skill default `feat/…`)
Claim-Run: 62c726c6-d257-4254-a60c-972fa2540437
Claim-Lock: refs/om-locks/issue-7 @ 4e5d52aa4a4d6ba28a36426234f445672e721821

## Goal

Write `.ai/analysis/2026-11-12-ghapp-reuse-audit.md`: the exact GHAPP source revision, its license and notice obligations, and a reuse-or-rewrite decision with reasons for each candidate component (JWT signing, installation token client, token cache, credential helper, `gh` wrapper, keyring import).

## Scope

- Read GHAPP source only, from public clones under `$TMPDIR/agent-id/`, in an isolated `HOME` and Git configuration.
- One new analysis file. No Go code, no copied GHAPP code, no new Go module dependency.

## Non-goals

- Copying or rewriting GHAPP code (Phase 1 stories).
- Running `ghapp auth configure`, `ghapp setup`, `ghapp auth reset`, or anything that changes ghapp, `gh`, or Git configuration. Only `ghapp version`, `ghapp token` (tracker work) and `go version -m` on the binary are used.
- Deciding the open rows of `ARCHITECTURE.md` (agent-id license, keyring, config format) or adding a dependency. The audit names them as questions for the owner.

## Implementation plan

### Phase 1: Audit

1. Identify the installed binary's module and revision, find the repository that contains it, and compare it with `operator-kit/ghapp-cli`.
2. Read `LICENSE`, look for `NOTICE`/`AUTHORS` files and per-file copyright headers at that revision; read the licenses of the libraries the token path uses.
3. Read each candidate component and write the analysis with the component table.

### Phase 2: Deliver

1. Run the validation gate in an isolated environment, re-read the diff, review, and finalize the PR.

## Risks

- The issue names `operator-kit/ghapp-cli` as the candidate; the revision may live elsewhere. The audit records what the clones show.
- Docs-only change: the Go gate still runs; it proves nothing about the document itself.

## Progress

PR: #31

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Audit

- [x] 1.1 Identify and confirm the upstream revision — 96735af
- [x] 1.2 Record license and notice obligations — 96735af
- [x] 1.3 Write the component reuse-or-rewrite analysis — 96735af

### Phase 2: Deliver

- [x] 2.1 Validation gate, review, and PR finalization — 60d4693

### Phase 3: Owner decisions

- [x] 3.1 Record the owner decisions in the audit, ARCHITECTURE.md, and the CODE_REVIEW.md provenance rule — 45a4217
