---
id: repo-always-bootstrap
description: |
  Brand-new classic Node pair. Tests G1 (docs/spec-workflows.md §4.10):
  every dev service is a git repository with a scaffold commit once
  bootstrap finishes — `git init -b main`, a runtime-class-scoped
  `.git/info/exclude` (never a tracked `.gitignore`), and `git add -A &&
  git commit -m "scaffold"` for whatever the agent wrote.

  Status `promote: containerCheck` (docs/spec-scenarios.md §9.3 table G):
  the runner evaluates containerCheck today, but this file does not
  carry one yet — a follow-up adds a direct
  `git -C /var/www rev-parse HEAD` / `git log --oneline` / `.git/info/
  exclude` non-empty / no-`.gitignore` check and flips the row to `gate`.
  Today's oracle coverage (expectedServices/never) proves bootstrap
  completed cleanly; it does not yet reach into the container to prove
  the repo itself.
seed: empty
tags: [repo-always, bootstrap, classic-route, git-foundation, node]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.10
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
  noFailedProcesses: true
  liveness: {service: appstage, marker: "small-api-ready"}
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: no-gitignore-authored
    description: |
      Agent should never author a tracked `.gitignore` for the scaffold
      — zcp's own exclude seeding lives in `.git/info/exclude`, never a
      committed file. Surfaces whether an agent reflexively writes one
      out of habit, which would sit alongside (not conflict with) zcp's
      exclude but is still off-contract for this scenario's checks.
  - id: scaffold-commit-is-real-content
    description: |
      The scaffold commit must carry the agent's actual written files —
      not the pre-existing empty "zcp init" placeholder commit from
      post-mount InitServiceGit. Surfaces whether the provision
      StepChecker's self-heal (`ops.EnsureScaffoldRepo`) fires after the
      scaffold lands rather than only at mount time.
---

I want to deploy a small Node.js API. Name the dev service `appdev` and the stage service `appstage`. I need both a development environment and a staging slot for testing builds. Make sure the page served at `/` on `appstage` contains the text "small-api-ready".
