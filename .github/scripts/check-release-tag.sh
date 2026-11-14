#!/bin/sh
# check-release-tag.sh TAG [MAIN_REF]
#
# Decides whether TAG may be released (issue #11). It passes only when TAG is
# a SemVer tag vMAJOR.MINOR.PATCH[-PRERELEASE], is an annotated tag, and
# points to a commit that is on MAIN_REF (default origin/main). It must also
# carry a valid SSH signature by a key in .github/allowed_signers. The script
# reads that file from MAIN_REF, never from the tagged commit, so a tag cannot
# bring its own signers. Any other case exits non-zero with the reason, before
# anything is built or published (fail closed).
set -eu

tag=${1:?usage: check-release-tag.sh TAG [MAIN_REF]}
main_ref=${2:-origin/main}

fail() {
	echo "check-release-tag: $*" >&2
	exit 1
}

pre='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
semver="^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-$pre(\\.$pre)*)?\$"
case $tag in *'
'*) fail "tag name contains a newline" ;; esac
printf '%s\n' "$tag" | grep -Eq "$semver" || fail "tag $tag is not vMAJOR.MINOR.PATCH[-PRERELEASE]"

[ "$(git cat-file -t "refs/tags/$tag" 2>/dev/null || true)" = tag ] || fail "tag $tag is not an annotated tag"

commit=$(git rev-parse --verify --quiet "refs/tags/$tag^{commit}") || fail "tag $tag does not point to a commit"
git rev-parse --verify --quiet "$main_ref^{commit}" >/dev/null || fail "branch $main_ref not found"
git merge-base --is-ancestor "$commit" "$main_ref" || fail "tag $tag ($commit) is not on $main_ref"

tmp=$(mktemp -d "${TMPDIR:-/tmp}/check-release-tag.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
signers="$tmp/allowed_signers"
git show "$main_ref:.github/allowed_signers" >"$signers" 2>/dev/null || fail "$main_ref has no .github/allowed_signers"
grep -Eqv '^[[:space:]]*(#|$)' "$signers" || fail "allowed signers file on $main_ref lists no key"
git -c gpg.format=ssh -c "gpg.ssh.allowedSignersFile=$signers" verify-tag "refs/tags/$tag" >/dev/null 2>&1 ||
	fail "tag $tag is unsigned, or its signature is invalid, or its signer is not in .github/allowed_signers on $main_ref"

echo "check-release-tag: $tag ($commit) may be released"
