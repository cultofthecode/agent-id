# Execution plan — Git configuration precedence and wrapper bypass matrix (E02-T01)

Issue: #13
Branch: `research/13-git-config` (set by the operator for this run instead of `feat/<slug>`)
Engine: om-auto-create-pr (steps: 6, --loop: no)

## Goal

A reproducible, tested matrix of how inherited Git configuration and shell startup files change the effective Git behavior of a process that gets aidentity's `GIT_CONFIG_COUNT` entries, with each personal-credential leak classified as neutralized, detected by `doctor`, or a documented bypass, and a recommended `GIT_CONFIG_*` set for `internal/gitint`.

## Scope

- `.ai/analysis/2026-11-12-git-precedence-matrix.md` — the analysis and the matrix.
- `.ai/analysis/2026-11-12-git-precedence-matrix.sh` — the experiment script that reproduces every row in a throwaway isolated environment under `$TMPDIR/agent-id/` (fake helpers, fake tokens, local repositories, a local header-capture listener on 127.0.0.1 in 18000–18999).

## Non-goals

- No product code, no change to the public CLI, no new Go module dependency.
- Implementing the credential helper (E02-S08) or the `gh` injection.
- Deciding the OPEN rows of `ARCHITECTURE.md`: the recommended set is a proposal for the owner and the `internal/gitint` tickets.
- No real credentials, no keychain access (osxkeychain is simulated by a fake helper), no network beyond 127.0.0.1.

## Implementation plan

### Phase 1: Experiments

1. Write the experiment script: isolated environment, fake helpers that log invocations, the precedence, credential-helper, URL-rewrite, header, identity, signing, SSH, `GIT_CONFIG_COUNT` and shell-startup cases.
2. Run it on macOS (Apple Git 2.54, bash 3.2, zsh 5.9) and in an `aid-` Docker container on Ubuntu 24.04 (Git 2.43, the CI runner's distribution); record both outputs.

### Phase 2: Analysis

1. Write the matrix document: precedence model, one row per configuration with command, observed result, and classification.
2. Derive the recommended `GIT_CONFIG_*` set and the `doctor` checks, and list open points for the owner.

### Phase 3: Delivery

1. Run the validation gate in an isolated environment.
2. Open the PR, review pass, summary comment.

## Risks

- Behavior differs between Git versions; the matrix records the versions it ran on.
- The script must never reach the real keychain or real configuration: it sets its own `HOME`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM`, uses fake helper names, and never invokes `osxkeychain`.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Experiments

- [ ] 1.1 Write the experiment script
- [ ] 1.2 Run on macOS and Ubuntu 24.04 and record results

### Phase 2: Analysis

- [ ] 2.1 Write the matrix document
- [ ] 2.2 Derive the recommended GIT_CONFIG set and doctor checks

### Phase 3: Delivery

- [ ] 3.1 Run the validation gate
- [ ] 3.2 Open the PR, review pass, summary comment
