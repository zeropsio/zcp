---
id: rollback-stage-from-ledger
description: |
  Existing dev/stage Node pair; appstage has already received TWO
  deploy-from-commit pushes (preseed: deploy-from-commit-twice.sh) — each
  push produced its own appVersion, so appstage now carries an ACTIVE
  appVersion (the newer push) and a BACKUP one (the older push;
  live-verified 2026-09-14: the previously-active version flips to
  BACKUP once a newer one activates). User wants stage rolled back to
  the version before the current one, right now, no rebuild. Tests G5
  (docs/spec-workflows.md §8 R2 generalised / §12.6 GF-8): the correct
  move is `zerops_deploy targetService=appstage appVersion=<the older
  appVersion id>` — rollback re-activates that recorded BACKUP appVersion
  in place (`stack.deploy.backup`, no build). The agent learns the id
  from the status envelope's deploy attempts or `zerops_events` — never
  from a Git tag or by guessing. The scenario ID is retained for matrix stability.

  Status `promote: <platform-side appVersion check>` (docs/spec-
  scenarios.md §9.3 table G): the runner has no oracle family today for
  "the active appVersion after == a specific id learned during preseed"
  or "no stack.build process ran on appstage after the agent started" —
  `expectedServices` checks status shape only and `containerCheck` runs
  an SSH command inside a service, neither reads a specific appVersion
  id. A follow-up either adds such a family or accepts these two as
  live-verification-only; `toolArg` + `mustOffer` below already cover
  what the current families can express. The seed-side `probe` (proving
  the preseed saved two non-empty distinct appVersion IDs) is unaffected — it is a different
  family (`seedExpect`), asserting the preseed ran, not the rollback's
  outcome.
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
        [ "$(sort -u /tmp/zcp-preseed-appversions | wc -l)" -eq 2 ]
preseedScript: preseed/deploy-from-commit-twice.sh
tags: [rollback, deploy-from-commit, deploy-evidence, git-foundation, cross-deploy, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.9
  liveness: {service: appstage, marker: "nodejs"}
  toolArg:
    - {always: "zerops_deploy{targetService=appstage,appVersion~.+}"}
    - {never: "zerops_deploy{targetService=appstage,appVersion=latest}"}
    - {max: 0, call: "zerops_import"}
    - {max: 0, call: "zerops_deploy{sha~.+}"}
  mustOffer: ["(?i)(no|without a) rebuild", "(?i)previous (version|commit|deploy)|appVersion"]
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
      Agent must learn the PRIOR appVersion id before issuing the
      rollback — from the status envelope's deploy attempts, from
      `zerops_events` — never by guessing or
      asking the user to supply a commit or id by hand.
  - id: rollback-is-appversion-not-a-new-verb
    description: |
      There is no separate rollback tool/action: the correct move is
      `zerops_deploy targetService=appstage appVersion=<id>` — the SAME
      tool as a normal deploy, distinguished only by an appVersion
      parameter naming a specific recorded id (not "latest"). An agent
      that goes looking for a dedicated rollback action, that reverts via
      a fresh git commit + a normal (re-build) deploy, or that calls
      `zerops_deploy sha=<prior sha>` (a NEW build — the fallback only,
      once no BACKUP appVersion remains) is off the intended path.
---

Something's wrong with the latest thing running on `appstage` — I want it
rolled back to whatever was running right before this deploy, immediately,
no rebuild. Can you find what that was and put it back?
