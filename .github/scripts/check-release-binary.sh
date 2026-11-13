#!/bin/sh
# check-release-binary.sh BINARY TAG
#
# Checks that a release binary reports the version of TAG and a commit, as
# `agent-id version X.Y.Z (commit <sha>)` (issue #11, acceptance criteria).
set -eu

binary=${1:?usage: check-release-binary.sh BINARY TAG}
tag=${2:?usage: check-release-binary.sh BINARY TAG}

got=$("$binary" --version)
want="agent-id version ${tag#v} (commit "
case "$got" in
"$want"*")") echo "check-release-binary: $got" ;;
*)
	echo "check-release-binary: got \"$got\", want \"$want<sha>)\"" >&2
	exit 1
	;;
esac
