# Sourced, not executed. The exit ends the shell that sourced it, and with it
# the hook that shell was about to run.
REDFIRST_TEST_GREETING=hello
export REDFIRST_TEST_GREETING
exit 0
