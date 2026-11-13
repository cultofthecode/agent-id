# gh under a GitHub App installation token: command matrix, token and host precedence

- Issue: #14 (E02-T02). Feeds E02-S09 (`gh` token injection) and `internal/ghint`.
- Decisions in play: `PRODUCT_BRIEF.md` §9.3 (the `gh` wrapper), §14 (GitHub CLI compatibility); `ARCHITECTURE.md`, GitHub CLI integration.
- Date: 2026-11-12. Analysed version: `gh` 2.103.3 (2026-10-26) and its library `github.com/cli/go-gh/v2` v2.17.0.
- No real token was used in an experiment. Every token below is a fake with the prefix `ghs_aidtest_`.

## Summary

1. `gh` picks its token per **host**, not per command. For `github.com`, `*.github.com`, `*.ghe.com`, and `github.localhost` the order is `GH_TOKEN`, `GITHUB_TOKEN`, then `hosts.yml`, then the OS keyring. For every other host the order is `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `hosts.yml`, keyring. An empty variable counts as unset.
2. The host comes from `--hostname`, `-R/--repo` or `GH_REPO` (`[HOST/]OWNER/REPO`), the git remotes of the current repository, or `GH_HOST`, in that order of specificity. **A command that reaches a host other than `github.com` silently falls back to the developer's `hosts.yml` or keyring.** That is the main leak path that the injection must close.
3. Repository-scoped commands work with an installation token when the App has the matching permission: `pr create/view/list/merge/comment`, `issue create/list/view/comment/edit/close`, `api` on repository endpoints, `repo view/clone OWNER/REPO`, `run *`, `release *`.
4. Commands that need a **user** fail or do the wrong thing: anything that resolves `@me` or a bare repository name through the viewer login, `gh status`, `gist`, `repo create/fork` for a personal account, `org list`, `ssh-key`, `gpg-key`, `codespace`, `agent-task`, and `api /user…`.
5. `gh auth *` needs special handling. `login`, `switch`, `logout`, and `refresh` refuse while `GH_TOKEN` is set for `github.com`, but they still work against another host. `setup-git` **writes the global Git configuration** (demonstrated below). `status` reads and validates every personal account in `hosts.yml`. `token` prints the installation token.

## Method

- Source reading of `cli/cli` at tag `v2.103.3` and `cli/go-gh` at `v2.17.0`. The citations below use `cli@v2.103.3:<path>:<line>` and `go-gh@v2.17.0:<path>:<line>`.
- The `gh help environment` topic of the same version (`cli@v2.103.3:pkg/cmd/root/help_topic.go:41-140`).
- An offline experiment (appendix A): the real `gh` 2.103.3 binary, run in an isolated environment with fake tokens. It covers only behavior that `gh` decides locally (token choice, host choice, `gh auth` guards). It makes no network request to GitHub.
- GitHub documentation and community reports, for server-side behavior. Those rows are marked **E2E**: confirm them in the end-to-end suite with a disposable repository and a test App (`SDLC.md`, Validation gate).
- One incidental observation: during tracker work for this issue (`GH_TOKEN=$(ghapp token) gh …`, the agent's normal tracker credential, outside any test), `gh api user` answered `HTTP 403 Resource not accessible by integration`, and `gh api /app` answered `HTTP 401 A JSON web token could not be decoded`. The token was not printed or stored.

The `testenv.FakeGitHub` was not used. `gh` speaks plain HTTP only to `github.localhost`, at the fixed URL `http://api.github.localhost/` (`cli@v2.103.3:internal/ghinstance/host.go:53-67`): that needs name resolution for `api.github.localhost` and port 80, outside the 18000–18999 range that `AGENTS.md` allows. Every other host is HTTPS. A fake GitHub that `gh` can reach is a design question for E02-S09's process tests (open question 5).

Evidence codes in the tables: **S** source, **X** offline experiment, **D** GitHub documentation or community report, **O** incidental observation, **E2E** server behavior still to confirm.

## How gh picks its token

`gh` resolves a token for the host of each request through `AddAuthTokenHeaderWithRefresh` (`cli@v2.103.3:api/http_client.go:206`), which calls `AuthConfig.ActiveTokenWithRefresh` (`cli@v2.103.3:internal/config/config.go:332-344`). That function takes the credential from `AuthConfig.ActiveToken` (`config.go:295-330`) and refreshes it only when it is a refreshable OAuth credential, so refresh never applies to a token from an environment variable. (`AddAuthTokenHeader`, `http_client.go:186-201`, is the backward-compatible entry point for external callers and uses the same order.)

