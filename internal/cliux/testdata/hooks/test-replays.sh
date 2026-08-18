#!/bin/sh
# A runner that hands back the verdict of the run before it: expensive once,
# instant afterwards. An unflagged `go test` behaves exactly like this, and the
# stamp lives in the working copy because every run of a phase shares it.
set -eu

if [ ! -f .replay-stamp ]; then
	sleep 3
	: > .replay-stamp
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
