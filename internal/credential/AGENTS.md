# internal/credential

Loads the private key that signs a profile's App credentials (#16, brief §10). It knows no provider and never parses the profile config.

- `Source` is the interface: `PrivateKeyPEM()` returns the PEM bytes of one checked and parsed RSA key, because the GitHub adapter's `New` takes PEM bytes and parses them itself, enforcing at least 2048 bits (`.ai/specs/2026-11-13-github-provider-adapter.md`). The caller owns the slice and clears it. `File` (a `file:` reference) is the only implementation. A keyring backend is OPEN in `ARCHITECTURE.md`: do not add one, or a dependency for one, without the owner's decision.
- `File.Path` is absolute or `~/…`; `~/` expands to `$HOME` at load time. Any other form fails with `ErrInvalidReference`. Until #17 adds `profile.KeyRef`, the caller passes the path of a `file:` reference.
- Fail closed. The key file (or the target of a symlink) must be a regular file owned by the current user with no group or other bits (`0600` or stricter); the check runs on the opened descriptor, so the file checked is the file read. Content must be exactly one unencrypted RSA key, PKCS#1 or PKCS#8 PEM, at most 64 KiB. The type is checked with `stat` before the open as well, so opening a device has no side effect. Paths with control characters are rejected.
- Known limitation (macOS): an extended ACL (`chmod +a`) can grant read access that the mode bits do not show, and it is not detected. Detecting it needs `fgetxattr` on the descriptor through `golang.org/x/sys/unix`, a new dependency that waits for the owner's decision (PR #53, open question 5). Linux POSIX ACLs show in the group bits through the mask and are rejected.
- The decisions are pure functions with hostile-input table tests: `resolvePath`, `checkKeyFile`, `parsePrivateKey`. Keep new rules there, not in `File.PrivateKeyPEM`.
- Errors wrap one of the package sentinels, name the path (and the symlink target) and the fix, and never contain key material: never wrap a parser error, never echo file content or a PEM block type. `assertNoKeyMaterial` checks every error in the tests.
- The key stays in memory: never write it, cache it, or log it. The read buffer is cleared; only a copy of the file content is returned.
- Tests call `testenv.Isolate(t)` and generate keys at run time; no key file is committed (`*.pem` is ignored). Nothing here touches a keychain.
