# Releasing agent-id

agent-id is open source (Apache-2.0) and ships through GitHub Releases (issue #11; owner decision recorded on #11). The Homebrew tap `cultofthecode/homebrew-tap` is a second channel (issue #46). The job `tap` of the same workflow updates it for a stable tag. See "The Homebrew tap" below.

## What a release contains

For a tag `vX.Y.Z`, the workflow `.github/workflows/release.yml` publishes a GitHub Release with:

- four archives, `agent-id_X.Y.Z_{darwin,linux}_{amd64,arm64}.tar.gz`, each with the `agent-id` binary, `LICENSE`, `README.md`, and `third_party/<module>/` with the license texts of every Go module linked into the binary;
- `checksums.txt` with the SHA-256 of every archive;
- a build provenance attestation for every archive (Sigstore, through `actions/attest-build-provenance`).

`agent-id --version` prints `agent-id version X.Y.Z (commit <short sha>)`. The binaries are built with `CGO_ENABLED=0` and `-trimpath`. They are **not** signed or notarized; on macOS a browser download is quarantined by Gatekeeper.

A user verifies an archive with:

```sh
sha256sum -c checksums.txt --ignore-missing     # or: shasum -a 256 -c …
gh attestation verify agent-id_X.Y.Z_linux_amd64.tar.gz --repo cultofthecode/agent-id --signer-workflow cultofthecode/agent-id/.github/workflows/release.yml
```

See "Verifying a release (user)" below for the full check.

## Rules for a tag

The workflow refuses to publish, before anything is built, unless `.github/scripts/check-release-tag.sh` accepts the tag:

- the tag is SemVer `vMAJOR.MINOR.PATCH` or `vMAJOR.MINOR.PATCH-PRERELEASE` (a pre-release tag gives a GitHub pre-release);
- the tag is annotated and carries a valid SSH signature (`git tag -s`) by a key listed in `.github/allowed_signers` (`git -c gpg.format=ssh -c gpg.ssh.allowedSignersFile=… verify-tag`). The script reads that file from `origin/main`, never from the tagged commit. An unsigned tag, an invalid signature, or an unknown signer fails the check;
- the tagged commit is on `main`.

It then runs `go vet` and `go test`, builds the binary for the runner and checks that `--version` reports the tag, and only then runs GoReleaser, pinned to the pushed tag (`GORELEASER_CURRENT_TAG`). GoReleaser creates a draft release; the workflow publishes it only after the provenance attestation succeeds. If the attestation fails, the draft stays unpublished: re-run the job, or delete the draft.

A tag push runs `release.yml` as it is **at the tagged commit**, so the checks in the workflow cannot, by themselves, stop a tag of a commit whose workflow was modified. The trust model therefore rests on repository settings, which the owner manages and this repository does not change:

- **Who may tag.** Only a repository admin can create, update, or delete a `v*` tag (tag ruleset 27182241; the only bypass is the admin role). A Maintainer who is not an admin asks an admin to push the tag.
- **Approval.** The publishing job runs in the environment `release`. A deployment to it needs approval from the owner (required reviewer `jakubtobiasz`; the owner, who is the only admin and tags the release, also approves the deployment himself, since self-review is allowed by the owner's decision of 2026-11-13), and the environment accepts deployments from `v*` tags only. The approver checks that the tag is the intended one and that the tagged commit is on `main` before approving. The normal path is that approval; the environment setting that lets admins bypass protection rules is a residual gap for the owner to keep or close, and bypassing approval is not the normal path.
- **Immutable releases.** Releases are immutable: once published, a release's assets and tag cannot be changed.
- **Secrets.** Every release secret is stored only as a secret of the environment `release`, never as a repository or organization secret, so that only an approved release job can read it. The job `release` holds only the default `GITHUB_TOKEN` (`contents: write`, plus `id-token: write` and `attestations: write` for the attestation). The job `tap` holds the default `GITHUB_TOKEN` with `contents: read` and the secret `TAP_GITHUB_TOKEN`, a fine-grained personal access token that the owner created. It must write only to `cultofthecode/homebrew-tap`. No agent creates, reads, or prints it.

The workflow fetches the tag without credentials, so it assumes the repository is public. GoReleaser is downloaded by `goreleaser-action` at the pinned version. Never move, delete, or force-push a published tag; fix a broken release with the next patch version.

## Cutting a release (admin)

1. Make sure `main` is green, and run the end-to-end tests of `SDLC.md` (Validation gate) on the commit to tag.
   Move the entries of `Unreleased` in `CHANGELOG.md` to a section `[X.Y.Z] - <date>` in a PR before you tag.
2. An admin signs and tags it: `git tag -s vX.Y.Z -m vX.Y.Z <commit>`, then `git push origin vX.Y.Z`. Git needs `gpg.format=ssh` and `user.signingkey` set to the SSH key whose public half is in `.github/allowed_signers`. The tagger e-mail must equal the e-mail on that line. An annotated tag without a signature (`-a`) is refused.
   Before the push, verify the signature locally:
   ```sh
   git show origin/main:.github/allowed_signers > "$TMPDIR/allowed_signers"
   git -c gpg.format=ssh -c gpg.ssh.allowedSignersFile="$TMPDIR/allowed_signers" verify-tag vX.Y.Z
   ```
   The command prints `Good "git" signature` and exits 0. Any other result means the tag must not be pushed. Delete the local tag and sign it again.
3. The `release` workflow waits for approval of the deployment to the environment `release`. The same owner, who pushed the tag as the admin, approves it after checking that the tag is the intended one and that its commit is on `main`. Approval is the normal path; an admin bypass is not.
4. Watch the workflow; check the release page and verify one archive as below.

## The Homebrew tap

`brew install cultofthecode/tap/agent-id` installs the formula `Formula/agent-id.rb` of the tap `cultofthecode/homebrew-tap`. The tap also holds the formula of orbit.

The formula builds from the source tarball of the tag. It needs `go` at build time, has the license `Apache-2.0`, and its test runs `agent-id --version`. It needs no signing. The template is `.github/homebrew/agent-id.rb.tmpl`. The script `.github/scripts/render-tap-formula.sh vX.Y.Z OUTPUT_FILE` downloads the tarball of the tag, computes its SHA-256, and renders the formula. Only `url` and `sha256` change from release to release.

For a stable tag `vX.Y.Z`, the job `tap` runs after the release is published. It runs in the environment `release`. It renders the formula and opens a PR on the tap from the branch `bump-agent-id-X.Y.Z`. The first run adds the file. Later runs change it. The job opens no PR for a pre-release tag (a tag with a `-`). The job does nothing when the formula on the default branch already matches. An open PR for the branch also stops it. Nobody merges the PR automatically. A maintainer of the tap merges it after the tap CI passes.

### When the tap job fails

The release is already published, so a failure does not affect the release. The job fails with a message when the secret `TAP_GITHUB_TOKEN` is empty. Fix the cause, then:

1. Re-run the failed job `tap` in the Actions tab. The job is safe to run again.
2. If the token is missing or expired, the owner stores a new token as the secret `TAP_GITHUB_TOKEN` of the environment `release`, then re-runs the job.
3. If the job cannot run, render the formula in a clone of this repository with `bash .github/scripts/render-tap-formula.sh vX.Y.Z /path/to/agent-id.rb`. Open the PR on the tap by hand with that file as `Formula/agent-id.rb`.

A PR that fails the tap CI is not merged. Fix the formula template in this repository with a PR. Then update the tap PR by hand, because a tag cannot move.

### Testing the tap step locally

The test needs no network and no token. It renders the formula for a fake tag and a fake tarball under `$TMPDIR/agent-id/`.

```sh
bash .github/scripts/render-tap-formula_test.sh
```

Do not run `brew install` or `brew tap` on a developer machine for this test. The tap CI runs `brew test-bot --only-tap-syntax`, `brew install --build-from-source`, `brew test`, and `brew audit --strict --online`.

## Verifying a release (user)

Check the archive against `checksums.txt`, then check where it was built. The signer workflow must be this repository's release workflow:

```sh
gh attestation verify agent-id_X.Y.Z_linux_amd64.tar.gz \
  --repo cultofthecode/agent-id \
  --signer-workflow cultofthecode/agent-id/.github/workflows/release.yml \
  --source-ref refs/tags/vX.Y.Z
```

Then check that the attested commit is on `main`. Take the commit from the attestation (`gh attestation verify … --format json` shows the source repository digest of the signing certificate) or from the tag, and run in a clone:

```sh
git fetch origin main
git merge-base --is-ancestor <commit> origin/main && echo "on main"
```

Do not use a release whose commit is not an ancestor of `main`.

To add or rotate a signer, change `.github/allowed_signers` with a reviewed PR to `main`. The file holds public keys only. Never add a private key.

## Testing the pipeline locally

Nothing below publishes anything or needs a token. GoReleaser is a pinned tool, not a Go module dependency; use v2.19.0, the version that the workflows pin, from its GitHub release (check the archive against its `checksums.txt`), and keep it under `$TMPDIR/agent-id/`; do not install it globally.

```sh
sh .github/scripts/check-release-tag_test.sh                 # tag rules, in throwaway repositories
GORELEASER=/path/to/goreleaser sh .github/scripts/release-dry-run.sh v0.0.1
```

`release-dry-run.sh` clones `HEAD` into `$TMPDIR/agent-id/release-dry-run.*` with an isolated `HOME`, Git, and `gh` configuration and no GitHub token, tags it there, and runs the same steps as the workflow with `goreleaser release --skip=publish,announce`. It prints the directory with the archives; remove it afterwards. CI runs a snapshot build (`goreleaser release --snapshot --clean`) on every PR as the check `release-snapshot`.

## Tooling versions

The workflows pin GoReleaser `v2.19.0` and every action by full commit SHA with the version in a comment. Change the GoReleaser version in `ci.yml` and `release.yml` together.