| Order | `github.com`, `*.github.com`, `*.ghe.com`, `github.localhost` | Any other host (GitHub Enterprise Server) | Evidence |
|---|---|---|---|
| 1 | `GH_TOKEN` | `GH_ENTERPRISE_TOKEN` | S `go-gh@v2.17.0:pkg/auth/auth.go:63-95`; X cases 1, 6 |
| 2 | `GITHUB_TOKEN` | `GITHUB_ENTERPRISE_TOKEN` | S same; X cases 2, 7 |
| 3 | `hosts.yml` → `<host>.oauth_token` in `GH_CONFIG_DIR` | same | S same; X case 3 |
| 4 | the active user's credential (per-user `hosts.yml` entry or keyring `gh:<host>`/`<user>`) | same | S `config.go:314-318` |
| 5 | the unkeyed keyring entry `gh:<host>` | same | S `config.go:322-324`, `:382-384` |

Facts that matter for the injection:

- **An empty variable is unset.** `GH_TOKEN=` falls through to `hosts.yml` (X case 4). The wrapper must never start `gh` with an empty token: that is a silent fallback to the developer's account.
- **The family of variables depends on the host only.** `GH_TOKEN` is never sent to an enterprise host, and `GH_ENTERPRISE_TOKEN` is never used for `github.com` (X cases 5, 6).
- **Host normalization.** `api.github.com`, `uploads.github.com`, and every other `*.github.com` name map to `github.com`; the comparison is case-insensitive (`go-gh@v2.17.0:pkg/auth/auth.go:178-200`; X cases 9, 10). Release asset uploads to `uploads.github.com` therefore get `GH_TOKEN`.
- **Tenancy hosts** (`<name>.ghe.com`, GitHub Enterprise Cloud with data residency) use `GH_TOKEN` too (X case 8). Not in MVP scope (brief §8), but a token for `github.com` reaches them if a remote points there.
- **Redirects.** The token is not attached when a redirect changes the host (`cli@v2.103.3:api/http_client.go:237-246`). `run view --log`, `run download`, and `release download` follow redirects to blob storage without the token.
- **`api_host`.** Version 2.103 added an `api_host` key in `hosts.yml`; a request to an `api_host` uses the token of the host that it stands in for (`api/http_client.go:190-197`). It does not change the order above.
- **Keyring reads ignore `GH_CONFIG_DIR`.** Steps 4–5 read the OS keyring under the service name `gh:<host>`. Pointing `GH_CONFIG_DIR` to an empty directory removes step 3 and the per-user lookup of step 4. It does not remove step 5 (inference from `config.go:322-324`; not run, to avoid touching the developer's keychain).

## How gh picks its host

| Source | Effect | Evidence |
|---|---|---|
| `--hostname` (`gh api`, `gh auth *`) | Explicit host for that command. | S |
| `-R/--repo [HOST/]OWNER/REPO`, then `GH_REPO` | Overrides the git remotes completely. The host part, when present, selects the token family. | S `cli@v2.103.3:pkg/cmdutil/repo_override.go:75-85`; `gh help environment` |
| git remotes of the current directory | Sorted `upstream`, `github`, `origin`, then the others; filtered to hosts that have a token (env or `hosts.yml`) plus `github.com` and the default host; the first one wins. | S `cli@v2.103.3:pkg/cmd/factory/remote_resolver.go:34-90`, `pkg/cmd/factory/default.go:58-90` |
| `GH_HOST` | Default host when nothing above names one. With `GH_HOST` set, remotes on other hosts are dropped **without fallback**; `gh` fails if none matches. | S `remote_resolver.go:72-86`; X case 11 (the mirror case: remote on `github.com`, `GH_HOST` on another host) |
| `hosts.yml` with exactly one host | Default host when `GH_HOST` is unset. | S `go-gh@v2.17.0:pkg/auth/auth.go:146-156` |
| none | `github.com`. | S same |

Consequences:

- `GH_HOST=github.com` makes repository commands fail closed when the only remote is on another host: `none of the git remotes configured for this repository correspond to the GH_HOST environment variable. Try adding a matching remote or unsetting the variable`. X case 11 shows this on the mirror case (remote on `github.com`, `GH_HOST=ghe.example.test`); both directions go through the same branch of `remote_resolver.go:72-86`.
- `-R`, `GH_REPO`, and `--hostname` **bypass** that filter. `gh pr list -R ghe.example.com/o/r` inside the agent context reads the developer's enterprise credential from `hosts.yml` or the keyring. Removing `GH_ENTERPRISE_TOKEN` makes this worse, not better, for that host: the fallback is the personal credential. The wrapper must refuse a host other than the profile's host (open question 4).
- An inherited `GH_HOST=<enterprise host>` with an injected `GH_TOKEN` makes `gh` use the enterprise variables or the personal enterprise credential, not the injected token (X case 6). The wrapper must pin `GH_HOST`.

## Command matrix

Assumptions: `GH_TOKEN` holds an installation token (`ghs_…`) of the profile's App; the host is `github.com`; the repository is one the installation can access; `gh` runs without a TTY, as agents usually run it. "Permission" names the GitHub App permission that the operation is expected to need. It is inferred from the resource that the endpoint touches, not checked against GitHub's per-endpoint permission tables; confirm the exact set in **E2E**.

Legend: ✅ works · ⚠️ works with a limit that the agent must know · ⛔ user-centric or unsafe: agent-id should stop it with the error in the next section (code in the last column).

### pr

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `pr create --title … --body …` (branch already pushed, or `--head`) | ✅ | Creates the PR as `<app-slug>[bot]`. | Pull requests: write; Contents: read | S, D, E2E | — |
| `pr create` (branch not pushed, no TTY) | ⚠️ | `aborted: you must first push the current branch to a remote, or use the --head flag` (`pr/create/create.go:1006-1008`). Push with `git push` first; Git uses the agent-id credential helper. | — | S | — |
| `pr create` (interactive, no push access) | ⛔ | Calls the viewer login (`create.go:1027`) and offers to fork into the viewer's account. An installation has no user account to fork into. | — | S, E2E | `AID-GH-USER` |
| `pr create --assignee @me`, `--reviewer @me`, `pr edit --add-assignee @me` | ⛔ | `@me` becomes the bot login (`pr/shared/params.go:298-314`). A bot cannot be assigned or requested as a reviewer (E2E). | — | S, E2E | `AID-GH-ME` |
| `pr create --project …` | ⚠️ | Needs organization Projects permission; without it the call fails. | Projects (organization): write | S, D | — |
| `pr view`, `pr view <n>` | ✅ | Errors on project fields are ignored (`api/queries_projects_v2.go:330-342`, `pr/shared/finder.go:288-294`). The default field set includes `statusCheckRollup` (`pr/view/view.go:92-97`), so the App also needs read access to checks and commit statuses (D: a fine-grained token without it fails on that field). | Pull requests: read; Checks: read; Commit statuses: read | S, D, E2E | — |
| `pr list` | ✅ | `--author @me` / `--assignee @me` filter on the bot login, which is rarely what the agent means. | Pull requests: read | S | — |
| `pr merge` | ✅ | `--auto` needs auto-merge enabled on the repository; `--admin` works only if the App is a bypass actor of the rules; a PR that changes `.github/workflows/` needs Workflows: write; `--delete-branch` deletes the remote branch through the API and the local branch through Git. | Contents: write; Pull requests: write | S, D, E2E | — |
| `pr comment` | ✅ | `--edit-last` / `--delete-last` act on the last comment of the bot, not of the developer. | Pull requests: write (or Issues: write) | S, D | — |
| `pr status` | ⚠️ | Runs as the bot: "created by you" and "requesting your review" describe the bot (`pr/status/status.go:137`, `pr/status/http.go:189-198`). Not useful to the agent; harmless. | Pull requests: read | S | — |
| `pr checkout`, `pr diff`, `pr checks` | ✅ | `checkout` fetches through Git and the agent-id credential helper. `pr checks --watch` can outlive the token (see Token lifetime). | Pull requests: read; Checks: read | S | — |

### issue

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `issue create`, `edit`, `close`, `reopen`, `comment`, `lock`, `pin` | ✅ | Acts as `<app-slug>[bot]`. | Issues: write | S, D | — |
| `issue list`, `view` | ✅ | `view` ignores project-field errors like `pr view` (`issue/view/view.go:129-139`). | Issues: read | S | — |
| `issue create/edit --assignee @me`, `issue list --assignee @me` | ⛔ | Same `@me` rule as `pr`. | — | S, E2E | `AID-GH-ME` |
| `issue status` | ⚠️ | Viewer-based (`issue/status/status.go:75`); reports the bot's issues. | Issues: read | S | — |
| `issue develop` | ✅ | Creates a linked branch. | Contents: write; Issues: write | D, E2E | — |
| `issue transfer`, `issue delete` | ⚠️ | `transfer` needs the installation on the destination repository; `delete` needs repository admin. | Issues: write; Administration: write | D, E2E | — |

### api

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `api repos/{owner}/{repo}/…`, `api graphql` on repository data | ✅ | Depends on the endpoint and the App's permissions; `Resource not accessible by integration` (403) when a permission is missing. | per endpoint | D | — |
| `api user`, `api user/…`, `api graphql` with `viewer { organizations … }` and other user-only fields | ⛔ | `HTTP 403 Resource not accessible by integration` (O for `api user`). | — | O, D | `AID-GH-USER` |
| `api app`, `api app/installations…` | ⛔ | App endpoints need the App JWT, not an installation token: `HTTP 401 A JSON web token could not be decoded` (O). | — | O, D | `AID-GH-USER` |
| `api --hostname <other host> …` | ⛔ | Sends the developer's credential for that host (see host rules). | — | S | `AID-GH-HOST` |
| `api -H 'Authorization: …'` | ⚠️ | An explicit header wins over the injected token (`api/http_client.go:232-235`). Documented bypass, not a leak of the agent token. | — | S | — |
| `api graphql -f query='{viewer{login}}'` | ✅ | Expected to return `<app-slug>[bot]` (D: community recipe that checks for the `[bot]` suffix). | — | D, E2E | — |

### repo

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `repo view` (current repository), `repo view OWNER/REPO` | ✅ | — | Metadata: read | S | — |
| `repo view NAME`, `repo clone NAME` (no owner) | ⛔ | The owner becomes the viewer login (`repo/view/view.go:98-104`, `repo/clone/clone.go:148-156`), so `gh` looks for `<app-slug>[bot]/NAME` and fails with "not found". | — | S, E2E | `AID-GH-ME` |
| `repo clone OWNER/REPO` | ✅ | `gh` looks the repository up through the API, then runs `git clone`; the transport uses the agent-id credential helper. Adds an `upstream` remote for forks. | Metadata: read; Contents: read | S | — |
| `repo create`, `repo fork`, `repo list` (no owner) | ⛔ | Personal account operations (`repo/create/create.go:374`, `repo/create/http.go:117`, `repo/list/http.go:178`). | — | S | `AID-GH-USER` |
| `repo edit`, `repo archive`, `repo delete`, `repo set-default` | ⚠️ | `edit/archive/delete` need Administration: write. `set-default` writes the repository's `.git/config` (local to the repository, not the developer's global configuration). | Administration: write | S, D | — |

