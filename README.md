# agent-id

agent-id gives AI coding agents their own GitHub App identity for one process and its descendants, without changing how the developer uses Git and `gh`. The developer keeps working as themselves; Claude Code, Codex, or another agent commits, pushes, and opens pull requests as its own bot, with installation tokens refreshed automatically.

**Status:** pre-alpha skeleton. Nothing works yet beyond `agent-id --version`. Local mode is not a security sandbox.

```sh
go build ./... && go test ./...
```

- `PRODUCT_BRIEF.md` — the product brief
- `AGENTS.md` — project overview and rules for agents, including mandatory test isolation
- `ARCHITECTURE.md` — system structure and decisions
- `SDLC.md` — how work moves from an issue to a merged PR
- Backlog: https://github.com/cultofthecode/agent-id/issues

No license has been chosen yet (`PRODUCT_BRIEF.md` §14).
