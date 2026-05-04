---
id: cross-deploy-stage-promote-from-dev
description: |
  Existing dev/stage Node pair with Postgres, dev verified healthy.
  User wants to promote the build artefact currently in `appdev` to
  `appstage` without rebuilding. Tests cross-deploy classification
  (DM-3): source != target, deployFiles is post-build-tree relative,
  agent selects sourceServiceStackId of the dev runtime. Counterpart
  scenario to self-deploy DM-2; surfaces whether the deploy atom
  guidance distinguishes self vs cross.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [cross-deploy, stage-promote, dev-stage, deploy-mode-asymmetry, deployFiles, no-rebuild]
area: develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Your `appdev` Node service is verified healthy and you want the
  exact same build promoted to `appstage` — not rebuilt from source,
  the same bytes. You expect the agent to use a cross-deploy from
  appdev to appstage. Push back if the agent proposes a fresh build,
  re-runs migrations, or treats this as a develop-iteration loop.
notableFriction:
  - id: cross-deploy-classification
    description: |
      Agent must select cross-deploy (sourceServiceStackId of dev,
      target appstage) rather than self-deploy or rebuild. Surfaces
      whether ClassifyDeploy + DM-3 are reachable from develop atoms.
  - id: post-build-tree-deployfiles
    description: |
      Cross-deploy deployFiles is post-build-tree relative. ZCP does
      not stat-check source. Surfaces whether agent guesses deployFiles
      of a self-deploy (containing `.`) vs a cross-deploy (artefact
      paths only).
---

I have `appdev` working great — verified, healthy, the build looks correct. Promote the exact same build to `appstage`. I do not want to rebuild from source again, just push the same bytes.
