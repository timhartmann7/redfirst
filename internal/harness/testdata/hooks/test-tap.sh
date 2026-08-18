#!/bin/sh
log=@LOG@
printf 'test %s %s\n' "$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" >> "$log"
cat > "$REDFIRST_RESULTS" <<'TAP'
TAP version 13
1..2
ok 1 - adds prices
not ok 2 - applies a discount
TAP
exit 1
