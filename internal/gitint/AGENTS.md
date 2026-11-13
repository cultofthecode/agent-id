# internal/gitint

The Git data of an agent context: the author and committer variables and the signing-off entries. Spec: `.ai/specs/2026-11-13-git-commit-identity.md` (#21); requirements in `.ai/specs/capabilities/git-identity.md`. Always `risk-high`: it decides whose name a commit carries and whose key signs it.

- **Pure.** Functions take values and return values; no file, no network, no environment read, no shared state (every call returns fresh slices and maps).
- **Names no forge.** It imports `internal/provider` for the `Identity` type and nothing else from the repository; `activation` imports it, never the reverse.
- **Never echoes a value.** `ErrIdentityInvalid` names the field and the rule, not the name or the email: a malformed value may be an injection attempt.
- **`GIT_CONFIG_*` only through activation's append rule** (`activation.Build`). `gitint` returns entries; it never numbers them.
- **Order is fixed:** the identity group (`commit.gpgSign`, `tag.gpgSign`, `tag.forceSignAnnotated`), then #22's transport group.
- Attribution, not proof (brief §10): `--author`, `-S`, `git -c`, and a shell startup file bypass it. No text may claim that bot commits are verified.
