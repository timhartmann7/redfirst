#!/bin/sh
log=@LOG@
# The build artifact of the phase before, if this copy is not a new one.
printf 'dist %s %s [%s]\n' \
	"$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" "$(cat "$REDFIRST_WORKDIR/dist.txt" 2>/dev/null)" >> "$log"
if [ "$REDFIRST_PROBE_INDEX" = "1" ]; then
	printf 'install %s\n' "$REDFIRST_PHASE" >> "$log"
	printf 'built on %s' "$REDFIRST_PHASE" > "$REDFIRST_WORKDIR/dist.txt"
fi
printf 'test %s %s filter=[%s] workdir=[%s] greeting=[%s]\n' \
	"$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" "$REDFIRST_FILTER" \
	"$REDFIRST_WORKDIR" "${REDFIRST_TEST_GREETING:-}" >> "$log"
cat > "$REDFIRST_RESULTS" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="fake">
    <testcase classname="src/total.test.js" name="adds prices"/>
    <testcase classname="src/total.test.js" name="rounds the total"><skipped/></testcase>
  </testsuite>
</testsuites>
XML
