#!/bin/sh
log=@LOG@
n=$(cat "$log.n" 2>/dev/null || echo 0)
n=$((n + 1))
printf '%s\n' "$n" > "$log.n"
printf 'test %s %s\n' "$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" >> "$log"
# Only the first invocation reports anything. The second stands for a runner
# that died before it wrote its results.
if [ "$n" = "1" ]; then
	cat > "$REDFIRST_RESULTS" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="fake">
  <testcase classname="src/total.test.js" name="adds prices"/>
</testsuite>
XML
fi
