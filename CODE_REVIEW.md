# Code review rules — agent-id

`om-code-review` (and so `om-auto-review-pr`) applies this file automatically, in addition to its built-in checklist. The code so far is the CLI skeleton in `cmd/agent-id` and the test helpers in `internal/testenv`, so the repo-specific section is still short. Add rules as conventions appear, and cite the files that set them.

## Review priorities

In order:

1. **Correctness** — the change does what the ticket or spec asks. The code handles edge cases: empty, null, error, concurrent, and large input. Tests cover the new behavior and the regression that the change fixes.
2. **Security** — the code validates input from users, the network, files, and the environment before use. Code, logs, and fixtures contain no secrets. Credentials are obtained fail-closed.
3. **Contracts** — CLI flags, output that scripts parse, the profile config format, and the environment that `exec` and `env` emit change only deliberately. A change that affects consumers includes a migration note.
4. **Maintainability** — the change follows existing patterns and does not add a parallel one. Names explain intent. There is no dead code and no commented-out block.

## Repo-specific checks

These rules come from `ARCHITECTURE.md`, `AGENTS.md`, and `PRODUCT_BRIEF.md`.

- **Test isolation.** A test that runs agent-id code, `git`, or `gh` without `testenv.Isolate(t)`, reads or writes the real `HOME`, `~/.gitconfig`, `~/.config/gh`, the ghapp configuration, or a keychain, uses a real token or reaches `api.github.com`, binds a port outside 18000–18999, or installs anything globally is a **blocker**.
- **No personal configuration changes.** Code that writes to the global or system Git configuration, the `gh` configuration, shell startup files, or the keychain of the developer is a blocker (brief §10).
- **Fail closed.** A path on which an agent operation can silently use the developer's credentials (an inherited `GH_TOKEN`, a personal credential helper, an SSH remote, a cached personal token) is a blocker (brief §8).
- **Secrets.** A token, private key, or JWT in output, errors, logs, test fixtures, golden files, or generated shell code is a blocker. Fake tokens use `testenv.FakeToken`.
- **Credential helper.** It answers only for the expected protocol, host, and (where configured) repository path; table tests include look-alike hosts and other protocols.
- **Honest claims.** Text that says local mode isolates the agent, or that App pushes produce verified commits, is a major finding (brief §9.3, §10).
- **Provenance.** Code copied or closely adapted from GHAPP (`operator-kit/ghapp-cli`, `cultofthecode/ghapp-cli`) without its MIT license notice is a **blocker**. The notice text is in `.ai/analysis/2026-11-12-ghapp-reuse-audit.md` §2. The owner decided on 2026-11-12 that agent-id rewrites GHAPP-related code and reuses none of it, so copied code needs a new owner decision even when it carries the notice.
- **Open decisions.** Code that hard-wires an answer to an OPEN row of `ARCHITECTURE.md` (commercial model and hosting, config format, keyring, signing, auto-detection, the `gh` injection policy) without the owner's recorded decision is a major finding.
- **Slices and modules.** One operation is one package with `contract.go`, `handler.go`, `command.go`, and `handler_test.go`. Commands hold no decisions. A slice does not import another slice. Only `cmd/agent-id` imports an adapter. Only test files and the test helper packages `internal/provider/providertest` and `internal/provider/github/githubtest` import `internal/testenv`; core tests use `providertest.Fake`, never `githubtest` (dependency rule 6).
- **Provider boundary.** A GitHub-specific import, type, hostname, App JWT or installation-token detail, or `gh` wiring outside `internal/provider/github` (other than the adapter's construction in `cmd/agent-id`, the test-only packages `internal/provider/github/githubtest` (the fake GitHub App) and `internal/testenv` (the inherited GitHub variables and real-token prefixes its guards reject) with their use from test files; the module path `github.com/cultofthecode/agent-id` does not count) is a **major** finding (`ARCHITECTURE.md`, Provider abstraction and dependency rule 7). A new forge arrives as an adapter that passes the provider contract tests, not as a change to the core.
- **Security decisions are pure functions** with table tests that include hostile input.
- **Errors** are wrapped with `%w` and matched with `errors.Is`. Only `cmd/agent-id` prints errors and sets exit codes; `exec` preserves the child's exit status.
- **Style.** `gofmt`. Doc comments on packages and exported identifiers; no comments inside function bodies. No abstraction for single-use code. A new Go module dependency needs a stated reason and the owner's yes.

## Validation gate

The gate is `validation.commands` in `.ai/agentic.config.json`. Reviewers run it in an isolated environment (`AGENTS.md`) and state the result in the review.

## Severity guidance

- **Blocker** — incorrect behavior, a leaked secret, a write to personal configuration, a fallback to personal credentials, a broken contract without a migration path, or failing validation or required CI. A blocker stops approval.
- **Major** — missing tests for new behavior, unhandled error paths, or a `risk-high` change without the evidence that `SDLC.md` requires. Fix it before the merge.
- **Minor** — readability, naming, or small duplication. Fix it or file a follow-up issue.
- **Nit** — style preference. A nit never blocks.

Report a failing required check or a merge conflict as a blocker finding together with the full review, never instead of it (see `SDLC.md`).
