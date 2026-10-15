#!/bin/bash
# Preseed for mono-e1-f1-single (F1): plant the fixture repo into
# appdev's /var/www, then leave one uncommitted edit and one untracked file
# that every deploy of the dev setup must carry.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo mono-single-bun appdev /var/www
ssh appdev "cd /var/www && echo '// wip: preseed uncommitted' >> index.ts && echo wip > wip-untracked.txt"
