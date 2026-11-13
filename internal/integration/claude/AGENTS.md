# internal/integration/claude

The Claude Code integration: the `SessionStart` hook slice (`sessionstart`) and the pure text functions of the guarded block and the refusal line. Spec: `.ai/specs/2026-11-13-env-claude-session-start.md` (#26); requirements in `.ai/specs/capabilities/claude-code-session-start.md`. Always `risk-high`: the text is a shared contract that runs in users' shells before every Bash command.

- **Never edit Claude settings.** No write to `.claude/settings.json`, `.claude/settings.local.json`, or `~/.claude`. The hook is documented in `README.md`, never installed (OQ3: no `claude-code install` in the MVP). Tests use a fake `HOME` and a fake settings file under `$TMPDIR/agent-id/`.
- **Write only a guarded block or a refusal line**, and only to the file named by `CLAUDE_ENV_FILE`, after the safety checks of the spec (step 1): no symlink, a regular file of the current user, not writable by others.
- **Fail closed.** A failed activation appends a refusal line (exit 3): every later command stops with 125. Exit 4 only when there is no safe or parseable file to write.
- **No secret in the text.** The block comes from `activation.Render`, which scans for token material. A refusal reason is cut to 200 characters, cleaned of control characters, and single-quoted; a profile name that does not match the profile rule is never printed.
- **Names no forge.** It imports `internal/activation` only.
- **Markers are contract version 1.** A change needs a new version in the marker and a README migration note.
