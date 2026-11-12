# Backlogs

## PRODUCT_BRIEF.md
Backlog source: PRODUCT_BRIEF.md
Last filed: 2026-11-12

Parents are linked as GitHub sub-issues; each epic carries a checklist. Milestones match the roadmap phases of brief §13. Issues with `needs-decision` wait for the owner's answer to an open question of brief §14.

| Id | Issue | Title | Epic | Depends on | Decision needed |
|---|---|---|---|---|---|
| E01 | [#1](https://github.com/cultofthecode/agent-id/issues/1) | Phase 0 — Standalone foundation: name, license, config layout, test harness, release pipeline | — | — | no |
| E02 | [#2](https://github.com/cultofthecode/agent-id/issues/2) | Phase 1 — GitHub identity MVP: an agent commits, pushes and opens a PR as its own bot | — | — | no |
| E03 | [#3](https://github.com/cultofthecode/agent-id/issues/3) | Phase 2 — Multi-agent identities: named profiles, concurrent sessions, Codex and generic launchers | — | E02 (#2) | no |
| E04 | [#4](https://github.com/cultofthecode/agent-id/issues/4) | Phase 3 — Hardened runtime: credential broker, isolation, policy and audit | — | E03 (#3) | no |
| E05 | [#5](https://github.com/cultofthecode/agent-id/issues/5) | Phase 4 — Provider expansion (optional): evaluate GitLab, Bitbucket, Azure DevOps on demand | — | E02 (#2) | no |
| E01-T01 | [#6](https://github.com/cultofthecode/agent-id/issues/6) | Decide and record the product name, binary name, config directory and environment prefix (aidentity vs agent-id) | #1 | — | yes |
| E01-T02 | [#7](https://github.com/cultofthecode/agent-id/issues/7) | Audit GHAPP for provenance, license obligations and reuse vs rewrite | #1 | — | no |
| E01-T03 | [#8](https://github.com/cultofthecode/agent-id/issues/8) | Choose the license of agent-id and add LICENSE and required notices | #1 | E01-T02 (#7) | yes |
| E01-T04 | [#9](https://github.com/cultofthecode/agent-id/issues/9) | Spec the profile configuration v1: location, format, schema, key references and identity cache | #1 | — | no |
| E01-T05 | [#10](https://github.com/cultofthecode/agent-id/issues/10) | Extend the test harness: fake GitHub App API, local bare repositories and isolated git/gh process tests | #1 | — | no |
| E01-T06 | [#11](https://github.com/cultofthecode/agent-id/issues/11) | Release pipeline: tagged GitHub Releases with darwin/linux archives, checksums and provenance | #1 | E01-T01 (#6), E01-T03 (#8) | yes |
| E01-T07 | [#12](https://github.com/cultofthecode/agent-id/issues/12) | Protect main: require the gate check, merge commits only, no direct pushes | #1 | — | no |
| E02-T01 | [#13](https://github.com/cultofthecode/agent-id/issues/13) | Research: Git configuration precedence and wrapper bypass matrix for agent mode | #2 | — | no |
| E02-T02 | [#14](https://github.com/cultofthecode/agent-id/issues/14) | Research: which gh commands work with an installation token, and how gh picks its token and host | #2 | — | no |
| E02-S01 | [#15](https://github.com/cultofthecode/agent-id/issues/15) | GitHub provider: sign the App JWT, mint installation tokens and resolve the bot identity | #2 | E01-T05 (#10) | no |
| E02-S02 | [#16](https://github.com/cultofthecode/agent-id/issues/16) | Credential source: load the App private key from a protected file, keyring behind an interface | #2 | E01-T04 (#9) | no |
| E02-S03 | [#17](https://github.com/cultofthecode/agent-id/issues/17) | Profile store: read, validate and write profile configuration v1 | #2 | E01-T04 (#9) | no |
| E02-S04 | [#18](https://github.com/cultofthecode/agent-id/issues/18) | Token manager: cache installation tokens safely and refresh them before expiry | #2 | E02-S01 (#15) | no |
| E02-S05 | [#19](https://github.com/cultofthecode/agent-id/issues/19) | The developer can run `aidentity setup github --profile <name>` for an existing App and sees the resolved bot | #2 | E02-S01 (#15), E02-S02 (#16), E02-S03 (#17) | no |
| E02-S06 | [#20](https://github.com/cultofthecode/agent-id/issues/20) | The developer can run `aidentity exec --profile <name> -- <cmd>` and only that process tree is in agent mode | #2 | E02-S03 (#17) | no |
| E02-S07 | [#21](https://github.com/cultofthecode/agent-id/issues/21) | In agent mode, `git commit` uses the bot as author and committer, with personal signing turned off | #2 | E02-S06 (#20) | no |
| E02-S08 | [#22](https://github.com/cultofthecode/agent-id/issues/22) | In agent mode, HTTPS Git operations authenticate as the App, SSH remotes are rewritten, and nothing else gets a token | #2 | E02-S04 (#18), E02-S07 (#21), E02-T01 (#13) | no |
| E02-S09 | [#23](https://github.com/cultofthecode/agent-id/issues/23) | In agent mode, ordinary `gh` commands use a fresh App token without touching the personal gh account | #2 | E02-S04 (#18), E02-S06 (#20), E02-T02 (#14) | no |
| E02-S10 | [#24](https://github.com/cultofthecode/agent-id/issues/24) | The developer can run `aidentity whoami` and `aidentity doctor` and sees identity, credential source and conflicts without secrets | #2 | E02-S08 (#22), E02-S09 (#23) | no |
| E02-S11 | [#25](https://github.com/cultofthecode/agent-id/issues/25) | Fail-closed suite: missing, invalid, expired and denied App credentials never fall back to personal credentials | #2 | E02-S05 (#19), E02-S08 (#22), E02-S09 (#23) | no |
| E02-S12 | [#26](https://github.com/cultofthecode/agent-id/issues/26) | The developer can launch Claude Code normally and a SessionStart hook activates the agent profile | #2 | E02-S07 (#21), E02-S08 (#22), E02-S09 (#23) | no |
| E02-S13 | [#27](https://github.com/cultofthecode/agent-id/issues/27) | End-to-end validation: a bot and the developer work side by side in a disposable GitHub repository | #2 | E02-S10 (#24), E02-S11 (#25), E02-S12 (#26) | no |
| E03-T01 | [#28](https://github.com/cultofthecode/agent-id/issues/28) | Decide whether agent-specific commit signing is a later feature | #3 | — | yes |
| E03-T02 | [#29](https://github.com/cultofthecode/agent-id/issues/29) | Decide whether and how profiles are auto-detected beyond explicit exec | #3 | E02-S12 (#26) | yes |
