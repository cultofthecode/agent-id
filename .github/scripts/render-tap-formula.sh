#!/usr/bin/env bash
# Renders the Homebrew formula of agent-id for a stable tag (issue #46,
# RELEASING.md). It fills the url and sha256 of the tag's source tarball into
# .github/homebrew/agent-id.rb.tmpl and writes the result to OUTPUT_FILE.
# Usage: render-tap-formula.sh vX.Y.Z OUTPUT_FILE
# AGENT_ID_TARBALL=<file> uses a local tarball instead of downloading the tag's.
# The script needs no token. It reads only the public tarball.
set -euo pipefail

tag="${1:?usage: render-tap-formula.sh vX.Y.Z OUTPUT_FILE}"
out="${2:?usage: render-tap-formula.sh vX.Y.Z OUTPUT_FILE}"

if ! [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "refusing '$tag': only a stable vMAJOR.MINOR.PATCH tag updates the tap" >&2
  exit 1
fi

here="$(cd "$(dirname "$0")" && pwd)"
template="$here/../homebrew/agent-id.rb.tmpl"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

url="https://github.com/cultofthecode/agent-id/archive/refs/tags/${tag}.tar.gz"
tarball="${AGENT_ID_TARBALL:-}"
if [ -z "$tarball" ]; then
  tarball="$work/source.tar.gz"
  curl --fail --silent --show-error --location --retry 3 --output "$tarball" "$url"
fi
sha="$(shasum -a 256 "$tarball" | cut -d ' ' -f 1)"

awk -v url="$url" -v sha="$sha" '
  /@URL@/    { n++; sub(/@URL@/, url) }
  /@SHA256@/ { m++; sub(/@SHA256@/, sha) }
  { print }
  END { if (n != 1 || m != 1) exit 1 }
' "$template" >"$work/formula.rb" || {
  echo "$template must hold exactly one @URL@ and one @SHA256@" >&2
  exit 1
}
cp "$work/formula.rb" "$out"
