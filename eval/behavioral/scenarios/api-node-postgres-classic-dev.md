---
id: api-node-postgres-classic-dev
description: |
  "Mám rozjet Node.js API s Postgres databází, klasická REST,
  zatím jen pro vývoj — chci to mít na Zerops abych mohl iterovat
  na kódu" — natural-Czech user request hitting the classic-route
  (no recipe) + dev-only path with managed dep. Different from
  kanban scenario: NO recipe slug in prompt (agent goes classic by
  default), no Laravel/PHP, no storage. Different from landing
  scenario: HAS managed dep (postgres), DEV-ONLY mode (live mount
  iteration), Node compiled runtime.

  Surfaces the agent must navigate:

   1. Classic-route vs recipe-attempt — no slug, no framework name
      in prompt; agent should NOT search for "node-api" recipe (it
      doesn't exist). Discover returns classic only; agent picks
      classic without false attempts.
   2. Dev-only mode — explicit "pro vývoj" + "iterovat na kódu";
      agent MUST pick bootstrapMode=dev (live SSHFS mount), NOT
      simple (immutable).
   3. Postgres managed dep resolution — user says "Postgres
      database"; agent adds `postgresql@<version>` under
      runtime.dependencies, NOT as a separate top-level target.
   4. Classic-scaffold zerops.yaml — no recipe yaml to consume;
      agent generates a minimal one for nodejs runtime + connects
      to db via `${db_*}` cross-service refs.
   5. Start command + initCommands — agent must scaffold
      package.json + index.js with HTTP listen on PORT, plus
      `zerops.yaml` build/run blocks.

seed: empty
tags: [bootstrap, classic-route, dev-mode, dynamic, node, postgres, managed-dep, czech-prompt]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi backend dev, chceš si rychle rozjet Node.js REST API s
  Postgres jako persistent storage. Žádné existující kódy ani repo
  — všechno od nuly. Cílem je iterovat na kódu live (dev mode),
  ne produkční immutable kontejner. Žádná stage zatím, žádná
  produkce.

  Tvoje preference:
   - Accept default Node.js verzi (latest stable 22.x je fine).
   - Accept default postgres verzi (Zerops katalog).
   - Pokud agent navrhne stage pair "for safety", odmítni: "ne, jen
     dev, stage si udělám až bude potřeba."
   - Pokud agent navrhne simple-mode (immutable), odmítni: "ne,
     potřebuju dev mode abych mohl editovat live."
   - Pokud agent navrhne HA pro postgres, odmítni: "není potřeba,
     dev only."
   - Pokud agent navrhne nějaký framework (Express, Fastify, Hono),
     accept the first one — nemáš preferenci.
   - Pokud se agent ptá na hostname, "api" stačí.
   - Pokud se agent ptá na project name, "node-api" nebo cokoli
     krátkého.
   - Pokud agent chce git remote, "nemám zatím žádný, ZCP ať řeší
     deploy interně."

  Co očekáváš na konci:
   - api service (nodejs@22 nebo podobné) ACTIVE
   - db service (postgresql) ACTIVE
   - Health endpoint (GET / nebo /health) co vrátí 200
   - Subdomain URL pro testing

notableFriction:
  - id: no-recipe-search-noise
    description: |
      User prompt nemá ŽÁDNÝ recipe slug ani framework keyword.
      Agent NESMÍ zkoušet `zerops_knowledge recipe=node-api` nebo
      pulling recipe corpus. Classic-route je jediná správná
      cesta. Surfaces whether the bootstrap-route atom dispatches
      cleanly without recipe-attempt overhead.
  - id: dev-only-vs-standard-vs-simple
    description: |
      Three-way distinction:
       - simple = immutable single runtime (no mount, push-only)
       - dev-only = mutable single runtime (live mount editing)
       - standard = dev+stage pair (full flow)
      User wording "iterovat na kódu" + "pro vývoj" + "jen dev"
      signals dev-only. Surfaces whether atom telegraphs the
      mode-axis clearly.
  - id: postgres-as-dependency-not-target
    description: |
      Postgres is a CREATE resolution under runtime.dependencies
      (not a separate top-level provision target). Plan must list
      it nested. Schema rejects top-level postgres if it's a dep.
  - id: classic-scaffold-zerops-yaml
    description: |
      No recipe yaml → agent generates zerops.yaml from scratch.
      Must have:
       - `build.base: nodejs@22`
       - `build.buildCommands: [npm install]`
       - `build.deployFiles: ./` (or compiled subset)
       - `run.base: nodejs@22`
       - `run.start: node index.js` (or whatever scaffold creates)
       - `run.envVariables` with `${db_*}` cross-service refs
       - `run.ports: [{port: 3000, httpSupport: true}]`
      Surfaces classic-scaffold quality on a representative Node+
      postgres stack.
  - id: initCommands-migration-pattern
    description: |
      If agent uses ORM (Prisma, TypeORM, Sequelize), it must wire
      migration init via `zsc execOnce ${appVersionId}
      --retryUntilSuccessful -- <migration-cmd>`. Surfaces whether
      the develop-loop atom telegraphs this pattern. If agent uses
      plain pg client, no migration init needed (table create at
      app boot).
---

Mám rozjet Node.js API s Postgres databází, klasická REST, zatím jen pro vývoj — chci to mít na Zerops abych mohl iterovat na kódu. Žádná stage, žádná produkce, žádné existující repo. Použij project `waAzEFn6SBaysG4YE4rv7A`.
