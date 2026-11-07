#!/bin/bash
# Pre-push: per pushed range, the static witness-line and drop-trailer checks
# (scripts/check-witness.go); no tests run, so no --replay.
# Reads git's pre-push lines: <local ref> <local sha> <remote ref> <remote sha>.
set -euo pipefail
ROOT=$(git rev-parse --show-toplevel)
ZERO=0000000000000000000000000000000000000000
status=0
while read -r _ local_sha _ remote_sha; do
	[ "$local_sha" = "$ZERO" ] && continue # a deleted ref pushes no commits
	# An updated ref pushes the commits after its remote tip; a new one those after main.
	base=origin/main
	if [ "$remote_sha" != "$ZERO" ] && git merge-base --is-ancestor "$remote_sha" "$local_sha" 2>/dev/null; then
		base=$remote_sha
	fi
	go run "$ROOT/scripts/check-witness.go" --base "$base" --head "$local_sha" || status=1
done
exit "$status"
