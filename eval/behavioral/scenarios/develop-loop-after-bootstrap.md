---
id: develop-loop-after-bootstrap
description: |
  "Vytvoř mi Express API s Postgres, pak mi přidej GET /version endpoint
  a deploynu ho" — natural-Czech chained scenario testing bootstrap →
  develop transition within one session. No tokens needed; pure
  greenfield + iterate on dev mount.

  Surfaces the agent must navigate:

   1. Bootstrap-to-develop session handoff — after bootstrap completes
      (close step), the agent should auto-enter develop or recognize the
      next prompt as a develop intent. No need to re-bootstrap or close
      the session manually.
   2. Live-mount edit + deploy semantics — dev-mode runtime serves files
      from `/var/www` mount; agent edits in place, then `zerops_deploy`
      pushes the edit. NOT a "push to GitHub" workflow — this is the
      pre-CI dev iteration loop.
   3. Endpoint scaffolding on top of recipe / hand-scaffold — agent edits
      existing Express route file (or scaffold creates one if classic).
      Verify endpoint reachable post-deploy.
   4. Develop-flow scope — `zerops_workflow workflow=develop` with
      `scope=["appdev"]` since only the dev runtime changed. Don't ask
      to redeploy db (no code).

seed: empty
tags: [bootstrap, develop-chain, dev-mode, node, postgres, czech-prompt, real-life]
area: bootstrap-and-develop
retrospective:
  promptStyle: briefing-future-agent
verification:
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
  noFailedProcesses: true
  retrospectiveMustNotMention:
    - smuggled
    - hand-edited yaml
    - had to overwrite
userPersona: |
  Jsi backend dev co začíná od nuly. Cíl: postavit Express API +
  Postgres na Zerops v dev módu, pak na něj přidat jednoduchý
  `GET /version` endpoint co vrátí JSON s aktuálním commit-SHA / build
  ID. Iteruješ live na dev kontejneru (mount), žádný stage.

  Tvoje preference:
   - Akceptuj defaults (Node 22, Postgres latest, hostname `appdev`).
   - Pokud agent navrhne dev + stage pair, odmítni: "jen dev, nechci
     stage."
   - Pokud agent navrhne HA mode pro db, odmítni: "není potřeba HA,
     dev only."
   - Pokud agent navrhne recipe nebo framework jiný než Express,
     accept first reasonable (Express je default ok).
   - Pokud se agent ptá na project name, "express-versioned" nebo
     cokoli krátkého.
   - Pokud se agent ptá na konkrétní endpoint shape, řekni: "stačí
     `GET /version` co vrátí JSON `{ version: 'dev-<timestamp>' }`."
   - Po prvním deploy, prompt na "přidej /version endpoint" — chceš
     to udělat AŽ POTÉ co dev runtime běží.

  Co odmítneš:
   - Agent chce uzavřít session a začít znovu pro develop fáze →
     "ne, pokračujeme v téhle session."
   - Agent navrhne CI/CD setup, GitHub repo → "tohle jen dev mount,
     žádný CI ještě."
   - Agent dělá příliš mnoho roundtripů než pochopí intent →
     "stačí podívat na zerops_workflow status a pokračovat."

  Co očekáváš na konci:
   - appdev service ACTIVE s běžícím Express
   - db service ACTIVE s postgres
   - GET /version endpoint vrací 200 + JSON s nějakou verzí
   - URL na ověření (subdomain)
   - Žádný stage, žádné CI/CD

notableFriction:
  - id: bootstrap-to-develop-session-handoff
    description: |
      Po dokončení bootstrap close step, agent by měl bezešvově
      přejít na develop workflow (`workflow=develop scope=[appdev]`)
      bez nového session start. Surfaces whether the close atom
      telegraphs the next-step pivot vs. agent assuming a fresh
      bootstrap is needed.
  - id: live-mount-edit-vs-git-push
    description: |
      Dev mode = SSHFS mount, agent edits in /var/www directly. Develop
      atom should make this clear so agent doesn't try to git commit +
      push (no git remote at this stage). Pure file-edit + deploy via
      `zerops_deploy`.
  - id: endpoint-scaffold-on-top-of-recipe
    description: |
      If recipe-route ran, the recipe's Express scaffold has its own
      routes file (e.g. `routes.ts` or `app.ts`). Agent should EDIT
      the existing file to add /version, not create a new router.
      Surfaces whether develop-flow atoms telegraph the on-mount edit
      pattern.
  - id: scope-narrowed-to-runtime-only
    description: |
      `zerops_workflow workflow=develop scope=["appdev"]` not
      `scope=["appdev","db"]` — db has no code to deploy. Agent
      mustn't widen scope reflexively.

---

Vytvoř mi Express API s Postgres databází na Zerops, jenom dev mode. Spusť to do eval-zcp projektu (UUID waAzEFn6SBaysG4YE4rv7A). Žádný stage, žádný CI. Až poběží, řekni mi URL — potom ti dám další prompt pro úpravu kódu.
