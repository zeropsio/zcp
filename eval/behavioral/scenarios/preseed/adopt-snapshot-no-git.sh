#!/bin/bash
# Preseed for repo-adopt-initialized-no-git (G6): a service deployed outside
# zcp whose working tree carries NO repository — the shape of a hand-pushed
# `zcli push --no-git` service. Files on disk (including a secret .env that
# must never be committed) are what adopt's initialized case has to leave
# untouched, uncommitted, on disk (docs/spec-workflows.md §8 GLC-7).
set -eu
ssh appdev 'cd /var/www && rm -rf .git && \
  printf "untracked cargo\n" > cargo-untracked.txt && \
  printf "SECRET=preseed-secret-value\n" > .env && \
  test ! -d .git && echo "preseed: appdev has no repository, cargo + .env planted"'