### auth

| Command | Status | Behavior and limits | Evidence | Error |
|---|---|---|---|---|
| `auth status` | ⛔ | With an env token it asks the API for the viewer login and the token scopes (`auth/status/status.go:486-500`). It also walks **every host and every account in the developer's `hosts.yml`** and validates their tokens over the network (`status.go:279-360`), printing the developer's logins. `-h <host>` limits the walk to one host (`status.go:311`) and `--active` skips the non-active accounts (`status.go:336`); neither changes the verdict. | S | `AID-GH-AUTH` |
| `auth token` | ⛔ | Prints the active token, i.e. the installation token (`auth/token/token.go:63-90`). | S, X (cases 1–10 use it with fake tokens) | `AID-GH-AUTH` |
| `auth login`, `auth switch`, `auth logout`, `auth refresh` on `github.com` | ⛔ | `gh` refuses while `GH_TOKEN` is set: `The value of the GH_TOKEN environment variable is being used for authentication.` / `To have GitHub CLI store credentials instead, first clear the value from the environment.` (`login.go:222-226`, `switch.go:159`, `logout.go:148`, `refresh.go:192`; X case 13). | S, X | `AID-GH-AUTH` |
| the same with `--hostname <other host>` | ⛔ | The guard checks that host's variables, so it passes, and `gh` writes the developer's `hosts.yml` or keyring. | S | `AID-GH-AUTH` |
| `auth setup-git` | ⛔ | Runs `git config --global` to install `gh` as the credential helper for `github.com` and `gist.github.com` (`auth/shared/gitcredentials/helper_config.go:41-56`). X case 14 shows the four entries it wrote to the isolated global Git configuration. Outside isolation this changes the developer's `~/.gitconfig`. | S, X | `AID-GH-AUTH` |
| `auth git-credential` | ⚠️ | Not called by agents directly; Git calls it when the developer's global configuration lists `!gh auth git-credential`. With `GH_TOKEN` set it answers `username=x-access-token` and the installation token (`auth/gitcredential/helper.go:16`, `:120-147`), without agent-id's host and path checks. The Git integration resets inherited helpers (`ARCHITECTURE.md`, Git integration), so this path should not run in agent mode. | S | — |

