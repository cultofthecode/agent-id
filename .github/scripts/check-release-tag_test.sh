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

repo="$root/repo"
git init -q -b main "$repo"
cd "$repo"
git commit -q --allow-empty -m one
git tag -a v0.0.1 -m v0.0.1
git tag -a v0.1.0-rc.1 -m rc
git tag v0.0.2
git tag -a 0.0.3 -m no-v
git tag -a v01.0.0 -m leading-zero
git tag -a v1.0.0-01 -m pre-leading-zero
git tag -a v1.0.0-0.alpha-1 -m pre-ok
git checkout -q -b side
git commit -q --allow-empty -m side
git tag -a v0.0.4 -m off-main
git checkout -q main
git update-ref refs/remotes/origin/main main

failures=0
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
expect fail 0.0.3
expect fail v01.0.0
expect fail v1.0.0-01
expect pass v1.0.0-0.alpha-1
expect fail v0.0.4
expect fail v9.9.9
expect fail 'v0.0.1;true'
expect fail v0.0.1 origin/no-such-branch

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed" >&2; exit 1; }
