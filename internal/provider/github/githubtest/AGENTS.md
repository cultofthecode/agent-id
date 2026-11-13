# internal/provider/github/githubtest

A fake GitHub App on top of `testenv.FakeAPI`, on the GitHub side of the provider boundary (ADR #35). Only tests of `internal/provider/github` import this package (depguard, `.golangci.yml`); core tests use `providertest.Fake`. It may import `internal/testenv` and `internal/provider`.

- `NewApp(t, fake)` registers `GET /app`, `POST /app/installations/{id}/access_tokens`, `GET /users/{slug}[bot]`, and `GET /installation/repositories`. Call `testenv.Isolate(t)` first and talk to it through `testenv.NewHTTPClient(t)`.
- The App key is generated per test (`PrivateKeyPEM`, PKCS#1). Never commit a key, and write one to disk only under the test root.
- JWTs are checked as GitHub checks them: RS256 with the App key, `iss` the App ID, `exp` in the future and at most ten minutes after `iat`, `iat` at most a minute ahead. Keep `verifyJWT` a pure function with table tests of hostile input.
- Installation tokens are `testenv.FakeToken` values. `SetTokenLifetime` and `FailTokens` control expiry and the failure status; a token the fake did not issue, or one that expired, answers 401.
- A real-looking token fails the test through `testenv.CheckFakeToken`, and no message contains a token.
- `App.Provider(installationID)` puts the same fake behind `provider.Provider` and shares its state: a token it issues is valid on the fake HTTP API. It passes `providertest.RunContract`; keep it passing when the contract changes.
