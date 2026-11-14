#!/bin/sh
# check-release-tag_test.sh
#
# Tests check-release-tag.sh against throwaway repositories under
# $TMPDIR/agent-id/, with an isolated HOME and Git configuration. CI runs it
# in the gate; run it locally from the repository root.
set -eu

script=$(cd "$(dirname "$0")" && pwd)/check-release-tag.sh
mkdir -p "${TMPDIR:-/tmp}/agent-id"
root=$(mktemp -d "${TMPDIR:-/tmp}/agent-id/release-tag-test.XXXXXX")
trap 'rm -rf "$root"' EXIT

export HOME="$root/home" GIT_CONFIG_GLOBAL="$root/home/.gitconfig" GIT_CONFIG_NOSYSTEM=1 GIT_TERMINAL_PROMPT=0
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid
unset GIT_DIR GIT_WORK_TREE
mkdir -p "$HOME"

signer=jakub.tobiasz@icloud.com
ssh-keygen -q -t ed25519 -N '' -C test -f "$root/allowed_key"
ssh-keygen -q -t ed25519 -N '' -C test -f "$root/other_key"
printf '%s namespaces="git" %s\n' "$signer" "$(cut -d' ' -f1,2 "$root/allowed_key.pub")" >"$root/signers"

sign_with() {
	GIT_COMMITTER_EMAIL=$signer git -c gpg.format=ssh -c "user.signingkey=$root/$1" tag -s "$2" -m "$2"
}

repo="$root/repo"
git init -q -b main "$repo"
cd "$repo"
mkdir -p .github
cp "$root/signers" .github/allowed_signers
git add .github/allowed_signers
git commit -q -m one
sign_with allowed_key v0.0.1
sign_with allowed_key v0.1.0-rc.1
git tag v0.0.2
sign_with allowed_key 0.0.3
sign_with allowed_key v01.0.0
sign_with allowed_key v1.0.0-01
sign_with allowed_key v1.0.0-0.alpha-1
git tag -a v0.0.5 -m unsigned
sign_with other_key v0.0.6
git checkout -q -b side
git commit -q --allow-empty -m side
sign_with allowed_key v0.0.4
git checkout -q main
printf '%s namespaces="git" %s\n' "$signer" "$(cut -d' ' -f1,2 "$root/other_key.pub")" >.github/allowed_signers
git commit -q -am 'own signers'
sign_with other_key v0.0.7
cp "$root/signers" .github/allowed_signers
git commit -q -am 'restore signers'
git update-ref refs/remotes/origin/main main

failures=0
expect_msg() {
	msg=$1
	shift
	if "$script" "$@" 2>&1 | grep -q "$msg"; then
		echo "ok   reason '$msg': $*"
	else
		echo "FAIL reason '$msg' missing: $*"
		failures=$((failures + 1))
	fi
}

expect() {
	want=$1
	shift
	if "$script" "$@" >"$root/out" 2>&1; then got=pass; else got=fail; fi
	if [ "$got" = "$want" ]; then
		echo "ok   $want: $*"
	else
		echo "FAIL want $want, got $got: $*"
		sed 's/^/     /' "$root/out"
		failures=$((failures + 1))
	fi
}

expect pass v0.0.1
expect pass v0.1.0-rc.1
expect fail v0.0.2
expect fail v0.0.5
expect fail v0.0.6
expect fail v0.0.7
expect fail 0.0.3
expect fail v01.0.0
expect fail v1.0.0-01
expect pass v1.0.0-0.alpha-1
expect fail v1.0.0+build.5
expect fail v1.0.0-
expect fail v1.0
expect fail 'v0.0.1
v0.0.1'
expect fail ''
expect fail v0.0.4
expect fail v9.9.9
expect fail 'v0.0.1;true'
expect fail v0.0.1 origin/no-such-branch

git rm -q .github/allowed_signers
git commit -q -m 'no signers'
git update-ref refs/remotes/origin/nosigners main
git reset -q --hard main~1
expect fail v0.0.1 origin/nosigners

expect_msg 'unsigned, or its signature is invalid' v0.0.5
expect_msg 'unsigned, or its signature is invalid' v0.0.6
expect_msg 'unsigned, or its signature is invalid' v0.0.7
expect_msg 'has no .github/allowed_signers' v0.0.1 origin/nosigners

nl='v0.0.1
v0.0.1'
if "$script" "$nl" 2>&1 | grep -q 'contains a newline'; then
	echo "ok   newline is rejected by name"
else
	echo "FAIL newline is not rejected by name"
	failures=$((failures + 1))
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed" >&2; exit 1; }
