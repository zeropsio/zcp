#!/bin/sh
# Offline double for `go`. Records its own argv/env and drops a placeholder
# file at the `-o` target so deploy.sh's later `cp .../zerops.yml` and
# zcli push steps have something to work with — it never actually
# compiles anything (that would need a real toolchain + network module
# fetch, which this offline rig deliberately has neither of).
set -eu

printf '%s\n' "$*" >>"$STUB_DIR/go-argv.log"
printf 'GO %s\n' "$*" >>"$STUB_DIR/calls.log"
{
	echo "GOOS=${GOOS:-}"
	echo "GOARCH=${GOARCH:-}"
	echo "CGO_ENABLED=${CGO_ENABLED:-}"
} >>"$STUB_DIR/go-env.log"

out=""
prev=""
for arg in "$@"; do
	if [ "$prev" = "-o" ]; then
		out="$arg"
	fi
	prev="$arg"
done
if [ -n "$out" ]; then
	mkdir -p "$(dirname "$out")"
	printf '#!/bin/sh\necho fake-zcp\n' >"$out"
	chmod +x "$out"
fi
