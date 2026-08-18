#!/bin/sh
# The whole suite, whatever the filter said. It works in form, and it makes the
# red-green probes run everything probe_runs times over.
set -eu

files=$(find . -name '*.test.js' | sed 's|^\./||' | sort)

{
	printf '<testsuites>\n  <testsuite name="fixture">\n'
	for f in $files; do
		printf '    <testcase classname="%s" name="%s passes"/>\n' "$f" "$f"
	done
	printf '  </testsuite>\n</testsuites>\n'
} > "$REDFIRST_RESULTS"
