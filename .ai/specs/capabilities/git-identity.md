# Capability: git-identity

Bot author and committer, and personal signing off, inside an agent context. Implemented by `internal/gitint` (pure) and wired by `internal/activation` (#21). Attribution, not proof (brief §10): `git commit --author`, `-S`, `git tag -s`, `git -c commit.gpgSign=true`, and a shell startup file that unsets the variables bypass it; bot commits are unsigned, never "verified"; a branch that requires signed commits rejects them until agent signing (#28).

## Requirement: Bot author and committer in agent mode
The system SHALL make every commit created by an ordinary `git commit` inside an agent context carry the profile's bot name and email as author and committer, whatever the global, repository, `includeIf`, or `git -c user.*` configuration says.

### Scenario: Personal identity configured
- WHEN the isolated global config sets `user.name=Dev`, `user.email=dev@example.com`, `author.email=a@example.com`, and the test runs `agent-id exec --profile claude -- git commit --allow-empty -m x` in a fresh repository
- THEN `git log -1 --format='%an <%ae>|%cn <%ce>'` prints `claude-dev[bot] <12345678+claude-dev[bot]@users.noreply.github.com>|claude-dev[bot] <12345678+claude-dev[bot]@users.noreply.github.com>`

### Scenario: Inherited identity variables
- WHEN the parent environment has `GIT_AUTHOR_NAME=Dev` and `GIT_COMMITTER_EMAIL=dev@example.com`
- THEN the commit made inside the context still has the bot as author and committer

## Requirement: Personal signing off in agent mode
The system SHALL turn off commit signing, tag signing, and forced signing of tags with a message inside an agent context, so that no inherited signing configuration runs a signer.

### Scenario: Global SSH signing
- WHEN the global config sets `commit.gpgSign=true`, `tag.gpgSign=true`, `tag.forceSignAnnotated=true`, `gpg.format=ssh`, `user.signingKey`, and `gpg.ssh.program` points to a fake signer that logs every call, and the agent runs `git commit --allow-empty -m x`, `git tag -a v1 -m v1`, and `git tag -m v2 v2` (no `-a`)
- THEN the fake signer's log is empty, `git cat-file commit HEAD` has no `gpgsig` header, and neither `git cat-file tag v1` nor `git cat-file tag v2` has a signature block

### Scenario: forceSignAnnotated is load-bearing
- WHEN the same fixture runs `git tag -m v3 v3` in an environment built from a plan without the `tag.forceSignAnnotated=false` entry (a test-only plan)
- THEN the fake signer is called, proving that the entry, not `tag.gpgSign=false`, stops it

### Scenario: Repository-level signing
- WHEN only the repository config sets `commit.gpgSign=true`
- THEN the commit made inside the context is unsigned and the signer is not called

## Requirement: Inherited process configuration preserved
The system SHALL keep every well-formed inherited `GIT_CONFIG_COUNT` entry effective inside the context, except where one of its own keys is set later by agent-id, which then wins as the last value.

### Scenario: Unrelated inherited entry
- WHEN the parent has `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=core.abbrev`, `GIT_CONFIG_VALUE_0=12`
- THEN `git config core.abbrev` inside the context prints `12`

### Scenario: Inherited signing entry is overridden
- WHEN the parent has `GIT_CONFIG_COUNT=1`, `GIT_CONFIG_KEY_0=commit.gpgSign`, `GIT_CONFIG_VALUE_0=true`
- THEN the last value of `git config --get-all commit.gpgSign` inside the context is `false` and the inherited `true` is still listed before it, `git config commit.gpgSign` prints `false`, and the commit is unsigned

## Requirement: Developer terminal unchanged
The system SHALL NOT change the identity or signing of commits made outside the agent context.

### Scenario: Commit after the agent
- WHEN, after the agent's commit, the test commits in the same repository with the parent environment
- THEN author and committer equal what `git var GIT_AUTHOR_IDENT` and `git var GIT_COMMITTER_IDENT` report in the parent environment (name and email only), neither is the bot, the fake signer was called once, and the global and repository config files hash the same as before the agent ran

## Requirement: No start without a valid identity
The system SHALL refuse to start the command, with exit status 125, when the provider cannot supply the bot identity from the profile or the identity fails the check, and SHALL NOT contact the provider to obtain it.

### Scenario: Malformed cached identity
- WHEN the provider returns the name `claude\nmallory` (a test fake)
- THEN `exec` exits 125 with `ErrIdentityInvalid`, the message does not contain the name, and the command does not run

### Scenario: No network for the identity
- WHEN `exec` starts a command for a profile with a cached identity while the fake GitHub records requests
- THEN the fake GitHub received no request
