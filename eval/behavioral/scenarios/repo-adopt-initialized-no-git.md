---
id: repo-adopt-initialized-no-git
description: |
  Unmanaged standard pair deployed outside ZCP whose dev service carries
  NO repository at all (preseed removes /var/www/.git — the shape of a
  service someone hand-pushed with `zcli push --no-git`). The working
  tree holds application files, an untracked cargo file and a secret
  `.env`. Tests G6 (docs/spec-workflows.md §8 GLC-7, initialized case):
  adopt brings the repository to the same commit-ready state bootstrap
  leaves a fresh service in — init-if-missing, identity set-if-absent,
  an empty marker HEAD if none was reachable — WITHOUT staging or
  committing anything it found, and records `repo.provenance=initialized`.
  zcp's part stops there; the persona then asks for a `.gitignore` and a
  baseline commit of the found files, which is agent work done over SSH
  against the container's own git state, never `git init`/`git commit`
  run by hand from the ZCP-side mount (root-owned `.git/objects/` —
  `develop-first-deploy-write-app.md`).

  The containerCheck proves the intended end state, not the absence of
  one: `cargo-untracked.txt` is tracked in HEAD — zcp's adopt step
  never stages files it finds (GLC-7), so a tracked cargo file can only
  be the agent's baseline commit; `.env` stays on disk with its preseed
  value but is never tracked and never enters any commit, ever;
  `.gitignore` is tracked and carries a rule for `.env`; no tag was
  written; and HEAD holds at least one commit. Author is not the
  discriminator — the container's ambient git identity is
  `agent@zerops.io` until git-push-setup derives a human one from the
  PAT (GLC-3), so this commit legitimately carries the robot address;
  content (a tracked file zcp never stages) is what proves it is the
  agent's.
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
    - {service: appdev, cmd: "git -C /var/www rev-list --count HEAD", match: "^[1-9][0-9]*\\s*$"}
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD --name-only | grep -qx cargo-untracked.txt && echo cargo-tracked || echo cargo-not-tracked", match: "^cargo-tracked"}
    - {service: appdev, cmd: "grep -q untracked /var/www/cargo-untracked.txt && echo cargo-on-disk", match: "^cargo-on-disk"}
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD --name-only | grep -qx .env && echo env-tracked || echo env-not-tracked", match: "^env-not-tracked"}
    - {service: appdev, cmd: "grep -q preseed-secret-value /var/www/.env && echo env-on-disk", match: "^env-on-disk"}
    - {service: appdev, cmd: "git -C /var/www log --all --format=%H -- .env | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www ls-files --error-unmatch .gitignore >/dev/null 2>&1 && echo gitignore-tracked", match: "^gitignore-tracked"}
    - {service: appdev, cmd: "grep -q '\\.env' /var/www/.gitignore && echo env-ignored", match: "^env-ignored"}
    - {service: appdev, cmd: "git -C /var/www tag -l | wc -l", match: "^\\s*0"}
  meta:
    - {hostname: appdev, field: repo.provenance, expect: initialized}
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  You already have a working standard pair (appdev/appstage, nodejs@22)
  plus a Postgres db. You pushed the dev code by hand with zcli, so
  there is no git repository on the dev service — just files. ZCP
  doesn't know about it yet. You want to connect ZCP to what's already
  running — no new services, no redeploy, no rebuild. Once adopt is
  done, you'd like a `.gitignore` in place and the current files
  committed as a starting point, since nothing tracked them before. Push
  back if the agent proposes re-importing, recreating or re-deploying
  anything, or asks you to set up git by hand first.
notableFriction:
  - id: initialization-is-zcp-work-agent-owns-the-commit
    description: |
      The repository init and identity/HEAD guarantee are zcp's doing
      inside adopt (GLC-7) — but staging and committing the files found
      on disk is not: zcp never commits user files. An agent that skips
      writing `.gitignore` and making the baseline commit the persona
      asked for is leaving that half of the job undone; the containerCheck
      gates it directly (cargo-untracked.txt must land in a tracked
      commit — zcp's adopt step never stages found files, so a tracked
      cargo file can only be the agent's). An agent that instead reaches
      for `git init`/`git commit` on the mounted tree BY HAND from the
      ZCP-side mount (GLC-5 forbids
      mount-side git init — it leaves root-owned `.git/objects/` that
      breaks the container's own git, `develop-first-deploy-write-app.md`)
      is doing the right work the wrong way — the commit must happen
      inside the container, over SSH.
  - id: secret-never-committed
    description: |
      `.env` is on disk and must stay on disk, and must never enter any
      commit — zcp's own adopt step never stages it, and an agent
      writing the baseline commit must exclude it via `.gitignore`
      first. An agent that "helps" by committing everything, or by
      deleting the `.env`, destroys user state.
---

In my Zerops project (UUID {{projectId}}) I already have a standard pair running — `appdev` and `appstage` (nodejs@22), plus `db` (postgresql@18). I pushed the dev code by hand with zcli, so ZCP doesn't know about it yet. Please connect ZCP to what I have — adopt route — so the services get ServiceMeta and I can use the develop workflow normally. Don't create anything new, don't redeploy. Tell me when adopt is done.
