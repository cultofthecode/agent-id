# Changelog

All notable changes to agent-id are in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). The project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - TBD

First release. agent-id gives an AI coding agent its own GitHub App identity for one process and its descendants. It supports GitHub.com, macOS and Linux, and Bash and Zsh. Local mode is not a security boundary.

### Added

- **Product name and license.** The product and the binary are named `agent-id` (#39). The code uses the Apache-2.0 license (#41).
- **Profiles.** The profile store reads, validates, and writes the version 1 profile file (#52, #78). The GitHub adapter validates the `github` block of a profile (#79).
- **`agent-id setup github`.** The command checks an existing GitHub App and writes an agent profile. It writes nothing when the check fails (#82).
- **`agent-id exec --profile <name> -- <cmd>`.** The command runs a command, and only its process tree, as the bot. The command fails closed. The child gets no App token in its environment (#86).
- **`agent-id env`.** The command prints the child environment as one block of shell statements. The block holds no token (#98).
- **Claude Code hook.** `agent-id claude-code session-start` is a `SessionStart` hook. It adds the block to `CLAUDE_ENV_FILE`. When activation fails, it adds a refusal line, and every command of the session stops with exit status 125 (#98).
- **`agent-id whoami` and `agent-id doctor`.** `whoami` shows the context, the bot identity, and the credential source. `doctor` checks the configuration, the credentials, and the conflicts of a profile. Both support `--json`. Both redact secrets (#92, #96).
- **Git credential helper and SSH rewrites.** In an agent context, Git gets a bot token from `agent-id git-credential`. The helper answers only for the profile host over HTTPS. agent-id rewrites SSH remotes of that host to HTTPS (#103, #116).
- **`gh` shim.** The `gh` command in an agent context starts a launcher. The launcher gives each `gh` call a fresh bot token. It refuses `gh alias` and `gh config`, and it never runs `gh` with an empty token (#104, #123).
- **Commit identity.** In an agent context, commits carry the bot as author and committer. agent-id switches off Git signing with the personal key (#89).
- **Token cache.** The token manager caches installation tokens on disk and refreshes them before they expire (#66, #81).
- **Release chain.** A signed `v*` tag starts the release workflow. The workflow runs in the `release` environment and needs approval. It publishes four archives for macOS and Linux, `checksums.txt`, and a build provenance attestation for each archive (#47, #83, #126). For a stable tag, the workflow also opens a pull request on the Homebrew tap `cultofthecode/homebrew-tap` (#114).
- **End-to-end check.** The `e2e` tests and a runbook let the owner run agent-id against real GitHub by hand (#109, #121).

### Security

- **Fail closed.** If agent-id cannot get an agent credential, it refuses to start the command. It never uses the personal credentials of the developer. A test suite covers a missing, invalid, expired, and denied App credential (#86, #88).
- **Git header checks.** Before a command starts, agent-id checks the Git configuration it inherits. It refuses an `extraHeader` that Git would send to the provider host. The check compares URL patterns as Git does. It covers `http://` hosts, and any port or scheme (#99, #101, #110, #113).
- **Private key checks.** The App key loads only from a protected file. agent-id refuses a key file with a macOS ACL, a key in an unsafe parent directory, and a symlink or `..` path that skips a directory check (#53, #90, #94, #115, #124).
- **Token handling.** Tokens do not appear in output, errors, or logs. The token cache uses `0600` files in a private directory (#66, #81).
- **Signed release tags.** The release workflow refuses a tag without a valid SSH signature (#126).
- **Test isolation.** Tests run with a temporary home directory. Child processes cannot reach the network except on local ports 18000–18999 (#32, #84).

### Documentation

- `README.md` describes the install steps, the commands, and the key rotation steps (#122, #130).
- `ARCHITECTURE.md` records the provider abstraction. GitHub is the first provider (#38).
- Analyses cover Git configuration precedence and bypasses, the `gh` token and host precedence, and the reuse of GHAPP code (#30, #31, #34).
- Specifications describe the profile file, `exec`, `env`, `whoami` and `doctor`, `setup github`, the token manager, and the commit identity (#33, #48, #51, #54, #59, #60, #61, #65, #80).
- `SDLC.md` records the merge rules and the `main` ruleset (#102). `RELEASING.md` describes the release and the Homebrew tap (#47, #83, #114).

[Unreleased]: https://github.com/cultofthecode/agent-id/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/cultofthecode/agent-id/releases/tag/v0.1.0
