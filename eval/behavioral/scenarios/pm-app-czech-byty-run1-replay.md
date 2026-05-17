---
id: pm-app-czech-byty-run1-replay
description: |
  Faithful replay of a real 2026-05-16 customer run that the env-var
  audit was based on (run1.txt, ~444 lines JSONL). Czech non-engineer
  prompt asking for a small internal project management app for a
  real-estate development firm. Single-runtime Node + Postgres scope.

  Pre-fix friction (what the live agent hit on the released binary):
    - 4 BUILD_FAILED deploys bisecting `HOSTNAME: 0.0.0.0` in
      run.envVariables (empty logs, no diagnostic)
    - DATABASE_URL=${db_connectionString} broke Prisma (connectionString
      omits /dbName) — 1-5 extra deploys + manual schema grant
    - Agent wrote 3 "memory feedback" files about Zerops env quirks,
      reflecting over-confident incomplete bisects

  This scenario re-runs the SAME prompt against the audit-built binary
  to verify Phase 0-4 fixes prevent the friction.
seed: empty
tags: [bootstrap, classic-route, develop, dev-only, node, postgres, prisma, czech-prompt, real-life, env-var-audit-replay]
area: env-var-audit-replay
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  # Expected NEGATIVE findings — agent should NOT hit these now.
  - id: hostname-bisect-deflected
    description: |
      Agent should NOT iterate `HOSTNAME: 0.0.0.0` deploys. Either:
      (a) develop-reserved-env-names atom deflects before writing,
      (b) CheckReservedEnvNames preflight in deploy_validate.go
          rejects with structured error naming HOSTNAME, OR
      (c) if a deploy somehow gets through to BUILD_FAILED with empty
          logs, the deploy_failure.go baseline names HOSTNAME as the
          dominant cause rather than pointing at buildCommands.
    suspectedCauses:
      # Filled by audit's Phase 1+3 fix path
      - internal/content/atoms/develop-reserved-env-names.md
      - internal/ops/deploy_validate.go::CheckReservedEnvNames
      - internal/ops/deploy_failure.go:140 (empty-logs baseline)
  - id: prisma-connectionString-deflected
    description: |
      Agent should compose `DATABASE_URL` explicitly with `/${db_dbName}`
      on the FIRST write, not after hitting "permission denied for schema
      public". develop-first-deploy-env-vars + develop-env-var-model
      should teach the composed form; discover annotation should also
      carry the completenessFlags.includesDbName=false hint.
    suspectedCauses:
      - internal/content/atoms/develop-first-deploy-env-vars.md (Postgres bullet)
      - internal/content/atoms/develop-env-var-model.md (worked example)
      - internal/ops/helpers.go::annotateConnectionStringShape
---

udelej mi projct mangement app pro nasi firmu. zabyvame se bytovym developmentem. chci umet zadavat tasky a hlavne tam mit hezky denni prehled.
