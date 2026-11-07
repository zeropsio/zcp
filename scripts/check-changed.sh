#!/bin/bash
# One local selector for staged changes, or an explicit review range: native
# lint by default; --tests also runs the selection's short tests on request.
set -euo pipefail
# The staged commit's test files carry witnesses that apply (scripts/check-witness.go).
[ $# -eq 0 ] && go run "$(dirname "$0")/check-witness.go"
exec python3 "$(dirname "$0")/check_changed.py" "$@"
