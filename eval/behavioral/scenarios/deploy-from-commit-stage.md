---
id: deploy-from-commit-stage
description: |
  Existing dev/stage Node pair, both buildFromGit-deployed (real cloned
  repo mounted on appdev). User wants a SPECIFIC commit — not necessarily
  the current HEAD — shipped to appstage, and wants to be able to prove
  later exactly which commit is running there. Tests G3 (docs/
  spec-workflows.md §4.9): `zerops_deploy sha=` deploy-from-commit and the
  zcp/deploy/* annotated-tag ledger it leaves in appdev's repo.

  Status `promote: containerCheck` (docs/spec-scenarios.md §9.3 table G):
  the runner evaluates containerCheck today, but this file does not carry
  one yet — a follow-up adds a direct
  `git tag -l 'zcp/deploy/*/appstage/*'` check and flips the row to
  `gate`. Today's oracle coverage (toolArg/toolResult/liveness) proves the
  same behavior indirectly, via the deploy response rather than a
  container read.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [deploy-from-commit, ledger, git-foundation, cross-deploy, node]
area: develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.9
  liveness: {service: appstage, marker: "nodejs"}
  toolArg:
    - {always: "zerops_deploy{targetService=appstage}"}
  toolResult:
    - {tool: zerops_deploy, contains: "\"sha\":\""}
    - {tool: zerops_deploy, contains: "\"appVersionId\":\""}
  noFailedProcesses: true
  never: ["zerops_import{override=true}", "zerops_delete"]
userPersona: |
  Your `appdev` and `appstage` are both healthy Node services, cloned from
  the same GitHub repo. You want to ship the exact commit currently
  checked out on `appdev` to `appstage` — and you want to be able to ask
  later "what commit is running on stage" and get a real answer, not a
  guess from the deploy timestamp. Push back if the agent proposes a plain
  cross-deploy without naming a commit, or claims it can't track which
  commit landed where.
notableFriction:
  - id: sha-param-discovery
    description: |
      Agent must discover `zerops_deploy` takes a `sha` parameter (rather
      than resolving the commit itself and passing it as `workingDir` or
      some other field) and pass the commit reachable in appdev's mounted
      repo.
  - id: ledger-as-the-answer
    description: |
      When the user later asks "what's running on stage", the agent
      should point at the deploy response's own `sha`/`appVersionId`
      fields (or the `zcp/deploy/<project>/appstage/<appVersionId>`
      annotated tag in appdev's repo) rather than inventing a tracking
      mechanism or claiming there is no way to know.
---

I want to ship the exact commit currently checked out on `appdev` to
`appstage` — not just "whatever's in the working tree right now" in some
vague sense, the actual commit, by its sha. Can you do that, and
afterwards tell me exactly what's now running on appstage so I can check
it later?
