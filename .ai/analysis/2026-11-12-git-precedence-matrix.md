# Git configuration precedence and wrapper bypass matrix for agent mode

- **Issue:** #13 (E02-T01), epic #2, Phase 1
- **Date:** 2026-11-12
- **Status:** research result and proposal. It feeds `internal/gitint` and the credential helper (E02-S08). The OPEN row "Supported Git paths and documented bypasses" of `ARCHITECTURE.md` stays OPEN until the owner accepts or changes it. Section 8 lists the points that need the owner's decision.
- **Inputs:** `PRODUCT_BRIEF.md` §9.3, §10, §14; `ARCHITECTURE.md` (Git integration, Security model)
- **Experiment:** [`2026-11-12-git-precedence-matrix.sh`](2026-11-12-git-precedence-matrix.sh)

## 1. Summary

agent-id can neutralize most inherited configuration that would make an agent use the developer's credentials or identity, but only if its `GIT_CONFIG_*` entries are **scoped to the GitHub URL** and **appended after** inherited entries. The findings below come from the experiment (row ids refer to section 4):

1. **Order of sources.** `GIT_CONFIG_COUNT` entries come after system, global, `includeIf`, and repository configuration, and before `GIT_CONFIG_PARAMETERS` and `git -c` (P1–P3). An inherited `GIT_CONFIG_PARAMETERS` therefore overrides agent-id (C19b), and an agent's own `git -c` always can (C20b).
2. **`credential.*` follows config order, not URL specificity.** A `credential.https://github.com.helper=` reset clears every helper collected before it for that host, including generic helpers, Apple Git's system `osxkeychain`, `includeIf` helpers, and path-scoped helpers (C3, C6, C9, C11, C14, C16). A generic reset is not needed, and it would also remove the developer's helpers for other hosts.
3. **`http.*` follows URL specificity.** A generic `http.extraHeader=` does not clear a host-scoped header (H4). A host-scoped reset clears generic and host-scoped headers (H5, H8) but not a path-scoped header (H7). So agent-id neutralizes the `actions/checkout` style header but can only *detect* a path-scoped one.
4. **URL rewrites use the longest matching prefix (the inherited rule wins a tie) and are applied once.** agent-id's SSH→HTTPS rewrites fix SSH remotes and SSH `pushurl`s (U1, U10, U13). A personal `insteadOf` or `pushInsteadOf` that rewrites `https://github.com/` to SSH wins against anything agent-id can add (U2–U8). The agent then pushes over SSH with the developer's key (K1, K2). This is the largest gap: it can be detected but not neutralized through Git configuration.
5. **When the agent-id helper refuses, Git falls back to askpass.** If the helper answers nothing, Git asks `GIT_ASKPASS`, `core.askPass`, or `SSH_ASKPASS`. An IDE's askpass can then supply the developer's session (A1, A2, A4). A helper that refuses with `quit=1` stops this on both Git versions (A5, X5). `credential.interactive=false` does **not** stop it on Git 2.43 (A6).
6. **Identity and signing are fully neutralized** by the author and committer variables and `commit.gpgSign`/`tag.gpgSign`/`tag.forceSignAnnotated=false` (I1–I3, I6, S2, S4, S6–S8). The exceptions are the agent's own `--author` and `-S` (I4, S3).
7. **Shell startup files run after agent-id.** `zsh -c` always reads `~/.zshenv`. Interactive and login shells read more files (R1–R7). Any of them can unset the entries or shadow `git`/`gh` with a function (R8–R10). A shell function `gh` that injects another token exists on the machine where this research ran.
8. **`gh auth git-credential` as a helper** returns `GH_TOKEN` when the variable is set (GH1). Without it, the helper returns the personal token from `hosts.yml` (GH2). The scoped reset removes this helper (C9), so it matters only if the agent adds it again.

The complete recommended set (section 5) was run against one combined hostile configuration and held on both platforms (X1–X6).

## 2. How to reproduce

```sh
sh .ai/analysis/2026-11-12-git-precedence-matrix.sh
```

