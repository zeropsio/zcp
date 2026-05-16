---
id: kanban-laravel-minimal-standard-pair
description: |
  "Vytvoř kanban na receptu zerops-laravel-minimal, chci dev i stage
  abych mohl testovat prod-style build před release" — natural-Czech
  variant of kanban-laravel-minimal-dev-only forking on bootstrapMode.
  Tests recipe + standard topology + cross-deploy dev → stage post-
  bootstrap. Persona explicitly wants the pair.

  Surfaces the agent must navigate (delta from dev-only variant):

   1. Standard mode (dev + stage pair) — explicit "dev i stage" in
      prompt; agent MUST pick `bootstrapMode=standard`, generate
      paired hostnames (e.g. `appdev`/`appstage`), and use the
      recipe's `zeropsSetup: dev` / `zeropsSetup: prod` defaults.
   2. Cross-deploy mechanic — after first dev deploy, agent should
      surface the dev → stage cross-deploy workflow (recipe stage
      builds via `setup: prod`, validates production-shaped build
      before promotion to a future prod project).
   3. Same APP_KEY at project scope — pair shares the database +
      cookies; APP_KEY must propagate to both dev + stage containers.
   4. Recipe's pair shape pinned — recipe-laravel-minimal ships
      `appdev` + `appstage` services in the import yaml; agent
      uses them verbatim. Renaming hostnames is collision recovery
      only (no collisions on a fresh project).

seed: empty
tags: [bootstrap, recipe-route, standard-mode, php, laravel, postgres-or-mariadb, cross-deploy, czech-prompt, real-life]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář, který si chce postavit kanban-style task tracker v
  Laravelu na Zerops. Začínáš od nuly — žádné existující služby na
  Zerops, žádný git repo. Tentokrát ale chceš celý dev/stage pair
  abys mohl ladit production-style build (compiled assets, no dev
  packages) před případnou promocí do produkce.

  Tvoje preference:
   - Akceptuješ default rozhodnutí pokud má agent jasný důvod.
   - Pokud agent navrhne pouze dev (single container), odmítni:
     "ne, chci pair — dev i stage, ať můžu testovat prod-style
     build před release."
   - Akceptuj MariaDB / PostgreSQL substituci pokud Zerops katalog
     nemá MySQL — agent by měl uvést náhradu ve finálním summary.
   - Pokud agent navrhne HA mode pro managed deps, odmítni: "není
     potřeba HA, jen dev pair pro iteraci."
   - Pokud se agent ptá na project name a ty nevíš, řekni něco jako
     "kanban-pair nebo cokoli krátkého, co dává smysl."
   - Pokud se agent ptá na hostnames, akceptuj recipe defaults
     (`appdev` + `appstage`).
   - Pokud se agent ptá na git remote, řekni "žádný ještě nemám,
     použij recipe buildFromGit URL, fork si udělám později."

  Co odmítneš:
   - Agent navrhne dev-only fallback "for simplicity" → "ne, dev i
     stage, ať můžu odladit prod build."
   - Agent skočí rovnou na launch-production / prod token →
     "tohle je jen dev/stage pair, žádná produkce ještě."
   - Agent zkusí classic-route místo recipe → "explicitně chci
     recipe-laravel-minimal."

  Co očekáváš na konci:
   - `appdev` service ACTIVE s běžícím Laravel kanban appkou
   - `appstage` service ACTIVE (po cross-deploy z dev) — stejný kód,
     prod-style build pipeline
   - `db` service ACTIVE (Postgres nebo MariaDB)
   - APP_KEY shared across dev + stage at project scope
   - URL aspoň jednoho z prostředí pro live verify

notableFriction:
  - id: standard-vs-dev-only-mode-pick
    description: |
      User explicitly says "dev i stage" — agent MUST pick standard
      mode and not collapse to dev-only "for simplicity". Surfaces
      whether bootstrap-mode atom telegraphs the three-way choice
      clearly (simple / dev-only / standard) AND whether the agent
      respects user-stated topology over a "simpler default".
  - id: cross-deploy-dev-to-stage
    description: |
      Standard pair canonical lifecycle is: dev deploys land code,
      cross-deploy promotes to stage running `setup: prod` build.
      Surfaces whether develop-active atoms telegraph the
      cross-deploy workflow as the next step after dev is up,
      vs. agent treating stage as "just another runtime to push to".
  - id: app-key-shared-project-scope
    description: |
      APP_KEY must be set at project scope so dev and stage share
      the value (sessions / encrypted columns survive the L7
      balancer routing requests to either pair half). Per-service
      env var breaks this. Surfaces whether the recipe gotcha
      atom telegraphs project-scope requirement for the pair shape.
  - id: pair-hostname-defaults
    description: |
      Recipe ships `appdev` + `appstage` defaults; agent uses them
      verbatim unless user objects. Pinned in the recipe import
      yaml's hostname fields. Surfaces whether the recipe atom
      makes this clear vs. agent inventing new names.
  - id: managed-dep-substitution-disclosure
    description: |
      Same as dev-only variant — MySQL → MariaDB substitution
      should be disclosed up front. Pair shares the same dep
      regardless of substitution.
---

Vytvoř mi kanban na receptu zerops-laravel-minimal, chci dev i stage abych mohl testovat prod-style build před release. Spusť to do eval-zcp projektu (UUID waAzEFn6SBaysG4YE4rv7A). Žádná produkce ještě, jen dev + stage pair. Až poběží dev i stage, řekni mi URL aspoň jednoho z nich.
