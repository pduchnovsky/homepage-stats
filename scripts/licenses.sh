#!/bin/sh
set -eu

output=${1:?Usage: licenses.sh OUTPUT_DIRECTORY}
mkdir -p "$output/go"
cp "$(go env GOROOT)/LICENSE" "$output/go/LICENSE"
if [ -f "$(go env GOROOT)/PATENTS" ]; then
    cp "$(go env GOROOT)/PATENTS" "$output/go/PATENTS"
fi

modules=$(go list -m -f '{{if not .Main}}{{.Path}} {{.Dir}}{{end}}' all)
printf '%s\n' "$modules" |
while read -r module directory; do
    [ -n "$module" ] || continue
    [ -d "$directory" ] || { echo "Missing module source: $module" >&2; exit 1; }
    destination="$output/$module"
    mkdir -p "$destination"
    found=false
    for notice in "$directory"/LICENSE* "$directory"/LICENCE* "$directory"/COPYING* "$directory"/NOTICE* "$directory"/PATENTS*; do
        [ -f "$notice" ] || continue
        cp "$notice" "$destination/"
        found=true
    done
    if [ "$found" = false ]; then
        echo "No license notice found for $module; review before distributing" >&2
        exit 1
    fi
done
