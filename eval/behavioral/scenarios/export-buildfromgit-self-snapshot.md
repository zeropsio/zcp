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
  - id: route-to-workflow-export-vs-legacy-tool
    description: |
      Two surfaces produce a project YAML snapshot: legacy
      `zerops_export` tool (returns full project YAML with cleartext
      secrets in envVariables/envSecrets, no env classification, no
      schema validation) versus `zerops_workflow workflow="export"`
      (three-call narrowing with env classify-prompt + buildFromGit-
      aware import.yaml + schema validation gate). The new workflow
      is canonical when the user wants a re-importable bundle. Earlier
      eval run (suite 20260520-162314) showed the agent reaching for
      the legacy tool 9× and never invoking `workflow="export"` — the
      "self-snapshot" phrase routed to the wrong surface. Verify the
      agent routes to `workflow="export"` from natural language
      describing a re-importable bundle + buildFromGit migration.
  - id: export-three-call-shape
    description: |
      Export is a stateless three-call narrowing keyed by per-request
      WorkflowInput (target service, variant, env classifications).
      Surfaces whether the agent walks all three calls or tries to
      submit a single-shot export.
  - id: classify-prompt-suggested-bucket-acceptance
    description: |
      classify-prompt rows now carry server-computed `suggestedBucket`
      + `rationale` (Phase 2 of plans/archive/env-discover-
      three-changes-2026-05-20.md). Agent should accept the suggestion verbatim for
      unambiguous keys (credential-pattern hits → auto-secret;
      ZCP_API_KEY / GIT_TOKEN → infrastructure) without re-deriving
      name-pattern bias. Surfaces whether the new field reduces the
      pre-Phase2 13/13 transcripts that re-walked the four-bucket
      detection rules in agent prose.
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

The `app` service is working fine but I deploy it by pushing directly. I want to turn this project into a re-importable bundle that points at GitHub: switch the deploy to buildFromGit from `https://github.com/example/teamapi` AND give me the matching `zerops-project-import.yaml` + `zerops.yaml` pair so a teammate (or my future self) can recreate the project from scratch later. Use whatever workflow path is canonical for this — I don't want a raw cleartext dump of the project state, I want the curated re-importable form.
