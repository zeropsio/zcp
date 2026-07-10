---
id: launch-production-pipeline-local
description: |
  Local-mode launch-production followed by configuring-pipeline.
  Agent runs on the user's Mac (not in a Zerops container); ZCP
  uses the FS+exec path to read source zerops.yaml + git remote
  (no SSH). The launched response carries a pipeline-not-configured
  blocker; the agent walks the user through the Zerops dashboard
  config and re-calls to refresh.

  Sibling to the container scenario; same Part B trust model and
  Path B implementation. Verifies that the env-aware source-state
  reader correctly handles local-mode + the rest of the
  configuring-pipeline flow doesn't depend on container context.
seed: deployed
fixture: fixtures/greenfield-node-postgres.yaml
tags: [launch-production, pipeline-extension, path-b, local-mode, dashboard-driven]
area: launch
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You're running ZCP locally on your Mac, not in a Zerops container.
  Your dev + stage work fine. You want to launch production. ZCP
  reads zerops.yaml from your local filesystem and git remote from
  your local repo. When ZCP reaches ready-to-launch, it may offer to
  mint the launch token itself from a one-time platform delegation —
  no token value crosses the conversation; if so, confirm explicitly
  that it should go ahead. If instead ZCP falls back to asking you to
  generate one manually, you generate it and delete it right after
  launch, same as the container flow. After the launch, the runtime
  needs CD pipeline config — ZCP gives you a dashboard deep-link +
  recommendation, you configure, and re-run to confirm.
notableFriction:
  - id: local-mode-source-read
    description: |
      Source state read in local mode uses os.Exec + filesystem, not
      SSH. Surfaces whether readSourceState handles the local branch
      correctly when invoked from launch-production (analogous to
      export workflow's same dual-path).
  - id: same-pipeline-flow-local
    description: |
      Pipeline check + dashboard guidance + recheck on resume work
      identically in local mode. Surfaces any accidental dependency
      on container env vars or SSH state in the pipeline path.
---

I'm on my Mac, dev and stage are running on Zerops, I want to launch production now. Project name `myapp-prod`. If you can mint the launch token yourself, confirm with me first; otherwise I'll generate one manually when asked. After launch, walk me through any pipeline setup I need to do in the dashboard.
