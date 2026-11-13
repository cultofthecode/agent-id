# internal/provider

The boundary between agent-id's core and a code host (ADR #35). GitHub is the first adapter (`internal/provider/github`, #15); GitLab and Bitbucket are later implementations of the same interface.

- Core packages import this package, never `internal/provider/github` or anything under it. Only the composition root (`cmd/agent-id/main.go`) wires an adapter. depguard enforces it, test files included.
- One method per responsibility the ADR names (tokens, commit identity, the per-host Git credential, CLI integration, host); the GitHub adapter (#15) settled it. Change it together with `providertest.RunContract` and every adapter.
- Failures are reported as `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrUnavailable`, or `ErrRateLimited`, wrapped with `%w`, so core fails closed without knowing the host. `ErrRateLimited` wraps `ErrUnavailable`; no other class matches another. A failed `Token` returns the zero `Token`.
- `TokenSubject()` names, without a secret, whose token `Token` mints (GitHub: `app-<id>-installation-<id>`); it matches `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, never changes, and keys the token cache (`internal/token`). Two installations must report two subjects.
- `Token` and `GitCredential` redact their secret under every `fmt` verb. `CLIEnvironment` returns a secret; never print it.
- `Onboarder` (#19) is the provider's setup: `SetupFields` (flag names `^[a-z][a-z0-9-]*$`, all required), a pure `Check` that refuses token-, JWT- and PEM-shaped values without echoing them, and `Onboard`, which calls `Check` first and returns a `SetupResult` with no secret in it. `providertest.FakeOnboarder` serves core tests; `providertest.RunOnboarderContract` runs against every onboarder.
- `providertest.Fake` is the provider of core tests. `providertest.RunContract` runs against every provider: the fake, the fake GitHub App (`githubtest.App.Provider`), and the GitHub adapter (`internal/provider/github`).
