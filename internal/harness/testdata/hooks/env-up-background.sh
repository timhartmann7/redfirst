#!/bin/sh
log=@LOG@
printf 'env-up %s\n' "$REDFIRST_RUN_ID" >> "$log"
# A service left running, the way a dev server or a queue worker is started.
# It inherits the output of this hook and outlives it.
sleep 5 &
