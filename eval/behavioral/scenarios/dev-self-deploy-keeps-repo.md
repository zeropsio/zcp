---
id: dev-self-deploy-keeps-repo
description: |
  Existing dev/stage Node pair, both buildFromGit-deployed (real cloned
  repo mounted on appdev). The preseed already ran ONE deploy-from-commit
  push to appstage, so appdev's repo carries a real refs/zcp/env/appstage
  + refs/zcp/deploy/* ledger entry before the agent is spawned. Tests G4
  (docs/spec-workflows.md's Git Lifecycle section, GLC-1/GLC-2): a plain
  dev self-deploy (`zerops_deploy targetService=appdev`, no sha) runs
  `buildSSHCommand`'s safety-net on the SAME repo — init-if-missing
  (no-op here), identity set-if-absent (no-op), `.git/info/exclude`
  re-seeded (idempotent), HEAD guarantee (no-op, HEAD already reachable)
  — and none of that self-heal composition ever touches the
  `refs/zcp/*` namespace the preseed's ledger lives in, nor re-writes
  `.git/info/exclude` from scratch (it appends missing lines only).

  Status `promote: containerCheck` (docs/spec-scenarios.md §9.3 table G):
  the runner evaluates containerCheck today, but this file does not
  carry one yet — a follow-up adds a direct
  `git -C /var/www for-each-ref refs/zcp` (non-empty, ledger survives)
  and a re-check that `.git/info/exclude` still carries the runtime
  class's patterns after the deploy, and flips the row to `gate`.
  Today's oracle coverage (liveness/toolArg) proves the self-deploy
  shipped the new response text to appdev only; it does not yet reach
  into the container to prove the ledger/exclude survived untouched.
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
        [ "$(git for-each-ref refs/zcp/deploy/* | wc -l)" -eq 1 ]
preseedScript: preseed/deploy-from-commit-once.sh
tags: [repo-always, git-foundation, self-deploy, ledger, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8 GLC-1/GLC-2
  liveness: {service: appdev, marker: "self-deploy-keeps-ledger"}
  toolArg:
    - {always: "zerops_deploy{targetService=appdev}"}
    - {max: 0, call: "zerops_import"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  Your `appdev` and `appstage` are both healthy Node services, cloned
  from the same GitHub repo. You just want a small text change on the
  dev service itself, shipped to dev only — nothing to do with stage.
  Push back if the agent proposes touching appstage, resolving/naming a
  commit sha, or treating this as anything other than an ordinary dev
  self-deploy.
notableFriction:
  - id: self-deploy-not-sha-deploy
    description: |
      This is a plain `zerops_deploy targetService=appdev` — the agent
      must not reach for `sha=` (that's G3/G5's move) or try to name a
      commit; the task is "edit and ship dev", not "ship a specific
      commit".
  - id: safety-net-must-not-touch-ledger
    description: |
      buildSSHCommand's GLC-2 safety-net (init-if-missing, identity
      set-if-absent, exclude re-seed, HEAD guarantee) runs on every
      self-deploy on THIS SAME repo that already carries the preseed's
      refs/zcp/* ledger. None of those four guards write, move, or
      delete any ref outside HEAD itself — a regression here would
      silently corrupt or orphan the ledger the next `sha=` deploy or
      rollback depends on.
  - id: exclude-reseed-is-additive
    description: |
      The exclude-seed fragment re-runs on every deploy call, appending
      only lines not already present in `.git/info/exclude` — it must
      never rewrite the file from scratch (which would be harmless here
      since the patterns are the same, but is the exact mechanism that
      would silently drop a hand-added line on a real user's container).
---

I want to change the text my Node app responds with on `appdev` — just a
small wording change — and ship that to dev. I don't need it on
`appstage`, that's a separate step for later. Make the root response
include the text `self-deploy-keeps-ledger` and deploy it.