### run

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `run list`, `view`, `view --log`, `watch`, `download` | ✅ | Logs and artifacts redirect to blob storage without the token. `watch` can outlive the token. | Actions: read | S, D | — |
| `run rerun`, `cancel`, `delete` | ✅ | — | Actions: write | D, E2E | — |
| `workflow run` | ✅ | Dispatch needs Actions: write. | Actions: write | D, E2E | — |

### release

| Command | Status | Behavior and limits | Permission | Evidence | Error |
|---|---|---|---|---|---|
| `release list`, `view`, `download` | ✅ | Downloads follow redirects without the token. | Contents: read | S, D | — |
| `release create`, `edit`, `delete`, `upload` | ✅ | Uploads go to `uploads.github.com`, which gets `GH_TOKEN` by normalization. Creating a tag on a commit that changes workflows can need Workflows: write. `--discussion-category` needs Discussions: write. | Contents: write | S, D, E2E | — |

### Other user-centric commands

| Command | Why it fails | Evidence | Error |
|---|---|---|---|
| `gh status` | Cross-repository dashboard of the viewer plus notifications (`status/status.go:224`, `:261`). | S, D | `AID-GH-USER` |
| `gist *` | Gists belong to a user (`gist/edit/edit.go:170`). | S, D | `AID-GH-USER` |
| `org list` | Lists the viewer's organizations (`org/list/http.go:52`). | S | `AID-GH-USER` |
| `ssh-key *`, `gpg-key *`, `codespace *` | User account resources. | D | `AID-GH-USER` |
| `project * --owner @me` | User projects of the viewer (`project/shared/queries/queries.go:1239-1245`). Organization projects work with Projects permission. | S | `AID-GH-ME` |
| `agent-task *` | `gh` refuses non-OAuth tokens itself: `this command requires an OAuth token. Re-authenticate with: gh auth login` (`agent-task/agent_task.go:90-106`). The hint would send the agent to `gh auth login`. | S | `AID-GH-USER` |
| `extension install/upgrade/create` | Installs into the developer's `gh` data directory; `create` reads the viewer login (`extension/manager.go:688`). Out of scope for an agent identity. | S | `AID-GH-USER` |

