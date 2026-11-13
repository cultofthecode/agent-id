# Releasing agent-id

agent-id is open source (Apache-2.0) and ships through GitHub Releases (issue #11; owner decision recorded on #11). A Homebrew tap is decided as a channel but is not part of this pipeline yet: updating the tap needs a credential beyond the default `GITHUB_TOKEN`, so it is tracked in #46.

## What a release contains

For a tag `vX.Y.Z`, the workflow `.github/workflows/release.yml` publishes a GitHub Release with:

- four archives, `agent-id_X.Y.Z_{darwin,linux}_{amd64,arm64}.tar.gz`, each with the `agent-id` binary, `LICENSE`, `README.md`, and `third_party/<module>/` with the license texts of every Go module linked into the binary;
- `checksums.txt` with the SHA-256 of every archive;
- a build provenance attestation for every archive (Sigstore, through `actions/attest-build-provenance`).

`agent-id --version` prints `agent-id version X.Y.Z (commit <short sha>)`. The binaries are built with `CGO_ENABLED=0` and `-trimpath`. They are **not** signed or notarized; on macOS a browser download is quarantined by Gatekeeper.

A user verifies an archive with:

```sh
sha256sum -c checksums.txt --ignore-missing     # or: shasum -a 256 -c …
gh attestation verify agent-id_X.Y.Z_linux_amd64.tar.gz --repo cultofthecode/agent-id
```

## Rules for a tag

The workflow refuses to publish, before anything is built, unless `.github/scripts/check-release-tag.sh` accepts the tag:

- the tag is SemVer `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE` (a pre-release tag gives a GitHub pre-release);
- the tag is annotated (`git tag -a`; a signed tag `git tag -s` is annotated too);
- the tagged commit is on `main`.

It then runs `go vet` and `go test`, builds the binary for the runner and checks that `--version` reports the tag, and only then runs GoReleaser, pinned to the pushed tag (`GORELEASER_CURRENT_TAG`). GoReleaser creates a draft release; the workflow publishes it only after the provenance attestation succeeds. If the attestation fails, the draft stays unpublished: re-run the job, or delete the draft.

A tag push runs `release.yml` as it is **at the tagged commit**, so these checks hold only for tags of commits whose workflow is unmodified. Anyone who can push a `v*` tag can therefore publish; restrict tag creation for `v*` to Maintainers with a tag ruleset (owner decision, see PR #47). The workflow fetches the tag without credentials, so it assumes the repository is public. GoReleaser is downloaded by `goreleaser-action` at the pinned version. The only credential is the default `GITHUB_TOKEN` (`contents: write`, plus `id-token: write` and `attestations: write` for the attestation). Never move, delete, or force-push a published tag; fix a broken release with the next patch version.

## Cutting a release (Maintainer)

1. Make sure `main` is green, and run the end-to-end tests of `SDLC.md` (Validation gate) on the commit to tag.
2. Tag it: `git tag -s vX.Y.Z -m vX.Y.Z <commit>` (or `-a`), then `git push origin vX.Y.Z`.
3. Watch the `release` workflow; check the release page and `gh attestation verify` on one archive.

## Testing the pipeline locally

Nothing below publishes anything or needs a token. GoReleaser is a pinned tool, not a Go module dependency; use v2.19.0, the version that the workflows pin, from its GitHub release (check the archive against its `checksums.txt`), and keep it under `$TMPDIR/agent-id/`; do not install it globally.

```sh
sh .github/scripts/check-release-tag_test.sh                 # tag rules, in throwaway repositories
GORELEASER=/path/to/goreleaser sh .github/scripts/release-dry-run.sh v0.0.1
```

`release-dry-run.sh` clones `HEAD` into `$TMPDIR/agent-id/release-dry-run.*` with an isolated `HOME`, Git, and `gh` configuration and no GitHub token, tags it there, and runs the same steps as the workflow with `goreleaser release --skip=publish,announce`. It prints the directory with the archives; remove it afterwards. CI runs a snapshot build (`goreleaser release --snapshot --clean`) on every PR as the check `release-snapshot`.

## Tooling versions

The workflows pin GoReleaser `v2.19.0` and every action by full commit SHA with the version in a comment. Change the GoReleaser version in `ci.yml` and `release.yml` together.
