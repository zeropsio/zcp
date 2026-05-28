---
id: env-cross-runtime-ref-lifecycle
description: |
  Cross-RUNTIME env reference through the first-deploy lifecycle. A worker
  runtime references a value the API runtime exposes; under the default
  service isolation that needs an explicit ${api_*} ref in run.envVariables
  (no auto-inject). Before the API's first deploy its yaml-baked env isn't on
  the platform yet, so the ref can't be confirmed — deploy-preflight must WARN,
  not FAIL (a hard fail would block a legitimate first deploy). After the API
  deploys, the same ref resolves via the API's app-version userDataList.
seed: empty
tags: [develop, multi-runtime, cross-service-env, first-deploy, lifecycle, node, postgres]
area: bootstrap-and-develop
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  # Informational only — does NOT gate anything. Cues for the local grader.
  - id: trap-1
    description: |
      Cross-service wiring must be an explicit ${host_var} ref in
      run.envVariables — the default service isolation does NOT auto-inject a
      sibling's vars. An agent relying on a bare ${worker sees api_*} without
      declaring the ref produces config that's blank in production.
    suspectedCauses:
      - internal/content/atoms/develop-env-var-model.md (explicit ${host_var} rule)
      - internal/content/atoms/develop-platform-rules-common.md (no auto-inject)
  - id: trap-2
    description: |
      When the worker references the API's run.envVariables var and the API has
      NOT been deployed yet, deploy-preflight must classify the miss as a
      never-deployed-sibling WARN (deploy proceeds), NOT a hard env-ref FAIL.
      After the API's first deploy the ref resolves from its app-version
      yaml-baked env. A FAIL here blocks a valid bootstrap order.
    suspectedCauses:
      - internal/tools/deploy_preflight.go (preflightEnvRefs lifecycle partition)
      - internal/ops/env_effective.go (AppVersionEnvVars / IsRuntimeNeverDeployed gate)
---

Build me a Node API plus a separate Node background worker. The worker pulls
jobs and calls the API, so it needs the API's base URL. Both share one Postgres.
