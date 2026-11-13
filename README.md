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

## License

agent-id is licensed under the [Apache License, Version 2.0](LICENSE). It is open source; releases will ship through GitHub Releases and a Homebrew tap. It contains no code from GHAPP (`ghapp-cli`): GHAPP-related code is written from scratch, so no GHAPP notice applies (GHAPP audit, PR #31).
