---
id: repo-always-adopt-baseline
description: |
  Unmanaged standard pair deployed outside ZCP (no ServiceMeta, no git
  history — same pre-state as `adopt-existing-standard-pair`). Tests G2
  (docs/spec-workflows.md §8 GLC-7): adopt tags the running appVersion as a
  baseline (`zcp/baseline/<appVersionId>`) on every adopted dev service —
  `git init` first only if `/var/www` isn't already a repo, otherwise
  only the tag moves — and persists `ServiceMeta.Repo.BaselineAppVersion`.

  Status `promote: containerCheck` (docs/spec-scenarios.md §9.3 table G):
  the runner evaluates containerCheck today, but this file does not
  carry one yet — a follow-up adds a direct
  `git tag -l 'zcp/baseline/*'` check (asserting it names the service's
  running appVersion id) and flips the row to `gate`. Today's oracle
  coverage (expectedServices/unchanged/never) proves adopt completed
  without touching infra; it does not yet reach into the container to
  prove the tag itself.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
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
      Tagging /var/www with zcp/baseline/<appVersionId> must not read as
      a code change to the user — it's a local git-history bookmark, not
      a deploy or a push. Surfaces whether the agent narrates this
      correctly (no "I deployed a marker commit" framing) when a repo
      already existed and only the tag moved.
  - id: init-only-when-missing
    description: |
      If /var/www already has a `.git/` (e.g. the service was cloned via
      buildFromGit), AdoptBaseline must only move the tag — no init, no
      new baseline commit, HEAD unchanged. Surfaces whether the agent (or
      a misbehaving tool call) accidentally re-inits an existing repo.
---

In my Zerops project (UUID {{projectId}}) I already have a standard pair running — `appdev` and `appstage` (nodejs@22), plus `db` (postgresql@18). It was all set up by hand through the dashboard, so ZCP doesn't know about it yet. Please connect ZCP to what I have — adopt route — so the services get ServiceMeta and I can use the develop workflow normally. Don't create anything new, don't redeploy. Tell me when adopt is done.
