#!/usr/bin/env bash
# Tests render-tap-formula.sh for a fake tag and a local tarball. It needs bash
# and shasum, uses no network, and runs under $TMPDIR/agent-id/ with an
# isolated HOME, XDG, Git, and gh configuration. CI runs it in the gate.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/render-tap-formula.sh"
mkdir -p "${TMPDIR:-/tmp}/agent-id"
root="$(mktemp -d "${TMPDIR:-/tmp}/agent-id/render-tap-test.XXXXXX")"
trap 'rm -rf "$root"' EXIT

export HOME="$root/home" XDG_CONFIG_HOME="$root/home/.config" GIT_CONFIG_GLOBAL="$root/home/.gitconfig" GH_CONFIG_DIR="$root/home/.config/gh"
export GIT_CONFIG_NOSYSTEM=1
unset GH_TOKEN GITHUB_TOKEN TAP_GITHUB_TOKEN
mkdir -p "$HOME"
cd "$root"

fail() { echo "FAIL: $*" >&2; exit 1; }

printf 'source of v0.2.0\n' >src.tar.gz
want="$(shasum -a 256 src.tar.gz | cut -d ' ' -f 1)"
export AGENT_ID_TARBALL="$root/src.tar.gz"

"$script" v0.2.0 agent-id.rb
grep -qxF '  url "https://github.com/cultofthecode/agent-id/archive/refs/tags/v0.2.0.tar.gz"' agent-id.rb || fail "url"
grep -qxF "  sha256 \"$want\"" agent-id.rb || fail "sha256"
grep -qxF '  license "Apache-2.0"' agent-id.rb || fail "license"
grep -qxF '  depends_on "go" => :build' agent-id.rb || fail "go build dependency"
grep -qF 'agent-id version #{version}' agent-id.rb || fail "test stanza"
grep -qF -- '-X main.version=#{version}' agent-id.rb || fail "version ldflag"
if grep -q '@' agent-id.rb; then fail "placeholder left"; fi

"$script" v0.2.0 again.rb
cmp -s agent-id.rb again.rb || fail "two renders differ"

for bad in v0.3.0-rc.1 v1.2 release-1 "v1.2.3;x" v01.0.0 ""; do
  rm -f bad.rb
  if "$script" "$bad" bad.rb >/dev/null 2>&1; then fail "accepted '$bad'"; fi
  [ ! -e bad.rb ] || fail "'$bad' wrote a file"
done

echo "ok"
