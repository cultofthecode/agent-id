# agent-id

agent-id gives AI coding agents their own GitHub App identity for one process and its descendants, without changing how the developer uses Git and `gh`. The developer keeps working as themselves. Claude Code, Codex, or another agent commits, pushes, and opens pull requests as its own bot, with installation tokens refreshed automatically.

**Status:** pre-alpha. The MVP supports GitHub.com, macOS and Linux, and Bash and Zsh. Local mode is not a security sandbox.

## Install

Build the binary from a clone of this repository. You need Go:

```sh
go build -o agent-id ./cmd/agent-id
```

Put `agent-id` on your `PATH`, then check it:

```sh
agent-id --version
```

Each release publishes archives for macOS and Linux on GitHub Releases. See [RELEASING.md](RELEASING.md) for how to verify an archive.

## Commands

- `agent-id setup github` verifies a GitHub App and writes an agent profile.
- `agent-id exec --profile <name> -- <cmd>` runs a command as the bot. Git and `gh` use the bot identity.
- `agent-id env --profile <name> --shell zsh` (or `--shell bash`) prints the child environment as a shell block. It holds no token.
- `agent-id whoami` shows the context, identity, and credential source of this shell. Add `--json` for JSON output.
- `agent-id doctor --profile <name>` checks a profile's configuration, credentials, and conflicts. Add `--json` for JSON output.
- `agent-id claude-code session-start --profile <name>` is the Claude Code `SessionStart` hook. It activates the profile for the Bash commands of a session.

Run `agent-id <command> --help` for every flag. Git runs the hidden `agent-id git-credential` command for its credential requests. Do not run it by hand.

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

## Run an agent as the bot

Run one command as the bot:

```sh
agent-id exec --profile claude -- git push origin my-branch
```

Start Claude Code as the bot:

```sh
agent-id exec --profile claude -- claude
```

Check the identity that the command uses:

```sh
agent-id exec --profile claude -- agent-id whoami
agent-id doctor --profile claude
```

To apply the profile to every Bash command of a plain `claude` session, add a `SessionStart` hook. The hook is documented in [internal/integration/claude/README.md](internal/integration/claude/README.md). agent-id never writes your Claude settings. You add the hook yourself.

## Rotate or revoke the App key

1. Generate a new private key on the App's settings page and save it with mode `0600`.
2. Run `agent-id setup github --profile <name> …` again with the new `--private-key` path.
3. Delete the old key on the App's settings page, then delete the old file.

Deleting a key in GitHub does not revoke installation tokens already minted with it, for example in agent-id's token cache. They stay valid until they expire, at most one hour. The App key can mint tokens for every installation of the App, so keep it out of repositories, shared directories, and agent-readable locations where you can. In local mode an agent running as your user can still read files you can read: agent-id controls attribution and credentials on its supported paths, and it is not a security boundary.

## Documentation

- `PRODUCT_BRIEF.md`: the product brief
- `AGENTS.md`: project overview and rules for agents, including mandatory test isolation
- `ARCHITECTURE.md`: system structure and decisions
- `SDLC.md`: how work moves from an issue to a merged PR
- Backlog: https://github.com/cultofthecode/agent-id/issues

## License

agent-id is licensed under the [Apache License, Version 2.0](LICENSE). It is open source. Releases ship through GitHub Releases (see [RELEASING.md](RELEASING.md)). A Homebrew tap is a second channel. It contains no code from GHAPP (`ghapp-cli`): GHAPP-related code is written from scratch, so no GHAPP notice applies (GHAPP audit, PR #31).
