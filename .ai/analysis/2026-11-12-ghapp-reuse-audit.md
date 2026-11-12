# GHAPP reuse audit: provenance, license obligations, reuse vs rewrite

- **Issue:** #7 (E01-T02), epic #1, Phase 0
- **Date:** 2026-11-12
- **Status:** recommendation. The owner confirms it on #7 or on the PR. #8 (E01-T03, license of agent-id) depends on it.
- **Method:** read the source only. The repositories were cloned anonymously into `$TMPDIR/agent-id/ghapp-audit/` with a temporary `HOME` and Git configuration, then deleted. Nothing ran that changes the ghapp, `gh`, or Git configuration. The only `ghapp` command used was `ghapp version`, and `go version -m` read the build information of the binary.

## Summary

1. The installed `ghapp` was built from commit `6cf72fc22665368ba067cca6f84989177316eaef` of **`github.com/cultofthecode/ghapp-cli`** (tag `v0.1.2`). That commit is **not** in `github.com/operator-kit/ghapp-cli`. It is one commit on top of the upstream head `3a5e67686a26d4916d6bf5c60cb7a8b065ab4502` ("Add license"). The commit changes only the installer, the release configuration, the README, and the self-update URL. Every component this audit evaluates is byte-identical at `6cf72fc` and at upstream `3a5e676`.
2. License: **MIT**, `Copyright (c) 2026 Ross Morsali`, from the `LICENSE` file that upstream added in `3a5e676`. The repository has no `NOTICE` or `AUTHORS` file and no per-file copyright or SPDX headers. Code is reusable only from `3a5e676` or later. The upstream tags `v0.1.0` (`4506227`) and `v0.1.1` (`d6a85d9`) predate the `LICENSE` file.
3. Recommendation: **rewrite all six components; copy no GHAPP code.** No component meets aidentity's requirements as it is: fail closed, process scope only, a configurable API base URL for the fake GitHub, a keyed and locked token cache under XDG directories, and validation of the request path. Each component is small (46–134 lines), so a rewrite costs little. With no copied code, agent-id needs no GHAPP notice, and #8 can choose any license. The license does not force this choice: MIT would allow reuse with the notice below.
4. No runtime dependency on GHAPP is proposed. aidentity does not read GHAPP's configuration, its token cache, or its keyring entry, and it does not call the `ghapp` binary (brief §1, §13 Phase 0).

## 1. Upstream identification

| Fact | Value | How to check |
|---|---|---|
| Installed binary | `ghapp 0.1.2 (commit: 6cf72fc22665368ba067cca6f84989177316eaef, built: 2026-11-11T21:40:19Z)` | `ghapp version` |
| Go module path in the binary | `github.com/operator-kit/ghapp-cli` (`mod … v0.1.2`) | `go version -m "$(command -v ghapp)"` |
| VCS stamp in the binary | `vcs.revision=6cf72fc…`, `vcs.modified=false`, `-X main.commit=6cf72fc…` | same |
| Repository that contains `6cf72fc` | `github.com/cultofthecode/ghapp-cli`, public, tag `v0.1.2` → `6cf72fc`. GitHub lists it as a standalone repository (`fork: false`), but it carries the full upstream history. | `git clone https://github.com/cultofthecode/ghapp-cli && git -C ghapp-cli tag --contains 6cf72fc` |
| Repository that the issue names | `github.com/operator-kit/ghapp-cli`, public, head `3a5e676` (2026-02-23), tags `v0.1.0`, `v0.1.1`. It does **not** contain `6cf72fc`. | `git clone https://github.com/operator-kit/ghapp-cli && git -C ghapp-cli cat-file -t 6cf72fc` fails |
| Parent of `6cf72fc` | `3a5e676` "Add license", the upstream head | `git log -2 6cf72fc` |
| Delta `3a5e676..6cf72fc` | `.github/workflows/release.yml`, `.goreleaser.yaml`, `README.md`, `install.ps1`, `install.sh`, `internal/selfupdate/check.go`, `internal/selfupdate/check_test.go`. These are release and self-update URLs only, and no Go file outside `internal/selfupdate` changes. | `git diff --stat 3a5e676 6cf72fc` |
| Components compared | `internal/auth`, `internal/tokencache`, `internal/gitcred`, `internal/cmd`, `internal/config`, `internal/authstate`, `internal/shellinit`, and `cmd` are identical between the two commits. | `git diff --quiet 3a5e676 6cf72fc -- <those paths>` exits 0 |

