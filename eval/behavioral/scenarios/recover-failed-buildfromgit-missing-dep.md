---
id: recover-failed-buildfromgit-missing-dep
description: |
  Tier-1 FAILED-state recovery scenario. The fixture imports the
  python-hello-world-app via buildFromGit but deliberately omits
  the `db` service the repo's zerops.yaml hard-references in env
  vars + initCommand (migrate.py). The runtime container builds
  successfully, then crashes on init when migrate.py cannot resolve
  `${db_hostname}`. The api service settles in FAILED status before
  the agent runs.

  This scenario is the empirical baseline for measuring ZCP's
  FAILED-state recovery surface (plans/zcp-failed-state-recovery-
  2026-05-04.md). Pre-Phase-C it surfaces that the agent has no
  structured Recovery on FAILED services in `zerops_discover` /
  `zerops_verify` / `zerops_deploy` — they pass through normal flow
  until something downstream errors with no actionable next step.
seed: settled
fixture: fixtures/python-simple-failed-no-db.yaml
tags: [recovery, failed-state, buildfromgit, missing-dep, simple-mode, python]
area: recovery
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §8
  liveness: {service: api, marker: "python"}
  noFailedProcesses: true
  allowFailed: [api]
  never: ["zerops_import{override=true}"]
userPersona: |
  Your Python service `api` is supposed to be running on Zerops
  but it's broken — you can't tell why, only that the deploy
  doesn't work. You expect the agent to figure out the cause from
  logs/state and fix it. You don't know that the underlying
  zerops.yaml in the repo expects a Postgres database next to it.
  If the agent suggests adding a database, that's plausible; agree
  if the rationale is grounded in evidence (logs, env vars). Push
  back if the agent proposes recreating the project from scratch
  instead of fixing what's there.
notableFriction:
  - id: detect-failed-state
    description: |
      The agent should detect FAILED status on `api` immediately
      via `zerops_discover` (or first verify call) — before
      attempting to deploy / dev_server / SSH. Surfaces whether
      ZCP routes the agent to a FAILED-state recovery atom or
      lets it discover the problem by hitting downstream errors.
  - id: read-runtime-logs
    description: |
      The crash cause (`migrate.py` failing on unresolved
      `${db_hostname}`) lives in runtime logs, not build logs.
      Surfaces whether the agent reaches for `zerops_logs
      facility=application` early or wastes turns on build-side
      surfaces.
  - id: structural-fix-not-rebuild
    description: |
      Once the cause is identified, the right fix is to add a
      `db: postgresql@*` service via `zerops_import override=true`
      (or equivalent), then re-deploy. Surfaces whether the agent
      reaches for that recovery vs. tearing the project down.
  - id: no-wasted-deploy-on-failed
    description: |
      Calling `zerops_deploy` on a service that's already in FAILED
      status without first fixing the underlying cause is wasted
      motion. Surfaces whether ZCP gates deploy on pre-existing
      FAILED state or silently re-runs the broken deploy cycle.
---

The `api` Python service in this Zerops project is failing — I expected it to be running. Diagnose what's wrong and fix it so the service ends up healthy.
