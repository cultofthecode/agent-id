# Execution plan — gh installation-token matrix (E02-T02, #14)

Source doc: `PRODUCT_BRIEF.md` §9.3, §14 (GitHub CLI compatibility); issue #14.
Engine: om-auto-create-pr (steps: 4, --loop: no)
Branch: `research/14-gh-install-token` (operator-assigned worktree and branch; the operator asked for the PR to target `main`).
Claim: run `11a23a94-d263-4590-aefe-76ac00208ec4`, lock `refs/om-locks/issue-14`.

## Goal

Record, for a GitHub App installation token in `GH_TOKEN`, which common `gh` commands work, how `gh` picks its token and host, and which error aidentity should show for unsupported user-centric commands.

## Scope

- One analysis document: `.ai/analysis/2026-11-12-gh-installation-token-matrix.md`.
- Evidence from the `gh` v2.103.3 and go-gh v2.17.0 source, the `gh` environment help, and an offline experiment with fake tokens in an isolated environment.

## Non-goals

- Implementing the `gh` injection (E02-S09) or any product code.
- Using a real token in any experiment.
- Deciding open product questions; they are listed in the document for the owner.

## Risks

- Some rows depend on GitHub server behavior that only a real installation token shows (for example the GraphQL `viewer` answer). The document marks those rows as needing confirmation in the end-to-end suite.

## Implementation Plan

### Phase 1: Research and write-up

1. Read the `gh` source for token resolution, host selection, and the per-command use of the viewer login.
2. Run the offline precedence experiment in an isolated environment with fake tokens.
3. Write the analysis document with the matrix, the precedence rules, and the proposed error messages.
4. Run the validation gate and re-read the diff.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Research and write-up

- [ ] 1.1 Read the gh source for token, host, and viewer use
- [ ] 1.2 Run the offline precedence experiment
- [ ] 1.3 Write the analysis document
- [ ] 1.4 Run the validation gate and re-read the diff
