# agent-id exec — the child environment contract

> ⚠️ Extended by #135 (2026-11-14): `exec` also sets the shell hook variables `ZDOTDIR`, `BASH_ENV`, and `CLAUDE_ENV_FILE`, and records three `AGENT_ID_USER_*` variables. See "Shell functions and aliases (#135)" below. The environment contract changed, and Jacob approves it with the PR.

> ⚠️ Superseded by `.ai/specs/2026-11-13-env-claude-session-start.md` (2026-11-13) — sections: Architecture (the `Activate` return type and the new `Plan.Unshadow` field). Partly superseded: everything else in this spec stands.

> 🔧 Amended by #23 (2026-11-13): the `gh` stub in `neutral-bin` is now the launcher of the `gh` shim, see `.ai/specs/2026-11-13-gh-shim.md`. Where this spec says "refusing stub", read "launcher that starts `agent-id cli gh`". Every other rule of the neutral baseline stays.
>
> 🔒 Amended by #68 (2026-11-13): `exec` fails closed from its first merge. The child never reaches the developer's credentials through Git or `gh`, independently of #21–#23. See [Neutral credential baseline](#-neutral-credential-baseline-68). The amendment replaces the delivery-order rule "no release may contain `exec` before #21, #22, and #23", and it moves O1 from #22 into #20.

> 🔧 Amended by the follow-up PR for #69–#77 (2026-11-13; post-merge review of #48, findings M1–M8 and N1; #87 in part). Each amendment is marked `(#NN)` in place. In short: the `GIT_CONFIG_COUNT` rule and the idempotence property (#69); re-entry removes the previous plan's own `PATH` directories and `Set` names, recorded in two new context variables (#70); no inherited environment value is echoed in an error (#71); the parent environment is normalized to unique names before any decision (#72); the "Git entries changed" scenario is tested where it can fail (#73); the full O2 refused set is in `ARCHITECTURE.md` (#74); the three conflicts with the GitHub adapter spec are settled (#75); the owner's sign-off of contract v1 is pending (#76, OQ5); `ENOENT` from `execve` is 127 (#77).

