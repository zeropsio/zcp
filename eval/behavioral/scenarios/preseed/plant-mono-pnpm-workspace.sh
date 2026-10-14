#!/bin/bash
# Preseed for mono-e1-f3-parts (F3): plant the fixture repo into appdev's
# /var/www without its dev-server declaration, so the parts come from the
# repo's zerops.yml setups alone.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo mono-pnpm-workspace appdev /var/www
ssh appdev "cd /var/www && git rm -q -r .zcp && git commit -q -m 'fixture: no dev-server declaration'"
