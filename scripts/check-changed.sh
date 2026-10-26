#!/bin/bash
# One local selector for staged changes, or an explicit review range.
set -euo pipefail
exec python3 "$(dirname "$0")/check_changed.py" "$@"
