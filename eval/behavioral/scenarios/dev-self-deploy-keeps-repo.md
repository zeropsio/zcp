---
id: dev-self-deploy-keeps-repo
description: |
  Existing buildFromGit dev/stage pair with one previous commit deploy to
  stage. Tests G4: ordinary dev self-deploy ships the working tree with
  its repository. The init/identity/exclude/HEAD safety-net preserves
  existing history and appends missing exclude patterns only.
  The preseed also plants user cargo on appdev (lib-repo-cargo.sh): a
  feature branch, a user commit, a release tag, a custom ref, a dirty
  tracked file, an untracked file, a hand-added exclude line, a user
  identity and a foreign origin. The containerCheck runs INSIDE the
  replacement container and proves each item travelled with the
  artifact (`zcli push -g`, workspace-state all — P10): history reachable,
  branch, tag, ref, uncommitted content, exclude line, identity, origin
  all intact, and no zcp-authored tag. Ignored files (the exclude's own
  `user-private-dir/`) are platform-dropped on a container replacement
  and are deliberately NOT asserted here.
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
        git rev-parse --verify HEAD >/dev/null &&
        [ "$(wc -l < /tmp/zcp-preseed-appversions)" -eq 1 ]
preseedScript: preseed/deploy-from-commit-once.sh
tags: [repo-always, git-foundation, self-deploy, repo-preservation, node]
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
  containerCheck:
    - {service: appdev, cmd: "git -C /var/www merge-base --is-ancestor refs/preseed/head HEAD && echo history-kept", match: "^history-kept"}
    - {service: appdev, cmd: "git -C /var/www symbolic-ref --short HEAD", match: "^feature/preseed-cargo"}
    - {service: appdev, cmd: "git -C /var/www tag -l v0.1-user-release", match: "^v0.1-user-release"}
    - {service: appdev, cmd: "git -C /var/www rev-parse -q --verify refs/t3/checkpoints/preseed >/dev/null && echo ref-kept", match: "^ref-kept"}
    - {service: appdev, cmd: "grep -q 'v2 uncommitted' /var/www/cargo-tracked.txt && test -f /var/www/cargo-untracked.txt && echo dirty-kept", match: "^dirty-kept"}
    - {service: appdev, cmd: "grep -qxF user-private-dir/ /var/www/.git/info/exclude && echo exclude-kept", match: "^exclude-kept"}
    - {service: appdev, cmd: "git -C /var/www config user.email", match: "^preseed@example\\.com"}
    - {service: appdev, cmd: "git -C /var/www remote get-url origin", match: "^https://example\\.invalid/preseed/repo\\.git"}
    - {service: appdev, cmd: "git -C /var/www tag -l 'zcp/*' | wc -l", match: "^\\s*0"}
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
      buildSSHCommand's init/identity/exclude/HEAD safety-net runs on the
      same repository after the stage deploy. Existing history and any
      user-owned release tags must survive the dev container replacement.
      Deployment and rollback do not depend on any automatic Git tag.
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
