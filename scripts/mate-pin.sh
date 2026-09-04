#!/usr/bin/env bash
# mate-pin.sh reproduces internal/mate/testdata/serve-help.golden.txt from the
# actual pinned mate release, so the golden a reviewer reads is never a
# transcription: downloads zerops-mate-<version>.tgz from the fork's GitHub
# releases, prints its SHA-256 (the value that belongs in mate.PinnedSHA256 —
# this script never writes Go source), npm-installs it into a scratch prefix,
# runs `mate serve --help` from the installed bin, and writes the golden.
#
# Usage:
#   scripts/mate-pin.sh <version>       # e.g. scripts/mate-pin.sh 0.2.5
#
# Part of the release ritual (docs/spec-mate.md §2.1c): after moving
# PinnedVersion/PinnedSHA256, run this and commit the (possibly unchanged)
# golden in the same commit as the pin.
#
# COLUMNS controls the width `--help` wraps to — the golden test parses long
# flags out of the text, not bytes, so a narrow terminal that wraps a flag's
# description onto its own line still produces a parseable golden; a wide one
# (COLUMNS=200) is what verification reproduces byte-for-byte.
set -euo pipefail

if [ $# -ne 1 ]; then
  echo "usage: $0 <version>" >&2
  exit 1
fi
VERSION="$1"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GOLDEN_PATH="$REPO_ROOT/internal/mate/testdata/serve-help.golden.txt"

ASSET="zerops-mate-${VERSION}.tgz"
RELEASE_URL="https://github.com/zeropsio/mate/releases/download/v${VERSION}/${ASSET}"

WORKDIR="$(mktemp -d)"
trap 'rm -rf -- "$WORKDIR"' EXIT

TARBALL="$WORKDIR/$ASSET"
echo "==> Downloading $RELEASE_URL" >&2
curl -fsSL -o "$TARBALL" "$RELEASE_URL"

SHA256="$(shasum -a 256 "$TARBALL" | awk '{print $1}')"
echo "SHA-256: $SHA256"

PREFIX="$WORKDIR/install"
echo "==> npm install --prefix $PREFIX $ASSET" >&2
npm install --prefix "$PREFIX" --no-audit --no-fund --loglevel=error "$TARBALL" >&2

BIN="$PREFIX/node_modules/.bin/mate"
if [ ! -x "$BIN" ]; then
  echo "$0: $BIN not found after install" >&2
  exit 1
fi

mkdir -p "$(dirname "$GOLDEN_PATH")"
echo "==> $BIN serve --help > $GOLDEN_PATH" >&2
"$BIN" serve --help >"$GOLDEN_PATH"

echo "==> wrote $GOLDEN_PATH" >&2
