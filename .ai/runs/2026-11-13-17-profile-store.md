# Execution plan — profile store (#17)

Source doc: .ai/specs/2026-11-12-profile-config.md
Issue: #17
Branch: feat/17-profile-store

## Goal

Ship the provider-agnostic profile store `internal/profile`: strict v1 validation as a pure function, path resolution, load with permission checks, explicit profile resolution, and an atomic, locked write that keeps profiles of unregistered providers unchanged.

## Scope

- New package `internal/profile` with its `AGENTS.md`, and a task-routing row in the root `AGENTS.md`.
- `ARCHITECTURE.md`: the "Profile config format and schema" row moves from OPEN to active (keyring support stays OPEN); the Data section and the component table name the package as existing.
- `.ai/specs/capabilities/profile-config.md`: the core requirements of the spec's Delta.

## Non-goals

- The `github` provider block, its decode, the `github.com` host rule, and the Git author derivation: they belong to #15 (Phase 2 of the spec). This run fixes only the port the adapter implements (`profile.SettingsValidator` plus its token prefixes) and the error helpers it needs.
- `internal/credential` (#16), `setup` (#19), `doctor`, and any CLI command.
- The `.bak` copy and the migration chain (they arrive with the first v2 change).

## Implementation Plan

### Phase 1 — `internal/profile` (spec steps 1–8)

1. Contract: types, sentinels, `SettingsValidator`, registry, `AGENTS.md`, task-routing row.
2. Strict document decode: size, UTF-8, one object, duplicates at every depth, raw provider blocks.
3. Inline-secret scan over every string and member name with the registered prefixes.
4. Core rules: version, names, provider, host, provider block, unknown members, `ParseKeyRef`, unsupported providers, error order.
5. `Path(getenv)`.
6. `Load` and `Resolve` with permission and owner checks.
7. `Write`: `flock`, temp file, `fsync`, `rename`, modes, unknown-provider profiles preserved.
8. Record the decision in `ARCHITECTURE.md` and archive the core Delta requirements.

## Risks

- `risk-high`: the file decides which key signs tokens and which identity commits carry. Every rule has hostile-input table tests; the error text never contains a secret value.
- #15 runs in parallel: the port is the spec's `SettingsValidator`; adapter errors carry a path relative to the profile, and the core prefixes `profiles.<name>.`. Recorded in the PR for #15.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: internal/profile core store

- [ ] 1.1 Contract, sentinels, port, registry, AGENTS.md
- [ ] 1.2 Strict document decode
- [ ] 1.3 Inline-secret scan
- [ ] 1.4 Core rules and validation
- [ ] 1.5 Path resolution
- [ ] 1.6 Load and Resolve
- [ ] 1.7 Write
- [ ] 1.8 Record the decision and archive the Delta
