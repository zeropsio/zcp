---
id: rollback-stage-from-ledger
description: |
  Existing dev/stage Node pair; appstage has already received TWO
  deploy-from-commit pushes (preseed: deploy-from-commit-twice.sh) —
  refs/zcp/env/appstage points at the newer commit, refs/zcp/deploy/*
  holds both entries. User wants stage rolled back to the version before
  the current one. Tests G5 (docs/spec-workflows.md §4.9): the ledger is
  the rollback's source of truth, and a rollback is just another
  deploy-from-commit call naming the earlier sha — no separate rollback
  primitive exists.

  Status `promote: containerCheck` (docs/spec-scenarios.md §9.3 table G):
  the runner evaluates containerCheck today, but this file does not carry
  one yet on the post-rollback verification — a follow-up adds a direct
  `refs/zcp/env/appstage == oldest ledger entry` check and flips the row
  to `gate`. The seed-side `probe` (proving the preseed left two ledger
  entries) already uses a containerCheck-shaped SSH command, but that is
  a different family (`seedExpect`), not this row's `promote:` target.
seed:
  mode: deployed
  fixture: fixtures/nodejs-standard-deployed.yaml
  expect:
    services:
      - {hostname: appdev, status: [ACTIVE]}
      - {hostname: appstage, status: [ACTIVE]}
    probe:
      service: appdev
      cmd: >-
        git rev-parse --verify refs/zcp/env/appstage >/dev/null 2>&1 &&
        [ "$(git for-each-ref refs/zcp/deploy/* | wc -l)" -eq 2 ]
preseedScript: preseed/deploy-from-commit-twice.sh
tags: [rollback, deploy-from-commit, ledger, git-foundation, cross-deploy, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.9
  liveness: {service: appstage, marker: "nodejs"}
  toolArg:
    - {always: "zerops_deploy{targetService=appstage}"}
    - {max: 0, call: "zerops_import"}
  mustOffer: ["(?i)(no|without a) rebuild", "(?i)refs/zcp/deploy|ledger|previous (version|commit|deploy)"]
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  Your `appstage` just got a bad-looking change — you want it rolled back
  to whatever was running before the current deploy, right now, without
  waiting on a fresh build. Push back if the agent proposes rebuilding
  from source, asks you to identify a commit by hand instead of reading
  the history itself, or claims there's no way to know what ran before.
notableFriction:
  - id: ledger-read-before-act
    description: |
      Agent must read the ledger (`git log refs/zcp/deploy/*` on appdev,
      or equivalent) to find the PRIOR sha before issuing the rollback —
      guessing or asking the user to supply the commit is a miss.
  - id: rollback-is-deploy-not-a-new-verb
    description: |
      There is no separate rollback tool/action in this slice: the
      correct move is `zerops_deploy sha=<prior sha>` again. An agent
      that goes looking for a dedicated rollback action, or that tries to
      revert via a fresh git commit + normal deploy, is off the intended
      path.
---

Something's wrong with the latest thing running on `appstage` — I want it
rolled back to whatever was running right before this deploy, immediately,
no rebuild. Can you find what that was and put it back?
