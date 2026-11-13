# Capability: token-cache

The token manager (`internal/token`) hands every consumer a currently valid provider token for a key, from a private on-disk cache that it refreshes before expiry, without exposing the token. Source: `.ai/specs/2026-11-13-token-manager.md` (#18).

## Requirement: Serve a fresh cached token without a request
The token manager SHALL return a cached token without calling the provider while the token stays valid for longer than the refresh margin.

### Scenario: Token valid for 30 minutes
- WHEN the cache holds a token for the key that expires in 30 minutes, and the margin is 5 minutes
- THEN `Token` returns that token and the provider sees no request

## Requirement: Refresh ahead of expiry
The token manager SHALL mint, cache, and return a new token when the cached token expires within the margin, has expired, or is missing or unreadable.

### Scenario: Token expiring in 2 minutes
- WHEN the cache holds a token that expires in 2 minutes, and the margin is 5 minutes
- THEN `Token` mints one new token, writes it to the cache, and returns it

### Scenario: Corrupt entry
- WHEN the cache file of the key is truncated JSON, has an unknown member, stores another key, holds a token with a newline, or has mode `0644`
- THEN `Token` mints a new token, replaces the file with a `0600` file, and returns the new token

## Requirement: One mint across concurrent processes
The token manager SHALL serialize refreshes of one key across processes, so that concurrent requests on a missing or stale entry mint one token.

### Scenario: 20 processes on an empty cache
- WHEN 20 separate processes request a token for the same key at the same time on an empty cache
- THEN the provider sees exactly one mint, and every process gets the same token

## Requirement: Entries never cross keys
The token manager SHALL return a cached token only for the key it was cached under.

### Scenario: Two installations
- WHEN tokens are requested for two sources that differ only in their token subject (two installations of one App)
- THEN each gets its own token, and neither cache entry is returned for the other

## Requirement: Fail closed
The token manager SHALL return an error and no token when a valid token cannot be obtained, and SHALL NOT include a token in any error.

### Scenario: Refresh denied
- WHEN the cached token expires within the margin and the provider fails with `ErrUnauthorized`
- THEN `Token` returns the zero token and an error that matches `provider.ErrUnauthorized` and contains neither the old nor any other token

### Scenario: Insecure cache directory
- WHEN the cache directory has mode `0755`, is a symlink, or belongs to another user
- THEN `Token` fails with `ErrInsecureCache` without reading the entry or calling the provider

## Requirement: Private, atomic cache files
The token manager SHALL store each entry in a `0600` file inside a `0700` directory and SHALL replace it atomically.

### Scenario: Fresh cache
- WHEN `Token` mints on an empty cache
- THEN the directory has mode `0700`, the entry has mode `0600`, and no temporary file remains
