# Execution plan — rename the product to agent-id (#6)

Source issue: https://github.com/cultofthecode/agent-id/issues/6 (E01-T01)
Decision: Jacob, 12 November 2026, 22:40 Europe/Warsaw — the product name is `agent-id` everywhere, replacing the working name `aidentity` (decision comment on #6).
Branch: `chore/6-rename-agent-id` (named by the owner's task brief)

## Goal

Apply the recorded name decision: the binary, the composition root package, the config and cache directory name, the environment prefix, and every non-historical reference become `agent-id`.

## Scope

- `cmd/aidentity` → `cmd/agent-id`; `commandName = "agent-id"`; `--version` prints `agent-id version …`; package and doc comments, tests.
- Environment prefix `AIDENTITY_` → `AGENT_ID_` in `internal/testenv` (the prefix that `Isolate` scrubs) and its tests.
- Config and cache directory names in `ARCHITECTURE.md`: `$XDG_CONFIG_HOME/agent-id/`, `$XDG_CACHE_HOME/agent-id/`.
- Docs: `AGENTS.md`, `ARCHITECTURE.md` (name row becomes active with owner and date), `README.md`, `CODE_REVIEW.md`, `SDLC.md`, `internal/testenv/AGENTS.md`, `.ai/specs/backlog.md`.
- `.gitignore`: the root binary that `go build` leaves is `/agent-id`.
- CI and `.golangci.yml` carry no working-name reference today (checked with grep); nothing to change there.

## Non-goals

- `PRODUCT_BRIEF.md` keeps its historical mentions of the working name; no rewrite of history.
- The repository name and Go module path `github.com/cultofthecode/agent-id` stay.
- The availability check (package, domain, trademark) is a pre-launch item, not part of this change.
- GitHub issue titles and bodies are not edited.

## Implementation Plan

### Phase 1: Code

- 1.1 Move `cmd/aidentity` to `cmd/agent-id`, set `commandName` to `agent-id`, and update comments and tests.
- 1.2 Change the scrubbed environment prefix in `internal/testenv` to `AGENT_ID_` and update its test.

### Phase 2: Docs and configuration

- 2.1 Rename every non-historical reference in docs, `.ai/`, and `.gitignore`.
- 2.2 Record the name decision as active in `ARCHITECTURE.md` (Key decisions) with owner and date.

### Phase 3: Validation

- 3.1 Run the full gate in an isolated environment and check `--version` of the built binary.

## Risks

- A stale `AIDENTITY_` variable in a developer shell would no longer be scrubbed by `testenv.Isolate`; no code reads that prefix, so it cannot change a test's behavior.
- Planned issues (#19, #20, #24) still spell commands with the working name in their GitHub text; the backlog table here is updated, the issues are not.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Code

- [ ] 1.1 Move cmd/aidentity to cmd/agent-id and rename the binary
- [ ] 1.2 Rename the scrubbed environment prefix in internal/testenv

### Phase 2: Docs and configuration

- [ ] 2.1 Rename non-historical references in docs, .ai/, and .gitignore
- [ ] 2.2 Record the name decision as active in ARCHITECTURE.md

### Phase 3: Validation

- [ ] 3.1 Run the full gate in an isolated environment