## Proposed agent-id error messages

Every message goes to stderr, starts with `agent-id:`, names the profile, the bot, and the command, never contains a token, and ends with what the agent can do instead. The wrapper exits non-zero without starting `gh`. Exact exit code: open question 3.

| Code | When | Message |
|---|---|---|
| `AID-GH-USER` | A command that needs a user account | `agent-id: "gh <command>" needs a personal GitHub account and does not work with the GitHub App identity <bot> (profile <profile>). Run it in your own terminal, outside the agent context.` |
| `AID-GH-ME` | `@me`, or a repository name without an owner | `agent-id: "@me" and repository names without an owner resolve to the App identity <bot> (profile <profile>), not to a person. Name the user or the OWNER/REPO explicitly.` |
| `AID-GH-AUTH` | `gh auth login/logout/switch/refresh/setup-git/status/token` | `agent-id: "gh auth <subcommand>" is managed by agent-id in agent mode (profile <profile>, identity <bot>). Use "agent-id whoami" to see the active identity and "agent-id doctor" to check credentials.` |
| `AID-GH-HOST` | `--hostname`, `-R HOST/…`, `GH_REPO=HOST/…`, or a remote on a host other than the profile's | `agent-id: profile <profile> is configured for <profile host>; "gh" was asked to use <requested host>. agent-id does not send another host's credentials from the agent context.` |
| `AID-GH-TOKEN` | No token could be obtained (fail closed, brief §8) | `agent-id: could not obtain an installation token for profile <profile> (<reason without secrets>). gh was not started; run "agent-id doctor".` |

## Input for E02-S09 (the injection)

These follow from the evidence. They do not decide the open questions below.

- Set `GH_TOKEN` to a fresh installation token on every invocation. Refuse to start `gh` when the token is empty (X case 4).
- Remove `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, and `GITHUB_ENTERPRISE_TOKEN` from the child environment, as `ARCHITECTURE.md` already states. Removing the enterprise variables does **not** close the enterprise path by itself: `hosts.yml` and the keyring take over (see host rules).
- Set `GH_HOST` to the profile's host, so that remote selection fails closed (X case 11).
- Check `GH_REPO`, `-R/--repo`, and `--hostname` against the profile's host before starting `gh` (open question 4).
- Resolve the real `gh` by absolute path, skipping the wrapper itself, and mark the child environment so the wrapper detects a nested call. Calls back into `gh` through `GH_PATH` from extensions (`extension/manager.go:134`) and the absolute path that `auth setup-git` writes reach the real `gh` directly; they inherit the injected `GH_TOKEN`, which is correct.
- A **shell function or alias named `gh`** in the developer's startup files shadows a wrapper on `PATH` in every interactive or login shell that the agent starts. A function that sets `GH_TOKEN` from another tool replaces the injected token without any error. Claude Code runs its shell commands with the user's profile loaded. `doctor` should report `type gh` when it is not the expected executable.
- Token lifetime: installation tokens last about one hour (brief §10). Long-running commands (`run watch`, `pr checks --watch`) keep the token that they started with. The token manager should hand the wrapper a token with enough remaining life; the margin belongs to the token manager's design.
- `GH_DEBUG=api` logs request and response headers and bodies (`go-gh@v2.17.0:pkg/api/http_client.go:101-113`). The logger is `henvic/httpretty` v0.2.0, whose default sanitizers mask `Authorization` and `Proxy-Authorization` (`internal/header/header.go:39-49`, applied in `printer.go:742` unless `SkipSanitize` is set); neither `cli` nor `go-gh` sets `SkipSanitize`. The token header therefore does not leak. Response bodies are logged in full, so treat debug output as sensitive for endpoints that return credentials.

