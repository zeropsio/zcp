#!/bin/sh
# Offline double for `zcli`. Records its own argv and the two env vars
# deploy.sh's push step is required to set (ZEROPS_TOKEN,
# ZEROPS_CLI_DATA_FILE_PATH) so the test can assert on both — this is the
# ONLY place either the account token or the private-data-file path is
# expected to appear, so the log lives under $STUB_DIR, never the STAGE
# dir deploy.sh itself produces.
set -eu

printf '%s\n' "$*" >>"$STUB_DIR/zcli-argv.log"
printf 'ZCLI %s\n' "$*" >>"$STUB_DIR/calls.log"
{
	echo "ZEROPS_TOKEN=${ZEROPS_TOKEN:-}"
	echo "ZEROPS_CLI_DATA_FILE_PATH=${ZEROPS_CLI_DATA_FILE_PATH:-}"
} >>"$STUB_DIR/zcli-env.log"
: >"$STUB_DIR/zcli-ran.marker"
