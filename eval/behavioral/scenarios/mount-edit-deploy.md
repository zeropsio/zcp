---
id: mount-edit-deploy
description: |
  Absorbs `existing-standard-appdev-only-reminders` (adopt an existing
  standard pair, scope work to appdev only, leave appstage alone) and
  `develop-edit-path-vs-deploy-source` (edit at the SSHFS mount, deploy
  from the mount, not a separate working-directory copy). Adds a
  mid-run usersim interruption: after the deploy is reported done, the
  user asks to "start develop" again, and the agent must recognize the
  session is already active rather than start a second one.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, develop, standard-pair, scoped-dev-only, node, mount, usersim]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4
  expectedServices:
    - {hostname: appdev, status: [ACTIVE]}
    - {hostname: appstage, status: [ACTIVE]}
    - {hostname: db, status: [ACTIVE]}
  liveness: {service: appdev, marker: "team-notes ok"}
  unchanged: [appstage]
  noFailedProcesses: true
  toolArg:
    - {always: "zerops_deploy{workingDir∈/var/www/appdev}"}
    - {never: "Bash{command~ln -s}"}
    - {never: "zerops_deploy{targetService=appstage}"}
  mustOffer: ["already active"]
  never: ["zerops_import{override=true}", "zerops_delete"]
userSim:
  maxTurns: 3
notableFriction:
  - id: mount-path-is-the-deploy-source
    description: |
      Edits belong at the SSHFS mount `/var/www/appdev/`; `zerops_deploy`
      resolves its self-deploy source from that same mounted path when
      `workingDir` names it — no working-directory copy, no symlink.
  - id: dev-only-scope
    description: |
      The user names appdev and explicitly excludes appstage. Deploy
      targets appdev only; appstage's active app-version must not move.
  - id: session-already-active
    description: |
      After the deploy is reported done, the usersim says "start develop"
      a second time in the same conversation. The agent should explain
      that a develop session is already active rather than attempt to
      start a second one (internal/tools/workflow.go's detectActiveWorkflow
      guard: "A %q workflow session is already active — cannot start a %q
      workflow inside it.").
---

## Starting state

An existing Node.js `appdev`/`appstage` pair backed by a managed `db`,
created outside this session. Both runtime services already have live
buildFromGit deployments; nothing is broken.

## Task

Work directly in the `appdev` mount and add a `/team-notes` endpoint to
the API that returns the text `team-notes ok`. Ship it to `appdev` only —
leave `appstage` exactly as it is; I don't want a stage deploy right now.

## Resources

Once you tell me the deploy is done, I'll ask you to "start develop" one
more time — just to make sure everything's still in order. If you tell
me a session is already active instead of starting a new one, that's the
answer I'm looking for.
