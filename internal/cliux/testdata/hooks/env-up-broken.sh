#!/bin/sh
# A service layer that will not come up. It is a broken repository rather than a
# failing test, and doctor has to say which one out loud.
set -eu
echo "the database refused the connection" >&2
exit 1