> 🔀 **Supersedes** (#75) `.ai/specs/2026-11-13-github-provider-adapter.md`, step 6 (Git and CLI: "Dropping inherited variables stays with `internal/cliint` (#23)") and the "Interface" bullet ("Nothing else changes… the variables to drop (#23)"), as follows:
> 1. **`GH_HOST` is in the drop list.** The adapter's `CLIEnvironment` returns `GH_TOKEN` and `GH_HOST`, and the contract requires the drop list to contain every name `CLIEnvironment` returns. GitHub's list is `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST`. `GH_HOST` is a host pin, not a secret; it is dropped so that an inherited personal `GH_HOST` cannot steer `gh` to another host. The child then has no `GH_HOST`, and `gh` defaults to `github.com`, where it meets the decoy token. #23 sets `GH_HOST` per `gh` invocation.
> 2. **The drop list belongs to #20, not #23.** #68 made the neutral baseline part of #20, and the baseline needs the list. **#20 adds the exec-only answers** (`CredentialVariables`, `Ready`, `GitHosts`, `NeutralCLI`, `TokenPrefixes`), but not to `provider.Provider`: they go on a second port, `provider.Activator` (see Architecture). The adapter spec's "nothing else changes" therefore still holds for `provider.Provider`.
> 3. **`exec` never reads the private key.** The adapter's `New` takes the PEM bytes, so every `exec` would read the key if it built a `Provider`. Instead, the adapter builds an `Activator` from the profile alone (GitHub: `NewActivator`), with no key and no network. A `Provider` is built only by code that mints tokens (#22, #23).

Issue: #20 (E02-S06). Implemented by #20; extended by #21 (Git identity), #22 (Git transport), #23 (`gh` injection); reused by #26 (`agent-id env`) and read by #24 (`whoami`, `doctor`).

Extends: `.ai/specs/2026-11-12-profile-config.md` (it consumes `Resolve` and decides the consumer rule for an unresolved identity that the profile spec left to #20).

## 📝 TLDR

The developer needs to start one agent process, and only that process tree, under a bot profile, while their own terminal stays personal. Today agent-id has no activation at all. This spec proposes `agent-id exec --profile <name> [--] <command> [args…]`: it resolves the profile, builds the child environment with one pure function, refuses a nested start with a different profile, and then **replaces itself** with the command (`execve`), so the exit status and every signal belong to the command by construction. It also fixes the environment contract (the `AGENT_ID_*` context variables, removal of inherited forge tokens, the append rule for `GIT_CONFIG_*`) that #21–#23 extend and that `agent-id env` (#26) prints.

Decisions this spec follows:

- **Nested exec with a different profile is an explicit error that names both profiles; there is no override in the MVP. The same profile is idempotent** (project manager, 2026-11-13, on #20; owner delegated technical decisions).
- **O1–O4 of the Git precedence analysis** (project manager, 2026-11-13, on #20, #22, #24): O1 — `exec` refuses a repository whose GitHub remote still resolves to SSH after the rewrite set; O2 — refuse an inherited `GIT_CONFIG_PARAMETERS` that sets `credential`, `url`, `http`, or signing keys (as decided; widened by this spec to the `gpg`, `include` and `includeif` sections and `core.askpass` (review of #86), see Inherited Git process configuration, #74), otherwise keep it; O3 — SSH fallback is documented and detected (`doctor`), the `GIT_SSH_COMMAND` shim is Phase 3; O4 — Git 2.43 is the minimum.
- **Explicit selection only** (brief §14; profile spec): `--profile` is required. The context variable that `exec` sets is a marker for descendants and diagnostics, never a way to select a profile.
- **Provider abstraction** (ADR #35): `internal/activation` names no forge; the variables to drop and the hint for an unresolved identity come from the provider adapter.
- **Fail closed from the first merge** (project manager, 2026-11-13, #68, from the post-merge review of #48, finding F1). From its first version, `exec` neutralizes personal credentials in the child:
  1. it resets the `credential.helper` list to empty through `GIT_CONFIG_*`;
  2. it sets `GIT_TERMINAL_PROMPT=0`;
  3. it points `GH_CONFIG_DIR` at an empty directory owned by agent-id;
  4. it gives the child no developer or bot token until #23 (`GH_TOKEN` holds only a fixed, non-secret decoy that closes `gh`'s keychain fallback: OQ4, required by the owner's Definition of Done of 13.11);
  5. it refuses to start while a GitHub remote resolves to SSH (O1, moved from #22 into #20);
  6. the regression test belongs to the fail-closed suite #25.

## 📝 Problem Statement

- Brief §8 *Process-scoped execution* is P0: a selected profile must apply to a child and its descendants, and the parent terminal must stay unchanged. Brief §3 principle 1: context over global state.
- The child environment is the only activation mechanism (`ARCHITECTURE.md`, System context). Every later story writes into it: #21 adds the author and committer and the signing entries, #22 the credential helper and SSH rewrites, #23 the `gh` shim. Without one owner and one composition rule, each story would invent its own append and nesting logic, and a nested start would duplicate `GIT_CONFIG_*` entries (#26 acceptance: "nothing is duplicated").
- The environment that `exec` and `env` emit is a shared contract (`SDLC.md`, risk inference; `AGENTS.md`, Ask first). Scripts, `whoami`, and the Claude Code hook depend on its variable names.
- An agent that starts with the developer's inherited `GH_TOKEN` would act as the developer: the fail-open path that brief §5 and §8 forbid.
- Signal and exit-status handling decides whether `exec` is invisible to the developer. A wrapper that forwards `SIGINT` to a child that already got it from the terminal makes Claude Code see Ctrl-C twice, which exits it.

## 📝 Proposed Solution

1. **Command.** `agent-id exec --profile <name> [--] <command> [args…]`. Flag parsing stops at the first non-flag argument, so `agent-id exec --profile claude claude --resume` works without `--`; `--` stays accepted. `--profile` is required. `exec` has no other flag.
2. **Read the inherited context** (pure). Decide fresh start, idempotent re-entry (same profile), or refusal (different profile, damaged or unknown context). Nesting is decided first, so a refused nested start names both profiles even when the inner profile is also not ready.
3. **Resolve.** Load the config and `Resolve(name)` (profile spec). Take the provider's key-free `Activator` for the profile (#75). Ask it whether the profile is ready to activate: in the MVP, the GitHub adapter answers "not ready" when `github.identity` is missing, with its own hint (`run agent-id setup github --profile <name>`). `exec` refuses such a profile and never resolves the identity over the network.
4. **Check the inherited Git process configuration** (pure). Refuse a malformed `GIT_CONFIG_COUNT` set (G2–G4 of the analysis) and an inherited `GIT_CONFIG_PARAMETERS` that sets a forbidden key (O2).
5. **Prepare the neutral directories** (#68). Ensure that the empty `gh` configuration directory and the `gh` stub directory exist, are owned by the user, and are private. See [Neutral credential baseline](#-neutral-credential-baseline-68).
6. **Build** the child environment with one pure function from the parent environment and an activation plan (below). The parent is first normalized to unique names (#72). The result never contains a token or key.
7. **Remote checks** (#68; O1, D2, D3). With the **child** environment and in the working directory, refuse a remote on the provider's host when it resolves to SSH (O1), carries a password in its user-info (D2), or gets an `Authorization` header that survives the plan's reset (D3). The rewrites of #22 count automatically, because the check runs with the child's `GIT_CONFIG_*` entries. Later stories may register further checks in the same list, in a fixed order.

Steps 2–7 are one root-package function, `activation.Activate`, so that `agent-id env` (#26) runs exactly the same pipeline (dependency rule 2: a slice never imports another slice).

8. **Look up** the command with the child's `PATH` (so the `gh` shim of #23 wins, as it would in the agent's shell), then `execve` it with the child environment. agent-id is gone; its process id, open descriptors, controlling terminal, process group, and working directory now belong to the command.

`exec` opens no network connection and prints nothing on success. The only files it writes are the two agent-id-owned neutral directories and the stub script of step 5, and only when they are missing or their content differs. It writes nothing in the developer's configuration.

### The activation plan

`internal/activation` owns the plan and the builder; the other modules contribute data to it, as in `ARCHITECTURE.md` (Context runtime depends on `gitint` and `cliint`):

| Plan field | Meaning | Contributed by |
|---|---|---|
| `Profile` | the resolved profile name | #20 |
| `Mechanism` | `exec` or `env` | #20, #26 |
| `Binary` | absolute path of the running agent-id binary | #20 |
| `Drop` | variable names removed from the child: the core part (`GIT_ASKPASS`, `SSH_ASKPASS`, #68) plus the provider's `CredentialVariables()`, which must be non-empty (an empty provider list is `ErrPlanUnsafe`, fail closed) | the provider (CLI integration: `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST` for GitHub, #75) — #20 wires it |
| `Set` | variables set in the child | #21 (author and committer) |
| `GitConfig` | ordered `key=value` entries appended to the process-scoped Git configuration | #21 (signing), #22 (helper reset, helper, `useHttpPath`, `extraHeader` reset, SSH rewrites, `core.askPass`) |
| `PathPrepend` | directories prepended to `PATH` | #23 (the `gh` shim directory) |

In #20, `Profile`, `Mechanism`, `Binary` and `Drop` are filled, plus the **neutral credential baseline** of #68 in `Drop`, `Set`, `GitConfig` and `PathPrepend` (next section). The builder treats the other fields generically and is fully tested with them in #20, so #21–#23 add data, not composition logic.

### 🔒 Neutral credential baseline (#68)

**Invariant.** `exec` never starts a child in which Git or `gh`, used through the supported paths, can reach the developer's credentials. This holds from the first merge of #20, whether or not #21, #22 and #23 have landed. It is enforced by the plan below and by the refusals of step 7, and tested by #25. It does not depend on release discipline. #21–#23 later replace the neutral entries with the bot's identity, helper and shim. They never remove the baseline, so the invariant holds at every point in between.

#20 contributes these plan entries (`<host>` is the provider's `Host()`). Forge-specific values (on the key-free `Activator`, #75: the CLI's configuration variable, the decoy variables and their value, the CLI binary name for the stub) come from the provider through the interface method `NeutralCLI() NeutralCLI{ConfigDirVar string; Decoys map[string]string; Binary string; ForbiddenConfigFiles []string}` (GitHub: `GH_CONFIG_DIR`; `GH_TOKEN` and `GH_ENTERPRISE_TOKEN` → `agent-id-no-token`; `gh`; `hosts.yml`). `providertest.RunContract` requires: every decoy name is one of `CredentialVariables()`; every decoy value passes the `Build` secret guard and has no registered token prefix; `Binary` and `ConfigDirVar` are non-empty; the core creates the directories generically under `<cache>/agent-id/<provider>/`, so no GitHub name lives outside the adapter (ADR #35):

| Plan field | Entry | Closes | Replaced later by |
|---|---|---|---|
| `GitConfig` | `credential.helper` = *(empty)*, appended after the inherited entries | Every helper collected before it, for every host: system `osxkeychain`, global, `includeIf`, repository, path-scoped, inherited `GIT_CONFIG_COUNT` (analysis C1–C18; a generic empty value resets the list in config order). Personal helpers of other hosts are also off inside the agent context. | #22 appends the agent-id helper for `<host>` after this entry |
| `GitConfig` | `http.https://<host>/.extraHeader` = *(empty)* | A generic or host-scoped personal `Authorization` header (H1, H3, H5, H8) | stays |
| `GitConfig` | `core.askPass` = *(empty)* | A configured askpass program (A2, A3) | stays |
| `Set` | `GIT_TERMINAL_PROMPT=0` | Git's terminal prompt for a username or password | stays |
| refusal (step 7) | a `~/.netrc` (or `$HOME/.netrc` of the child) with a `machine` entry for a provider host, or a `default` entry | Git's HTTP transport reads `.netrc` itself, with no helper involved, so none of the resets above stop it (found by the independent review of #80 on Git 2.54: the server received `Authorization: Basic` from a fake `.netrc`) | stays |
| `Drop` (core part) | `GIT_ASKPASS`, `SSH_ASKPASS` | An IDE's askpass answering for the developer (A1, A4). `GIT_TERMINAL_PROMPT=0` does not stop these. | stays |
| `Drop` (provider part) | `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST` (`CredentialVariables`, must be non-empty and contain every name `CLIEnvironment` returns, #75) | Inherited personal tokens, and an inherited host pin | #23 sets the bot token per `gh` invocation, never in the environment |
| `Set` (provider's `NeutralCLI`) | GitHub: `GH_CONFIG_DIR=<cache>/agent-id/github/cli-config` | The developer's `hosts.yml` and per-user lookups (gh analysis, steps 3–4) | stays; #23 keeps it |
| `Set` (provider's `NeutralCLI`) | GitHub: `GH_TOKEN=agent-id-no-token` and `GH_ENTERPRISE_TOKEN=agent-id-no-token`: fixed, non-secret decoys with no token prefix | The keychain fallback of `gh` however it is started: by absolute path, or through a `PATH` that a startup file re-prepends (`brew shellenv`). `gh` takes `GH_TOKEN` (or, for other hosts, `GH_ENTERPRISE_TOKEN`) before `hosts.yml` and the keyring (gh analysis, token order), so it sends the decoy and GitHub answers 401. Verified offline with `gh auth token`, which printed the decoy although a `hosts.yml` with a fake token was present. | #23's shim replaces `GH_TOKEN` with the bot token for each invocation |
| `PathPrepend` (provider's `NeutralCLI`) | `<cache>/agent-id/github/neutral-bin`, holding the `gh` stub | Gives a clear agent-id message instead of `gh`'s 401 when `gh` is started by name. The decoy, not the stub, is what closes the keychain path. | #23 replaces the stub with the shim in its own directory, prepended before this one |

| `Set` (shell hook, `exec` only, #135) | `ZDOTDIR=<cache>/agent-id/<provider>/shell-hook`, `BASH_ENV=<shell-hook>/bash-env.sh`, `CLAUDE_ENV_FILE=<shell-hook>/claude-env.sh` | A shell function or alias named `git` or the provider's CLI (`gh`) from the developer's start files. A function wins over `PATH` by name, so it can send a non-agent token to the real binary. | stays |
| `Set` (shell hook, `exec` only, #135) | `AGENT_ID_USER_ZDOTDIR`, `AGENT_ID_USER_BASH_ENV`, `AGENT_ID_USER_CLAUDE_ENV_FILE`: the developer's own `ZDOTDIR`, `BASH_ENV`, and `CLAUDE_ENV_FILE` (empty when unset) | The hook files run the developer's own files first, so the developer's other start behavior stays the same. | stays |

`<cache>` is `$XDG_CACHE_HOME` when it is an absolute path, and `$HOME/.cache` otherwise. This is the same rule as the token cache.

**Shell functions and aliases (#135).** Issue #135 found that a shell function named `gh` or `git` from `~/.zshrc` wins over `PATH`. A function that calls the real binary by absolute path can send a non-agent token. The SessionStart hook removes such functions. `exec` removes them with three mechanisms, because the shells start differently:

1. **Claude Code: `CLAUDE_ENV_FILE`.** Claude Code sources `~/.zshrc` or `~/.bashrc` once per session, saves the functions in a snapshot, and sources the snapshot before each Bash command. It then sources `CLAUDE_ENV_FILE`, then runs the command. `ZDOTDIR` and `BASH_ENV` run before the snapshot, so they are too early (analysis in the comments of #135). The `claude-env.sh` file runs after the snapshot. Each `exec` has its **own** file, `shell-hook/sessions/<pid>.<random>/claude-env.sh` (see "Session files" below). The file first sources the developer's own `CLAUDE_ENV_FILE` (the path is a quoted literal in the file; `AGENT_ID_USER_CLAUDE_ENV_FILE` records it), then runs `unset -f` and `unalias` for `git` and the provider's CLI.
2. **zsh: `ZDOTDIR`.** The `shell-hook` directory holds a wrapper for `.zshenv`, `.zprofile`, `.zshrc`, and `.zlogin`. Each wrapper sources the developer's own file with the developer's `ZDOTDIR` (from `AGENT_ID_USER_ZDOTDIR`, else `HOME`), then restores the wrapper `ZDOTDIR`, then removes the functions and aliases. A change of `ZDOTDIR` by the developer's file is kept. A `HISTFILE` that points into the wrapper directory moves back to the developer's directory. A login shell can reorder `PATH` (macOS runs `path_helper`), so each wrapper puts `AGENT_ID_PATH_PREPEND` first on `PATH` again.
3. **bash: `BASH_ENV`.** The `bash-env.sh` file sources the developer's own `BASH_ENV` (from `AGENT_ID_USER_BASH_ENV`), then removes the functions and aliases. Bash reads `BASH_ENV` only in a non-interactive shell that is not a login shell.

The three variables are set only by the `exec` mechanism. The `env` mechanism prints a block that already removes the functions and aliases, and Claude Code owns `CLAUDE_ENV_FILE` there. On re-entry (a nested `exec`), the hook takes the recorded `AGENT_ID_USER_ZDOTDIR` and `AGENT_ID_USER_BASH_ENV` values, so nesting never records agent-id's own files as the developer's. `AGENT_ID_USER_CLAUDE_ENV_FILE` is the exception: in a nested `exec` it names the outer exec's session file (see below). `stripContribution` removes the three names through `AGENT_ID_SET_NAMES`. The shared hook files (the zsh wrappers and `bash-env.sh`) are written by `Ensure` into the private `shell-hook` directory (`0700`, files `0600`, same checks as the other two directories). agent-id never writes the developer's start files or `~/.claude`.

**Session files (#135, review of PR #138).** The first version shared one `claude-env.sh` for all execs of a provider. Claude Code and other tools append to `$CLAUDE_ENV_FILE` (`>> "$CLAUDE_ENV_FILE"`), and `exec` replaces its own process, so nothing removed the lines at the end of a session. Another exec then read them. So now:

- `exec` (the `exec` mechanism only; `agent-id env` and the `gh` shim do not make one) creates `shell-hook/sessions/` (`0700`, same checks as the other directories). In it, it creates a directory `<pid>.<random>` (`0700`) and in that directory `claude-env.sh`, opened with `O_CREATE|O_EXCL|O_NOFOLLOW`, mode `0600`, under an exclusive `flock`. The pid is the pid of the agent process, because `execve` keeps it. `CLAUDE_ENV_FILE` names this file. A file that exists already, or a symlink, is an error (`ErrNeutralDir`) and nothing is written through it.
- **Cleanup.** At every `exec` start, before it creates its own directory, `exec` removes each session directory of the current user whose pid is not alive (`kill(pid, 0)` gives `ESRCH`) and each one older than **30 days** (the modification time of the directory), even when the pid is alive, because the system can reuse a pid. It removes only real directories named `<pid>.<random>`, with `RemoveAll`, which never follows a symlink. A symlink, a file, a directory of another user, and a directory with another name stay. A failed removal is not an error. Residual limit: a session that runs longer than 30 days loses its file, and then its `unset` lines no longer run in that session.
- **Nested `exec`** (same profile; another profile is refused as before) makes its own new file. That file sources the outer exec's session file (taken from `CLAUDE_ENV_FILE`, and only when the path is inside `shell-hook/sessions/`; otherwise the recorded developer value is used). The outer file sources the developer's file. So the outer session's appended lines still run, and the inner lines do not reach the outer file. The outer file is not changed.
- **Lock.** `ensureFile` (the shared hook files and the launcher) opens the existing file with `O_NOFOLLOW`, checks the descriptor, and holds the same exclusive `flock` that the #98 SessionStart hook takes, from the read to the `rename`. A session file is never rewritten after it is created.
- **Residual risk.** Lines that a process appends after agent-id's `unset` and `unalias` lines can define `gh` or `git` again. This now holds **only in the same session**, because no other session reads the file. agent-id does not guard against it: a process that can append to the file of its own session can also define a function in its own shell.
- **The #98 hook** appends to the inherited `CLAUDE_ENV_FILE`. With a file per session, two sessions with two profiles do not see each other's block, and no refusal line crosses sessions.

Limits (not a security boundary): a function under another name (`ghx`), an alias that bash already parsed on the same line, `bash -i` and `bash -l` (they read `~/.bashrc` and the profile, not `BASH_ENV`), `fish` and other shells, and a developer's file that is sourced after the hook. Claude Code reads the snapshot from `$HOME`, not from `ZDOTDIR`. Whether a future Claude Code version keeps this order is not verified by agent-id. `doctor` and `whoami` warn about definitions in the start files (below).

**The three directories (step 5).** They are `<cache>/agent-id/<provider>/cli-config`, `<cache>/agent-id/<provider>/neutral-bin`, and, since #135, `<cache>/agent-id/<provider>/shell-hook`. `exec` creates each directory with mode `0700` when it is missing, together with a missing `agent-id` parent (`0700`). It then checks, on an opened descriptor, that the directory is not a symlink, is owned by the current user, and has no group or other bits. Otherwise it refuses with `ErrNeutralDir`, naming the path and the fix, and never `chmod`s a directory it did not just create.

- `cli-config` must not contain `hosts.yml` (the GitHub adapter's `NeutralCLI` names the forbidden file). If it does, `exec` refuses with `ErrNeutralDir`: a `gh auth login` run inside the agent context would otherwise turn the empty directory into a token store. Other files that `gh` writes there (`state.yml`, `config.yml`) are allowed.
- `neutral-bin/<Binary>` (`gh`) is a regular file with mode `0700`, owned by the user, with exactly this content:

  ```sh
  #!/bin/sh
  printf '%s\n' 'agent-id: gh is not available in this agent context: this agent-id version gives gh no bot token, and it never uses your personal login. Run gh in your own terminal.' >&2
  exit 1
  ```

  `exec` writes the file atomically (temporary file, `0700`, `rename`) when it is missing or its content differs. A symlink or a file owned by someone else is refused. The stub is a self-contained script, not a link to the agent-id binary, so an upgrade or removal of the binary cannot leave a dangling entry. A dangling entry would let the shell fall through to the real `gh`.

**Remote checks (step 7).** The remote checks below run only inside a Git work tree (`git rev-parse --is-inside-work-tree` with the child environment), and are skipped outside one or when `git` is not installed: then there is no Git path at all. For each remote, `exec` reads the effective fetch URL (`git ls-remote --get-url <remote>`) and every push URL (`git remote get-url --push --all <remote>`) with the child environment, so the effective `insteadOf`/`pushInsteadOf` rules apply. Remotes are not the only path: a global `url.<ssh>.insteadOf=https://<host>/` rewrite would send every later `git clone https://<host>/…` of the agent over SSH with the developer's key. So step 7 also evaluates the rewrite rules of the system, global and command scopes (`git config --list -z --show-origin` with the child environment, run twice: in an empty directory outside any repository, and in the working directory, so that a global `includeIf "gitdir:…"` rewrite and the starting repository's own rules are seen) with a pure `RewritesToSSH(entries, "https://<host>/")`. It evaluates fetch and push separately, as Git does (verified on Git 2.54 in the review of #80): fetch uses only the longest matching `insteadOf`; push first applies the longest matching `pushInsteadOf`, and falls back to `insteadOf` only when no `pushInsteadOf` matches; an explicit `pushurl` is not subject to `pushInsteadOf` and is covered by the per-remote O1 check. It refuses (`ErrSSHRewrite`) when the provider's HTTPS base, or any path under it, would be rewritten to SSH. A URL belongs to the provider when its host (scheme form, or the `[user@]host:path` scp form) matches one of the provider's `GitHosts()` (a method of the key-free `Activator`; GitHub: `github.com`, `ssh.github.com`) case-insensitively, or when it is an SSH alias whose `ssh -G <alias>` `hostname` matches.

| Check | Refuses when | Error |
|---|---|---|
| O1 | a provider URL uses SSH: scp form, `ssh://`, `git+ssh://`, `ssh+git://`. Before #22 every SSH form is refused; after #22 its rewrites turn the common forms into HTTPS first. | `ErrSSHRemote` |
| D2 | a provider URL has a password in its user-info | `ErrRemoteCredentials` |
| D3 | any `http.<url>.extraHeader` value, other than one that agent-id's own plan entries set (#22 may add some later), survives for a provider URL. `git config --get-urlmatch` is not used, because it prints only the last value of the best match. The pure `EffectiveHeaders(entries, url)` evaluates the ordered `--list -z` entries with Git's URL-specificity rule and the empty-value reset (H2, H4, H5, H7, H8). The check runs for every provider Git host (the baseline reset covers only `Host()`), and for every `http.<url>.extraheader` pattern that matches one of them, with Git's host-pattern rule (`*` matches exactly one label, labels counted) and the user and path scopes of the pattern (review of #86). Patterns and URLs are compared as Git's `urlmatch.c` does, never with Go's `net/url` (#100): the scheme and host are lower-cased, the password is dropped, a default port is dropped, the path is the raw rest of the URL (a query and a fragment stay in it, as in Git), percent escapes are upper-cased and decoded only for unreserved characters (so `%2F` stays `%2F`), unsafe bytes such as TAB, DEL and a space are escaped, and `.`/`..` segments are resolved only before the first `?` or `#`. The host keeps a trailing dot, which counts in the host length that ranks a pattern (one trailing dot per side is ignored when matching, two never match). A pattern with a user part, even an empty one (`https://@host/`), matches only URLs with the same user part and ranks above a pattern without one. A reset (an empty value) counts only when its pattern matches in this way. `ScopedHeaders` evaluates each header pattern at its own URL and at a URL below its path (a last segment longer than any pattern), because a reset with a longer path, such as `/org//` against `/org/`, matches the pattern's own URL but not the URLs below it, and Git still sends the header there. A generic `http.extraheader` gets the same two evaluations at every provider host root (`HostRootHeaders`), because a reset with the path `//` matches the root only. A non-empty header whose pattern the matcher cannot parse (no scheme, a bad `%` escape, a port with a leading zero, a second `@`, a `..` above the root, a dot segment at the end) is refused at once (`ErrRemoteHeader`, message "cannot parse as Git does"), because Git may still apply it; an unparsable empty reset is skipped, which never weakens the check. `TestUrlmatchAgreesWithGit` compares the matcher with `git config --get-urlmatch` on about 4,000 pattern and URL pairs, `TestUrlmatchRankAgreesWithGit` compares which of two entries wins on about 2,900 more, and `TestCheckNeverPassesAHeaderThatGitSends` runs `checkHostHeaders` on random entry sets and requires a refusal whenever Git sends a header to a provider URL, and `TestRealGitSendsWhatTheCheckPredicts` compares it with the header that real Git sends to a local listener. Any surviving header is refused, not only `Authorization`: `Cookie`, `Private-Token` and custom headers can carry credentials too. | `ErrRemoteHeader` |
| netrc | any of the child's `$HOME/.netrc`, `$HOME/_netrc`, or the file named by `$NETRC` exists and has a `machine` entry for a provider host (compared case-insensitively) or a `default` entry. When `HOME` is unset, the user's home from the password database is used, as libcurl does. Apple Git 2.54 (libcurl 8.7.1) reads only `$HOME/.netrc`, but Linux libcurl builds differ, so all three are checked. A pure netrc tokenizer handles `machine`/`default`/`login`/`password`/`macdef`. An unreadable or unparsable file is refused. Values are never printed. | `ErrNetrc` |

Every Git or `ssh` call has a 10 s timeout. A timeout, a failed `ssh -G` for a host that could be an alias, or any other error makes `exec` refuse (`ErrRemoteCheck`) rather than guess. An alias passed to `ssh -G` must match `^[A-Za-z0-9._-]+$` and not start with `-`, and is passed after `--`. Messages name the remote and the configuration source of the rule, never a URL's user-info or a header value.

**Not covered** (documented bypasses; local mode is not a sandbox, brief §10):
- a generic `http.extraheader` over plain `http://<host>/`: the check and the baseline reset cover `https://` only, and Git sends a generic header to `http://github.com/…` in plain text (found in the review of #101; follow-up #107);
- hosts that are not Git transport hosts (decision of #100): `GitHosts()` stays `github.com`, `ssh.github.com` (and `gist.github.com` through the same rule for `*.github.com`). It does not grow to `api.github.com`, `codeload.github.com` or `uploads.github.com`: Git never clones or pushes through them, and the `gh` path is covered by the neutral CLI configuration, not by Git headers. Git LFS sends its batch request to the repository host (`https://github.com/<owner>/<repo>.git/info/lfs`), which is covered. LFS object transfers use pre-signed URLs on other hosts (for example `*.githubusercontent.com`); a header that the developer scoped to such a host with `http.<url>.extraHeader` is a documented bypass. A generic `http.extraheader` is still refused for all hosts. Revisit this when #22 adds the credential helper;
- the agent's own `git -c credential.helper=…`, or a `git clone git@github.com:…` that names an SSH URL itself, which uses the developer's SSH agent (SSH neutralization for the provider's hosts is the Phase 3 `GIT_SSH_COMMAND` shim, O3);
- a repository other than the starting one with its own `insteadOf` to SSH, or a `.netrc` written after the start;
- the agent unsetting the decoy and calling `gh` by absolute path;
- `env -i`;
- shell startup files that undo the environment (R8–R10).

`doctor` (#24) reports the detectable ones.

### Alternatives considered

| Option | For | Against | Verdict |
|---|---|---|---|
| **`execve`: agent-id replaces itself with the command** | Exit status, signals, job control, and the terminal are the command's own; no double `SIGINT`; no code to forward signals; the command's process id is the one the developer sees. Used by `env(1)`, `direnv exec`, `sudo` without a PTY. | No code runs after the command: no cleanup, no exit message. agent-id needs neither: it creates no temporary state, and tokens are fetched per use by the helper and the shim. | **Chosen** |
| Fork, wait, and forward `SIGINT`/`SIGTERM`/`SIGHUP` (the issue's sketch) | Could clean up or report after the child ends. | Terminal-generated `SIGINT` reaches the whole foreground process group, so forwarding delivers it twice (Claude Code exits on a second Ctrl-C). Needs `SIGTSTP`/`SIGCONT`, `SIGWINCH`, and exit-by-signal emulation (`128+n`) to be invisible. More code on a `risk-high` path. | Rejected |
| Fork with the child in its own process group | Avoids the double signal. | The child loses the terminal's foreground group unless agent-id hands the terminal over (`tcsetpgrp`); interactive agents break in subtle ways. | Rejected |
| Same profile nested: pass the environment through unchanged | Simplest no-op. | A startup file that unset the entries (R8 of the analysis) or a profile whose identity was refreshed would be carried forward silently. | Rejected; a deterministic rebuild gives the same result when the context is intact, picks up a refreshed profile, and refuses a context whose Git entries changed after activation (R8). |
| Same profile nested: rebuild without removing the previous contribution | — | Appends the Git entries again (duplicates, #26 acceptance). | Rejected |
| Different profile nested: override (the inner profile wins) | Flexible. | The outer process tree would mix two bots; decided against for the MVP. | Rejected (decision, 2026-11-13) |
| Mint or check a token before the start | Fails before the agent starts. | Makes every start depend on the network; the helper and the shim fail closed at use anyway. | Not in the MVP (OQ1) |

Comparable tools: `aws-vault exec` and `op run` start the command as a child to inject secrets into its environment; agent-id puts no secret in the environment, so it does not need a supervising parent. `direnv exec` and `env(1)` use `execve` and the exit codes 125–127 that this spec adopts.

## 📝 Delta

### Capability: exec

#### ADDED Requirement: Only the command's process tree is in agent mode
The system SHALL start the command with the child environment of the resolved profile and SHALL NOT change the environment, the files, or the configuration of the invoking process or its terminal.

##### Scenario: Parent unchanged
- WHEN a test process runs `agent-id exec --profile claude -- sh -c 'exit 0'` in an isolated environment and then inspects its own environment and the isolated `HOME`
- THEN its environment is byte-identical to the state before the run, and no file under `HOME`, `XDG_*`, or `GIT_CONFIG_GLOBAL` was created or modified except agent-id's own `<cache>/agent-id/<provider>/cli-config` and `neutral-bin` (#68)

##### Scenario: Marker in the child only
- WHEN the command is `sh -c 'printf %s "$AGENT_ID_ACTIVE_PROFILE"'`
- THEN it prints `claude`, and the invoking process has no `AGENT_ID_ACTIVE_PROFILE`

#### ADDED Requirement: Exit status and signals belong to the command
The system SHALL replace its own process with the command, so that the command's exit status and every signal sent to the process are the command's.

##### Scenario: Exit status
- WHEN `agent-id exec --profile claude -- sh -c 'exit 7'` finishes
- THEN the exit status is 7

##### Scenario: SIGTERM
- WHEN the test starts `agent-id exec --profile claude -- sh -c 'trap "exit 42" TERM; : > "$READY"; while :; do sleep 1; done'`, waits until the file `$READY` exists (so `execve` happened and the trap is installed), and sends SIGTERM to the process id it started
- THEN the command's trap runs and the exit status is 42

##### Scenario: Killed by a signal
- WHEN the command is killed by SIGKILL
- THEN the waiting process observes termination by SIGKILL, not an exit code

#### ADDED Requirement: agent-id's own failures use 125–127
The system SHALL exit with 125 when agent-id fails before starting the command, 126 when the command was found but could not be executed, and 127 when the command was not found, and SHALL print one error line to stderr starting with `agent-id: exec: `.

##### Scenario: Command not found
- WHEN the command is `does-not-exist`
- THEN the exit status is 127 and stderr is `agent-id: exec: "does-not-exist": command not found`

##### Scenario: Missing script interpreter (#77)
- WHEN the command is a regular, executable script found in `PATH` (or named with `/`) whose first line is `#!/nonexistent/interpreter`, so that `execve` returns `ENOENT` after a successful lookup
- THEN the exit status is 127 (as `env(1)` and the shells report it) and stderr is `agent-id: exec: "<command>": interpreter or loader not found`; a script that exists but is not executable gives 126 when it is named with `/`, and is skipped by the `PATH` lookup (127 if nothing else matches) otherwise

##### Scenario: Missing profile flag
- WHEN `agent-id exec -- true` runs
- THEN the exit status is 125 and the message is `agent-id: exec: --profile is required`

##### Scenario: Unknown flag
- WHEN `agent-id exec --bogus --profile claude -- true` runs
- THEN the exit status is 125, not 1

#### ADDED Requirement: Inherited forge tokens never reach the command
The system SHALL remove from the child environment every variable that the profile's provider names as a credential variable, and SHALL NOT place any token or key in the child environment.

##### Scenario: Personal GH_TOKEN
- WHEN the parent has `GH_TOKEN` and `GITHUB_TOKEN` set to fake personal tokens and the profile's provider is `github`
- THEN no developer value reaches the command: `GITHUB_TOKEN` is absent, and `GH_TOKEN` equals the provider's fixed decoy (#68)

##### Scenario: No secret added to the child environment
- WHEN the child environment is built for any profile of the test fixtures from a parent with no token-shaped value
- THEN no value of the child contains a registered token prefix, `-----BEGIN`, or a JWT shape

##### Scenario: Empty drop list fails closed
- WHEN the provider of a profile declares no credential variables
- THEN the exit status is 125 (`ErrPlanUnsafe`) and the command does not run

##### Scenario: Inherited GH_HOST dropped (#75)
- WHEN the parent has `GH_HOST=ghe.example.test` and the profile's provider is `github`
- THEN the command has no `GH_HOST`

##### Scenario: Duplicate names in the parent (#72)
- WHEN the parent environment (built by the test through the `Execer`'s `envp`, not through a shell) holds `GH_TOKEN` twice with two fake personal values, `GITHUB_TOKEN` three times (a fake value last), and the entries `GH_TOKEN` (no `=`) and `=x`
- THEN the command has exactly one `GH_TOKEN`, equal to the decoy, no `GITHUB_TOKEN`, no entry without `=`, and every name occurs once

Inherited variables that agent-id does not know (for example `HOMEBREW_GITHUB_API_TOKEN`) pass through unchanged; `doctor` reports inherited token-shaped values (#24), and widening the drop list is OQ3.

#### ADDED Requirement: Nested exec
The system SHALL rebuild the environment idempotently when an exec runs inside a context of the same profile, and SHALL refuse to start when the inherited context belongs to a different profile, is damaged, or has an unknown contract version.

##### Scenario: Same profile is idempotent
- WHEN `agent-id exec --profile claude -- agent-id exec --profile claude -- env` runs
- THEN the inner environment equals the outer one: the same `GIT_CONFIG_COUNT`, the same entries, and no duplicate `PATH` element

##### Scenario: Idempotent for an inherited count of 0 or empty (#69)
- WHEN `Build` runs twice with the same plan on a parent that has `GIT_CONFIG_COUNT=0`, `GIT_CONFIG_COUNT=` (empty), or no count, once with an empty `Plan.GitConfig` and once with a non-empty one
- THEN `Build(Build(E, p), p)` equals `Build(E, p)`, and the child's `GIT_CONFIG_COUNT` is the canonical decimal `BASE + len(Plan.GitConfig)` on both paths

##### Scenario: Re-entry with a changed plan (#70)
- WHEN the outer context was built with `AGENT_ID_BIN=/a/agent-id`, `PathPrepend=[/x/shim-a]` and `Set={FOO}`, and the inner `Build` runs with `AGENT_ID_BIN=/b/agent-id`, `PathPrepend=[/x/shim-b]` and `Set={BAR}`
- THEN the inner `PATH` holds `/x/shim-b` once and no `/x/shim-a`, `FOO` is absent, `BAR` is set, and `AGENT_ID_BIN` is `/b/agent-id` (unit level: the fixed #20 plan has one fixed shape, so a process test cannot change it)

##### Scenario: Different profile is an error
- WHEN `agent-id exec --profile reviewer -- true` runs inside a context of profile `claude`
- THEN the exit status is 125, the command does not run, and the message names `claude` and `reviewer`

##### Scenario: Git entries changed after activation
- WHEN a startup file of the agent's shell unset `GIT_CONFIG_COUNT`, or another tool appended entries after agent-id's, and `agent-id exec --profile claude -- true` runs in that shell
- THEN the exit status is 125 (`ErrContextInvalid`) and no inherited entry is dropped

The check compares `GIT_CONFIG_COUNT` with `AGENT_ID_GIT_CONFIG_END`, so it refuses an appended entry for any plan, and an unset or changed count whenever `END > 0`. Only one state passes vacuously: an unset count with `BASE == END == 0`, which needs an empty `Plan.GitConfig` and no inherited entries (#73). The scenario "a startup file unset the count" therefore cannot fail there. It is **unit-level** for an arbitrary plan (the tables cover a non-empty plan, an appended entry, and the vacuous state as a documented accepted case, not a special case in code), and the **process test** belongs to the first ticket whose plan carries Git entries. Under #68 that is #20 itself: the baseline contributes three entries (`credential.helper`, `extraHeader`, `core.askPass` resets), so the process test of step 8 runs in #20 and cannot pass vacuously. Were the baseline ever removed from #20's plan, the process test would move to #22 and #20 would have no such test.

##### Scenario: Unknown context version
- WHEN the parent has `AGENT_ID_CONTEXT=2`
- THEN the exit status is 125 and the message says that the context comes from an incompatible agent-id

#### ADDED Requirement: Inherited Git process configuration
The system SHALL refuse to start when the inherited `GIT_CONFIG_COUNT` set is malformed, or when an inherited `GIT_CONFIG_PARAMETERS` sets a `credential.*`, `url.*`, `http.*`, `gpg.*`, `include.*`, `includeIf.*`, signing key, or `core.askPass`, and SHALL keep every other inherited `GIT_CONFIG_*` entry and `GIT_CONFIG_PARAMETERS` unchanged.

##### Scenario: Malformed count
- WHEN the parent has `GIT_CONFIG_COUNT=2` and no `GIT_CONFIG_KEY_1`
- THEN the exit status is 125 and the message names `GIT_CONFIG_KEY_1` without printing any value

##### Scenario: Token-shaped count (#71)
- WHEN the parent has `GIT_CONFIG_COUNT` set to `testenv.FakeToken` (and, in a second case, to a fake `github_pat_…` value)
- THEN the exit status is 125, the message names only `GIT_CONFIG_COUNT`, and it contains no substring of the value of 8 or more characters

##### Scenario: Forbidden parameter
- WHEN the parent has `GIT_CONFIG_PARAMETERS="'credential.helper'='store'"`
- THEN the exit status is 125 and the message names `credential.helper`

##### Scenario: Include in parameters
- WHEN the parent has `GIT_CONFIG_PARAMETERS="'include.path'='/tmp/x.gitconfig'"` or an `includeIf.<condition>.path` key
- THEN the exit status is 125 and the message names `include.path`, or `includeIf.<…>.path` with the condition omitted (#71)

##### Scenario: Harmless parameter kept
- WHEN the parent has `GIT_CONFIG_PARAMETERS="'core.pager'='less'"`
- THEN the command sees the same `GIT_CONFIG_PARAMETERS`

#### ADDED Requirement: Fail closed from the first merge (#68)
The system SHALL start the command only with the neutral credential baseline in place. In that state Git has no credential helper, no terminal or askpass prompt, and no personal `Authorization` header for the provider's host; `gh` started by name is the refusing stub and finds no `hosts.yml`; and no token variable is inherited. This SHALL hold whether or not #21–#23 have landed.

##### Scenario: Personal credential helper neutralized
- WHEN the isolated global Git config sets a fake recording helper as `credential.helper` and as `credential.https://github.com.helper`, the system config sets another, and the command runs `git ls-remote https://<fake host>/o/r.git` (or `git credential fill` for `https://github.com/o/r.git`) against the local Git server of `internal/testenv`
- THEN no fake helper records a call, Git asks no prompt (`GIT_TERMINAL_PROMPT=0`), and the operation fails without credentials

##### Scenario: Askpass neutralized
- WHEN the parent has `GIT_ASKPASS` and `SSH_ASKPASS` set to fake recording programs and the global config sets `core.askPass`
- THEN none of them records a call during the operation above

##### Scenario: Empty GH_CONFIG_DIR and refusing gh stub
- WHEN the parent's `GH_CONFIG_DIR` holds a fake `hosts.yml` with a fake personal `oauth_token`, and the command runs `gh api user`
- THEN the command's `GH_CONFIG_DIR` is agent-id's own directory, which has no `hosts.yml`; `GH_TOKEN` is the decoy; `gh` resolves to the stub, which prints agent-id's message and exits 1; and the fake `hosts.yml` value never reaches any process (a fake `gh` placed later on `PATH` records zero calls)

##### Scenario: gh by absolute path gets the decoy, not the keychain
- WHEN the command runs the real `gh` by absolute path as `gh auth token` (skipped when `gh` is not installed; offline)
- THEN it prints `agent-id-no-token`, proving that `gh`'s token resolution stops at `GH_TOKEN` before `hosts.yml` and the keyring

##### Scenario: netrc refused
- WHEN the child's `$HOME/.netrc` has `machine github.com login x password <fake>` or a `default` entry
- THEN the exit status is 125 (`ErrNetrc`), the message names the file and not the password, and a transport test against the local Git server shows that no `Authorization` header was sent because the command never ran

##### Scenario: Global rewrite to SSH refused
- WHEN the isolated global config holds `url.git@github.com:.insteadOf=https://github.com/` and the working directory is not a repository
- THEN the exit status is 125 (`ErrSSHRewrite`), naming the rule and its scope

##### Scenario: No token in the child
- WHEN the parent has fake values in `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN`
- THEN none of the developer's values reaches the command: `GITHUB_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` are absent, `GH_TOKEN` and `GH_ENTERPRISE_TOKEN` hold only the fixed decoy, and no value of the command's environment matches a registered token prefix, `-----BEGIN`, or a JWT shape

##### Scenario: Refusal on an SSH remote
- WHEN the working directory is a repository whose `origin` is `git@github.com:o/r.git`, or `https://github.com/o/r.git` rewritten to SSH by a personal `url.git@github.com:.insteadOf`, or a `pushurl` of `ssh://git@ssh.github.com:443/o/r.git`
- THEN the exit status is 125 (`ErrSSHRemote`), the message names the remote and the configuration source of the rule, and the command does not run

##### Scenario: Refusal on embedded credentials or a surviving header
- WHEN a remote is `https://u:secret@github.com/o/r.git`, or the repository config holds a path-scoped `http.https://github.com/o/r.git.extraheader` with an `Authorization` value followed by a harmless second header, or a host-scoped `Cookie` header added after agent-id's reset
- THEN the exit status is 125 (`ErrRemoteCredentials` or `ErrRemoteHeader`), and the message contains neither `secret` nor the header value

##### Scenario: Neutral directories are checked
- WHEN `<cache>/agent-id/github/cli-config` is a symlink, is group-writable, or contains `hosts.yml`, or `neutral-bin/gh` is a symlink
- THEN the exit status is 125 (`ErrNeutralDir`), and nothing is changed

#### ADDED Requirement: Unresolved identity
The system SHALL refuse to activate a profile that its provider reports as not ready, with the provider's hint, and SHALL NOT contact the provider to resolve it.

##### Scenario: Profile without identity
- WHEN profile `reviewer` has no `github.identity`
- THEN the exit status is 125, the message contains `agent-id setup github --profile reviewer`, and the fake GitHub received no request

## 📝 Architecture

```mermaid
flowchart LR
    Root["cmd/agent-id (exists)"] --> Exec["internal/activation/exec (new slice)"]
    Exec --> Act["internal/activation (new module)"]
    Act -->|ProfileResolver port| Store["internal/profile (#17, planned)"]
    Act --> Prov["internal/provider (interface, ADR #35)"]
    Act -.plan data, later.-> Git["internal/gitint (#21, #22)"]
    Act -.plan data, later.-> Cli["internal/cliint (#23)"]
    Env["internal/activation/env (#26, planned)"] --> Act
    Diag["internal/diagnose (#24, planned)"] -->|reads the context| Act
```

`internal/activation` is the only owner of the child environment; `exec` executes it and `env` (#26) prints it. Dashed edges are data that later stories add to the plan.

- **`internal/activation`** (module root, new). `ReadContext`, `Decide`, `CheckInheritedGit`, `ParseGitConfigParameters` and `Build` are pure. `Activate` orchestrates the pipeline and performs I/O only through its ports (profile resolver, provider source, the neutral-directory port, remote checks); the `Execer` is the slice's port:
  ```go
  type Plan struct {
      Profile     string
      Mechanism   string            // "exec" or "env"
      Binary      string
      Drop        []string
      Set         map[string]string
      GitConfig   []GitConfigEntry
      PathPrepend []string
  }
  type GitConfigEntry struct{ Key, Value string }
  type Context struct{ Version, Profile, Mechanism, Binary string; GitConfigBase, GitConfigEnd int; PathPrepend, SetNames []string } // the inherited context, if any
  type Deps struct {
      Profiles  ProfileResolver // narrow port: Resolve(name) → the profile's provider name and host; satisfied by internal/profile (#17)
      Providers ProviderSource  // narrow port: the key-free provider.Activator for a resolved profile (#75); built by the composition root
      Neutral   NeutralDirs     // narrow port: ensures and checks the `cli-config` and `neutral-bin` directories (#68)
      Checks    []Check         // remote checks run with the child environment (#68: O1, D2, D3)
  }
  type Request struct {
      Profile, Mechanism string
      Binary             string   // from ResolveBinary, resolved by the caller
      Dir                string   // working directory, for the remote checks
      Parent             []string // the parent environment
  }
  func Activate(ctx context.Context, d Deps, r Request) ([]string, error) // steps 2–7
  func ReadContext(parent []string) (ctx *Context, err error)              // pure
  func Decide(ctx *Context, profile string) (Start, error)                 // fresh | reenter | ErrNestedProfile
  func CheckInheritedGit(parent []string) error                            // G2–G4, O2; pure
  func Build(parent []string, plan Plan) ([]string, error)                 // pure, idempotent
  func ParseGitConfigParameters(v string) ([]string, error)                // keys only; pure
  ```
  It also owns the sentinel errors below, `ResolveBinary`, and `LookPath(env)` that uses the child's `PATH`. It imports `internal/provider` (the port) and no adapter.
- **`internal/activation/exec`** (slice, new): `contract.go` (`Input{Profile string; Argv []string}`, the exit-code error type), `handler.go` (dependencies: `activation.Deps` and an `Execer`; it calls `activation.Activate`, `LookPath`, then `Execer.Exec`), `command.go` (cobra, `SetInterspersed(false)`, `SetFlagErrorFunc` wrapping flag errors as 125, `--profile` checked by the handler, not `MarkFlagRequired`), `handler_test.go`. The slice imports only `internal/activation` and ports (dependency rule 2); it does not import `internal/profile`. Only the `Execer` that calls `syscall.Exec` sits behind a `darwin || linux` build tag, so every package still compiles on other platforms.
- **`cmd/agent-id`** registers the slice, builds `activation.Deps` (the profile store, and the `Activator` for a profile from the adapter registered for its `provider` name), and maps the slice's exit-code error to the process exit code. It currently returns 1 for every error (`cmd/agent-id/main.go`, `run`); it gains an error type that carries a code (`ExitCode() int`), used by `exec` only.
- **Provider ports** (ADR #35; settled by #75). `internal/provider.Provider` exists on `main` (`Name`, `Host`, `Token`, `Identity`, `GitCredential`, `CLIEnvironment`; settled by the GitHub adapter, #15) and **`exec` does not change it**. `exec` needs answers that depend on the profile only, never on the private key, so they live on a second, key-free port, `provider.Activator`, which the adapter builds from the profile alone (GitHub: `NewActivator`; it reads no key and makes no request). #20 adds the port, a contract suite for it in `providertest`, a fake `Activator`, and a `githubtest` control for the identity cache so that the suite can exercise `Ready`:
  - `CredentialVariables() []string` — the CLI-integration variables the core must remove from any child: the names that `CLIEnvironment` sets, plus their aliases. GitHub: `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST`. The contract requires a non-empty list that contains every name `CLIEnvironment` returns, so a provider cannot add a variable to `CLIEnvironment` and forget to drop it.
  - `Ready(ctx) error` — whether the profile can be activated without a network call; GitHub: the identity cache is present. Its error carries the adapter's hint.
  - `GitHosts() []string` (#68) — the host names whose Git remotes belong to the provider (GitHub: `github.com`, `ssh.github.com`; the "host and URL mapping" of ADR #35). The contract requires a non-empty list that contains `Host()`.
  - `NeutralCLI() NeutralCLI` (#68), as defined above.
  - `TokenPrefixes() []string` — the registered token prefixes for the secret guard of `Build`, so the core hard-codes no GitHub prefix.

  The `ProviderSource` port of `Deps` returns the `Activator` for a resolved profile; the composition root builds it from the adapter registered for the profile's `provider` name. Phase 1 needs none of it. The production wiring of #20 needs the GitHub adapter (#15): the GitHub drop list is the adapter's data, and a test in `cmd/agent-id` must prove that the production registry removes an inherited `GH_TOKEN`; without it, `exec` would fail open.
- **Remote checks** (step 7) are values of `type Check func(ctx context.Context, dir string, child []string) error`, run with the **child** environment in a fixed order. #20 registers O1, D2 and D3 (#68). They live in `internal/activation`, which runs `git` and `ssh` through a narrow `Runner` port so the decisions stay pure: `ClassifyRemoteURL(url, gitHosts)` and `HasPassword(url)` are table-tested. Later stories may append checks.
- **Dependencies on other work:** #17 (profile store) and #15 (GitHub adapter: drop list and readiness) are required before the `exec` command is registered; Phase 1 (the pure core) needs neither. Until #21–#23 land, a started command has the neutral credential baseline (#68): it cannot reach personal credentials, and Git and `gh` fail with agent-id's messages instead of acting as anyone.

## 📝 API Contracts

### Command

```text
agent-id exec --profile <name> [--] <command> [args…]
```

| Input | Rule | Failure |
|---|---|---|
| `--profile` | required (checked by the handler: `--profile is required`); resolved through the profile store, which validates the name | 125, the store's message (`ErrProfileNotFound`, `ErrConfigNotFound`, `ErrUnsupportedProvider`, …) |
| `<command>` | required; looked up with the child's `PATH` unless it contains `/` | 125 when absent; 127 when not found (non-executable files in `PATH` are skipped), and also when `execve` returns `ENOENT` after a successful lookup (a script whose `#!` interpreter, or a binary whose loader, is missing; or the file removed in between), as `env(1)` and the shells report it (#77); 126 when a name with `/` exists but cannot be executed, or `execve` fails with any other error (`EACCES`, `ENOEXEC`, `ELOOP`, `ENOTDIR`, `E2BIG`, …) |
| other flags | none; anything after the first non-flag argument belongs to the command | — |

### Exit status

| Status | When |
|---|---|
| the command's | always, once the command started (`execve` succeeded) |
| 125 | agent-id failed before starting the command: usage, config, profile, nesting, inherited Git configuration, neutral directories, remote checks, binary path |
| 126 | the command was found but `execve` failed with an error other than `ENOENT` (permission denied, not an executable format) |
| 127 | the command was not found in the child's `PATH`, or `execve` returned `ENOENT` (missing interpreter or loader, #77) |

`exec` writes nothing to stdout. On failure it writes exactly one line to stderr, `agent-id: exec: <message>`, printed by `cmd/agent-id`.

### Child environment (contract version 1)

Starting from the parent environment, in this order:

0. **Normalize** (#72). Reduce the parent to one value per name. An entry without `=` or with an empty name is discarded: a child cannot read it as a variable, and no later step could match it. When a name occurs more than once, the **last** occurrence wins (the value that bash and zsh children use) and the others are discarded. Go's `os.Environ` keeps the first and a libc `getenv` may differ, so the tests assert uniqueness and the decoy, not which duplicate won; the spec's rule is for the builder's output. Names compare byte for byte, case-sensitively. Every reader of the parent environment (`ReadContext`, `CheckInheritedGit`, `Build`) uses this one view, `NormalizeEnv` (pure), so a check and the child can never see different values. The output has unique names, and every later step acts on a name exactly once: `Drop`, the `AGENT_ID_*` removal, `Set`, the context variables, `GIT_CONFIG_*` and `PATH` apply to **every** occurrence of a name because there is only one. No earlier occurrence of a dropped name, such as a second `GH_TOKEN`, survives.
1. Remove every `AGENT_ID_*` variable, every variable in `Plan.Drop`, and, on re-entry, the previous contribution recorded by the inherited context (see Reading the inherited context).
2. Set the context variables:

   | Variable | Value | Consumers |
   |---|---|---|
   | `AGENT_ID_CONTEXT` | `1` (the contract version). Its presence means agent mode. | nested `exec`/`env`, `whoami`, `doctor` |
   | `AGENT_ID_ACTIVE_PROFILE` | the profile name (`^[a-z](?:[a-z0-9-]{0,30}[a-z0-9])?$`, the profile-config rule, shell-safe) | nested `exec`/`env`, `whoami`, `doctor` |
   | `AGENT_ID_MECHANISM` | `exec` or `env` | `whoami`, `doctor` |
   | `AGENT_ID_BIN` | absolute path of the agent-id binary | the credential helper entry (#22) and the `gh` shim (#23) |
   | `AGENT_ID_GIT_CONFIG_BASE` | the number of inherited `GIT_CONFIG_*` entries before agent-id's (an integer ≥ 0); always set | re-entry, `doctor` check D6 |
   | `AGENT_ID_GIT_CONFIG_END` | `GIT_CONFIG_COUNT` right after agent-id's append (`BASE + len(Plan.GitConfig)`); always set | re-entry, `doctor` check D6 |
   | `AGENT_ID_PATH_PREPEND` | the `Plan.PathPrepend` directories in order, joined with `:`; always set; the empty string is the empty list (#70) | re-entry, `doctor` |
   | `AGENT_ID_SET_NAMES` | the names in `Plan.Set`, sorted, without duplicates, and joined with `,`; always set; the empty string is the empty list (#70) | re-entry, `doctor` |
   | `AGENT_ID_USER_ZDOTDIR`, `AGENT_ID_USER_BASH_ENV`, `AGENT_ID_USER_CLAUDE_ENV_FILE` | exec only (#135): the developer's own `ZDOTDIR`, `BASH_ENV`, and `CLAUDE_ENV_FILE`; the empty string when unset. They are in `Plan.Set` and in `AGENT_ID_SET_NAMES`. | the shell hook files, re-entry, `doctor` |

   No variable selects a profile: commands that need a profile still take `--profile` (profile spec). The credential helper and the shim receive their profile on their own command line (#22, #23), not from these variables. `AGENT_ID_ACTIVE_PROFILE` is a deliberately different name from the reserved, non-existent selection variable `AGENT_ID_PROFILE` (profile spec). The two variables of #70 are part of contract version 1: version 1 has not been released and the owner has not signed it off (OQ5), so there is no compatibility rule for a context without them; such a context is damaged.
3. Set `Plan.Set`.
4. Append `Plan.GitConfig` after the inherited entries: `GIT_CONFIG_KEY_<base+i>`, `GIT_CONFIG_VALUE_<base+i>`. **`GIT_CONFIG_COUNT` is always written**, as the canonical decimal `base+len(Plan.GitConfig)`, also when `Plan.GitConfig` is empty (#69). An inherited `0`, empty, or unset count is the same state (base 0), and every path through `Build` (fresh, re-entry, empty or non-empty plan) normalizes it identically, which is what makes `Build` idempotent. The child of a parent with no count therefore has `GIT_CONFIG_COUNT=0`; Git reads it as no entries.
5. Prepend `Plan.PathPrepend` to `PATH`, after removing every existing `PATH` element equal to one of them.

Everything else is inherited unchanged, including `GIT_CONFIG_PARAMETERS` that passed the O2 check, `SSH_AUTH_SOCK`, and `GIT_SSH_COMMAND` (O3: documented, detected by `doctor`). The builder is the security decision (`ARCHITECTURE.md`, dependency rule 5) and has these properties, each a table or property test:

- **Idempotent:** `Build(Build(E, p), p) == Build(E, p)` for every `E` and `p` for which `Build` succeeds (the re-entry path), including a parent with an inherited count of `0`, empty, or unset, and an empty `p.GitConfig` (#69).
- **Changed-plan re-entry** (#70): `Build(Build(E, p1), p2) == Build(E, p2)` when every name in `p1.Set` is in `p2.Set` or absent from `E`, `E`'s `PATH` has no element equal to a `p1.PathPrepend` directory, and `E` has no `GIT_CONFIG_KEY_i`/`VALUE_i` at or above its count, and `p1.Drop ⊆ p2.Drop` (a name dropped, or a `PATH` element removed as a duplicate, by the first pass is not restored). The `Set` exception is deliberate: a name that only the previous plan set is unset on re-entry, not restored to the developer's value, which agent-id never recorded (fail-safe).
- **Unique names** (#72): the output has no repeated name, for any `E`, including duplicates and entries without `=`.
- **Append-only for Git:** inherited entries `0…base-1` are byte-identical in the result.
- **Order-stable:** the output is sorted by variable name, so equal inputs give equal outputs (golden tests, #26 rendering).
- **Secret-free:** the builder rejects a plan whose `Set`, `GitConfig`, or `PathPrepend` value contains a registered token prefix, `-----BEGIN`, a JWT shape, a newline, or a NUL byte, a `Set` name that is not `^[A-Za-z_][A-Za-z0-9_]*$` or that starts with `AGENT_ID_` or `GIT_CONFIG_` or is `PATH`, or a `PathPrepend` element that is not an absolute path, contains `:`, or repeats an earlier element, or a repeated `Set` name (`ErrPlanUnsafe`; #70, the recorded lists need unambiguous separators). This is a guard against a future contributor's bug, not input validation of user data.

### Reading the inherited context

| Inherited state | Decision |
|---|---|
| no `AGENT_ID_CONTEXT` | fresh start (other stray `AGENT_ID_*` variables are removed and ignored) |
| `AGENT_ID_CONTEXT=1`, `AGENT_ID_ACTIVE_PROFILE` equals `--profile` | re-entry: Build removes the previous contribution, then applies the plan |
| `AGENT_ID_CONTEXT=1`, a different valid profile | `ErrNestedProfile` |
| `AGENT_ID_CONTEXT` any other value | `ErrContextInvalid` (incompatible version) |
| `AGENT_ID_CONTEXT=1`, but `AGENT_ID_ACTIVE_PROFILE` missing or not a valid name, or `AGENT_ID_GIT_CONFIG_BASE`/`_END` missing, not integers, or `BASE > END`, or `AGENT_ID_PATH_PREPEND`/`AGENT_ID_SET_NAMES` missing, a recorded directory that is not absolute, or a recorded name that fails the `Set` name rule, or a recorded list with a duplicate entry or an unsorted name list (#70; an empty string is the empty list, not a list with one empty entry) | `ErrContextInvalid` (damaged) |
| `AGENT_ID_CONTEXT=1`, same profile, but `GIT_CONFIG_COUNT` (unset or empty counts as 0) differs from `AGENT_ID_GIT_CONFIG_END` | `ErrContextInvalid` (Git entries changed after activation: a startup file unset them, R8, or another tool appended after agent-id) |

Removing the previous contribution on re-entry uses **only what the inherited context recorded**, never the current plan (#69, #70):

- **Git.** With `GIT_CONFIG_COUNT == AGENT_ID_GIT_CONFIG_END` verified above, drop `GIT_CONFIG_KEY_n`/`GIT_CONFIG_VALUE_n` for `BASE ≤ n < END`. The count is then written by step 4 exactly as on a fresh start, so an inherited `0` or empty count gives the same result on both paths (#69).
- **`PATH`.** Remove every element equal to a directory in `AGENT_ID_PATH_PREPEND`, all occurrences, whatever the current plan prepends. A re-entry with a different `AGENT_ID_BIN` (allowed) or a refreshed plan leaves no old shim directory behind (#70).
- **`Set`.** Remove every name in `AGENT_ID_SET_NAMES`, then apply the current `Plan.Set`. A name that only the previous plan set is unset, not restored.

On a fresh start, a stray `AGENT_ID_PATH_PREPEND` or `AGENT_ID_SET_NAMES` without `AGENT_ID_CONTEXT` is removed with the other stray `AGENT_ID_*` variables and never used. The context variables are a convenience for a cooperating process tree, not a security boundary: an agent can edit them, unset them, or start another profile, and an edited list only changes the agent's own child (brief §10; a documented bypass).

### Inherited Git process configuration

- **`GIT_CONFIG_COUNT`** (G2–G4): when present, it must be a decimal integer `0 ≤ n ≤ 1000` or the empty string (0, as in Git); the cap of 1000 is agent-id policy, stricter than Git, and `GIT_CONFIG_KEY_i` and `GIT_CONFIG_VALUE_i` must exist for every `i < n` (an empty value is allowed, as in Git). Otherwise `ErrInheritedGitConfig` naming the variable that is missing or bad. A `GIT_CONFIG_KEY_i`/`VALUE_i` with `i ≥ n` is left alone (Git ignores it), and the append overwrites it.
- **`GIT_CONFIG_PARAMETERS`** (O2): parsed with Git's own quoting (`'key'='value'` pairs, the older `'key=value'` form, and a bare `'key'` meaning true, separated by spaces, with the `'\''` and `'\!'` escapes of Git's `sq_dequote`). A key is forbidden when, compared case-insensitively, its section is `credential`, `url`, `http`, `gpg`, `include`, or `includeif` (an include can pull in any of the others, and Git reads it after `GIT_CONFIG_COUNT`), or it is `commit.gpgsign`, `tag.gpgsign`, `tag.forcesignannotated`, `push.gpgsign`, `user.signingkey`, or `core.askpass` (it would override the `core.askPass` reset of the baseline; review of #86). A forbidden key or an unparseable value → `ErrInheritedGitConfig`. Anything else is kept unchanged and reported by `doctor` (D4).
- **Messages never echo an inherited environment value** (#71). They name variables, never values; among the variables of this contract the only inherited text printed is a profile name that passed the profile-name rule (`ErrNestedProfile`), and the context version is reported as `AGENT_ID_CONTEXT is not 1`, not echoed. The remote-check messages of step 7 are a separate class: they name remote names, configuration keys, file paths (including the file named by `$NETRC`, and the paths derived from `HOME` or `XDG_*` that the profile store and `ErrNeutralDir` print), aliases and header names, never a header value, password or user-info. For a bad or out-of-range `GIT_CONFIG_COUNT` the text is `inherited GIT_CONFIG_COUNT is not a decimal integer from 0 to 1000`: the raw string is not printed, not even cut, because a cut is not a redaction (a classic token is 40 characters, under the 64-character cut). For a missing entry the message names `GIT_CONFIG_KEY_<i>` or `GIT_CONFIG_VALUE_<i>`, where `<i>` is the integer parsed and bounded to 0..1000, never raw text. For `GIT_CONFIG_PARAMETERS` the message names the **section** of the forbidden key and, only when the variable is one of `helper`, `usehttppath`, `username`, `extraheader`, `insteadof`, `pushinsteadof`, `path`, `program`, `signingkey`, `gpgsign`, `forcesignannotated`, `askpass`, that variable (`credential.helper`). A section is spelled in lower case, as compared (`includeif`). It never prints a subsection (a URL or an include condition, shown as `<…>`), any other component, or a value, because they can hold credentials (C21, U11 of the analysis).

### Errors

| Sentinel | Status | Message after `agent-id: exec: ` |
|---|---|---|
| `ErrNoCommand` | 125 | `no command given; usage: agent-id exec --profile <name> [--] <command> [args…]` |
| `ErrProfileRequired` | 125 | `--profile is required` |
| a flag-parse error (unknown flag, missing value), wrapped by `SetFlagErrorFunc` | 125 | cobra's message, unchanged |
| profile store sentinels | 125 | the store's message, unchanged |
| `ErrProfileNotReady` (wraps the adapter's error) | 125 | `profile "reviewer": bot identity is not resolved; run agent-id setup github --profile reviewer` (the hint text is the adapter's) |
| `ErrNestedProfile` | 125 | `already in an agent context for profile "claude"; refusing to start profile "reviewer" inside it (nested exec with a different profile is not supported); start it from a terminal outside the agent context` |
| `ErrContextInvalid` | 125 | `the inherited agent context is <damaged: AGENT_ID_ACTIVE_PROFILE is missing \| from an incompatible agent-id (AGENT_ID_CONTEXT is not 1)>; start from a terminal outside the agent context` |
| `ErrInheritedGitConfig` | 125 | `inherited GIT_CONFIG_COUNT is not a decimal integer from 0 to 1000; every git command would fail; fix or unset the GIT_CONFIG_* variables` / `inherited GIT_CONFIG_COUNT requires GIT_CONFIG_KEY_1, which is not set; every git command would fail; fix or unset the GIT_CONFIG_* variables` / `inherited GIT_CONFIG_PARAMETERS sets credential.<…>.helper, which would override agent-id's credentials; unset GIT_CONFIG_PARAMETERS or remove that setting (agent-id doctor shows its source)` (#71: no inherited value, no subsection) |
| a remote check's error from a later story | 125 | the check's message |
| `ErrBinaryPath` | 125 | `cannot determine the path of the agent-id binary: <reason>` |
| `ErrPlanUnsafe` | 125 | `internal error: activation plan contains a forbidden value in <variable>` (never the value) |
| `ErrNeutralDir` (#68) | 125 | `<path>: <is a symlink \| is writable by group or others \| is owned by another user \| contains hosts.yml>; agent-id keeps this directory private and empty of credentials; <fix>` |
| `ErrSSHRemote` (#68, O1) | 125 | `remote "origin" reaches github.com over SSH (<scp form \| ssh:// \| alias "<alias>">)<, through <rule key> from <scope> <file>>; the agent would use your SSH key; use an HTTPS remote (agent-id doctor shows details)` |
| `ErrRemoteCredentials` (#68, D2) | 125 | `remote "origin" has a password in its URL; remove it (git remote set-url origin https://<host>/<owner>/<repo>.git)` (the URL is printed with the user-info replaced by `[redacted]`; `<host>` and `<owner>/<repo>` come from the remote, never a hard-coded host) |
| `ErrRemoteHeader` (#68, D3) | 125 | `remote "origin" gets an extra HTTP header (<header name>) on <host> from <scope> <file> that agent-id cannot reset; remove that http.<url>.extraheader entry` |
| `ErrRemoteHeader` (#100, unparsable pattern) | 125 | `an http.<url>.extraheader entry from <scope> <file> has a URL pattern that agent-id cannot parse as Git does, so Git may send its header to a Git host; remove that entry` |
| `ErrSSHRewrite` (#68, O1) | 125 | `<rule key> from <scope> <file> rewrites https://<host>/ to SSH; the agent would use your SSH key for every clone; remove the rule or scope it to a repository` |
| `ErrNetrc` (#68) | 125 | `<path> has credentials for <host> (or a default entry), which Git would send for the agent; move it aside while the agent runs` |
| `ErrRemoteCheck` (#68) | 125 | `cannot check remote "origin" (<timeout \| ssh -G failed \| git failed>); refusing to start` |
| `ErrCommandNotFound` | 127 | `"<command>": command not found` / `"<command>": interpreter or loader not found` (`ENOENT` from `execve` after a successful lookup, #77) / `"<command>" resolves to a relative path through an empty or "." PATH entry; use ./<command>` |
| `ErrCommandNotExecutable` | 126 | `"<command>": <permission denied \| exec format error \| …the errno text of any other execve failure>` (never `ENOENT`, which is 127, #77) |

Echoed values from the profile config are cut to 64 characters with control characters replaced by `?`, as in the profile spec. No inherited environment value, and no Git configuration value, is echoed (#71; names and paths per the rule above).

`LookPath(env)` follows Go's `exec.LookPath` semantics on the child's `PATH`: a name with `/` is used as given (126 when it exists but cannot be executed, 127 when it does not exist); otherwise each `PATH` element is tried in order, non-executable files and directories are skipped, and no match is 127. A match through a relative element (empty, `.`, or any path not starting with `/`) is a relative path and is refused (127, as Go's `exec.ErrDot`), so a command never runs from the current directory by accident.

### The binary path (`AGENT_ID_BIN`)

`os.Executable()` gives the running binary. When `os.Args[0]` resolves (absolute, relative, or through `PATH`) to a file that `os.SameFile` reports as the same binary, that path is used instead, made absolute and cleaned, so a Homebrew install records `/opt/homebrew/bin/agent-id` (stable across upgrades) rather than the versioned Cellar path. A path with a control character, a newline, or a non-UTF-8 byte is refused (`ErrBinaryPath`), because #22 embeds it in a Git configuration value.

## 📝 Edge Cases & Failure Scenarios

| Situation | What the user sees |
|---|---|
| `agent-id exec --profile claude` with no command | 125, usage line. |
| `agent-id exec --profile claude -- -v` | `-v` is the command; 127 unless a program named `-v` exists. |
| Command is a shell builtin or alias (`cd`, `ll`) | 127: `exec` runs programs, not shell syntax. Use `sh -c '…'`. |
| Command path contains `/` (`./run.sh`) | Not looked up in `PATH`; 126 or 127 from the file system. |
| Profile name with shell metacharacters | `Decide` first validates the syntax of `--profile` with the profile store's name rule (`ErrProfileNotFound`-style message naming the rule), so an invalid name is reported as such even inside a context; then nesting is decided. |
| Config file missing or invalid | 125 with the store's message (`run agent-id setup github --profile <name>`). |
| Profile of an unsupported provider | 125, `ErrUnsupportedProvider`. |
| Identity not yet resolved | 125 with the adapter's `setup` hint; no network. |
| Inside an `env`-activated Claude Code session (#26), same profile | Re-entry; the environment is identical except `AGENT_ID_MECHANISM` (`env` → `exec`) and, if a different agent-id binary runs, `AGENT_ID_BIN`. |
| Inside a session of another profile | 125, both names. |
| A startup file of the agent's shell unset `GIT_CONFIG_COUNT` but not the markers | 125 `ErrContextInvalid` (`GIT_CONFIG_COUNT` no longer equals `AGENT_ID_GIT_CONFIG_END`) on the next nested exec; `doctor` D6 explains. |
| Inherited `GIT_CONFIG_COUNT` with a value like `2 ` or `0x2` | 125, malformed. |
| Inherited `GIT_CONFIG_PARAMETERS` with `core.editor` only | Kept. |
| Git older than 2.43 | Not checked by `exec`. The remote checks use commands that also exist in older Git versions, and `doctor` reports the version (O4). |
| A repository with a GitHub SSH remote before #22 lands | 125 `ErrSSHRemote`: every SSH form is refused until #22's rewrites exist. Use an HTTPS clone. |
| `git push` inside the agent before #21–#22 land | Fails: no helper and no prompt, so Git reports missing credentials. This is the intended fail-closed result. |
| `gh` inside the agent before #23 lands | The stub's message, exit 1. |
| `ssh` not installed and a remote host that could be an alias | 125 `ErrRemoteCheck`: `exec` cannot prove the remote is not the provider's. |
| Ctrl-C in an interactive agent | Delivered once, to the agent only: agent-id is no longer running. |
| The agent-id binary is removed or upgraded during a long session | Later helper or shim invocations fail closed (Git and `gh` report a failed helper); `AGENT_ID_BIN` prefers the stable symlink to make this rare. |
| `exec` on Windows | Not supported (brief §8); only the `Execer` is built for darwin and linux, and elsewhere `exec` fails with 125. |
| The parent holds `GH_TOKEN` twice, or an entry `GH_TOKEN` without `=` (#72) | The entry without `=` is discarded; the duplicates collapse to the last value, and a dropped name is dropped in every occurrence. The child has one `GH_TOKEN`, the decoy. |
| Inherited `GIT_CONFIG_COUNT=0` or empty (#69) | Treated as base 0 and rewritten to the canonical count, so an `exec` inside an `exec` gives the same environment as one `exec`. |
| Nested exec with a different agent-id binary or a refreshed plan (#70) | The previous shim directories and `Set` names are removed using the recorded lists; the new plan's are applied. |
| A script whose shebang interpreter is missing (#77) | 127 `interpreter or loader not found`. |

## 📝 Risks & Impact Review

- **Contract.** The variable names, the contract version `1`, the exit codes 125–127, and the append rule are consumed by `env` (#26), `whoami`/`doctor` (#24), the Claude Code hook, and user scripts. A later change needs `AGENT_ID_CONTEXT=2` and a re-entry rule for version 1. This is the "environment that `exec` and `env` emit" of `AGENTS.md` (Ask first): the owner's confirmation is **pending** (#76, OQ5; #86 merged without it). The spec records no sign-off of contract v1 (variable names, version `1`, exit codes 125–127, the append rule and the always-written count, the #68 and #70 amendments); the decisions so far were taken under delegation, and merging this spec is not that confirmation. the follow-up code of #69–#72 and the first release wait for the answer.
- **Intermediate state on `main`** (amended by #68). Between #20 and #21–#23, a command started by `exec` cannot reach the developer's credentials through Git or `gh` on the supported paths: Git has no helper, no prompt, no personal header and no `.netrc` credentials for the provider, no SSH remote or rewrite to SSH; `gh` has a decoy token and an empty configuration. The documented bypasses (Not covered, above) remain, as everywhere in local mode. The guarantee is the enforced invariant of the neutral credential baseline, tested by #25, not a release rule. An earlier version of this spec relied on the prose rule "no release may contain `exec` before #21, #22, and #23", which the post-merge review of #48 (F1) classed as a fail-open blocker.
- **Generic helper reset** (#68, decision 1). Inside the agent context the developer's credential helpers for every host are off, not only the provider's. This is deliberate: the reset is the fail-closed baseline. The agent's access to other hosts is outside the MVP (brief §8).
- **Not a security boundary** (brief §10). The agent can unset the markers, call another binary, or start a shell that rewrites the environment; `exec` controls only what it starts. The spec claims nothing more.
- **`execve` removes the chance to clean up.** Any future feature that needs a supervising parent (a local token broker, audit events) belongs to the Phase 3 hardened runtime and would be a new mechanism, not a change to this one.
- **Refusing on inherited Git configuration may surprise** a developer whose environment carries `GIT_CONFIG_PARAMETERS` from another tool. The message names the key and the fix; refusing is the decided fail-closed behavior (O2).
- **Product direction vs. rule.** Nested-profile refusal and O1–O4 are recorded decisions. `execve`, the exit codes, the variable names, and the re-entry rule are technical decisions of this spec. OQ1–OQ3 below are product and security-model choices recorded for the owner with a recommendation; the spec applies the recommendation.

## 📝 Decisions in play

- Brief §3 principle 1, §7.2, §8 *Process-scoped execution*, §10 (not a boundary), §14 *Profile selection* (explicit only).
- `ARCHITECTURE.md`: Context runtime, dependency rule 5 (pure security decisions), Cross-cutting (exec returns the child's status), Key decisions (explicit `exec` canonical; `AGENT_ID_` prefix; provider abstraction).
- Profile spec: `Resolve`, no selection variable, the consumer rule for an unresolved identity (decided here).
- Project manager, 2026-11-13: nested different profile → error; O1–O4.

## Open Questions (owner) — recommendations applied

These change product behavior, not the design's structure. The spec applies the recommendation (except OQ5, a governance record that only the owner can give); each is reversible before the first release and does not block the spec. The implementation of the affected step waits for an answer only if the owner disagrees.

| # | Question | Recommendation (applied) | Why | Confirm? |
|---|---|---|---|---|
| OQ1 | Should `exec` obtain (or check) an installation token before it starts the command, so that an agent never starts without working credentials? | **No.** Start without network; the credential helper (#22) and the `gh` shim (#23) fail closed at use; `agent-id doctor --profile` is the explicit pre-check. | Keeps `exec` deterministic, fast, and usable offline; fail closed is preserved at the point of use; adding a check later is backward compatible. | owner, non-blocking |
| OQ2 | Should `exec` resolve a missing bot identity over the network on start, instead of refusing? | **No, refuse** with the adapter's `setup` hint. | The profile spec proposed this; no network on start (OQ1); a silent resolution would hide a half-configured profile. | owner, non-blocking |
| OQ3 | Should `exec` also remove inherited variables that are not the CLI's credential variables but commonly hold a personal GitHub token (`HOMEBREW_GITHUB_API_TOKEN`, `GITHUB_PERSONAL_ACCESS_TOKEN` of the GitHub MCP server, …)? | **No in the MVP:** remove only the provider's CLI credential variables; `doctor` reports every inherited value with a registered token prefix (#24). | The drop list stays a provider contract with a test; an open-ended name list would be incomplete anyway, and removing variables other tools rely on changes their behavior; local mode is not a sandbox (brief §10). | owner, non-blocking (security model) |
| OQ4 | (#68) How is the unkeyed keychain lookup of a `gh` started by absolute path closed? | **Resolved: a non-secret decoy `GH_TOKEN` (and `GH_ENTERPRISE_TOKEN`) from the provider's `NeutralCLI`**, plus the stub for a clear message. | The owner's Definition of Done for #20 (13.11) requires that the child cannot reach the keychain entry `gh:github.com`; the stub alone does not meet it (the independent review of #80, M3). This narrows decision 4 of #68: no *developer or bot* token is in the environment until #23, only a fixed decoy. | owner approve on #80 |
| OQ5 | (#76) Do you sign off environment contract v1: the variable names including the two added by #70, version `1`, exit codes 125–127, the append rule with the always-written `GIT_CONFIG_COUNT`, and the #68 amendment? | **Yes, confirm it in a comment on #20 or on this spec PR, and make it a condition of the first release**: #86 already implements the contract (merged), so the follow-up code that adds the #69–#72 changes is the last cheap moment to change it. | `AGENTS.md` (Ask first) puts the environment that `exec` and `env` emit under the owner's confirmation. Changes are cheap before the first release; after it they need `AGENT_ID_CONTEXT=2` and a re-entry rule for version 1. | **owner, blocking for the first release and for the follow-up code of #69–#72** (not applied: this spec states that the sign-off is pending) |

## Resolved assumptions (autonomous defaults)

Technical decisions taken by this spec, recorded for override:

| # | Question | Applied default | Why | Confirm? |
|---|---|---|---|---|
| A1 | How does `exec` run the command? | `execve`; no supervising parent. | Exit status and signals are the command's by construction; no double `SIGINT`. | reversible |
| A2 | Exit codes of agent-id's own failures? | 125 / 126 / 127 (`env(1)` convention). | Unambiguous next to the command's own codes. | reversible before the first release |
| A3 | Context variable names? | `AGENT_ID_CONTEXT=1`, `AGENT_ID_ACTIVE_PROFILE`, `AGENT_ID_MECHANISM`, `AGENT_ID_BIN`, `AGENT_ID_GIT_CONFIG_BASE`, `AGENT_ID_GIT_CONFIG_END`, `AGENT_ID_PATH_PREPEND`, `AGENT_ID_SET_NAMES` (#70). | `AGENT_ID_` prefix (#6); distinct from the reserved selection name `AGENT_ID_PROFILE`. | reversible before the first release |
| A4 | Same-profile nesting? | Deterministic rebuild after removing the previous contribution. | Idempotent; picks up a refreshed profile; refuses a context whose Git entries changed; no duplicates (#26). | reversible |
| A5 | Who appends `GIT_CONFIG_*` entries? | `internal/activation`, from plan data of #21/#22. | One composition rule; enables re-entry and `doctor` D6. | reversible |
| A6 | Where do O1, O2, G2–G4, D2 and D3 live? | `internal/activation`. O2 and G2–G4 are pure checks of the inherited environment. O1, D2 and D3 are remote checks with the child environment, in #20 (amended by #68; O1 was planned for #22). | O1 must hold from the first merge (#68). D2 and D3 close the same invariant with the same remote listing. | decided (#68) |
| A7 | Flag parsing | `--profile` required, interspersed flags off, `--` optional. | Matches `env`, `sudo`, `aws-vault exec`. | reversible |
| A8 | Output on success | None. | The command owns stdout and stderr. | reversible |

## 📋 Phasing

- **Phase 1 — pure core (`internal/activation`).** Context reading, nesting decision, inherited Git checks, builder, binary path, `LookPath`. Independently shippable and fully unit-tested; no command yet. Needs nothing beyond the repository.
- **Phase 2 — the `exec` slice.** Command, handler, exit-code mapping, process tests. Needs #17 (profile store) and #15 (the GitHub adapter supplies the drop list and readiness); handler tests use the fake `Activator` of `providertest` until then.
- **Phase 3 — record.** `ARCHITECTURE.md` and the capability archive.

## 📋 Implementation Plan

Every test calls `testenv.Isolate(t)` first. `risk-high` evidence per `SDLC.md`: hostile-input tables in each pure step, a process test proving the inherited personal token does not reach the command, a scan proving no token in any output or error.

### Phase 1 — `internal/activation`

1. **Contract.** Package `internal/activation` with its `AGENTS.md` (invariants: pure functions; the only owner of the child environment; never a token in the environment; no forge names), the types and sentinels above, and a row in the root `AGENTS.md` task routing. Test: the package builds; sentinels are distinct.
2. **Read the context and decide.** `ReadContext`, `Decide`. Table: no context; stray `AGENT_ID_*` without `AGENT_ID_CONTEXT`; same profile; different profile; `AGENT_ID_CONTEXT=2`, empty, `1 `; missing or invalid `AGENT_ID_ACTIVE_PROFILE` (uppercase, metacharacters, 33 characters); base not an integer, negative, or greater than the count; `AGENT_ID_PATH_PREPEND`/`AGENT_ID_SET_NAMES` missing, empty (accepted as the empty list), with a relative directory, a duplicate entry, an unsorted or badly named list (#70).
3. **Inherited Git configuration.** `CheckInheritedGit`, `ParseGitConfigParameters`. Tables: G2–G4 and the analysis's malformed counts (`x`, `-1`, `1001`, `2 `, `0x2`); every forbidden section in mixed case; `credential.https://user:tok@github.com.helper` (the message must not contain `user`, `tok`, or the URL); `include.path` and `includeIf.gitdir:/x/.path` (forbidden); `push.gpgSign`; old and new quoting; a bare key without `=`; `'\''` and `'\!'` escapes (`alias.x=!cmd` is harmless and kept); an unterminated quote; `GIT_CONFIG_COUNT=` (empty, accepted as 0); harmless keys kept; a `GIT_CONFIG_COUNT` that is token-shaped (`testenv.FakeToken`, `github_pat_…`) → the error names only the variable and shares no substring of 8 or more characters with the value (#71); duplicate `GIT_CONFIG_COUNT` entries (the last one is checked, #72).
4. **Build.** `Build` with its steps 0–5 and its properties (idempotent, changed-plan, unique names, append-only, order-stable, secret-free). Tables with hostile parents: inherited `GH_TOKEN`/`GITHUB_TOKEN` (fake), an inherited count of 2 with entries, stray `GIT_CONFIG_KEY_5`, `PATH` already containing a prepend directory twice, an empty `PATH`. Base and end markers are always set, also with an empty `Plan.GitConfig`. Re-entry tables: count equal to end (truncate), count unset (R8 → `ErrContextInvalid`), count greater than end (another tool appended → `ErrContextInvalid`, nothing dropped). Property test over generated parents and plans: idempotence and append-only. Secret guard: `testenv.FakeToken`, `-----BEGIN`, a JWT shape, a newline in `Set`, `GitConfig`, `PathPrepend` → `ErrPlanUnsafe` without the value in the error; a `Set` name `PATH`, `AGENT_ID_X`, `GIT_CONFIG_COUNT` or `a b`, and a relative or colon-containing `PathPrepend` element → `ErrPlanUnsafe` (#70).
   Hostile-input rows added by the follow-up (#69, #70, #72, #73):
   - Duplicates (#72): `GH_TOKEN` twice (personal first, personal last, and decoy-shaped last), `GITHUB_TOKEN` three times, `GIT_CONFIG_COUNT` twice with different values, an `AGENT_ID_CONTEXT` twice, entries without `=` (`GH_TOKEN`, an empty string) and `=x`. Expected: one value per name, none of the dropped names survives in any occurrence, and the checks (`ReadContext`, `CheckInheritedGit`) see the same last value as `Build`.
   - Inherited count `0`, empty, and unset, each with an empty and a non-empty `Plan.GitConfig`, fresh and re-entry, for idempotence (#69).
   - Re-entry with a changed `AGENT_ID_BIN`, a changed `PathPrepend`, a changed `Set` name set, and a damaged recorded list (relative directory, bad name, missing variable) (#70).
   - The "Git entries changed" check (#73) with a non-empty plan (an unset count and an appended entry are refused), and, as a documented case, the vacuous state (unset count, `BASE == END == 0`: accepted).
   - Property tests: idempotence, changed-plan re-entry, unique names, append-only.
5. **Binary path and lookup.** `ResolveBinary` with a symlinked binary in a temporary directory, a relative `os.Args[0]`, a path with a newline; `LookPath(env)` using the child's `PATH`, not the parent's.

### Phase 2 — `internal/activation/exec`

6. **Slice.** `contract.go`, `handler.go` (`activation.Activate(ctx, deps, Request{…})`: read context → validate the name and decide → resolve → ready → inherited Git → neutral directories → build → remote checks; then look up → `Execer.Exec`), `command.go` (`SetInterspersed(false)`, `SetFlagErrorFunc`, `--profile` checked in the handler). Handler tests with a fake profile resolver, a fake `provider.Activator` from `providertest` (drop list, readiness), a fake remote check and a fake neutral-directory port, and a recording `Execer`: every row of the Errors table (including an unknown flag and a missing `--profile` → 125), an empty drop list → `ErrPlanUnsafe`, a script with a missing interpreter → 127 and `execve` `EACCES` → 126 (#77), the order of checks (nesting before readiness before inherited Git before the neutral directories before the remote checks), and that the recorded environment equals `Build`'s output.
7. **Composition root** (after #15 and #17). Register the slice; build `activation.Deps` with the GitHub adapter; add the key-free `provider.Activator` port with `CredentialVariables` (including `GH_HOST`), `Ready`, `GitHosts`, `NeutralCLI` and `TokenPrefixes`, its contract suite and fake in `providertest`, and `githubtest` support (#75; `provider.Provider` stays unchanged); add the exit-code error type and map it in `run`; the default for other commands stays 1. Tests: `run` returns 125/126/127 for the slice's errors and 1 for others; with the production registry, a fake inherited `GH_TOKEN` value is not in the environment that `Activate` returns (`GH_TOKEN` equals the provider's decoy, #68).
8. **Process tests.** Build `agent-id` with `testenv.BuildBinary`; the package's `TestMain` calls `testenv.Main`, which fails the run if the developer's Git or `gh` configuration changed; write a fixture config with a fake App and a cached identity. Cases: `exit 7` → 7; SIGTERM with a trap, sent only after the command created its readiness file → 42; SIGKILL → killed by signal; parent environment unchanged, and the isolated `HOME` tree unchanged (hash before and after) except agent-id's own `<cache>/agent-id/<provider>/` directories; no fake `GH_TOKEN`/`GITHUB_TOKEN` value in the command (`GITHUB_TOKEN` absent, `GH_TOKEN` the decoy, #68); nested same profile prints an identical environment; nested different profile → 125; `GIT_CONFIG_COUNT` removed by `env -u` while the markers survive, then a nested exec → 125 (#73: it runs against the baseline's non-empty `GitConfig`, so it can fail; it is this scenario's process test); duplicate-name parent through the real `execve` envp (#72); `does-not-exist` → 127; a script with a missing interpreter → 127 (#77); profile without identity → 125 with the fake GitHub recording zero requests. Scan stdout and stderr of every case for `ghs_`, `-----BEGIN`, and a JWT shape. These tests skip on platforms other than darwin and linux.
8a. **Fail-closed baseline (#68), in the #25 suite.** Process tests with decoys that record any use:
   - a fake personal `credential.helper` in the isolated global and system config, and a host-scoped one;
   - fake `GIT_ASKPASS`, `SSH_ASKPASS` and `core.askPass`;
   - a fake `hosts.yml` with a fake `oauth_token` in the parent's `GH_CONFIG_DIR`;
   - fake values in all four token variables;
   - a fake `gh` later on `PATH`.

   Inside `agent-id exec`:
   - `git ls-remote` and `git push` against `testenv.NewGitServer` fail without a prompt, and every decoy records zero uses;
   - `gh api user` prints the stub's message and exits 1, and the fake `gh` records zero calls;
   - the child environment holds no token variable.

   The SSH remote cases give 125 for:
   - scp form, `ssh://`, `ssh.github.com:443`, and an SSH alias through a fake `~/.ssh/config` read by `ssh -G`;
   - a personal `insteadOf` to SSH;
   - an SSH `pushurl`.

   The D2 and D3 cases give 125, and the message does not contain the secret. Wildcard header patterns (#87): `*.github.com`, `gist.*.com` and `*.github.*` are each checked against the installed Git's own matching (`git config --get-urlmatch http https://github.com/o/r.git` is the test oracle, never the decision): a pattern that Git applies to a provider host is refused with that host in the message, and one that it does not apply is not.

   The neutral directory cases give 125: a symlink, a group-writable directory, a `hosts.yml` present, a stub that is a symlink or has different content (rewritten), and a missing parent (created `0700`).

   These tests run without #21–#23 and stay in the suite after they land. They prove that the baseline remains under the bot's entries: with #22 present, the decoy helper still records zero calls.
8b. **Pure tables.** `ClassifyRemoteURL` covers scp form, `ssh://`, `git+ssh://`, `ssh+git://`, upper-case hosts, `ssh.github.com:443`, look-alike hosts (`github.com.evil.example`, `evilgithub.com`), `https`, `file://`, and local paths. `HasPassword` covers user-only, user and password, a percent-encoded `@`, and no user-info. The baseline entries are tested for their order after inherited entries (C18, G1) and for idempotence under re-entry.

### Definition of Done for #20: the isolation test (owner decision, 13.11, #68)

#20 is done only when this process test passes in CI and on darwin. It is a required condition, not one case among others. It belongs to the #25 suite and runs in `testenv.Isolate`:

- a fake `HOME`, `XDG_*`, `GIT_CONFIG_GLOBAL` and `GH_CONFIG_DIR`, all under `$TMPDIR/agent-id/`, with `testenv.Main` checking that the real configuration is untouched;
- a recording fake credential helper;
- a fake `hosts.yml`.

No real keychain, token or network is used.

It proves that the child of `agent-id exec` does **not** see:

0. Setup notes: the system-scope helper uses `GIT_CONFIG_SYSTEM=<tmp file>` with `GIT_CONFIG_NOSYSTEM` unset for that test, because `testenv.Isolate` sets `GIT_CONFIG_NOSYSTEM=1`.
1. **The developer's credential helper**, neither the global one (`credential.helper` and `credential.https://github.com.helper` in the fake global config) nor the repository's (the same keys in the test repository's `.git/config`).
   - Inside `exec`, `printf 'protocol=https
host=github.com
path=o/r.git

' | git credential fill` prints no `username=` or `password=`, exits non-zero without prompting (`GIT_TERMINAL_PROMPT=0`), and every recording helper logs zero calls.
   - The same holds for `git ls-remote` against `testenv.NewGitServer`.
2. **The developer's `gh` configuration or the keychain entry `gh:github.com`.**
   - The child's `GH_CONFIG_DIR` is agent-id's own directory and has no `hosts.yml`.
   - The fake `hosts.yml`, holding a fake `oauth_token` in the parent's `GH_CONFIG_DIR`, is never opened: a fake `gh` placed later on `PATH` that would read it records zero calls.
   - `gh` by name is the stub: it prints agent-id's message and exits 1.
   - The child's `GH_TOKEN` is the decoy, so `gh` started any way, including by absolute path, resolves its token from `GH_TOKEN` and never reaches `hosts.yml` or the keyring. Proved offline: the real `gh` by absolute path runs `gh auth token` inside `exec` and prints the decoy (skipped when `gh` is not installed). The test never touches a real keychain.
2a. **The developer's `.netrc`.** A fake `$HOME/.netrc` with a provider entry makes `exec` refuse (125); a transport test against `testenv.NewGitServer` with that file, run with the netrc check switched off in a test-only harness, shows why the refusal is needed: Git sends the `.netrc` credentials.
3. **The developer's tokens.** With fake values (`testenv.FakeToken`) in `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` in the parent, the child's environment (`env` inside `exec`) contains no developer value in these variables (`GH_TOKEN` and `GH_ENTERPRISE_TOKEN` hold only the fixed decoy; `GITHUB_TOKEN` and `GITHUB_ENTERPRISE_TOKEN` are absent) and no value with a registered token prefix, `-----BEGIN`, or a JWT shape.

The test passes without #21–#23 and stays required after they land.

### Phase 3 — record

9. **Architecture and archive.** The Key decisions row for `exec` and the exit codes already exist on `main` (#86). Amend the row when the follow-up code lands, with the two context variables of #70, the always-written `GIT_CONFIG_COUNT` (#69), `GH_HOST` in the drop list (#75) and exit 127 for `ENOENT` (#77); archive the Delta into `.ai/specs/capabilities/exec.md`. The O1–O4 row and the Git integration bullet of `ARCHITECTURE.md` already record the full O2 refused set (the `credential`, `url`, `http`, `gpg`, `include` and `includeif` sections, plus `commit.gpgsign`, `tag.gpgsign`, `tag.forcesignannotated`, `push.gpgsign`, `user.signingkey` and `core.askpass`), updated with the #74 amendment; any later change keeps both in step with this spec (#74).
