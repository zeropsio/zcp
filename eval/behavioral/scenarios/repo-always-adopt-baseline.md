---
id: repo-always-adopt-baseline
description: |
  Unmanaged standard pair deployed outside ZCP with no ServiceMeta.
  Tests G2 (docs/spec-workflows.md §8 GLC-7): adopt preserves existing
  content history or snapshots files if there is no content HEAD, and
  persists ServiceMeta.Repo.BaselineAppVersion and provenance without tags.
  This buildFromGit fixture leaves cloned history in /var/www (observed
  live 2026-09-14), so the existing case preserves HEAD without a commit.
  A missing repo or empty marker HEAD would take the snapshot case.
  Neither case proves the source of the running appVersion.
  The preseed (plant-repo-cargo.sh) loads appdev's repository with user
  cargo — a feature branch, a user commit, a release tag, a custom ref,
  a dirty tracked file, an untracked file, a hand-added exclude line, a
  user identity and a foreign origin — and every containerCheck proves
  adopt left each item exactly as found: no commit, no checkout, no
  re-pointed origin, no identity rewrite, no tag.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: preseed/plant-repo-cargo.sh
tags: [repo-always, adopt-route, standard-mode, git-foundation, node]
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
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD | wc -l", match: "[1-9]"}
    - {service: appdev, cmd: "git -C /var/www merge-base --is-ancestor refs/preseed/head HEAD && echo history-kept", match: "^history-kept"}
    - {service: appdev, cmd: "git -C /var/www symbolic-ref --short HEAD", match: "^feature/preseed-cargo"}
    - {service: appdev, cmd: "git -C /var/www tag -l v0.1-user-release", match: "^v0.1-user-release"}
    - {service: appdev, cmd: "git -C /var/www rev-parse -q --verify refs/t3/checkpoints/preseed >/dev/null && echo ref-kept", match: "^ref-kept"}
    - {service: appdev, cmd: "grep -q 'v2 uncommitted' /var/www/cargo-tracked.txt && test -f /var/www/cargo-untracked.txt && echo dirty-kept", match: "^dirty-kept"}
    - {service: appdev, cmd: "grep -qxF user-private-dir/ /var/www/.git/info/exclude && echo exclude-kept", match: "^exclude-kept"}
    - {service: appdev, cmd: "git -C /var/www config user.email", match: "^preseed@example\\.com"}
    - {service: appdev, cmd: "git -C /var/www remote get-url origin", match: "^https://example\\.invalid/preseed/repo\\.git"}
    - {service: appdev, cmd: "git -C /var/www tag -l 'zcp/*' | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www rev-parse HEAD refs/preseed/head | uniq | wc -l", match: "^\\s*1"}
    - {service: appdev, cmd: "git -C /var/www status --porcelain -- cargo-tracked.txt cargo-untracked.txt", match: "(?s)^ M cargo-tracked\\.txt.*\\?\\? cargo-untracked\\.txt"}
    - {service: appdev, cmd: "test -f /var/www/user-private-dir/keep && echo ignored-kept", match: "^ignored-kept"}
  meta:
    - {hostname: appdev, field: repo.provenance, expect: existing}
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  You already have a working standard pair (appdev/appstage, nodejs@22)
  plus a Postgres db, all built by hand through the dashboard. ZCP
  doesn't know about it yet. You want to connect ZCP to what's already
  running — no new services, no redeploy, no rebuild. Push back if the
  agent proposes re-importing or recreating anything.
notableFriction:
  - id: baseline-tag-not-a-mutation
    description: |
      Adoption records the baseline appVersion and provenance in ServiceMeta.
      It neither tags nor deploys that baseline. Existing content history
      stays intact; a snapshot is only what was found on disk, not proof
      of the running artifact's source.
  - id: init-only-when-missing
    description: |
      With a content HEAD from buildFromGit, adoption must preserve HEAD
      without init or a new snapshot commit.

---

In my Zerops project (UUID {{projectId}}) I already have a standard pair running — `appdev` and `appstage` (nodejs@22), plus `db` (postgresql@18). It was all set up by hand through the dashboard, so ZCP doesn't know about it yet. Please connect ZCP to what I have — adopt route — so the services get ServiceMeta and I can use the develop workflow normally. Don't create anything new, don't redeploy. Tell me when adopt is done.
