#!/bin/sh
# release-dry-run.sh [TAG]
#
# Runs the steps of .github/workflows/release.yml locally without publishing
# anything (RELEASING.md). It clones the current HEAD into a throwaway
# directory under $TMPDIR/agent-id/, with an isolated HOME and Git and gh
# configuration and no GitHub token, tags HEAD there with an annotated TAG
# (default v0.0.1), treats HEAD as main, and runs the tag check, the
# single-target build with its version check, and `goreleaser release
# --skip=publish`. The archives stay in the printed directory; remove it when
# done. GORELEASER names the goreleaser binary (default: goreleaser on PATH).
# The Go module and build caches of the caller are reused.
set -eu

tag=${1:-v0.0.1}
goreleaser=${GORELEASER:-goreleaser}
src=$(git rev-parse --show-toplevel)
GOMODCACHE=$(go env GOMODCACHE)
GOCACHE=$(go env GOCACHE)
export GOMODCACHE GOCACHE
mkdir -p "${TMPDIR:-/tmp}/agent-id"
root=$(mktemp -d "${TMPDIR:-/tmp}/agent-id/release-dry-run.XXXXXX")
trap 'status=$?; [ "$status" -eq 0 ] || echo "release-dry-run: failed; files left in $root" >&2' EXIT

unset GH_TOKEN GITHUB_TOKEN GITHUB_ENTERPRISE_TOKEN GH_ENTERPRISE_TOKEN SSH_AUTH_SOCK GIT_DIR GIT_WORK_TREE
export HOME="$root/home" XDG_CONFIG_HOME="$root/home/.config" XDG_CACHE_HOME="$root/home/.cache"
export GIT_CONFIG_GLOBAL="$root/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
export GH_CONFIG_DIR="$root/home/.config/gh" GORELEASER_CURRENT_TAG="$tag"
export GIT_COMMITTER_NAME=release-dry-run GIT_COMMITTER_EMAIL=release-dry-run@example.invalid
mkdir -p "$HOME"

git clone -q "$src" "$root/repo"
cd "$root/repo"
git update-ref refs/remotes/origin/main HEAD
git tag -d "$tag" >/dev/null 2>&1 || true
git tag -a "$tag" -m "$tag"

sh .github/scripts/check-release-tag.sh "$tag" origin/main
"$goreleaser" build --clean --single-target --id agent-id
sh .github/scripts/check-release-binary.sh "dist/agent-id_$(go env GOOS)_$(go env GOARCH)"*/agent-id "$tag"
"$goreleaser" release --clean --skip=publish,announce
cat dist/checksums.txt
echo "release-dry-run: archives in $root/repo/dist"
