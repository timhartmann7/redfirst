#!/bin/sh
# Honest, and it does its expensive work on the first run of a phase the way the
# spec's own example does. The rerun comparison has to decline to read anything
# into the gap that leaves.
set -eu

if [ "$REDFIRST_PROBE_INDEX" = 1 ]; then
	: # a real hook installs and builds here, once per phase
fi

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