The script builds a throwaway isolated environment under `$TMPDIR/agent-id/` with the variables of `AGENTS.md` (Test isolation): `HOME`, the XDG directories, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_NOSYSTEM=1`, `GH_CONFIG_DIR`, `TMPDIR`. It unsets inherited tokens, identity, and Git configuration variables, and it removes everything on exit.

- **Fakes.** Credential helpers, the SSH client, the signer, and askpass are fake scripts that only log their calls. "osxkeychain" is a fake helper named `osxkeychain-sim`; the real one is never called. Every token has the prefix `ghs_aidtest_`.
- **Network.** None beyond a header-capture server that the script builds with Go and binds on `127.0.0.1` with a port in 18000–18999. The server answers 401 to every request and logs the decoded `Authorization` headers.
- **Method.** Credential rows use `git credential fill`, which runs the same helper list as a transport. H6 and X6 confirm that list on a real HTTP transport. URL rows use `git ls-remote --get-url` and `git remote get-url --push`. SSH rows push with a fake `core.sshCommand`.
- **gh row GH2.** It runs only with `AIDTEST_GH_STORE=1`. Without `GH_TOKEN`, `gh` may look up the OS keyring, and on macOS that is the real login keychain. GH2 was run only in the Linux container, where `gh` has no keyring.

Platforms tested, with the same script revision:

| Platform | Git | Shells | gh |
|---|---|---|---|
| macOS 27 arm64 (owner's machine) | 2.54.0 (Apple Git-157) | bash 3.2.57, zsh 5.9 | 2.103.3 (GH1 only) |
| Ubuntu 24.04 arm64 in Docker container `aid-git-matrix` (the CI runner's distribution) | 2.43.0 | bash 5.2.21, zsh 5.9 | 2.45.0 |

The results are identical on both platforms except A6 (marked) and GH2 (Linux only).

For Linux, run the script in a disposable container: install `git zsh gh`, mount the script, and pass `AIDTEST_GH_STORE=1`. If the container has no Go, pass `AIDTEST_CAPTURE_BIN` with a capture server built on the host from the Go source in the script (`GOOS=linux`).

## 3. Precedence model

What the experiment established, as rules that `internal/gitint` and `doctor` can rely on:

| Rule | Evidence |
|---|---|
| Configuration is read in this order, and the last value of a single-valued key wins: system → global (an `includeIf` file at the position of its include line) → repository → `GIT_CONFIG_COUNT` → `GIT_CONFIG_PARAMETERS` → `git -c`. `git config --show-scope` reports the last three as `command`. | P1–P3 |
| A child of `git -c`, such as an alias or a hook, inherits both the `GIT_CONFIG_COUNT` entries and the `-c` values. | G5 |
| `credential.<url>.*` entries apply in config order to every request they match, whether they match generically, by host, or by host and path. An empty `helper` resets the list collected so far. `useHttpPath` is last-one-wins, in order (C22, C25). A path-scoped helper matches even without `useHttpPath` (C10). | C2–C16, C22–C25 |
| `http.<url>.*` entries use URL specificity: a less specific entry is ignored once a more specific one has matched, even when it comes later. An empty `extraHeader` clears only the list at its own specificity and below. | H2, H4, H5, H7, H8 |
| `url.<base>.insteadOf` and `pushInsteadOf` use the longest matching prefix: a longer inherited rule beats a later aid rule (U5), and with equal length the inherited rule won (U4, U8). A rewrite is applied once and never chained (U3). `insteadOf` is also applied to an explicit `pushurl` (U10). | U1–U13 |
| The author and committer variables override `user.*`, `author.*`, `committer.*`, and `git -c user.email`, and they satisfy `user.useConfigOnly`. | I1–I3, I6 |
| `GIT_SSH_COMMAND` overrides `core.sshCommand`. | K3, K4 |
| When every helper answers nothing, Git asks `GIT_ASKPASS`, then `core.askPass`, then `SSH_ASKPASS`. A helper answer of `quit=1` stops the lookup. An empty `core.askPass` disables only the configured askpass. | A1–A5 |
| An inherited `GIT_CONFIG_COUNT` that is malformed (missing key or value, not a number) makes every Git command fail. | G2–G4 |

## 4. The matrix

**Classification**, for every configuration that leaks a personal credential or identity (acceptance criterion 2):

- **Neutralized** — the recommended entries of section 5 remove the effect. The row shows this.
- **Doctor** — Git configuration cannot neutralize it; the proposed `doctor` check of section 6 detects it.
- **Bypass** — a documented known bypass: the agent's own action, or a mechanism outside Git configuration. Where a doctor check also helps, the row says so.

"aid" in a row means the agent-id entries under test. The aid helper is a fake that answers `username=aid`.

### P — precedence

| Row | Configuration | Observed | Class |
|---|---|---|---|
| P1 | `aidtest.k` set in system, global, `includeIf` gitdir, local, `GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS`, `-c` | order: system, global, global (include), local, command (count), command (parameters), command (`-c`) | — |
| P2 | same, single-valued read | `-c` value | — |
| P3 | same without `PARAMETERS` and `-c` | `GIT_CONFIG_COUNT` value beats local | — |

### C — credential helpers (`git credential fill`, `https://github.com/o/r.git`)

