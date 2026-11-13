# Capability: github-provider

The GitHub adapter lets agent-id act as one installation of a GitHub App: mint installation tokens, resolve the bot's commit identity, and list the installation's repositories, without exposing a credential.

## Requirement: Installation token minting
The GitHub adapter SHALL mint an installation token with an RS256 App JWT and report the token's value and expiry as GitHub returned them.

### Scenario: Token from the fake GitHub
- WHEN the fake GitHub App has installation 7 and the adapter is built for that App and installation
- THEN `Token` sends `POST /app/installations/7/access_tokens` with an `Authorization: Bearer` JWT that `githubtest` accepts (RS256, `iss` the App ID, `exp` − `iat` ≤ 10 min), and returns the fake token with the reported `expires_at`

## Requirement: Failures map to provider errors without secrets
The GitHub adapter SHALL report every failure as one of the provider errors and SHALL NOT include a JWT, a token, or key material in the error.

### Scenario: Denied token request
- WHEN the fake answers 401, 403, 404, 429, or 503 to a token request
- THEN `Token` returns the zero `Token` and an error that matches `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrRateLimited`, or `ErrUnavailable` respectively, and the error text contains neither the JWT nor any token

### Scenario: Primary rate limit on a 403
- WHEN GitHub answers 403 with `X-RateLimit-Remaining: 0`
- THEN the error matches `ErrRateLimited` and `ErrUnavailable`, and not `ErrForbidden`

## Requirement: Bot commit identity
The GitHub adapter SHALL derive the commit name `<slug>[bot]` and email `<id>+<slug>[bot]@users.noreply.github.com` from the App's bot user.

### Scenario: Known slug and id
- WHEN the slug is `claude-dev` and the bot user id is 12345678
- THEN the name is `claude-dev[bot]` and the email is `12345678+claude-dev[bot]@users.noreply.github.com`

### Scenario: Answer for another account
- WHEN `GET /users/{slug}[bot]` answers with a different login or a type other than `Bot`
- THEN `Bot` and `Identity` fail with `ErrUnavailable` and return no identity

## Requirement: Installation repositories
The GitHub adapter SHALL list the repositories that an installation token can access, as `owner/name`.

### Scenario: Installation with two repositories
- WHEN the fake installation holds `o/a` and `o/b`
- THEN `Repositories` returns `o/a` and `o/b`

## Requirement: Credentials stay on the API host
The GitHub adapter SHALL NOT follow redirects and SHALL NOT accept a token value with characters outside visible ASCII.

### Scenario: Redirect to another host
- WHEN the API answers a token request with 307 to another listener
- THEN the adapter fails with `ErrUnavailable`, and the other listener receives no request

### Scenario: Token with a newline
- WHEN the token answer holds `"ghs_aidtest_x\nusername=evil"`, or a token without the `ghs_` prefix
- THEN `Token` fails with `ErrUnavailable` and returns the zero `Token`

Source: .ai/specs/2026-11-13-github-provider-adapter.md
