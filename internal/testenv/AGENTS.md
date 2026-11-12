# internal/testenv

Test isolation helpers. Only test files import this package (depguard, `.golangci.yml`).

- `Isolate(t)` is the first call of every test that runs aidentity code, `git`, or `gh`. It never reads or writes the real `HOME`; extend `inheritedVariables` when a new variable can carry a credential, an identity, or Git/`gh` configuration.
- `NewFakeGitHub(t)` is the only GitHub a test talks to. Register the routes a test needs; unknown routes answer 404. Tokens are `FakeToken(...)` with the prefix `ghs_aidtest_`.
- `Listen(t)` binds `127.0.0.1` with a port in 18000–18999 only. 19000–19999 belongs to orbit.
- Nothing here installs software, starts Docker or OrbStack, or touches the keychain.
