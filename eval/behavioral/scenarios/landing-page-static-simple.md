---
id: landing-page-static-simple
description: |
  "Potřebuju jednoduchou landing page na Zerops, statický HTML
  s nginx, žádná databáze, jen jeden kontejner co poběží" —
  natural-Czech user request hitting the simple-mode + static-no-deps
  path. Different shape from kanban scenario: no recipe needed (no
  framework match), no managed deps, simple-mode (single immutable
  runtime, no dev/stage iteration).

  Surfaces the agent must navigate:

   1. Simple-mode vs dev-only — both are "single runtime" but simple
      means IMMUTABLE (no SSH iteration, push-only deploys) while
      dev-only allows live editing on mount. User says "jen jeden
      kontejner co poběží" suggesting simple; agent should NOT
      reflexively default to dev-only.
   2. No recipe needed — static-nginx is too minimal for a recipe
      match; agent goes classic route.
   3. No managed deps — no DB, no cache; agent must NOT add postgres
      "for safety".
   4. Source code provenance — user doesn't have a git remote yet;
      agent writes HTML inline via mount + first deploy from there.

seed: empty
tags: [bootstrap, classic-route, simple-mode, static, nginx, no-managed-deps, czech-prompt]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi nejtechnický uživatel — frontend designer co potřebuje hodit
  na web jednu HTML stránku jako portfolio / landing. Žádný JS
  framework, žádný backend, žádná databáze. Chceš nejjednodušší
  variantu co jen funguje a běží stabilně.

  Tvoje preference:
   - Accept defaults pokud má agent jasný důvod.
   - Pokud agent navrhne dev-only (mutable runtime) místo simple-mode,
     řekni: "ne, nepotřebuju to editovat live, stačí immutable —
     jen push deploy."
   - Pokud agent navrhne db nebo jakýkoli managed service, odmítni:
     "nepotřebuju databázi, je to jen HTML."
   - Pokud agent navrhne framework (React, Vue, Astro...), odmítni:
     "ne, je to vanilla HTML, jeden index.html stačí."
   - Pokud agent chce git remote, řekni "nemám zatím žádný, ZCP ať
     řeší push deploy přímo, repo si udělám později."
   - Pokud se agent ptá na project name, "landing" / "site" /
     "portfolio" — cokoli krátkého.

  Akceptuješ:
   - Subdomain access (samozřejmě, je to landing page).
   - Default nginx config.
   - Minimální HTML content jako placeholder ("Hello from Zerops",
     "Coming soon", or your name + 2 sentences).

notableFriction:
  - id: simple-vs-dev-only
    description: |
      Both modes use a single runtime. "Simple" is immutable (push-
      only); "dev-only" allows live mount editing. User wording
      ("jen jeden kontejner co poběží") signals stable production-
      like service, NOT iterating-on-mount. Surfaces whether the
      bootstrap-mode atom telegraphs this distinction.
  - id: no-managed-dep-noise
    description: |
      Agent must NOT add postgres / redis / object-storage "for
      safety". Static landing has zero data persistence needs.
      Surfaces whether the recipe / scaffold atom over-suggests
      defaults.
  - id: nginx-runtime-not-php-nginx
    description: |
      Static = `nginx@1.22` runtime (no PHP). If agent picks
      `php-nginx` reflexively (because Laravel was the prior
      common case), surfaces overgeneralization. nginx-only base
      is a first-class Zerops runtime category.
  - id: classic-route-no-recipe-match
    description: |
      No Zerops recipe matches "static HTML page" — too minimal.
      Agent goes classic, scaffolds a minimal `zerops.yaml` for
      nginx + a sample `index.html`. Surfaces classic-route
      scaffold quality on the smallest possible stack.
---

Potřebuju jednoduchou landing page na Zerops — jen statický HTML s nginx, žádná databáze, jen jeden kontejner co poběží. Pošli mi URL až poběží. Project ID je `waAzEFn6SBaysG4YE4rv7A`.
