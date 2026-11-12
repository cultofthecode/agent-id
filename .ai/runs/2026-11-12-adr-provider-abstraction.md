# Execution plan — ADR: provider abstraction (#35)

Source doc: https://github.com/cultofthecode/agent-id/issues/35
Engine: om-auto-create-pr (steps: 4, --loop: no)
Branch: `docs/adr-provider-abstraction` (operator-chosen; overrides the default `feat/` prefix)

## Goal

Record the owner's decision (Jacob, 12 November 2026, 22:41 Europe/Warsaw) that every Git forge is reached through a provider interface in `internal/provider`, with GitHub as the first adapter in `internal/provider/github`, and state the import rule that keeps GitHub-specific code out of the core.

## Scope

- `ARCHITECTURE.md`: a provider-abstraction section (the ADR) naming the interface responsibilities from the issue (authentication and tokens, bot identity, per-host credential helper, CLI integration, host and URL mapping); the layout change from `internal/github` to `internal/provider` + `internal/provider/github`; GitLab and Bitbucket as later adapters of the same interface; the `provider` field of profile config v1; contract tests against a fake provider; dependency rule and Key decisions row.
- `AGENTS.md`: the rule "zero GitHub-specific imports outside `internal/provider/github`" (composition root excepted for wiring), and a task-routing row.
- `CODE_REVIEW.md`: the matching repo-specific review check, so reviewers enforce the rule.

## Non-goals

- No Go code: the interface, the GitHub adapter, and the contract tests are built by #15, #10, #22, #23 under the constraints recorded here.
- No GitLab, Bitbucket, or Azure DevOps adapter.
- No change to the profile-config spec (#9, PR #33 owns it); the ADR states the requirement only.
- No resolution of other OPEN rows (product name, config format, keyring).

## Implementation Plan

### Phase 1: Record the decision

- 1.1 Add the provider-abstraction ADR section, the layout and component changes, the dependency rule, and the Key decisions row to `ARCHITECTURE.md`.
- 1.2 Add the import rule and the routing row to `AGENTS.md`, and the review check to `CODE_REVIEW.md`.

### Phase 2: Verify

- 2.1 Run the validation gate in an isolated environment and re-read the diff for scope creep and honest claims.
- 2.2 Open the PR with `Refs #35`, labels, and review.

## Risks

- The brief (§3 principle 6, §13 Phase 4) still says to defer cross-provider abstraction; the ADR records that the owner's decision supersedes that deferral for package boundaries only, and the MVP runtime stays GitHub.com-only.
- Over-design: the ADR names responsibilities and boundaries, not method signatures, so #15 can shape the interface from real code.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Record the decision

- [ ] 1.1 Add the provider-abstraction ADR to ARCHITECTURE.md
- [ ] 1.2 Add the import rule to AGENTS.md and CODE_REVIEW.md

### Phase 2: Verify

- [ ] 2.1 Run the validation gate and re-read the diff
- [ ] 2.2 Open the PR, label it, and review it
