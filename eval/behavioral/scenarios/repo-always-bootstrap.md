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

  The containerCheck reaches into appdev and proves GF-2 actually
  happened, not merely that zcp didn't break it: a reachable HEAD, an
  identity set, no zcp tag, HEAD's tree non-empty (zcp's HEAD-guarantee
  marker is always an empty-tree commit and zcp never stages files, so
  a non-empty tree can only be the agent's baseline commit),
  `.gitignore` tracked in HEAD, `node_modules` never tracked, and
  `.env` never landing in HEAD. Author is not a useful discriminator
  here — the container's ambient git identity is `agent@zerops.io`
  until git-push-setup derives a human one from the PAT (GLC-3), so
  this commit legitimately carries the robot address; content, not
  authorship, is what proves it is the agent's.
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
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD | wc -l", match: "[1-9]"}
    - {service: appdev, cmd: "git -C /var/www ls-files --error-unmatch .gitignore >/dev/null 2>&1 && echo gitignore-tracked", match: "^gitignore-tracked"}
    - {service: appdev, cmd: "git -C /var/www ls-files node_modules | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www ls-tree -r HEAD --name-only | grep -qx .env && echo env-tracked || echo env-not-tracked", match: "^env-not-tracked"}
    - {service: appdev, cmd: "git -C /var/www tag -l 'zcp/*' | wc -l", match: "^\\s*0"}
    - {service: appdev, cmd: "git -C /var/www config user.email", match: ".+"}
  never: ["zerops_import{override=true}", "zerops_delete"]
notableFriction:
  - id: baseline-commit-is-agent-work
    description: |
      Writing a `.gitignore` for the scaffold and committing it — and
      excluding `node_modules` and any `.env` from ever reaching HEAD —
      is squarely the agent's job now (GF-2, §12.6): zcp seeds no ignore
      rules and commits none of the agent's own files. The containerCheck
      gates this directly: HEAD's tree must be non-empty — zcp's marker
      commit is always the empty tree and zcp never stages files, so a
      commit with files in it can only be the agent's — `.gitignore`
      must be tracked, and `node_modules`/`.env` must never land in HEAD.
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
