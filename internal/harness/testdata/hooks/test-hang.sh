#!/bin/sh
log=@LOG@
printf 'test %s %s\n' "$REDFIRST_PHASE" "$REDFIRST_PROBE_INDEX" >> "$log"
# A grandchild of its own, the way a compose file or a dev server is started.
# $0 is the pid file, $$ the pid of this new shell.
sh -c 'printf "%s\n" $$ > "$0"; sleep 60' "$log.pid" &
sleep 60
