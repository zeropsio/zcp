---
id: cadence-multiservice-spec-run2-replay
description: |
  Faithful replay of a real 2026-05-16 customer run (run2.txt, ~708
  lines JSONL). Czech ROLE/CÍL/PRAVIDLA-style detailed product spec
  for a Trello/Linear-like project-management web app. Multi-service:
  Postgres + Valkey + Meilisearch + object-storage + 4 runtime halves
  (apidev/apistage/appdev/appstage).

  Pre-fix friction:
    - ~10 BUILD_FAILED deploys bisecting HOSTNAME in run.envVariables
    - Agent suspected ${zeropsSubdomainHost} wouldn't resolve in
      run.envVariables and removed it (false positive — it DOES resolve)
    - DATABASE_URL=${db_connectionString} broke Prisma; agent eventually
      used superUser credentials + manual SQL grants (5+ deploys late)
    - Agent used ${search_masterKey} despite atom guidance to pick the
      narrow scoped key (atom present in 4 tool_results but ignored)

  This scenario verifies Phase 0-4 deflects HOSTNAME + connectionString.
  masterKey behavior is NOT directly addressed by this audit (atom
  guidance unchanged); observing whether multi-service complexity
  worsens or doesn't affect the avoided-trap outcomes is a side-signal.
seed: empty
tags: [bootstrap, classic-route, develop, multi-service, node, postgres, valkey, meilisearch, object-storage, prisma, czech-prompt, real-life, env-var-audit-replay]
area: env-var-audit-replay
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: hostname-bisect-deflected
    description: |
      Same as run1 replay — HOSTNAME in run.envVariables should be
      caught at preflight or named explicitly in the empty-logs
      baseline diagnosis. Multi-service complexity should not affect
      the deflection.
    suspectedCauses:
      - internal/ops/deploy_validate.go::CheckReservedEnvNames
      - internal/content/atoms/develop-reserved-env-names.md
  - id: prisma-connectionString-deflected
    description: |
      Compose DATABASE_URL explicitly on the FIRST write. Side observation:
      does the agent also use superUser/superUserPassword automatically
      (DDL pattern) or still require user prompting?
    suspectedCauses:
      - internal/content/atoms/develop-env-var-model.md
      - internal/ops/helpers.go::annotateConnectionStringShape
  - id: zeropsSubdomainHost-resolution-confusion-observation
    description: |
      Side-signal — observe whether the agent again suspects
      ${zeropsSubdomainHost} of not resolving. develop-env-var-model
      mentions auto-injected platform values; the run-1 retro confirmed
      the friction was speculative, not evidenced. No direct fix in
      audit; clear retro signal would suggest a follow-up atom.
    suspectedCauses:
      - internal/content/atoms/develop-env-var-model.md (does it disambiguate?)
  - id: search-masterKey-side-observation
    description: |
      Side-signal — atom develop-first-deploy-env-vars says "pick the
      narrow scoped key, never master". Run 2 agent ignored this 4×.
      Did Phase 1/2 reorganization change this outcome incidentally?
      No direct fix in audit; deflection here is bonus.
    suspectedCauses:
      - internal/content/atoms/develop-first-deploy-env-vars.md (search bullet)
---

# ROLE
Jsi produktový designér a frontend architekt pro moderní web. Doména: projektový management. Cílový systém: Claude (Anthropic).

# ŘÍDÍCÍ PRINCIP
Vizuální hierarchie, přístupnost a výkon mají přednost před efekty.

# CÍL
Navrhni komplexní webovou aplikaci pro projektový management, která bude zahrnovat správu úkolů, sledování pokroku, týmovou spolupráci a generování reportů. Zaměř se na uživatelskou přívětivost a škálovatelnost pro malé a střední podniky.

# PROVÁDĚCÍ PRAVIDLA
- Definuj komponentovou strukturu a props.
- Používej sémantické design tokeny, ne hardcoded barvy.
- Ke každé stránce uveď layout, obsah a responsivní chování.

# KONTEXT
- Doména: projektový management.
- Publikum: malé a střední podniky, projektoví manažeři, týmoví lídři.

# OMEZENÍ
- Musí být webová aplikace, responzivní design, podpora pro více uživatelů a rolí, integrace s kalendářem a notifikacemi. Použij moderní webové technologie.

# FORMÁT VÝSTUPU
Detailní specifikace funkcionalit, návrh uživatelského rozhraní (wireframes/mockupy), databázový model a doporučení pro technologický stack.
