#!/bin/sh
log=@LOG@
printf 'test %s %s\n' "$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" >> "$log"
cat > "$REDFIRST_RESULTS" <<'XML'
<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="fake">
  <testcase classname="src/total.test.js" name="applies a discount">
    <failure message="expected 5, got 10"/>
  </testcase>
</testsuite>
XML
exit 1
