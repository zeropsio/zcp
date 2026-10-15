#!/bin/sh
# build-candidate.sh OUT [PACKAGE] — builds a linux/amd64 farm binary
# (candidate or evaluator) from the current git checkout, stamped with that
# checkout's own HEAD.
#
# Go reads its VCS stamp only from a `.git` directory, so a build inside a
# linked worktree (whose `.git` is a file) stamps whatever repository
# encloses the worktree, or nothing. This script builds from a throwaway
# clone at the checkout's HEAD instead, carrying over its uncommitted
# changes, untracked files and the synced (gitignored) knowledge corpus, so
# `farm push --candidate` reports the revision that was really built and
# "modified" exactly when the checkout was.
set -eu

out=${1:?usage: build-candidate.sh OUT [PACKAGE]}
pkg=${2:-./cmd/zcp}
case $out in
/*) ;;
*) out=$(pwd)/$out ;;
esac

src=$(git rev-parse --show-toplevel)
head=$(git -C "$src" rev-parse HEAD)
common=$(cd "$src" && cd "$(git rev-parse --git-common-dir)" && pwd)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
git clone --quiet --shared --no-checkout "$common" "$tmp/src"
git -C "$tmp/src" checkout --quiet --detach "$head"

git -C "$src" diff --binary HEAD >"$tmp/changes.patch"
if [ -s "$tmp/changes.patch" ]; then
	git -C "$tmp/src" apply --whitespace=nowarn "$tmp/changes.patch"
fi
# Untracked files, then the synced corpus Go embeds at build time.
for listing in "--others --exclude-standard" "--others --ignored --exclude-standard -- internal/knowledge"; do
	# shellcheck disable=SC2086 # listing is a fixed set of git flags
	git -C "$src" ls-files -z $listing >"$tmp/files"
	if [ -s "$tmp/files" ]; then
		tar -C "$src" --null -T "$tmp/files" -cf - | tar -C "$tmp/src" -xf -
	fi
done

(cd "$tmp/src" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$out" "$pkg")
echo "built $out from $head" >&2
