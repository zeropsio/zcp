---
id: git-push-setup-with-cicd-method-prompt
description: |
  User's first successful deploy just landed (record-deploy stamped
  `FirstDeployedAt` on the dev runtime; `RemoteURL` is empty —
  git-push has never been configured for this service).

  In the redesigned flow (§2.1):
   1. After the first deploy success, atom corpus emits a soft
      recommendation atom (`develop-first-deploy-git-push-recommendation`)
      pointing the user at `action=git-push-setup`. No ASKUSERQUESTION,
      no chain — text-only soft guidance.
   2. User opts in: agent calls `action=git-push-setup`.
   3. Walkthrough collects RemoteURL, confirms.
   4. On confirm-stamp, ZCP co-emits a structured cicd method prompt
      (§2.5): "Auto-deploy stage on every push to `main`? Choose:
      (a) Actions push-mode [recommended], (b) Zerops native webhook,
      (c) None — manual `zcli push` only."
   5. Agent picks (a) Actions; ZCP runs ComposeActionsHandoff(Stage):
      - `.github/workflows/zerops-stage.yml` content (raw `zcli push
        --setup <stage-name>` on push to main, NOT zeropsio/actions)
      - secret-set command: `echo "$ZCP_API_KEY" | gh secret set
        ZEROPS_TOKEN_STAGE -R <owner>/<repo>` (or jq variant for local)
      - GitHub PAT scope reminder

  Validates the inline cicd prompt flow at git-push-setup confirm time
  (§2.5 + §7.2). User shouldn't need a separate `action=build-integration`
  hop for the common case.
seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [git-push-setup, cicd-stage, method-prompt-inline, actions-push-mode, distinct-secrets]
area: git-push
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Your first deploy just succeeded. You haven't set up git push yet —
  the code only lives in the Zerops build container. Now that you've
  proved the deploy works, you want to push to git AND auto-deploy
  stage on every push to main. You'd rather do both in one flow,
  not two separate ZCP actions.

  Prefer GitHub Actions push-mode for the stage cicd. You already use
  GitHub for this repo; raw `zcli push` in CI matches how prod will
  work later (consistent build-recipe model).
notableFriction:
  - id: soft-recommendation-atom
    description: |
      After first deploy, the recommendation appears as plain prose
      (not ASKUSERQUESTION, not chain). Surfaces whether the agent
      relays the suggestion to the user OR treats lack of structured
      prompt as "nothing to do here."
  - id: inline-cicd-method-prompt
    description: |
      At git-push-setup confirm time, ZCP co-emits the cicd method
      prompt in the SAME tool result. Surfaces whether the agent
      reads both surfaces (confirm-stamp + method-prompt) or only
      one. The two responses are atomic — no separate ZCP call
      required between them.
  - id: distinct-stage-secret-name
    description: |
      Stage cicd uses ZEROPS_TOKEN_STAGE (distinct from the prod
      secret ZEROPS_TOKEN_PROD). The compose output should name it
      correctly; otherwise stage + prod would collide on the same
      repo's secret.
  - id: raw-zcli-not-marketplace-action
    description: |
      Workflow YAML uses raw `zcli install/login/push --setup
      <name>`, NOT zeropsio/actions@v1.0.2 (which doesn't accept a
      setup parameter — see internal/tools/workflow_build_integration
      .go::actionsWorkflowYAML). Surfaces whether composer emits the
      raw form (correct) or the marketplace action (wrong shape).
  - id: pat-scope-thinking-ahead
    description: |
      git-push atom guidance mentions the PAT's recommended scopes
      are ALSO used later for CI/CD setup (Contents R+W, Secrets R+W,
      Actions R+W). Surfaces whether the agent relays this so the
      user creates the PAT once with all needed scopes, not twice.
---

My first deploy just succeeded. Now I want to set up git push AND auto-deploy stage on every push to main. Repo is on GitHub. Do this in one flow, not two — I shouldn't need to run separate ZCP actions. Use GitHub Actions push-mode for the CI/CD (raw `zcli push`, not the marketplace action). Use the same project-scoped Zerops token I'm already running ZCP with.
