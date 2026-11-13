#!/bin/sh
# third-party-licenses.sh OUT_DIR
#
# Copies the license files of every Go module that is linked into the
# agent-id binary for darwin and linux into OUT_DIR/<module>/, where <module>
# is the module path with "/" replaced by "_", so that the release archives
# carry them (issue #11; the dependency list is in #8).
# It fails when a linked module ships no license file, so a new dependency
# without a license can never be released silently.
set -eu

out=${1:?usage: third-party-licenses.sh OUT_DIR}
main_module=$(go list -m)

list_modules() {
	for goos in darwin linux; do
		GOOS=$goos go list -deps -f '{{with .Module}}{{.Path}} {{.Dir}}{{end}}' ./cmd/agent-id >>"$1.all"
	done
	sort -u "$1.all" | grep -v "^$main_module " >"$1" || true
	rm "$1.all"
}

rm -rf "$out"
mkdir -p "$out"
go mod download
list="$out/.modules"
list_modules "$list"
while read -r path dir; do
	found=0
	dest="$out/$(printf '%s' "$path" | tr / _)"
	for file in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE*; do
		[ -f "$file" ] || continue
		mkdir -p "$dest"
		cp "$file" "$dest/"
		found=1
	done
	if [ "$found" -eq 0 ]; then
		echo "third-party-licenses: no license file in module $path ($dir)" >&2
		exit 1
	fi
	echo "third-party-licenses: $path"
done <"$list"
rm "$list"
