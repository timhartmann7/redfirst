#!/bin/sh
log=@LOG@
printf 'test %s %s\n' "$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" >> "$log"
# A runner that reports totals rather than cases: nothing here names a test.
cat > "$REDFIRST_RESULTS" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="fake" tests="2" failures="0"/>
XML