## Open questions for the owner

The issue has no open questions, and this research does not resolve any. The design of E02-S09 needs these answers; they are listed here so that the injection ticket can carry `needs-decision` where it applies.

1. **`GH_CONFIG_DIR` in agent mode.** Keep the developer's directory (aliases, extensions, editor, pager stay; `hosts.yml` remains readable for other hosts) or point it at an agent-id-owned directory (removes the `hosts.yml` fallback but not the unkeyed keyring entry, and loses the developer's `gh` settings).
2. **Deny-list or allow-list.** Block the ⛔ commands above and pass everything else (closer to brief §3 principle 2, "native workflows"), or pass only the ✅ commands.
3. **Exit code** of a refused command: `1`, or a dedicated code that scripts can match.
4. **Cross-host arguments.** Parse `--hostname`, `-R`, and `GH_REPO` in the wrapper and refuse other hosts, or document them as a known bypass of local mode (brief §10).
5. **Process tests for `gh`.** `gh` reaches a local fake only through `github.localhost` (plain HTTP on port 80, outside the allowed port range) or an HTTPS fake whose certificate `gh` trusts. Choose one for `internal/ghint` process tests, or keep `gh` process tests to the offline checks of appendix A.
6. **`gh auth status` and `gh auth token`.** Refuse both (as proposed), or answer `auth status` with agent-id's own identity summary for tools that call it to check authentication.

## Appendix A — offline experiment

Isolated environment: temporary `HOME`, `XDG_CONFIG_HOME`, `GH_CONFIG_DIR`, `GIT_CONFIG_GLOBAL` under `$TMPDIR/agent-id/`, `GIT_CONFIG_NOSYSTEM=1`, `env -i` (no inherited tokens, no shell functions), update notifier and telemetry off. The fake `hosts.yml` has a plain-text `oauth_token`, so no case reaches the keyring. Cases that could touch the keychain (`auth logout`, `auth switch`, an enterprise host without a variable) were left out on purpose and are covered by source reading. The directory is removed on exit.

