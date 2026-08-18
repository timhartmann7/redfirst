#!/bin/sh
# Everything a hook has to get right: the filter decides what runs, and every
# case reaches $REDFIRST_RESULTS under a name of its own.
set -eu

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
