#!/bin/bash
# Preseed for devflow-own-runner-{a,b}: plant the existing repository with its
# own dev runner into appdev's /var/www, with no dev setup and no dev-server
# declaration.
set -eu
# shellcheck source=lib-fixture-repo.sh
. "$(dirname "$0")/lib-fixture-repo.sh"
plant_fixture_repo devflow-own-runner appdev /var/www
