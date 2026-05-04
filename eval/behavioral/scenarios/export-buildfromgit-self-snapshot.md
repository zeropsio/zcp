---
id: export-buildfromgit-self-snapshot
description: |
  Existing Node app deployed via direct push (no buildFromGit).
  User wants to switch future delivery to buildFromGit on the same
  GitHub repo and re-import the project from a self-snapshot. Tests
  the export workflow end-to-end: scope-prompt → classify-prompt →
  publish-ready three-call narrowing, single-repo self-referential
  bundle, schema-validation gating (bundle.errors → status
  validation-failed). Surfaces atoms covering the export workflow
  which no other scenario exercises.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [export, buildfromgit, self-snapshot, single-repo, three-call-narrowing, validation-gate]
area: export
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  You currently deploy by direct push and you want to move to
  buildFromGit so the platform clones and builds from your GitHub
  repository on every push. You also want a clean snapshot of the
  current project as an import.yaml that you can re-apply later or
  hand to a teammate. The repo URL is `https://github.com/example/teamapi`.
  Trust the agent's classification of which services should be in
  the snapshot. Push back if it proposes pushing to GitHub on your
  behalf or invents a different URL.
notableFriction:
  - id: export-three-call-shape
    description: |
      Export is a stateless three-call narrowing keyed by per-request
      WorkflowInput (target service, variant, env classifications).
      Surfaces whether the agent walks all three calls or tries to
      submit a single-shot export.
  - id: buildfromgit-vs-services-mode
    description: |
      `services[].mode` in import.yaml is the Zerops scaling enum
      (HA/NON_HA), not ZCP topology (dev/simple). Surfaces whether
      the export atom telegraphs this distinction.
  - id: validation-failed-gate
    description: |
      Schema-validation errors populate bundle.errors and flip
      response status to validation-failed BEFORE any git-push-setup.
      Surfaces whether the agent reads status= before chaining
      delivery setup.
---

The `app` service is working fine but I deploy it by pushing directly. I want to switch future deploys to buildFromGit on `https://github.com/example/teamapi`, and I'd like a self-snapshot import.yaml for the current project so I can re-apply it later.