The issue's hypothesis is half right. `operator-kit/ghapp-cli` is the origin of the code and of the copyright. The exact revision on JARVIS comes from the `cultofthecode` copy. **Cite the revision as `cultofthecode/ghapp-cli@6cf72fc`, which is upstream `operator-kit/ghapp-cli@3a5e676` plus release-URL changes.** A reviewer can check every path below in either repository.

Upstream history at `3a5e676` has seven commits from 2026-02-13 to 2026-02-23, all by the upstream author. The `cultofthecode` copy adds `6cf72fc` and, after the audited revision, `499eb0a` (an `install.sh` checksum fix that this audit does not use).

## 2. License and notice obligations

### What the revision contains

- `LICENSE`: the MIT License, `Copyright (c) 2026 Ross Morsali`. It first appears in `3a5e676`. Nothing changes it in `6cf72fc`.
- No `NOTICE`, `AUTHORS`, `COPYING`, or third-party notice file.
- None of the 38 `.go` files has a license, copyright, or SPDX header. None of them says it is derived from other code (`git grep -i -E 'copyright|spdx|derived from|adapted from'` matches only `LICENSE`).
- GitHub reports `MIT` for both repositories.

### What MIT requires

The only condition is that "the above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software". MIT does not constrain the license of agent-id. It is compatible with any choice for #8, open source or proprietary.

### Exact notice text to keep if any component is reused

The owner may overrule the recommendation for a component. If they do, the PR that copies or closely adapts GHAPP code must ship this text verbatim, for example as `third_party/ghapp-cli/LICENSE`, and must include it in every source or binary distribution, which matters for #11 (release archives):

