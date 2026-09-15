---
id: repo-always-bootstrap
description: |
  Brand-new classic Node pair. Tests G1 (docs/spec-workflows.md's Git
  Lifecycle section, GLC-1): every dev service is a git repository with a
  reachable HEAD once bootstrap finishes — `git init -b main`, identity
  set-if-absent, and the HEAD guarantee's empty `zcp init` marker commit
  if HEAD was otherwise unborn. zcp seeds no `.git/info/exclude` and
  authors no `.gitignore`, and mints no user-visible commit of the
  agent's own written files — the working tree stays whatever it is
  until something actually deploys or commits it (GLC-2's "dev container
  stays dirty across iterations" behavior). Writing `.gitignore` and
  making the baseline commit of the scaffold are the agent's job now
  (GF-2, §12.6).

  The containerCheck reaches into appdev: a reachable HEAD, an identity
  set, no zcp tag, and — if a `.gitignore` exists — that it was never
  committed by zcp's robot identity.
seed: empty
tags: [repo-always, bootstrap, classic-route, git-foundation, node]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8 GLC-1
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
  noFailedProcesses: true
  liveness: {service: appstage, marker: "small-api-ready"}
  containerCheck:
    - {service: appdev, cmd: "git -C /var/www rev-parse --verify HEAD >/dev/null && echo head-ok", match: "^head-ok"}
    - {service: appdev, cmd: "test ! -e /var/www/.gitignore || git -C /var/www log --format=%ae -- .gitignore | grep -qv agent@zerops.io && echo agent-owned", match: "^agent-owned"}
    - {service: appdev, cmd: "git -C /var/www tag -l 'zcp/*' | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www config user.email", match: ".+"}
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: no-gitignore-authored
    description: |
      Agent SHOULD author a `.gitignore` for the scaffold — zcp seeds no
      ignore rules of its own anymore, so an unignored `node_modules/` or
      `.env` reaching a future commit is now squarely the agent's gap to
      close. Surfaces whether the agent takes on the ignore-rules job it
      now owns, or assumes zcp still has it covered.
  - id: dirty-tree-is-not-a-bug
    description: |
      zcp never commits the agent's written files on the agent's behalf —
      after bootstrap the working tree can legitimately stay dirty
      (uncommitted) across iterations; only the empty "zcp init" HEAD
      marker (if any) is zcp's doing. Surfaces whether the agent mistakes
      this for a bug and tries to "fix" it by hand-authoring a scaffold
      commit itself, or correctly recognizes the baseline commit as its
      own responsibility.
---

I want to deploy a small Node.js API. Name the dev service `appdev` and the stage service `appstage`. I need both a development environment and a staging slot for testing builds. Make sure the page served at `/` on `appstage` contains the text "small-api-ready".
