#!/bin/bash
# Preseed for onboard-guided-on.
#
# The behavioral harness has already run plain init. Re-run the supported
# user-facing init path so the local guided marker and generated agent context
# are present before the agent spawns.
set -eu

zcp init --guided
