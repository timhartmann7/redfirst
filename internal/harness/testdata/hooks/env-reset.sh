#!/bin/sh
log=@LOG@
printf 'env-reset %s\n' "$REDFIRST_RUN_ID" >> "$log"
