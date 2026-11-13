# Runbook: end-to-end validation on GitHub (issue #27)

The owner runs this by hand. CI never runs it. No agent runs it.

The test proves these things on a real GitHub repository:

1. The developer's commit carries the developer's identity and is signed with the developer's personal signing key (an SSH key that the test makes in the isolated tree).
2. The bot's commit carries the bot's identity and is not signed, although the personal signing setup is active.
3. The bot pushes a branch through the Git credential helper (#22, PR #103). `git ls-remote` and `git fetch` show the branch and the bot identity, with no signature.
4. With an invalid App key, the credential helper refuses with its fixed message, the push fails, and no branch appears.
5. With `AGENT_ID_E2E_GH=1`: the bot opens a PR with `gh`, and GitHub shows the bot as the PR author and `verified` as `false`.

The run also fails when a command output holds a token or key shape. `testenv.Main` hashes the personal Git and `gh` configuration before and after the run, and fails the run when a file changed.

## Status of the steps

The git steps always run. They need the credential helper from #22 (PR #103, on `main`).

The `gh` steps (`gh pr create`, `gh pr view`, `gh api`) need the `gh` token injection (#23, PR #104). Until #104 merges, run without `AGENT_ID_E2E_GH`. The PR subtest then skips. When #104 merges, set `AGENT_ID_E2E_GH=1`.

## Manual steps that the test does not cover

The test does not drive a Claude Code session or wait for a token to expire. Do these by hand and attach the output to the issue. Read each output once and remove the key path first.

### Claude Code session beside a personal terminal (#26, PR #98)

1. Open two terminals in one checkout of the disposable repository. Use the isolated variables of your choice, never your real `gh` login in the agent terminal.
2. In terminal A, run `git config user.name` and `git log -1 --format=%an`. Expect your personal identity.
3. In terminal B, start Claude Code with the `SessionStart` hook that runs `agent-id claude-code session-start --profile <name>`. Ask it to run `agent-id whoami`, then to commit a file. Expect the bot as author and committer, and no signature.
4. In terminal A, run `git log -1 --format='%an|%G?'` and `git config user.name` again. Expect your personal identity and no bot variable.
5. Run `agent-id env --profile <name>` in a plain shell and check that the block holds no token.

### Repeat after token expiry

1. Run the command of this runbook once with `AGENT_ID_E2E_GH=1` when #104 is on `main`.
2. Wait more than 60 minutes. An installation token lives one hour.
3. In the same disposable repository, run `agent-id exec --profile e2e -- git push origin HEAD:refs/heads/agent-id-e2e/expiry-<number>`.
4. Expect success. The credential helper mints a new token when the cached one is near expiry.
5. Delete the branch with `agent-id exec --profile e2e -- git push origin --delete agent-id-e2e/expiry-<number>`.

## Prerequisites

- macOS or Linux, with `go`, `git`, and (for the push steps) `gh` on `PATH`.
- The disposable repository `cultofthecode/agent-id-e2e` (private). Its name must end with `-e2e`. The test refuses any other name.
- The test App `agent-id-e2e-bot`, installed only on that repository. App ID `5746535`. Installation ID `181640369`. Both values date from 2026-11-13. Check them in the App settings when the run fails with "installation not found".
- The App private key as a file on this machine. The file must have mode `0600` and belong to you.
- The default branch `main` exists in the repository. The App has Contents and Pull requests write permission.
- The commit `HEAD` of your checkout contains #22 (PR #103), #26 (PR #98), and the setup and exec slices.
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
| `AGENT_ID_E2E_GH` | `1` enables the `gh` steps (needs #23, PR #104). | no |

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
go test -tags e2e -count=1 -v ./e2e/
```

Add `AGENT_ID_E2E_GH=1` for the `gh` steps.

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
