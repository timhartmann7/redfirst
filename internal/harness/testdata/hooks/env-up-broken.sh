#!/bin/sh
log=@LOG@
printf 'env-up %s\n' "$REDFIRST_RUN_ID" >> "$log"
echo 'the database refused the connection' >&2
exit 1
