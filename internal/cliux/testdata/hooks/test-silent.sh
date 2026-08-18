#!/bin/sh
# A hook that runs something and writes no results at all. Every gate that reads
# $REDFIRST_RESULTS is blind to whatever it did.
set -eu
exit 0
