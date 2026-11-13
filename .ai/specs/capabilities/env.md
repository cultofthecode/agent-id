# Capability: env

`agent-id env --profile <name> --shell bash|zsh` prints the child environment of `agent-id exec` as one shell block. Implemented by `internal/activation` (`Render`) and `internal/activation/env` (#26).

## Requirement: Render the child environment as one shell block
The system SHALL print one block of `unset`, `unalias`/`unset -f`, and `export` statements chained with `&&`, computed for the profile and the environment of the `agent-id env` process. When evaluated in `bash` or `zsh`, the block SHALL turn that environment into the child environment of #20 with `AGENT_ID_MECHANISM=env`, and every value SHALL be quoted literally.

### Scenario: Eval equals exec
- WHEN a test evaluates `agent-id env --profile claude --shell bash` in `bash` and prints `env`, and separately runs `agent-id exec --profile claude -- env` from the same parent environment
- THEN both outputs are equal except for `AGENT_ID_MECHANISM` (`env` vs `exec`) and the shell's own variables (`PWD`, `SHLVL`, `_`)

### Scenario: Hostile values stay literal
- WHEN the parent `PATH` contains a directory named `$(touch pwned)'"; \n` and the plan prepends a directory
- THEN evaluating the block in `bash` and in `zsh` creates no file, and `PATH` equals the value that `Build` computed

### Scenario: The block fails as a whole
- WHEN `GH_TOKEN` is read-only in the shell (`readonly GH_TOKEN`) and the block is evaluated as `eval "$block" || echo failed`
- THEN `failed` is printed in `bash` (and in `zsh`, for the block alone). For the guarded block of `session-start`, which runs the block inside `eval`, both shells print the guard's message and exit 125, and zsh's own message names the variable

### Scenario: No secret
- WHEN the parent has a fake `GH_TOKEN` and the output is scanned for every registered token prefix, `-----BEGIN`, and a JWT shape
- THEN nothing matches, and the output contains `unset GH_TOKEN`

### Scenario: Unsupported shell
- WHEN `--shell fish` is passed
- THEN the exit status is 125, stdout is empty, and the message lists `bash` and `zsh`

## Requirement: Same failure rules as exec
The system SHALL refuse with exit 125 and empty stdout for every error that makes `exec` exit 125, including a nested start with a different profile.

### Scenario: Different profile inside a session
- WHEN `agent-id env --profile reviewer --shell zsh` runs where `AGENT_ID_ACTIVE_PROFILE=claude` and `AGENT_ID_CONTEXT=1`
- THEN the exit status is 125, stdout is empty, and stderr names both profiles
