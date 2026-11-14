# Runbook: end-to-end validation on GitHub (issue #27)

The owner runs this by hand. CI never runs it. No agent runs it.

The test proves these things on a real GitHub repository:

1. The developer's commit carries the developer's identity and is signed with the developer's personal signing key (an SSH key that the test makes in the isolated tree).
2. The bot's commit carries the bot's identity and is not signed, although the personal signing setup is active.
3. The bot pushes a branch through the Git credential helper (#22, PR #103). `git ls-remote` and `git fetch` show the branch and the bot identity, with no signature.
4. With an invalid App key, the credential helper refuses with its fixed message, the push fails, and no branch appears. The push uses its own empty token cache. The cache key does not hold the profile name, so a shared cache would give the bad profile the valid token of the earlier steps.
5. With `AGENT_ID_E2E_GH=1`: the bot opens a PR with `gh`, and GitHub shows the bot as the PR author and `verified` as `false`.

The run also fails when a command output holds a token or key shape. `testenv.Main` hashes the personal Git and `gh` configuration before and after the run, and fails the run when a file changed.

## Status of the steps

The git steps always run. They need the credential helper from #22 (PR #103, on `main`).

The `gh` steps (`gh pr create`, `gh pr view`, `gh api`) need the `gh` token injection (#23, PR #104, on `main`). Set `AGENT_ID_E2E_GH=1` to run them. Without it, the PR subtest skips.

The test fetches `main` from the remote as the bot and bases its commits on it. The remote `main` must exist, so that `gh pr create` finds a common history.

## Manual steps that the test does not cover

The test does not drive a Claude Code session or wait for a token to expire. Do these by hand and attach the output to the issue. Read each output once and remove the key path first.

### Claude Code session beside a personal terminal (#26, PR #98)

1. Open two terminals in one checkout of the disposable repository. Use the isolated variables of your choice, never your real `gh` login in the agent terminal.
2. In terminal A, run `git config user.name` and `git log -1 --format=%an`. Expect your personal identity.
3. In terminal B, start Claude Code with the `SessionStart` hook that runs `agent-id claude-code session-start --profile <name>`. Ask it to run `agent-id whoami`, then to commit a file. Expect the bot as author and committer, and no signature.
4. In terminal A, run `git log -1 --format='%an|%G?'` and `git config user.name` again. Expect your personal identity and no bot variable.
5. Run `agent-id env --profile <name> --shell zsh` (or `--shell bash`) in a plain shell. Check that the block holds no token.

### Repeat after token expiry

An installation token lives one hour. The token cache refreshes a token that expires within 5 minutes. This procedure proves the refresh. It runs in one isolated environment, because the cache must survive between the two pushes. The Go test removes its environment at the end, so it cannot do this.

Use `whoami` and the cache entry as proof. Do not use `doctor` as proof. `doctor` mints its own token and never reads the cache.

1. Build the binary from the repository root. Start a clean shell. Use the key path from the prerequisites.

   ```sh
   iso="${TMPDIR:-/tmp}/agent-id/expiry"; mkdir -p "$iso/home" "$iso/work" "$iso/bin"
   go build -o "$iso/bin/agent-id" ./cmd/agent-id
   env -i PATH="$iso/bin:$PATH" HOME="$iso/home" XDG_CONFIG_HOME="$iso/home/.config" XDG_CACHE_HOME="$iso/home/.cache" \
     XDG_STATE_HOME="$iso/home/.state" XDG_DATA_HOME="$iso/home/.data" GIT_CONFIG_GLOBAL="$iso/home/.gitconfig" \
     GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0 GH_CONFIG_DIR="$iso/home/.config/gh" TMPDIR="$iso" \
     ISO="$iso" KEY=/absolute/path/to/agent-id-e2e-bot.pem sh
   ```

   Run every later command in this shell. The shell has no `GH_TOKEN`, no `GITHUB_TOKEN`, and no personal Git configuration.

2. Set up the profile and base the work tree on the remote `main`.

   ```sh
   agent-id setup github --profile e2e --app-id 5746535 --installation-id 181640369 --private-key "$KEY"
   cd "$ISO/work" && git init -q -b main && git remote add origin https://github.com/cultofthecode/agent-id-e2e
   agent-id exec --profile e2e -- git fetch -q origin main
   git checkout -q -B main FETCH_HEAD
   ```

3. Push once. This fills the cache.

   ```sh
   branch=agent-id-e2e/expiry-$(date +%s)
   agent-id exec --profile e2e -- git commit -q --allow-empty -m "e2e: expiry"
   agent-id exec --profile e2e -- git push origin "HEAD:refs/heads/$branch"
   ```

4. Read the expiry of the cached token. The command prints the `expires_at` member only, never the token. Write down the value as E1.

   ```sh
   jq -r .expires_at "$XDG_CACHE_HOME"/agent-id/tokens/*.json
   agent-id exec --profile e2e -- agent-id whoami
   agent-id exec --profile e2e -- agent-id doctor --profile e2e
   ```

   Expect `whoami` to print `valid` and `doctor` to exit 0. Do not run `whoami` again until step 5 ends. A `whoami` run within 5 minutes of E1 refreshes the token and removes the proof.

5. Wait until the time is E1 minus 5 minutes or later. This takes about 55 minutes. Check the time with `date -u +%Y-%m-%dT%H:%M:%SZ`.

6. Push again. The credential helper must find a token inside the refresh margin and mint a new one.

   ```sh
   agent-id exec --profile e2e -- git commit -q --allow-empty -m "e2e: expiry 2"
   agent-id exec --profile e2e -- git push origin "HEAD:refs/heads/$branch"
   ```

7. Read the cached expiry again. Write down the value as E2.

   ```sh
   jq -r .expires_at "$XDG_CACHE_HOME"/agent-id/tokens/*.json
   agent-id exec --profile e2e -- agent-id whoami
   agent-id exec --profile e2e -- agent-id doctor --profile e2e
   ```

   Expect the second push to succeed, E2 to be later than E1, `whoami` to print `valid`, and `doctor` to exit 0. If E2 equals E1, the push did not refresh the token. Report it in the issue. If you waited past E1, the old token was already expired. The result is still valid: E2 later than E1 proves a new token.

8. Delete the branch and leave the shell.

   ```sh
   agent-id exec --profile e2e -- git push origin --delete "$branch"
   exit
   ```

Remove `$TMPDIR/agent-id/expiry` when you finish. Attach the E1 and E2 values and the outputs of `whoami` and `doctor`. Remove the key path first.

## Prerequisites

- macOS or Linux, with `go`, `git`, and (for the push steps) `gh` on `PATH`.
- The disposable repository `cultofthecode/agent-id-e2e` (private). Its name must end with `-e2e`. The test refuses any other name.
- The test App `agent-id-e2e-bot`, installed only on that repository. App ID `5746535`. Installation ID `181640369`. Both values date from 2026-11-13. Check them in the App settings when the run fails with "installation not found".
- The App private key as a file on this machine. The file must have mode `0600` and belong to you.
- The default branch `main` exists in the repository. The App has Contents and Pull requests write permission.
- The commit `HEAD` of your checkout contains #22 (PR #103), #23 (PR #104), #26 (PR #98), and the setup and exec slices.
- `ssh-keygen` on `PATH`. The test makes a throwaway signing key with it.

Never put the key in the repository. Never paste it in a terminal, a PR, or an issue.

## Environment variables

| Variable | Value | Required |
|---|---|---|
| `AGENT_ID_E2E` | `1`. Any other value skips the test. | yes |
| `AGENT_ID_E2E_KEY` | Absolute path of the App private key file. | yes |
| `AGENT_ID_E2E_APP_ID` | `5746535` | yes |
| `AGENT_ID_E2E_INSTALLATION_ID` | `181640369` | yes |
| `AGENT_ID_E2E_REPO_URL` | `https://github.com/cultofthecode/agent-id-e2e` | yes |
| `AGENT_ID_E2E_BOT_LOGIN` | `agent-id-e2e-bot[bot]` | yes |
| `AGENT_ID_E2E_GH` | `1` enables the `gh` steps. The `gh` token injection (#23, PR #104) is on `main`. | no |

The test removes every other `AGENT_ID_*`, `GH_TOKEN`, and `GITHUB_TOKEN` value from its children. It builds its own `HOME`, `XDG_*`, `GIT_CONFIG_GLOBAL`, and `GH_CONFIG_DIR` under `$TMPDIR/agent-id/`. It never reads your `~/.gitconfig` or your `gh` login.

## Command

Run from the repository root:

```sh
AGENT_ID_E2E=1 \
AGENT_ID_E2E_KEY=/absolute/path/to/agent-id-e2e-bot.pem \
AGENT_ID_E2E_APP_ID=5746535 \
AGENT_ID_E2E_INSTALLATION_ID=181640369 \
AGENT_ID_E2E_REPO_URL=https://github.com/cultofthecode/agent-id-e2e \
AGENT_ID_E2E_BOT_LOGIN='agent-id-e2e-bot[bot]' \
AGENT_ID_E2E_GH=1 \
go test -tags e2e -count=1 -v ./e2e/
```

The command sets `AGENT_ID_E2E_GH=1`, so the `gh` steps run. To skip them, remove that line. The PR subtest then skips.

Without `-tags e2e`, Go compiles no test of this package. Without `AGENT_ID_E2E=1`, the test skips. The test also fails at once when `CI` or `GITHUB_ACTIONS` is set.

## Expected output

Without `AGENT_ID_E2E_GH`:

```
--- PASS: TestEndToEnd
    --- PASS: TestEndToEnd/developer_commits_as_the_developer,_signed
    --- PASS: TestEndToEnd/bot_commits_as_the_bot,_unsigned
    --- PASS: TestEndToEnd/invalid_App_credentials:_no_push,_no_fallback
    --- PASS: TestEndToEnd/bot_pushes_a_branch_through_the_credential_helper
    --- SKIP: TestEndToEnd/bot_opens_a_PR_with_gh
    --- PASS: TestEndToEnd/no_secret_in_diagnostics
PASS
```

With `AGENT_ID_E2E_GH=1`, the PR subtest passes too. Save the output of the run as the transcript for the issue. The output holds no token. It can hold the absolute path of your key file (the setup summary and `doctor` print it). Replace that path with `<key>` before you attach the output, and read it once.

## What the test creates

- Branches named `agent-id-e2e/pr-<number>` and `agent-id-e2e/bad-<number>`. The `bad` branch must never exist.
- With `AGENT_ID_E2E_GH=1`: one PR titled `agent-id e2e: <branch>`.
- Nothing else in the repository. The commits live on those branches only.

## Cleanup

The test checks and deletes both branches when it ends, whatever the push result was, and closes its PR. A branch that does not exist counts as clean. When a cleanup step fails, the test prints `CLEANUP NEEDED` and the name. Then:

1. Open the repository's pull requests. Close each PR titled `agent-id e2e: ...`.
2. Open the repository's branches. Delete each branch that starts with `agent-id-e2e/`.
3. Delete nothing else. Do not delete the repository or the App.

The local work tree and the isolated `HOME` are under `$TMPDIR/agent-id/` and the test removes them.

## On failure

| Symptom | Action |
|---|---|
| Test skips | `AGENT_ID_E2E` is not `1`. |
| `... is not set` | Set the named variable. |
| `must name a repository ... ends with "-e2e"` | Fix `AGENT_ID_E2E_REPO_URL`. Do not rename a real repository. |
| `setup github` fails: rejected App credentials | Check the App ID, the key file, and the system clock. The key must be `0600`. |
| `setup github` fails: installation not found | Check the installation ID and that the App is installed on the repository. |
| Commit identity differs | Run `agent-id whoami` inside `agent-id exec --profile e2e -- ...` and compare. Report the difference in the issue. |
| `matches a token or key pattern: output withheld` | Treat this as a leak. Stop. Do not attach the output. Open a `security` issue without the value. Revoke the key if it printed. |
| `testenv` reports a changed personal file | Stop. Your Git or `gh` configuration changed. Compare it with your backup. Report in the issue. |
| The bad-credentials push fails without the refusal message | Another failure happened (network, helper missing). Read the output, fix the cause, run again. |
| The bad-credentials push succeeds | Treat this as a security bug. Stop. Check that no branch from the developer's account exists. Open a `security` issue. |
| PR author is not the bot | Report the login you see. Do not change the test to match it. |
| `CLEANUP NEEDED` | Follow the Cleanup section. |

Never lower a check to make the run pass. Never run the test with a personal token in the environment.
