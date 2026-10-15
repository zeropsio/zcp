#!/bin/bash
# Preseed for mono-e1-s8-dev-loop (S8 stand-in): plant the z3-shaped
# workspace into appdev's /var/www with no dev-server declaration.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo mono-z3-shape appdev /var/www
