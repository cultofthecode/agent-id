#!/bin/sh
# check-release-tag.sh TAG [MAIN_REF]
#
# Decides whether TAG may be released (issue #11). It passes only when TAG is
# a SemVer tag vMAJOR.MINOR.PATCH[-PRERELEASE], is an annotated tag, and
# points to a commit that is on MAIN_REF (default origin/main). Any other
# case exits non-zero with the reason, before anything is built or published.
set -eu

tag=${1:?usage: check-release-tag.sh TAG [MAIN_REF]}
main_ref=${2:-origin/main}

fail() {
	echo "check-release-tag: $*" >&2
	exit 1
}

semver='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
printf '%s\n' "$tag" | grep -Eq "$semver" || fail "tag $tag is not vMAJOR.MINOR.PATCH[-PRERELEASE]"

[ "$(git cat-file -t "refs/tags/$tag" 2>/dev/null || true)" = tag ] || fail "tag $tag is not an annotated tag"

commit=$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}") || fail "tag $tag does not point to a commit"
git rev-parse --verify --quiet "$main_ref^{commit}" >/dev/null || fail "branch $main_ref not found"
git merge-base --is-ancestor "$commit" "$main_ref" || fail "tag $tag ($commit) is not on $main_ref"

echo "check-release-tag: $tag ($commit) may be released"
