---
id: adopt-ubuntu-node-first-deploy-keeps-os
description: |
  Existing undeployed Node runtime imported as `ubuntu/nodejs@22` (the
  shape a bare import materializes to). The agent adopts it and does a
  first deploy of a tiny Express endpoint. The platform trap: `run.base`
  rewrites the service OS on every deploy and a bare `base: nodejs@22`
  in zerops.yaml resolves to alpine — so a scaffold written from bare
  examples silently flips the service to `alpine/nodejs@22`. Pass =
  the service type still carries `ubuntu/` after the deploy.
seed: settled
fixture: fixtures/ubuntu-node-imported.yaml
tags: [adopt, develop, first-deploy, node, dev-only, os-prefix, base-string]
area: adopt-and-develop
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You are a backend dev. Your ops colleague already created the Node
  service `app` in the Zerops project (it is running, nothing deployed
  yet). You want a minimal Express app with `GET /` returning
  `{"ok":true}` deployed to `app` and verified over its subdomain.

  Preferences:
   - Dev only. If the agent proposes a dev/stage pair or a stage slot,
     decline: "just the one dev service, no stage."
   - Accept defaults for everything else (Node 22, hostname `app`).
   - If the agent asks which OS / base image / Alpine vs Ubuntu you want,
     answer: "keep whatever the service already is — don't change it."
   - If the agent asks for a project name or anything cosmetic, answer
     with something short.
verification:
  expectedServices:
    - hostname: app
      status: [ACTIVE]
      type: ubuntu/nodejs@*
  noFailedProcesses: true
notableFriction:
  # Informational only — does NOT gate the run.
  - id: os-flip
    description: |
      The agent writes zerops.yaml with a bare `build.base: nodejs@22` /
      `run.base: nodejs@22` (or omits the OS prefix) although
      zerops_discover reported `ubuntu/nodejs@22`; the deploy then flips
      the service to `alpine/nodejs@22`. Watch for a verify step that
      reads /etc/os-release or a prepareCommands `apt-get` that fails.
    suspectedCauses:
      - internal/content/atoms/scaffold-zerops-yaml.md (type→base table is bare)
      - internal/content/atoms/develop-first-deploy-scaffold-yaml.md (bare placeholders)
      - internal/knowledge/themes/core.md (teaches deprecated `os:` sibling, bare base)
      - internal/knowledge/catalog_view.go (catalog collapses alpine/ubuntu to bare)
  - id: os-question
    description: |
      The agent asks the user which OS to use instead of copying the
      prefix from the discovered type. Not wrong, but a sign the
      guidance does not make "keep the discovered OS" the default.
  - id: first-deploy-atoms-skipped-on-adopt
    description: |
      An adopted `startWithoutCode` runtime carries a platform app
      version, so the envelope reads `deployed=true` before any code
      shipped and every first-deploy atom gated on `never-deployed`
      (intro / scaffold-yaml / execute) is silently skipped. Observed
      in runs 20260828-083747 and 20260828-085843: the agent copied
      the recipe's zerops.yaml verbatim (`base: nodejs@22` +
      `os: ubuntu` → alpine build container under an ubuntu run).
    suspectedCauses:
      - "internal/content/atoms/develop-first-deploy-scaffold-yaml.md (envelopeDeployStates gate = never-deployed)"
      - "internal/ops/env_effective.go IsRuntimeNeverDeployed (ActiveAppVersion set by startWithoutCode)"
      - "internal/knowledge/recipes/nodejs-hello-world.md:107-110 (legacy os sibling field — recipe corpus not migrated)"
---

The `app` Node service is already in my Zerops project — my ops colleague set it up, nothing is deployed yet. Write a minimal Express app with `GET /` returning `{"ok":true}`, deploy it to `app` and verify it responds. Dev only, no staging.
