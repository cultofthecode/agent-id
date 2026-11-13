# internal/gitint/credentialhelper

The slice behind `agent-id git-credential --profile <name> get|store|erase` (#22). Spec: `.ai/specs/2026-11-13-git-credential-helper.md`. Always `risk-high` and `security`: it hands a token to Git.

- **The decision is pure and lives in `internal/gitint`** (`ParseRequest`, `Decide`, `Render`, `TransportEntries`, in `transport.go`). The handler only loads, calls, and acts. Keep new rules in those functions with hostile-input tables.
- **Answer only for the profile host over HTTPS.** Another host is `Skip` (no output, exit 0). The profile host with the wrong protocol, port, user-info, path, a password, or a CR/NUL/repeated attribute is `Refuse`: stdout `quit=1`, exit 1.
- **Fail closed.** Any failure to get a token prints `quit=1` and one fixed message (`cannot get a token for profile "<name>" (<class>)`). The class is a fixed word, never `err.Error()`. A token appears only on stdout of a successful `get`, as two lines.
- **`store` and `erase` do nothing**: no profile lookup, no write, exit 0.
- **No `internal/token` import.** The composition root injects `Credential`, which uses `token.Manager`. The key loads only after `Allow`.
- **Tests** call `testenv.Isolate(t)`. Process tests run real `git` against `testenv.NewGitServer`; the test binary is the helper (`TestMain` child mode). The production GitHub adapter always talks to `api.github.com`, so no test mints through the built binary.
