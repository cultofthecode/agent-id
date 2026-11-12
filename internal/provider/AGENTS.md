# internal/provider

The boundary between agent-id's core and a code host (ADR #35). GitHub is the first adapter (`internal/provider/github`, #15); GitLab and Bitbucket are later implementations of the same interface.

- Core packages import this package, never `internal/provider/github` or anything under it. Only the composition root (`cmd/aidentity/main.go` until the rename of #6) wires an adapter. depguard enforces it, test files included.
- The interface is provisional: one method per responsibility the ADR names (tokens, commit identity, the per-host Git credential, CLI integration, host). Change it together with `providertest.RunContract` and every adapter.
- Failures are reported as `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, or `ErrUnavailable`, wrapped with `%w`, so core fails closed without knowing the host. A failed `Token` returns the zero `Token`.
- `Token` and `GitCredential` redact their secret under every `fmt` verb. `CLIEnvironment` returns a secret; never print it.
- `providertest.Fake` is the provider of core tests. `providertest.RunContract` runs against every provider: the fake, the fake GitHub App (`githubtest.App.Provider`), and the GitHub adapter.
