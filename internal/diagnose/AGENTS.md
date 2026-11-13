# internal/diagnose

The core of `agent-id whoami` and `agent-id doctor` (#24, spec `.ai/specs/2026-11-13-whoami-doctor-output.md`, capabilities `whoami`, `doctor`, `diagnostics-redaction`). A diagnostic command reads the things that hold secrets (Git configuration, remote URLs, the environment, the key file, the token), so every change here is `risk-high`.

- **No report field holds a secret.** `Snapshot`, `Result`, `WhoamiReport` and `DoctorReport` carry names, classes, flags, and expiry; a reflection test rejects `provider.Token`, `provider.GitCredential`, and `[]byte` anywhere in them. A token stays a local variable of the handler. Never add a field for a value that "might be useful".
- **Three layers.** Collection (gatherers keep names and flags, never values), `Redact` on every string (after `AbbreviateHome`), and `Guard` on every sanitized string and on the rendered bytes; a match withholds the output with exit 3. `Output` is the only way a command renders; `Patterns` come from `profile.Registry.TokenPrefixes()`, so there is one list of prefixes.
- **Pure checks.** Each check is a function `Snapshot → []Result` in `checks.go`, with a table test (hostile input included) and a `fix:` line on every `warn` and `fail`. `Run` returns the results in catalog order; a check that panics becomes `internal` and `fail`. The ids, statuses, JSON members, and exit codes are a contract (schema 1): never remove or retype one.
- **Deferred checks.** `DeferredChecks` lists the ids that wait for exec (#20), the Git integration (#22), and the `gh` shim (#23). `Run` reports each as `skip` with the reason; a deferred check must never read as `ok`. Replace an entry with a real check when its work merges, and delete the entry.
- **No forge names.** Labels, hosts, and token prefixes come from the provider and the registry. Fixed text mentions no provider.
- Static check and fix strings must pass `Guard` (a test enforces it): no token prefix, no `Authorization:` with a value.
- Tests call `testenv.Isolate(t)` when they touch the file system or run a child process; the pure tests need no environment.
