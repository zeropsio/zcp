---
id: repo-adopt-snapshot-no-git
description: |
  Unmanaged standard pair deployed outside ZCP whose dev service carries
  NO repository at all (preseed removes /var/www/.git — the shape of a
  service someone hand-pushed with `zcli push --no-git`). The working
  tree holds application files, an untracked cargo file and a secret
  `.env`. Tests G6 (docs/spec-workflows.md §8 GLC-7, snapshot case):
  adopt initializes the repository, seeds the class-scoped exclude,
  stages the files found on disk and mints ONE robot-attributed
  snapshot commit — and records `repo.provenance=snapshot`. The
  containerCheck proves what must and must not be in that snapshot:
  the cargo file is tracked, `.env` is NOT (excluded, still on disk),
  the tree is clean afterwards, the commit is the robot's, and no tag
  was written. Neither the snapshot nor its appVersion record claims
  the files built the running appVersion.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: preseed/adopt-snapshot-no-git.sh
tags: [repo-always, adopt-route, standard-mode, git-foundation, repo-preservation, node]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8 GLC-7
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
  unchanged: [appdev, appstage]
  containerCheck:
    - {service: appdev, cmd: "git -C /var/www rev-parse --verify HEAD >/dev/null && echo head-ok", match: "^head-ok"}
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD --name-only | grep -x cargo-untracked.txt", match: "^cargo-untracked\\.txt"}
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD --name-only | grep -qx .env && echo env-tracked || echo env-not-tracked", match: "^env-not-tracked"}
    - {service: appdev, cmd: "grep -q preseed-secret-value /var/www/.env && echo env-on-disk", match: "^env-on-disk"}
    - {service: appdev, cmd: "git -C /var/www status --porcelain | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www log -1 --format=%ae", match: "^agent@zerops\\.io"}
    - {service: appdev, cmd: "git -C /var/www rev-list --count HEAD", match: "^[12]\\s*$"}
    - {service: appdev, cmd: "git -C /var/www tag -l | wc -l", match: "^\\s*0"}
  meta:
    - {hostname: appdev, field: repo.provenance, expect: snapshot}
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  You already have a working standard pair (appdev/appstage, nodejs@22)
  plus a Postgres db. You pushed the dev code by hand with zcli, so
  there is no git repository on the dev service — just files. ZCP
  doesn't know about it yet. You want to connect ZCP to what's already
  running — no new services, no redeploy, no rebuild. Push back if the
  agent proposes re-importing, recreating or re-deploying anything, or
  asks you to set up git by hand first.
notableFriction:
  - id: snapshot-is-zcp-work-not-agent-work
    description: |
      The repository init and the snapshot commit are zcp's doing inside
      adopt (GLC-7). An agent that `git init`s or commits on the mounted
      tree by hand (GLC-5 forbids mount-side git init — root-owned
      objects) or asks the user to do it first is off the intended path.
  - id: secret-never-committed
    description: |
      `.env` is on disk and must stay on disk, but must never enter the
      snapshot commit — the exclude seed runs BEFORE staging. An agent
      that "helps" by committing everything, or by deleting the `.env`,
      destroys user state.
---

In my Zerops project (UUID {{projectId}}) I already have a standard pair running — `appdev` and `appstage` (nodejs@22), plus `db` (postgresql@18). I pushed the dev code by hand with zcli, so ZCP doesn't know about it yet. Please connect ZCP to what I have — adopt route — so the services get ServiceMeta and I can use the develop workflow normally. Don't create anything new, don't redeploy. Tell me when adopt is done.