```text
MIT License

Copyright (c) 2026 Ross Morsali

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

Each derived file also gets a provenance line at the top. MIT does not require this line, but brief §14 asks for attribution:

```go
// Portions derived from github.com/operator-kit/ghapp-cli at commit 3a5e676
// (MIT License, Copyright (c) 2026 Ross Morsali); see third_party/ghapp-cli/LICENSE.
```

### Obligations from libraries GHAPP depends on

These obligations come from libraries, not from GHAPP code. They apply only if agent-id adopts the same libraries as its own dependencies. Adopting any of them needs the owner's yes (`AGENTS.md`, Ask first).

| Library (version used by GHAPP) | License, holder | Used by GHAPP for |
|---|---|---|
| `github.com/jferrl/go-githubauth` v1.5.1 | MIT, (c) 2024 Jorge Ferrero | App JWT and installation token sources |
| `github.com/golang-jwt/jwt/v5` v5.3.1 | MIT, (c) 2012 Dave Grijalva, (c) 2021 golang-jwt maintainers | RS256 signing (through go-githubauth) |
| `golang.org/x/oauth2` v0.34.0 | BSD-3-Clause, The Go Authors | `oauth2.TokenSource` (through go-githubauth) |
| `github.com/zalando/go-keyring` v0.2.6 | MIT, (c) 2016 Zalando SE | OS keyring |
| `gopkg.in/yaml.v3` v3.0.1 | MIT and Apache-2.0 | config files |
| `github.com/mitchellh/go-ps` v1.0.0 | MIT, (c) 2014 Mitchell Hashimoto | parent-shell detection |
| `al.essio.dev/pkg/shellescape` v1.5.1 | MIT, (c) 2016 Alessio Treglia | indirect |

agent-id already depends on `spf13/cobra` (Apache-2.0), `spf13/pflag` (BSD-3-Clause), and `inconshreveable/mousetrap` (Apache-2.0). Their license texts must ship with release binaries whatever happens to GHAPP. This belongs to #8 and #11, not to this audit.

## 3. Components: reuse or rewrite

Paths are at `cultofthecode/ghapp-cli@6cf72fc`, which equals `operator-kit/ghapp-cli@3a5e676` for these paths. The obligation for every component is the same: a rewrite carries no GHAPP obligation, and a reuse carries the notice in §2.

| Component | Upstream path (lines) | Decision | Reason | Obligations | Implemented in |
|---|---|---|---|---|---|
| JWT signing | none in GHAPP. `internal/auth/token.go:52` and `internal/auth/appinfo.go:23` call `githubauth.NewApplicationTokenSource` from the go-githubauth library | **Rewrite** in `internal/github` | GHAPP has no JWT code of its own, so there is nothing to reuse. The choice is between Go's standard library (RS256 with `crypto/rsa` and `crypto/sha256`, about 60 lines, no new dependency) and adopting go-githubauth and golang-jwt as dependencies. That choice is a question for the owner (§5). | none from GHAPP; library notices only if the owner adopts the libraries | #15 |
| Installation token client | `internal/auth/token.go` (1–68) | **Rewrite** in `internal/github` | The API URL is fixed to the library default `https://api.github.com/`, so tests cannot reach `testenv.NewFakeGitHub`. No `context`, no HTTP client timeout, no repository or permission scoping (`InstallationTokenOptions` is unused), and only token and expiry are returned. | none | #15 |
| Bot identity (slug, user id, noreply email) | `internal/auth/appinfo.go` (1–95); email format at `internal/cmd/auth.go:75-76` | **Rewrite** in `internal/github` | Same fixed `https://api.github.com` URLs and `http.DefaultClient` without a timeout. Error messages embed the raw response body (`appinfo.go:48`, `:82`). `<id>+<slug>[bot]@users.noreply.github.com` is GitHub's documented convention, not GHAPP expression. | none | #15 |
| Token cache | `internal/tokencache/cache.go` (1–92) | **Rewrite** in `internal/token` | It is one file at a predictable path in the shared temporary directory (`$TMPDIR` or `/tmp` + `ghapp-token-cache`), not the XDG cache in a `0700` directory. It is keyed only by installation id, not by host, App, installation, and scope, so two profiles overwrite each other. There is no cross-process lock, the temporary name `path + ".tmp"` is predictable, and `os.WriteFile` keeps the mode of a file that already exists. Unreadable files are silently ignored. Sharing its path would also couple aidentity to GHAPP's cache at run time. | none | #18 |
| Git credential helper | `internal/gitcred/protocol.go` (1–53), `internal/cmd/credhelper.go` (1–62) | **Rewrite** in `internal/gitint/credentialhelper` | The host check (`credhelper.go:35`, exact `github.com` + `https`) is correct but ignores `path`, so it cannot enforce repository scope with `credential.useHttpPath`. The parser drops unknown and multi-valued keys and does not reject malformed input. The decision is not a pure function, which `ARCHITECTURE.md` rule 5 requires. Installation is `git config --global` (`internal/cmd/auth.go:119`), which aidentity forbids. The parser alone could be reused, but at about 40 lines it is cheaper to rewrite than to carry a notice. | none | #22 |
| `gh` wrapper | `cmd/gh-wrapper/main.go` (1–134); shell-function variant `internal/shellinit/{bash,zsh,fish,powershell}.go`; `hosts.yml` writer `internal/cmd/auth.go:725-786` | **Rewrite** in `internal/ghint` | It **fails open**. When the token cannot be resolved, `resolveToken` returns `""` and `gh` runs without `GH_TOKEN` (`main.go:28-30`, `:40-61`; shell hooks `bash.go:20`, `zsh.go:20`), so `gh` uses the developer's personal account. That breaks brief §8 and `CODE_REVIEW.md` (fail closed). It does not remove `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, or the host variables, and it does not pin the host. Its recursion guard compares directories only. The `auth configure` path writes `~/.config/gh/hosts.yml` and edits shell startup files, which aidentity forbids (brief §10). | none | #23 |
| Keyring import | `internal/auth/keyring.go` (1–46), `internal/cmd/setup.go:67-72` (`--import-key`), `validatePEM` at `setup.go:113-124` | **Rewrite, after the keyring decision** | Keyring support is **OPEN** (`ARCHITECTURE.md`, Key decisions), so neither copying nor writing it can start now. GHAPP's code is a thin wrapper around `zalando/go-keyring` (a new dependency) with a mutable package-level provider and the fixed service name `ghapp-cli`. Reading that entry would be a runtime dependency on GHAPP's data. Validating PKCS#1 or PKCS#8 PEM is a two-call standard-library pattern. | none | #16 (after the decision) |