```sh
#!/bin/sh
set -u
GH_BIN="${GH_BIN:-$(command -v gh)}"
root="${TMPDIR:-/tmp}/agent-id/gh-precedence"
rm -rf "$root"; mkdir -p "$root/home/.config/gh" "$root/repo"
trap 'rm -rf "$root"' EXIT

hosts_yml() {
  cat > "$root/home/.config/gh/hosts.yml" <<EOF
github.com:
    oauth_token: ghs_aidtest_HOSTS_YML
    user: aid-fake-user
    git_protocol: https
EOF
}

iso() {
  env -i PATH="/usr/bin:/bin:$(dirname "$GH_BIN")" HOME="$root/home" TMPDIR="$root" \
    XDG_CONFIG_HOME="$root/home/.config" GH_CONFIG_DIR="$root/home/.config/gh" \
    GIT_CONFIG_GLOBAL="$root/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 \
    GH_NO_UPDATE_NOTIFIER=1 GH_TELEMETRY=false DO_NOT_TRACK=1 GH_PROMPT_DISABLED=1 "$@"
}

run() { label=$1; shift; printf '%-58s -> ' "$label"; out=$(iso "$@" 2>&1); rc=$?; printf '%s (exit %s)\n' "$(printf '%s' "$out" | head -1)" "$rc"; }

hosts_yml
run "1 GH_TOKEN + GITHUB_TOKEN + hosts.yml" env GH_TOKEN=ghs_aidtest_GH_TOKEN GITHUB_TOKEN=ghs_aidtest_GITHUB_TOKEN "$GH_BIN" auth token --no-refresh
run "2 GITHUB_TOKEN + hosts.yml" env GITHUB_TOKEN=ghs_aidtest_GITHUB_TOKEN "$GH_BIN" auth token --no-refresh
run "3 hosts.yml only" "$GH_BIN" auth token --no-refresh
run "4 GH_TOKEN='' (empty) + hosts.yml" env GH_TOKEN= "$GH_BIN" auth token --no-refresh
run "5 GH_ENTERPRISE_TOKEN only, -h github.com" env GH_ENTERPRISE_TOKEN=ghs_aidtest_GH_ENTERPRISE "$GH_BIN" auth token --no-refresh -h github.com
run "6 GH_HOST=ghe.example.test, GH_TOKEN + GH_ENTERPRISE_TOKEN" env GH_HOST=ghe.example.test GH_TOKEN=ghs_aidtest_GH_TOKEN GH_ENTERPRISE_TOKEN=ghs_aidtest_GH_ENTERPRISE GITHUB_ENTERPRISE_TOKEN=ghs_aidtest_GITHUB_ENTERPRISE "$GH_BIN" auth token --no-refresh
run "7 GH_HOST=ghe.example.test, GITHUB_ENTERPRISE_TOKEN only" env GH_HOST=ghe.example.test GITHUB_ENTERPRISE_TOKEN=ghs_aidtest_GITHUB_ENTERPRISE "$GH_BIN" auth token --no-refresh
run "8 GH_TOKEN, -h acme.ghe.com (tenancy)" env GH_TOKEN=ghs_aidtest_GH_TOKEN "$GH_BIN" auth token --no-refresh -h acme.ghe.com
run "9 GH_TOKEN, -h api.github.com" env GH_TOKEN=ghs_aidtest_GH_TOKEN "$GH_BIN" auth token --no-refresh -h api.github.com
run "10 GH_TOKEN, -h GitHub.COM" env GH_TOKEN=ghs_aidtest_GH_TOKEN "$GH_BIN" auth token --no-refresh -h GitHub.COM

git -C "$root/repo" init -q
git -C "$root/repo" remote add origin https://github.com/aid-owner/aid-repo.git
inrepo() { label=$1; shift; printf '%-58s -> ' "$label"; out=$(cd "$root/repo" && iso "$@" 2>&1); rc=$?; printf '%s (exit %s)\n' "$(printf '%s' "$out" | head -1)" "$rc"; }
inrepo "11 remote github.com, GH_HOST=ghe.example.test" env GH_HOST=ghe.example.test GH_ENTERPRISE_TOKEN=ghs_aidtest_GH_ENTERPRISE "$GH_BIN" pr list
git -C "$root/repo" remote set-url origin https://gitlab.example.test/aid-owner/aid-repo.git
rm -f "$root/home/.config/gh/hosts.yml"
inrepo "12 remote not GitHub, GH_TOKEN only" env GH_TOKEN=ghs_aidtest_GH_TOKEN "$GH_BIN" pr list

hosts_yml
run "13 auth login --with-token while GH_TOKEN set" sh -c "echo ghs_aidtest_LOGIN | GH_TOKEN=ghs_aidtest_GH_TOKEN '$GH_BIN' auth login --with-token -h github.com"
run "14 auth setup-git while GH_TOKEN set" env GH_TOKEN=ghs_aidtest_GH_TOKEN "$GH_BIN" auth setup-git
printf '%-58s -> %s\n' "14b isolated global gitconfig after setup-git" "$(git config -f "$root/home/.gitconfig" --get-regexp 'credential\..*helper' | tr '\n' ';')"
printf '%-58s -> %s\n' "15 hosts.yml unchanged after all runs" "$(grep -c oauth_token "$root/home/.config/gh/hosts.yml") oauth_token line(s)"
```

