#!/bin/bash
# Preseed for mono-e4-variantb-shared-volume (F2 under variant B): plant the
# fixture repo into webdev's /var/www. The code volume at /var/www/ws is not
# mounted until the dev setups' first deploy, so the agent deploys dev and
# dev-api from here and then fills the volume.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo mono-bun-go-worker webdev /var/www
