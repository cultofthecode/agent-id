# agent-id

agent-id gives AI coding agents their own GitHub App identity for one process and its descendants, without changing how the developer uses Git and `gh`. The developer keeps working as themselves; Claude Code, Codex, or another agent commits, pushes, and opens pull requests as its own bot, with installation tokens refreshed automatically.

**Status:** pre-alpha. `agent-id setup github` writes an agent profile. `agent-id exec --profile <name> -- <cmd>` runs a command as that bot: Git and `gh` use the bot identity. Local mode is not a security sandbox.

```sh
go build ./... && go test ./...
```

- `PRODUCT_BRIEF.md` — the product brief
- `AGENTS.md` — project overview and rules for agents, including mandatory test isolation
- `ARCHITECTURE.md` — system structure and decisions
- `SDLC.md` — how work moves from an issue to a merged PR
- Backlog: https://github.com/cultofthecode/agent-id/issues

## Set up an agent profile

You need an existing GitHub App, an installation of it on the repositories the agent works on, and the App's private key (a `.pem` file from the App's settings page). Keep the key file readable only by you:

```sh
chmod 600 ~/keys/claude-dev.pem
agent-id setup github --profile claude --app-id 123456 --installation-id 789012 --private-key ~/keys/claude-dev.pem
```

- `--app-id` is the App ID on the App's settings page.
- `--installation-id` is the number at the end of the installation's settings URL.
- `--private-key` is the key file. A relative path is resolved against the current directory.

Setup verifies everything before it writes:

1. It loads the key and mints one installation token.
2. It resolves the App's bot (`<slug>[bot]` and its noreply commit email).
3. It lists the repositories the installation can reach.
4. Only then does it write the profile to `~/.config/agent-id/config.json` (or `$XDG_CONFIG_HOME/agent-id/config.json`).

The profile stores a reference to the key file, never the key, and setup does not keep or print the token. Setup does not copy, move, or change the mode of the key file. It changes no other file: not your Git configuration, not your `gh` configuration. Running setup again with the same profile name updates the profile.

## Rotate or revoke the App key

1. Generate a new private key on the App's settings page and save it with mode `0600`.
2. Run `agent-id setup github --profile <name> …` again with the new `--private-key` path.
3. Delete the old key on the App's settings page, then delete the old file.

Deleting a key in GitHub does not revoke installation tokens already minted with it, for example in agent-id's token cache. They stay valid until they expire, at most one hour. The App key can mint tokens for every installation of the App, so keep it out of repositories, shared directories, and agent-readable locations where you can. In local mode an agent running as your user can still read files you can read: agent-id controls attribution and credentials on its supported paths, and it is not a security boundary.

## License

agent-id is licensed under the [Apache License, Version 2.0](LICENSE). It is open source; releases ship through GitHub Releases (see [RELEASING.md](RELEASING.md)) and, later, a Homebrew tap. It contains no code from GHAPP (`ghapp-cli`): GHAPP-related code is written from scratch, so no GHAPP notice applies (GHAPP audit, PR #31).