| Row | Inherited configuration | Without aid | With aid | Class |
|---|---|---|---|---|
| C1–C3 | system `credential.helper` (Apple Git's system config sets `osxkeychain`) | system helper answers (C1) | generic reset → aid (C2); github.com-scoped reset → aid (C3) | Neutralized |
| C4–C6 | global + repository generic helpers | global helper answers (C4) | generic → aid (C5); scoped → aid (C6) | Neutralized |
| C7–C9 | `credential.https://github.com.helper` (as `gh auth setup-git` writes it) | it answers (C7) | generic → aid (C8); scoped → aid (C9) | Neutralized |
| C10–C12 | path-scoped `credential.https://github.com/myorg.helper` | it answers for `myorg/r`, even without `useHttpPath` (C10) | host-scoped reset → aid (C11, C12) | Neutralized |
| C13–C14 | `includeIf "gitdir:…"` file with a scoped helper | it answers (C13) | aid (C14) | Neutralized |
| C15–C16 | `includeIf "hasconfig:remote.*.url:https://github.com/**"` file with a helper | it answers (C15) | aid (C16) | Neutralized |
| C17–C18 | inherited `GIT_CONFIG_COUNT` entry with a helper | it answers (C17) | aid appended after it → aid (C18) | Neutralized |
| C19 | inherited `GIT_CONFIG_PARAMETERS` that adds a helper | — | aid answers first (C19) | Neutralized |
| C19b | inherited `GIT_CONFIG_PARAMETERS` that resets and adds a helper | — | **inherited helper answers** | Doctor; agent-id's handling is owner decision O2 |
| C20 | agent runs `git -c credential.helper=X` | — | aid answers first | Neutralized |
| C20b | agent runs `git -c credential.https://github.com.helper= -c …helper=X` | — | **X answers** | Bypass (agent's action) |
| C21 | URL with embedded `user:token` | — | **embedded credentials used; no helper is called** | Doctor (D2) |
| C22–C25 | `useHttpPath`: global scoped `false`, aid generic `true` | — | aid gets `path` when aid's `true` comes last (C22, C23); no `path` without it (C24) or when a later `false` follows (C25) | Neutralized when aid sets it last |

### A — fallback after the helper list

| Row | Configuration | Observed | Class |
|---|---|---|---|
| A1 | aid helper answers nothing; `GIT_ASKPASS` set (as IDE terminals do) | **askpass answers** | Neutralized by `quit=1` (A5) |
| A2 | aid helper answers nothing; `core.askPass` in global config | **askpass answers** | Neutralized by `quit=1`, or by `core.askPass=` (A3) |
| A3 | A2 + aid `core.askPass=` | no answer | Neutralized |
| A4 | aid helper answers nothing; `SSH_ASKPASS` set | **askpass answers** | Neutralized by `quit=1` |
| A5 | aid helper answers `quit=1`; `GIT_ASKPASS` and a personal helper set | no answer, nothing else called | Neutralized |
| A6 | aid helper answers nothing + `credential.interactive=false` | macOS/Git 2.54: no answer. **Ubuntu/Git 2.43: askpass answers** | Not usable on 2.43; rely on `quit=1` |

### U — remote URLs and rewrites (effective fetch URL | push URL)

aid rewrites in U1–U12: `url.https://github.com/.insteadOf` = `git@github.com:` and `ssh://git@github.com/` (the two forms that `ARCHITECTURE.md` names). U13 adds the extended set of section 5.

| Row | Remote or inherited configuration | Observed | Class |
|---|---|---|---|
| U1 | `git@github.com:o/r`, `ssh://git@github.com/o/r` | rewritten to `https://github.com/o/r.git` | Neutralized |
| U1 | `github.com:o/r`, `ssh://github.com/o/r`, `ssh://git@ssh.github.com:443/o/r`, `git+ssh://git@github.com/o/r` | **not rewritten by the two forms** | Neutralized by the extended set (U13) |
| U13 | the four above, plus `ssh+git://git@github.com/` and `git://github.com/` | all rewritten to HTTPS by the extended set | Neutralized |
| U2–U4 | personal `url.git@github.com:.insteadOf=https://github.com/` | **SSH for fetch and push**, even with aid rewrites and an aid identity rule `https://github.com/` → itself | Doctor (D1); SSH fallback is owner decision O3 |
| U5 | personal longer-prefix rule `https://github.com/myorg/` → SSH | **SSH**; a longer prefix always wins | Doctor (D1) |
| U6–U8 | personal `url.git@github.com:.pushInsteadOf=https://github.com/` | **push URL is SSH**, even with an aid `pushInsteadOf` identity rule | Doctor (D1) |
| U9–U10 | repository `remote.origin.pushurl=git@github.com:o/r.git` | SSH without aid (U9); HTTPS with aid rewrites (U10) | Neutralized |
| U11 | `pushurl` with embedded `user:token` | **credentials in the push URL** | Doctor (D2) |
| U12 | remote through an `~/.ssh/config` `Host` alias (`github-personal:o/r`) | **not rewritable**; SSH with the alias's key | Doctor (D1 with `ssh -G`) |

### K — SSH transport

| Row | Configuration | Observed | Class |
|---|---|---|---|
| K1 | remote `github.com:o/r` not rewritten; personal `core.sshCommand` | personal SSH command runs | Neutralized by the extended rewrites; otherwise Doctor (D1) |
| K2 | remote `git@github.com:o/r`, no aid | personal SSH command runs | Neutralized by the rewrites |
| K3 | aid `core.sshCommand=false` | SSH blocked, push fails (exit 128) | Possible fail-closed option; it would block SSH to every host (O3) |
| K4 | K3 + inherited `GIT_SSH_COMMAND` | **personal SSH command runs**; the variable wins | Bypass unless agent-id also handles the variable (O3) |

### H — HTTP headers on a real transport

| Row | Configuration | Observed `Authorization` | Class |
|---|---|---|---|
| H1 | global generic `http.extraHeader: Authorization: Bearer …` | **personal header sent**; no helper is asked | — |
| H2 | H1 + aid generic `http.extraHeader=` | none | Neutralized |
| H8 | H1 + aid host-scoped reset only | none | Neutralized (host-scoped reset is enough) |
| H3 | repository `http.<host>/.extraheader` (written by `actions/checkout`) | **header sent** | — |
| H4 | H3 + aid generic reset | **header still sent** | — |
| H5 | H3 + aid host-scoped reset | none | Neutralized |
| H7 | repository path-scoped `http.<host>/o/r.git.extraheader` + aid generic and host resets | **header sent** | Doctor (D3) |
| H6 | personal generic helper + aid host-scoped helper reset, real transport | first request without credentials, retry with `basic:aid`; only aid is called | Neutralized |

### I — author and committer identity

aid sets `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` to the bot.

| Row | Configuration | Observed | Class |
|---|---|---|---|
| I1 | global `user.name`/`user.email` | author and committer are the bot | Neutralized |
| I2 | + global `author.email`/`committer.email` | the bot | Neutralized |
| I3 | agent runs `git -c user.email=…` | the bot | Neutralized |
| I4 | agent runs `git commit --author=…` | **author is the given person**; committer is the bot | Bypass (agent's action; metadata is attribution, not proof, brief §9.3) |
| I5–I6 | `user.useConfigOnly=true` and no `user.email` | commit fails without aid (I5); succeeds as the bot with aid (I6) | Neutralized |

### S — signing (fake signer)

| Row | Configuration | Observed | Class |
|---|---|---|---|
| S1 | global `commit.gpgSign=true`, `gpg.format=ssh`, `user.signingKey` | **signer runs** | — |
| S2 | S1 + aid `commit.gpgSign=false` | no signer | Neutralized |
| S3 | agent runs `git commit -S` | **signer runs** | Bypass (agent's action) |
| S4 | repository `commit.gpgSign=true` + aid `false` | no signer | Neutralized |
| S5–S6 | global `tag.gpgSign=true` (OpenPGP) | signer runs (S5); none with aid `tag.gpgSign=false` (S6) | Neutralized |
| S7–S8 | global `tag.forceSignAnnotated=true` | no signer with aid `tag.gpgSign=false` (S7) or with `tag.forceSignAnnotated=false` added (S8) | Neutralized; the set keeps both entries |

### G — inherited `GIT_CONFIG_COUNT` and `GIT_CONFIG_PARAMETERS`

| Row | Configuration | Observed | Class |
|---|---|---|---|
| G1 | inherited `GIT_CONFIG_COUNT=2`; aid appends from index 2 (count 5) | inherited entries kept; aid helper answers | Neutralized (append, never replace) |
| G2 | `COUNT=2`, `KEY_1` missing | `error: missing config key GIT_CONFIG_KEY_1` | agent-id must refuse a malformed inherited set (fail closed) |
| G3 | `COUNT=1`, `VALUE_0` missing | `error: missing config value GIT_CONFIG_VALUE_0` | same |
| G4 | `COUNT=x` | `error: bogus count in GIT_CONFIG_COUNT` | same |
| G5 | `git -c` with `COUNT` set: what a child (alias, hook) sees | both `COUNT` and `-c` entries | — |

### R — shell startup files of the agent's shell

Each startup file appends its name to a marker variable.

| Row | Shell | Files read | Class |
|---|---|---|---|
| R1 | `bash -c` | none | — |
| R2 | `bash -ic` | `.bashrc` | — |
| R3 | `bash -lc` | `.bash_profile` | — |
| R4 | `bash -c` with inherited `BASH_ENV` | the `BASH_ENV` file | — |
| R5 | `zsh -c` | `.zshenv` (always) | — |
| R6 | `zsh -ic` | `.zshenv`, `.zshrc` | — |
| R7 | `zsh -lc` | `.zshenv`, `.zprofile` | — |
| R8 | `zsh -c` with `unset GIT_CONFIG_COUNT` in `.zshenv` | **the effective helper list is only the personal helper** | Bypass; Doctor (D6) when `doctor` runs inside the agent's shell |
| R9–R10 | `.zshrc` or `.bashrc` defines a `gh` function | `gh` resolves to the function | Bypass (functions and aliases are invisible to a child process) |

Not verified here: which startup files Claude Code's or Codex's shell tool reads. That belongs with the Claude Code `SessionStart` integration ticket.

### GH — `gh auth git-credential` as the inherited helper

| Row | Configuration | Observed | Class |
|---|---|---|---|
| GH1 | global `credential.https://github.com.helper=!gh auth git-credential`; `GH_TOKEN` set | returns `x-access-token` with the `GH_TOKEN` value | — |
| GH2 | same; no `GH_TOKEN`; personal token in `hosts.yml` (Linux only) | **returns the personal token** | Neutralized by the scoped helper reset (C9). It returns if the `gh` injection leaves `GH_TOKEN` out of the child environment and the agent adds the helper back (C20b). |

### X — the recommended set against a combined hostile configuration

Hostile configuration: system `osxkeychain-sim`; global generic, host-scoped, and path-scoped helpers; global generic `extraHeader`; `core.askPass`; personal `user.*`; SSH commit signing; `tag.gpgSign` and `tag.forceSignAnnotated`; a repository helper; `GIT_ASKPASS`; an inherited `GIT_CONFIG_COUNT` entry. Section 5's set and the bot identity variables are then added.

| Row | Check | Observed |
|---|---|---|
| X1 | `credential fill` for `github.com/o/r` | only aid is called; aid answers |
| X2 | remote `git@github.com:o/r.git` | HTTPS for fetch and push |
| X3 | commit and annotated tag | both succeed, no signer, author and committer are the bot |
| X4 | inherited `GIT_CONFIG_COUNT` entry | kept |
| X5 | aid refuses with `quit=1` | no answer; neither askpass is called |
| X6 | real transport, set built for the capture server URL, plus a repository `actions/checkout` header | first request without `Authorization`, retry with `basic:aid` only |

## 5. Recommended `GIT_CONFIG_*` set

agent-id appends these entries after any inherited, well-formed `GIT_CONFIG_COUNT` entries and sets `GIT_CONFIG_COUNT` to the new total (G1). `<host>` is the profile's host, `https://github.com` for the MVP. The helper command line is owned by E02-S08 and is shown only as a placeholder.

| # | Key | Value | Why | Rows |
|---|---|---|---|---|
| 1 | `credential.https://github.com.helper` | *(empty)* | Resets every helper collected so far for the host: system `osxkeychain`, global, `includeIf`, repository, path-scoped, inherited `COUNT`. It leaves other hosts alone. | C3, C6, C9, C11, C14, C16, C18 |
| 2 | `credential.https://github.com.helper` | `!<absolute path to agent-id> git-credential` *(E02-S08)* | The only helper for the host. | X1, H6 |
| 3 | `credential.https://github.com.useHttpPath` | `true` | The helper sees the repository path and can check repository scope. It must come after any inherited value. | C22–C25 |
| 4 | `http.https://github.com/.extraHeader` | *(empty)* | Clears generic and host-scoped `Authorization` headers, including the one `actions/checkout` writes. A path-scoped header remains (D3). | H5, H8, X6 |
| 5–11 | `url.https://github.com/.insteadOf` | `git@github.com:`, `ssh://git@github.com/`, `github.com:`, `ssh://github.com/`, `ssh://git@ssh.github.com:443/`, `git+ssh://git@github.com/`, `ssh+git://git@github.com/` (one entry each) | Moves every SSH form of a GitHub remote, including an explicit SSH `pushurl`, to HTTPS, where the helper decides. | U1, U10, U13, X2 |
| 12 | `commit.gpgSign` | `false` | Turns off inherited personal commit signing (brief §9.3). | S2, S4 |
| 13 | `tag.gpgSign` | `false` | Turns off inherited tag signing. | S6 |
| 14 | `tag.forceSignAnnotated` | `false` | Turns off forced signing of annotated tags. | S7, S8 |
| 15 | `core.askPass` | *(empty)* | Disables a configured askpass program for every host. Defense in depth behind `quit=1`. | A3 |

With the identity variables `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, `GIT_COMMITTER_NAME`, `GIT_COMMITTER_EMAIL` (I1–I3, I6).

**Requirements on the helper (E02-S08).** For a request for the profile's host that it refuses or cannot serve, the helper answers `quit=1`. If it answered nothing, Git would fall back to `GIT_ASKPASS`, `core.askPass`, or `SSH_ASKPASS` (A1, A2, A4), which can be an IDE that holds the developer's session. `quit=1` works on Git 2.43 and 2.54 (A5); `credential.interactive=false` does not work on 2.43 (A6).

**Requirements on `agent-id exec` and `env`.**

- Append, never replace (G1).
- Refuse a malformed inherited `GIT_CONFIG_COUNT` with a clear error, instead of starting a child in which every Git command fails (G2–G4).
- Applying the set again in a nested context is harmless, because each reset clears the previous helper.

**Left out deliberately:**

- A generic `credential.helper=` or `http.extraHeader=` reset. The scoped resets are enough for GitHub (C3, C6, H8), and generic resets would remove the developer's helpers and headers for other hosts.
- `url.<base>.pushInsteadOf` identity rules, and an identity `insteadOf`. They do not beat a personal rule (U4, U8).
- `git://` rewrites. GitHub no longer serves `git://`, and that protocol carries no credential.
- `core.sshCommand`. See O3.
- `credential.interactive`. Version-dependent (A6).

## 6. Proposed `doctor` checks

These rows cannot be neutralized through Git configuration. Each check follows the rule of `ARCHITECTURE.md`: a pure function over the output of `git config --list --show-scope --show-origin`, the remote URLs, and the environment, with table tests that include hostile input. Each check prints the source file of the offending entry and never a value that contains a credential.

| Id | Check | Rows |
|---|---|---|
| D1 | For each remote, the effective fetch and push URLs (`git ls-remote --get-url`, `git remote get-url --push`) must be `https://<host>/…`. A URL that resolves to GitHub over SSH fails: host `github.com` or `ssh.github.com`, or an SSH alias whose `ssh -G <alias>` hostname is one of them. The check names the `insteadOf`/`pushInsteadOf` rule responsible. | U2–U8, U12, K1 |
| D2 | A remote URL or `pushurl` with credentials in the user-info part fails. | C21, U11 |
| D3 | `git config --get-urlmatch http.extraheader <remote URL>` returns an `Authorization` header that survives the set. | H7 |
| D4 | `GIT_CONFIG_PARAMETERS` is present in the environment, or the helper list computed for `https://<host>/<repo>` is not exactly the agent-id helper. | C19b |
| D5 | `GIT_SSH_COMMAND` or `core.sshCommand` is set while a GitHub remote is not HTTPS (informational once D1 passes). | K1, K4 |
| D6 | Run inside the agent's context: the expected `GIT_CONFIG_*` entries and identity variables are present and unchanged. Catches a startup file that unsets or overrides them. | R8 |
| D7 | `GIT_ASKPASS`, `SSH_ASKPASS`, or `core.askPass` is set (informational; `quit=1` covers it). | A1–A4 |

## 7. Known bypasses (documentation)

None of these can be prevented in local mode. They match brief §10: local mode is attribution and convenience, not containment.

- The agent changes Git's configuration for its own command: `git -c credential.<host>.helper= -c …` (C20b), `git commit --author` (I4), `git commit -S` (S3), `git push` to a URL with embedded credentials (C21).
- A personal `insteadOf`/`pushInsteadOf` or an SSH host alias sends GitHub traffic over SSH with the developer's key (U2–U8, U12). `doctor` detects it (D1); whether agent-id blocks it is O3.
- A path-scoped `http.<url>.extraHeader` (H7). `doctor` detects it (D3).
- Shell startup files and shell functions of the agent's own shell: `~/.zshenv` for every `zsh -c`, `.bashrc`/`.zshrc` for interactive shells, `BASH_ENV` (R1–R10). They can unset the entries, export another `GH_TOKEN`, or wrap `git` and `gh`.
- Any process that removes the environment entirely (`env -i`) or calls a different `git` binary.

## 8. Open points for the owner

These are not decided here. Each needs the owner's answer on the follow-up ticket before code depends on it.

- **O1 — SSH remotes outside the rewrite set.** Section 5 rewrites seven SSH forms. Should agent-id cover only these, or also refuse (in `exec`) a repository whose GitHub remote still resolves to SSH after the set? The latter would turn D1 from a warning into a fail-closed start.
- **O2 — inherited `GIT_CONFIG_PARAMETERS`.** It overrides agent-id (C19b). The options are to refuse to start, to remove the variable from the child environment, or to report it only in `doctor`. The first two change the environment that `exec` and `env` emit, which needs the owner's yes (`AGENTS.md`, Ask first). Recommendation: refuse to start when the variable contains a `credential`, `url`, `http`, or signing key; otherwise keep it and report it.
- **O3 — SSH fallback after a personal rewrite.** Git configuration cannot win against a personal `https://github.com/` → SSH rule (U2–U8). The options are:
  - (a) document it and detect it in `doctor` (D1);
  - (b) set `GIT_SSH_COMMAND` to an agent-id shim that refuses GitHub hosts and runs the developer's SSH command for every other host. This would need handling of an inherited `GIT_SSH_COMMAND` (K4);
  - (c) `core.sshCommand=false`, which blocks SSH to every host (K3).

  Recommendation: (a) for the MVP, with (b) evaluated as part of hardening.
- **O4 — minimum Git version.** Everything in section 5 behaves the same on Git 2.43 and 2.54. Should 2.43 (Ubuntu 24.04) be the documented minimum? Older versions are not tested.

## 9. Follow-ups

- E02-S08 (credential helper): use rows 1–3 of section 5, answer `quit=1` on refusal, and use C10–C12, C22–C25, and A1–A6 as table-test cases.
- `internal/gitint`: build the set as a pure function with tests for the append and malformed-input cases (G1–G4). Rows X1–X6 can become a process test that runs real `git` in `testenv.Isolate`.
- `internal/diagnose`: checks D1–D7.
- The Claude Code `SessionStart` integration: verify which startup files the agent's shell tool reads (section 4, R).
