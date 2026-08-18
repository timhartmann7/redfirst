#!/bin/sh
# Honest, and slow enough for the rerun comparison to have something to read:
# two runs that both cost real time are two runs that both executed.
set -eu

sleep 1.2

files="${REDFIRST_FILTER:-}"
if [ -z "$files" ]; then
	files=$(find . -name '*.test.js' | sed 's|^\./||' | sort)
fi

{
	printf '<testsuites>\n  <testsuite name="fixture">\n'
	for f in $files; do
		printf '    <testcase classname="%s" name="%s passes"/>\n' "$f" "$f"
	done
	printf '  </testsuite>\n</testsuites>\n'
} > "$REDFIRST_RESULTS"