### Out of scope for reuse

| GHAPP area | Path | Why aidentity takes nothing |
|---|---|---|
| `auth configure` / `auth reset` / `auth status` | `internal/cmd/auth.go` | Global Git configuration (`user.name`, `user.email`, `credential.https://github.com.helper`, `url.https://github.com/.insteadOf`), `gh` `hosts.yml` edits, and PATH shim installation. Each is forbidden in aidentity (brief §9.3, §10; `AGENTS.md`, Never). aidentity uses `GIT_CONFIG_COUNT` and process environment only. |
| Shell hooks | `internal/shellinit/*` | Writes shell startup files and fails open (above). aidentity's `env` emits a process-scoped environment and never edits startup files. |
| Config and auth state | `internal/config`, `internal/authstate` | A different product configuration under `~/.config/ghapp/` that needs `yaml.v3` (a new dependency). The aidentity format is OPEN and is #9. |
| Self-update | `internal/selfupdate` | It is the only part that the `cultofthecode` commit changes. Distribution is OPEN (#11). |
| Test fixture | `internal/auth/testdata/test.pem` | A private key committed as a fixture. agent-id ignores `*.pem` and forbids committed key material; its tests generate keys at run time. |

## 4. Rules for the Phase 1 implementers

1. Implement from the GitHub REST documentation, the `gitcredentials(7)` and `git-credential(1)` protocol, `gh` environment documentation, and the aidentity specs. Do not copy GHAPP code. Because the license is MIT, reading GHAPP is not a problem, but copied or closely adapted code needs the notice.
2. A PR that copies or closely adapts GHAPP code says so in its body, adds the §2 notice and the per-file provenance line, and gets the owner's yes first (`AGENTS.md`, Ask first).
3. aidentity never reads `~/.config/ghapp/`, `$TMPDIR/ghapp-token-cache`, or the keyring service `ghapp-cli`, and never runs `ghapp`. An "import from ghapp" convenience would be a runtime dependency on GHAPP. It is not proposed, and adding it would need a product decision.

## 5. Questions for the owner

None of these blocks this audit. Each is posted on the PR and decided by the owner.

1. **Rewrite everything (recommended), or reuse some components with attribution?** This is the issue's non-blocking question. The recommendation is to rewrite all, which keeps provenance simple and leaves #8 unconstrained.
2. **Sign the App JWT and call the token API with the standard library (recommended, no new dependency), or adopt `github.com/jferrl/go-githubauth` (MIT, which pulls in `golang-jwt/jwt/v5` and `golang.org/x/oauth2`)?** The library supports `WithEnterpriseURL` and `WithHTTPClient`, so it could work against the fake GitHub. It adds three modules and their notices in place of about 100 lines of standard-library code. This decides #15.
3. **Should a provenance rule go into `CODE_REVIEW.md`** ("code copied from GHAPP without the §2 notice is a blocker")? The audit does not add it. The rule matters only if the answer to question 1 allows reuse.
