# internal/testenv

Test isolation helpers. Only test files and the test helper packages (`internal/github/githubtest`) import this package (depguard, `.golangci.yml`).

- `Isolate(t)` is the first call of every test that runs aidentity code, `git`, or `gh`. It never reads or writes the real `HOME`; extend `inheritedVariables` when a new variable can carry a credential, an identity, or Git/`gh` configuration. It also points the proxy variables at a guard on 127.0.0.1 that fails the test when `git`, `gh`, or another child process tries to reach the network.
- `NewFakeGitHub(t)` is the only GitHub API a test talks to. Register the routes a test needs; unknown routes answer 404. Tokens are `FakeToken(...)` with the prefix `ghs_aidtest_`. The App endpoints live in `internal/github/githubtest`.
- `NewHTTPClient(t)` is the HTTP client for code under test: it reaches only 127.0.0.1:18000–18999 and fails the test for any other host, such as `api.github.com`.
- `CheckFakeToken(t, source, token)` is how a fake handles a credential: a real-looking token (`ghs_`, `ghp_`, `gho_`, `ghu_`, `ghr_`, `github_pat_` without `ghs_aidtest_`) fails the test. Messages name the prefix, never the token.
- `NewBareRepository`, `Clone`, and `Git` create repositories under the test root and clones under the isolated `HOME`. `NewGitServer` serves them over HTTPS with `git http-backend`, asks for Basic credentials like GitHub, accepts only fake tokens, and records every username and password; it sets `GIT_SSL_CAINFO` for the test. Give `git` credentials through `GIT_CONFIG_COUNT`, as aidentity does.
- `BuildBinary` builds a package into the test root with `GOPROXY=off` and `GOTOOLCHAIN=local`; it never runs `go install`.
- `Main(m)` is the `TestMain` of every package that runs `git`, `gh`, or aidentity: it fails the run when `~/.gitconfig`, `~/.config/git`, or the `gh` configuration changed. It hashes those files and never prints them.
- `Recorder` lets a negative test prove that a fake or a guard would fail the test.
- `Listen(t)` binds `127.0.0.1` with a port in 18000–18999 only. 19000–19999 belongs to orbit.
- Nothing here installs software, starts Docker or OrbStack, or touches the keychain.