Output of the script above with `gh` 2.103.3 on macOS (darwin/arm64), 2026-11-12:

```text
1 GH_TOKEN + GITHUB_TOKEN + hosts.yml                      -> ghs_aidtest_GH_TOKEN (exit 0)
2 GITHUB_TOKEN + hosts.yml                                 -> ghs_aidtest_GITHUB_TOKEN (exit 0)
3 hosts.yml only                                           -> ghs_aidtest_HOSTS_YML (exit 0)
4 GH_TOKEN='' (empty) + hosts.yml                          -> ghs_aidtest_HOSTS_YML (exit 0)
5 GH_ENTERPRISE_TOKEN only, -h github.com                  -> ghs_aidtest_HOSTS_YML (exit 0)
6 GH_HOST=ghe.example.test, GH_TOKEN + GH_ENTERPRISE_TOKEN -> ghs_aidtest_GH_ENTERPRISE (exit 0)
7 GH_HOST=ghe.example.test, GITHUB_ENTERPRISE_TOKEN only   -> ghs_aidtest_GITHUB_ENTERPRISE (exit 0)
8 GH_TOKEN, -h acme.ghe.com (tenancy)                      -> ghs_aidtest_GH_TOKEN (exit 0)
9 GH_TOKEN, -h api.github.com                              -> ghs_aidtest_GH_TOKEN (exit 0)
10 GH_TOKEN, -h GitHub.COM                                 -> ghs_aidtest_GH_TOKEN (exit 0)
11 remote github.com, GH_HOST=ghe.example.test             -> none of the git remotes configured for this repository correspond to the GH_HOST environment variable. Try adding a matching remote or unsetting the variable (exit 1)
12 remote not GitHub, GH_TOKEN only                        -> none of the git remotes configured for this repository point to a known GitHub host. To tell gh about a new GitHub host, please use `gh auth login` (exit 1)
13 auth login --with-token while GH_TOKEN set              -> The value of the GH_TOKEN environment variable is being used for authentication. (exit 1)
14 auth setup-git while GH_TOKEN set                       ->  (exit 0)
14b isolated global gitconfig after setup-git              -> credential.https://github.com.helper ;credential.https://github.com.helper !/opt/homebrew/bin/gh auth git-credential;credential.https://gist.github.com.helper ;credential.https://gist.github.com.helper !/opt/homebrew/bin/gh auth git-credential;
15 hosts.yml unchanged after all runs                      -> 1 oauth_token line(s)
```

Case 12 shows a hint that would send the agent to `gh auth login`; the wrapper's `AID-GH-HOST` message should replace it.

## Appendix B — checking three rows against the source

The acceptance criteria ask a reviewer to check three commands. Suggested rows and where to look in `cli/cli` at `v2.103.3`:

| Row | File and lines | What to see |
|---|---|---|
| `auth setup-git` writes the global Git configuration | `pkg/cmd/auth/shared/gitcredentials/helper_config.go:41-56` | `git config --global --replace-all credential.<url>.helper ""`, then `--add … !<gh> auth git-credential` |
| `repo view NAME` uses the viewer login as the owner | `pkg/cmd/repo/view/view.go:98-104` | `api.CurrentLoginName`, then `currentUser + "/" + viewURL` |
| `auth status` walks every account in `hosts.yml` | `pkg/cmd/auth/status/status.go:279-360` | the loop over `authCfg.Hosts()` and `authCfg.UsersForHost(hostname)` |
