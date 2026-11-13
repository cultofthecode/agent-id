# Runbook: end-to-end validation on GitHub (issue #27)

The owner runs this by hand. CI never runs it. No agent runs it.

The test proves four things on a real GitHub repository:

1. The developer's commit carries the developer's identity.
2. The bot's commit carries the bot's identity, and no commit is signed.
3. The bot pushes a branch and opens a PR, and GitHub shows the bot as the PR author.
4. With an invalid App key, the bot push fails and no branch appears.

The run also fails when a command output holds a token or key shape. `testenv.Main` hashes the personal Git and `gh` configuration before and after the run, and fails the run when a file changed.

## Status of the push steps

Steps 3 and 4 need the bot to push and call `gh`. That needs token injection, which `agent-id exec` does not have yet (#22 credential helper, #23 `gh` shim). Until both merge, run without `AGENT_ID_E2E_PUSH`. The run then covers identity, signing, the negative push, and diagnostics. The PR steps skip. When #22 and #23 merge, set `AGENT_ID_E2E_PUSH=1`.

## Prerequisites

- macOS or Linux, with `go`, `git`, and (for the push steps) `gh` on `PATH`.
- The disposable repository `cultofthecode/agent-id-e2e` (private). Its name must end with `-e2e`. The test refuses any other name.
- The test App `agent-id-e2e-bot`, installed only on that repository. App ID `5746535`. Installation ID `181640369`.
- The App private key as a file on this machine. The file must have mode `0600` and belong to you.
- The default branch `main` exists in the repository. The App has Contents and Pull requests write permission.
- The commit `HEAD` of your checkout contains #26 (PR #98) and the setup and exec slices.

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
| `AGENT_ID_E2E_PUSH` | `1` enables the push and PR steps (needs #22 and #23). | no |

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

Add `AGENT_ID_E2E_PUSH=1` for the push steps.

Without `-tags e2e`, Go compiles no test of this package. Without `AGENT_ID_E2E=1`, the test skips. The test also fails at once when `CI` or `GITHUB_ACTIONS` is set.

## Expected output

Without `AGENT_ID_E2E_PUSH`:

```
--- PASS: TestEndToEnd
    --- PASS: TestEndToEnd/developer_commits_as_the_developer
    --- PASS: TestEndToEnd/bot_commits_as_the_bot
    --- PASS: TestEndToEnd/bot_commits_are_not_signed
    --- PASS: TestEndToEnd/invalid_App_credentials:_no_push,_no_fallback
    --- SKIP: TestEndToEnd/bot_pushes_a_branch_and_opens_a_PR
    --- PASS: TestEndToEnd/no_secret_in_diagnostics
PASS
```

With `AGENT_ID_E2E_PUSH=1`, the PR subtest passes too. Save the output of the run as the transcript for the issue. The output holds no token. It can hold the absolute path of your key file (the setup summary and `doctor` print it). Replace that path with `<key>` before you attach the output, and read it once.

## What the test creates

- Branches named `agent-id-e2e/pr-<number>` and `agent-id-e2e/bad-<number>`.
- One PR titled `agent-id e2e: <branch>`.
- Nothing else in the repository. The commits live on those branches only.

## Cleanup

The test deletes its branches and closes its PR when it ends, also on failure. When a cleanup step fails, the test prints `CLEANUP NEEDED` and the name. Then:

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
| The bad-credentials push succeeds | Treat this as a security bug. Stop. Check that no branch from the developer's account exists. Open a `security` issue. |
| PR author is not the bot | Report the login you see. Do not change the test to match it. |
| `CLEANUP NEEDED` | Follow the Cleanup section. |

Never lower a check to make the run pass. Never run the test with a personal token in the environment.
