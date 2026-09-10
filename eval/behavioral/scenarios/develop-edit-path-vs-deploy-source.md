---
# promote to required after two consecutive baseline greens (plan D9)
id: develop-edit-path-vs-deploy-source
description: |
  S5 self-review friction (runs 3/4/5, all three independently hit this):
  the agent edits at the SSHFS mount `/var/www/<hostname>/` — correct per
  AGENTS.md/CLAUDE.md guidance ("Edit files there with Read/Edit/Write, not
  SSH") — but `zerops_deploy` resolves the self-deploy source from
  `{workDir}/{hostname}/`, a different path under the working directory.
  The first deploy attempt fails `PREFLIGHT_FAILED` ("source mount
  {workDir}/{hostname} missing"). All three runs recovered via a manual
  symlink from the working directory to the SSHFS mount, none guided there
  by workflow guidance. Frozen expectation: despite the friction, the run
  still reaches a deployed, live service.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [develop, first-deploy, edit-path, deploy-source, mount, observe, s5-signal]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: observe
  spec: spec-workflows.md §4
  liveness: {service: appstage, marker: "nodejs"}
  noFailedProcesses: true
  never: ["zerops_import{override=true}"]
  askWhen: []
notableFriction:
  - id: edit-path-vs-deploy-source
    description: |
      Symptom (verbatim from S5 evidence, runs 3/4/5): `zerops_deploy`
      failed with `PREFLIGHT_FAILED` because it expected the source at
      `{workDir}/{hostname}` (e.g. `/home/zerops/s5/work-run5/appstage`),
      but the SSHFS mount — where AGENTS.md/CLAUDE.md instruct the agent to
      edit — lives at `/var/www/<hostname>/`. All three runs recovered by
      manually creating a symlink from the working-directory path to the
      SSHFS mount; nothing in the workflow guidance told them to.
    suspectedCauses:
      - internal/content/atoms/develop-first-deploy-write-app.md (no mount↔workDir bridging step)
      - deploy_ssh.go source-resolution (silently expects workDir/{hostname}, never the mount path)
---

The `appdev` Node app is healthy with Postgres. Add a small change to `appstage` (e.g. bump a version string or add a trivial endpoint) and deploy it so I can see it live. Use project `{{projectId}}`.
