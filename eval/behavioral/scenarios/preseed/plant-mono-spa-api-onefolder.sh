#!/bin/bash
# Preseed for mono-e1-f6-spa-api-onefolder: plant the fixture repo into appdev's /var/www.
# No part is declared in its zerops.yml and no dev-server declaration exists:
# the shape comes from the repo's folders alone.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo mono-spa-api-onefolder appdev /var/www
