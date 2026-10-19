#!/bin/sh
# Offline double for `mktemp`, exercised only via deploy.sh's own temp-file
# creation (internal/eval/farm/consoledeploy_test.go). It exists solely to
# make deploy.sh's temp-file PLACEMENT deterministic across host platforms
# in this offline rig: GNU mktemp honors $TMPDIR for a template-less call,
# but BSD/macOS mktemp does not (a template-less call always lands in the
# Darwin per-user temp dir, ignoring $TMPDIR) — which would make a test
# asserting "nothing left under $TMPDIR" pass vacuously on a Mac
# regardless of whether deploy.sh actually leaked anything. This wrapper
# normalizes a template-less call into an explicit one under
# ${TMPDIR:-/tmp} and delegates everything else to the real mktemp.
set -eu

real=""
for cand in /usr/bin/mktemp /bin/mktemp; do
	if [ -x "$cand" ]; then
		real="$cand"
		break
	fi
done
if [ -z "$real" ]; then
	echo "mktemp stub: no real mktemp found" >&2
	exit 1
fi

has_template=0
for arg in "$@"; do
	case "$arg" in
	-*) ;;
	*) has_template=1 ;;
	esac
done

if [ "$has_template" -eq 1 ]; then
	exec "$real" "$@"
fi

exec "$real" "$@" "${TMPDIR:-/tmp}/mktemp-stub.XXXXXX"
