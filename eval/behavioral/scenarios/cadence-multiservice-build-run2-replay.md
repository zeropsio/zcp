---
id: cadence-multiservice-build-run2-replay
description: |
  Build-path variant of cadence-multiservice-spec-run2-replay. The
  original Run 2 user gave a "ROLE/CÍL/FORMÁT" spec-style prompt and
  the agent ultimately built and deployed an 8-service Cadence app
  (apidev/apistage/appdev/appstage + db/cache/search/storage). The
  literal spec-only replay didn't exercise the build path; this
  variant appends explicit "vybuduj to a nasaď" so the build path
  fires.

  Verifies post-fix behavior on multi-service first-deploy:
    - HOSTNAME deflection (Phase 3 preflight)
    - DATABASE_URL composed with /${db_dbName} (Phase 1+2)
    - Prisma uses `db push` not `migrate dev` (Phase 4 atom update)
    - Search uses scoped key not master (existing atom guidance)
    - SSH usage references env vars rather than pasting (Phase 2b)
seed: empty
tags: [bootstrap, classic-route, develop, multi-service, node, postgres, valkey, meilisearch, object-storage, prisma, czech-prompt, build-path, env-var-audit-replay]
area: env-var-audit-replay
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: hostname-preflight-deflection
    description: Multi-service zerops.yaml authoring — agent must avoid HOSTNAME or get caught by CheckReservedEnvNames.
    suspectedCauses:
      - internal/ops/deploy_validate.go::CheckReservedEnvNames
      - internal/content/atoms/develop-reserved-env-names.md
  - id: prisma-db-push-default
    description: Agent should choose db push for fresh schema. If migrate dev attempted, recovers fast via the atom's superUser instruction.
    suspectedCauses:
      - internal/content/atoms/develop-first-deploy-env-vars.md (Prisma bullet)
  - id: search-narrow-key-pick
    description: Agent should pick scoped Meilisearch key, never master. Side-observation — not directly addressed by audit.
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

Pak to vybuduj a nasaď na Zerops. Chci to mít provozované, ne jen specifikaci.
