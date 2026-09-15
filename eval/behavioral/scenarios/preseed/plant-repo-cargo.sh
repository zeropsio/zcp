#!/bin/bash
# Preseed for repo-always-adopt-baseline (G2) and deploy-from-commit-stage (G3):
# the buildFromGit fixture leaves appdev with cloned history; plant user cargo
# on top so adopt (existing case) and a cross-deploy are proven to leave it
# untouched. Constants live in lib-repo-cargo.sh.
set -eu
# shellcheck source=lib-repo-cargo.sh
. "$(dirname "$0")/lib-repo-cargo.sh"
ssh appdev 'cd /var/www && git rev-parse --verify HEAD >/dev/null'
plant_repo_cargo appdev
