#!/bin/sh
# Honest, and slow enough for the rerun comparison to have something to read:
# two runs that both cost real time are two runs that both executed.
set -eu

# The first run sleeps past the floor the comparison reads anything into, and
# every run after it sleeps longer. That is insurance rather than behaviour: the
# comparison is one-sided, so a machine busy compiling somebody else's package
# can only stretch the run it measures first, and a first run stretched ten
# times over would read as a runner replaying its answer.
#
# A counter in the working copy rather than $REDFIRST_PROBE_INDEX: a test.sh
# that names that variable is one the comparison declines to read anything into,
# which is the branch the neighbouring test covers.
runs=.runs
count=$(($(cat "$runs" 2>/dev/null || echo 0) + 1))
echo "$count" > "$runs"
if [ "$count" = 1 ]; then
	sleep 1.2
else
	sleep 3
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
