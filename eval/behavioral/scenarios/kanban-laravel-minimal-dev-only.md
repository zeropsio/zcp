---
id: kanban-laravel-minimal-dev-only
description: |
  "Vytvoř kanban na receptu zerops-laravel-minimal a uděláš jenom dev
  service" — natural-Czech user request hitting the recipe-route +
  dev-only bootstrap path. End-to-end real-life test: agent picks
  recipe-laravel-minimal, asks the minimal questions, narrows to
  bootstrapMode=dev (single container, no stage pair), provisions on
  eval-zcp, deploys, and reports the live URL.

  Multiple choice surfaces the agent must navigate without
  reflexively defaulting:

   1. Recipe-route vs classic-route — explicit "na receptu" in the
      prompt; agent MUST take the recipe path even though Discover
      will also score the classic fallback.
   2. Dev-only vs standard pair — explicit "jenom dev service" in
      the prompt; agent MUST NOT add stageHostname (post-v9.54.1
      schema rejects top-level stageHostname; nested under runtime
      means dev-only path skips it entirely).
   3. Source repo — recipe-laravel-minimal has its own zerops.yaml
      with `setup: app`; agent uses buildFromGit (recipe canonical
      source). No user repo / git remote needed for this path.
   4. Project name + hostname — derivable from "kanban" intent
      keyword. Agent should propose + confirm rather than freeze on
      missing input.

seed: empty
tags: [bootstrap, recipe-route, dev-mode, php, laravel, mysql-or-mariadb, czech-prompt, real-life]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář, který si chce postavit kanban-style task tracker v
  Laravelu na Zerops. Začínáš úplně od nuly — žádné existující
  služby na Zerops, žádný git repo, jen chuť rozjet rychle dev
  prostředí na recipe-laravel-minimal abys mohl iterovat na kódu.

  Tvoje preference:
   - Akceptuješ default rozhodnutí pokud má agent jasný důvod.
   - Pokud agent navrhne MariaDB místo MySQL (Zerops katalog), je to
     OK — accept + požádej ať to zmíní v final summary.
   - Pokud agent navrhne standard pair (dev + stage) místo dev-only,
     odmítni: "ne, chci jen dev, žádný stage zatím."
   - Pokud agent navrhne HA mode pro managed deps, odmítni: "není
     potřeba HA, je to dev."
   - Pokud se agent ptá na project name a ty nevíš, řekni něco jako
     "ať tomu říká kanban nebo kanban-dev, co dává smysl."
   - Pokud se agent ptá na hostname, akceptuj jeho návrh (app, web,
     api — cokoli z toho).
   - Pokud se agent ptá na konkrétní git remote URL, řekni "žádný
     ještě nemám, použij recipe URL, fork si udělám později."

  Co odmítneš:
   - Agent navrhne adopt-existing flow → nemám nic existujícího.
   - Agent zkusí classic-route (scaffold zerops.yml from scratch) →
     řekni "explicitně chci recipe-laravel-minimal."
   - Agent požaduje launchKey nebo prod token → "tohle je jen dev,
     žádná produkce."

notableFriction:
  - id: recipe-vs-classic-pick
    description: |
      Discover returns recipe match + classic fallback. Agent must
      lock onto recipe-laravel-minimal because user said "na
      receptu". Surfaces whether route-pick atom guides correctly
      when keyword "recipe" / "na receptu" is present.
  - id: dev-only-no-stage
    description: |
      Agent must pick bootstrapMode=dev (single container) and skip
      stageHostname. Surfaces whether the bootstrap-mode atom
      telegraphs the dev/standard/simple choice axis.
  - id: laravel-app-key-classification
    description: |
      Recipe-laravel-minimal needs APP_KEY at project scope. Agent
      should auto-secret-classify it (envclass bias on _KEY suffix
      matches). User accepts the bias without override.
  - id: managed-dep-substitution-mysql-mariadb
    description: |
      recipe-laravel-minimal canonical is MySQL but Zerops catalog
      doesn't ship a MySQL service today. Agent should propose
      MariaDB as compatible substitution + tell user up front
      rather than silently picking. Persona accepts; surfaces atom
      guidance on substitution disclosure.
  - id: missing-git-remote-noop
    description: |
      User has no writable git remote. Agent must NOT block on
      git-push-setup (that's a develop-flow concern). Bootstrap +
      first deploy via recipe's buildFromGit URL must finish
      without git-push-setup atom firing prematurely.
---

Vytvoř mi kanban na receptu zerops-laravel-minimal a uděláš jenom dev service. Spusť to do eval-zcp projektu (UUID waAzEFn6SBaysG4YE4rv7A) abych mohl iterovat na kódu. Žádný stage zatím, žádná produkce. Až bude dev běžet, řekni mi URL.
