---
id: bootstrap-recipe-static-simple
description: Static runtime greenfield via recipe route — Vue + Vite SPA in simple mode. Plní Tier-1+2 gap (static runtime class má dnes nulové greenfield pokrytí), exercises develop-static-workflow + dist/-pattern deployFiles (DM-3) + nginx auto-start (no zerops_dev_server).
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  # Greenfield bootstrap+develop = 6-8 workflow calls (start discovery +
  # start route=recipe + complete×2-3 + start workflow=develop + close).
  # Calibrated post-first-run.
  workflowCallsMin: 6
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    # Recipe route je celý test — static runtime gap dnes vyplňuje právě
    # vue-static-hello-world recipe. Pin.
    - '"workflow":"bootstrap"'
    - '"route":"recipe"'
    - '"recipeSlug":"vue-static-hello-world"'
  forbiddenPatterns:
    # Classic route by ignoroval recipe match — to není intent.
    - '"route":"classic"'
    # Static runtime nepoužívá zerops_dev_server (nginx auto-startuje).
    # Pokud agent zavolá dev_server na static service, znamená to že vzal
    # špatnou guidance.
    - "zerops_dev_server"
  requireAssessment: true
  finalUrlStatus: 200
  # Simple mode = single hostname; agent picks `app` per recipe template.
  finalUrlHostname: app
followUp:
  - "Proč jsi vybral recipe route místo classic? Jaký byl confidence score na vue-static-hello-world v discovery response?"
  - "Jak vypadal `deployFiles` v zerops.yaml (literál) — a proč je to `dist/~` a ne `dist/` nebo `[.]`? Kdo strip-nul prefix a kdo by ne?"
  - "Spustil jsi `zerops_dev_server`? Pokud ne, proč ne pro static runtime — jak se start liší od dynamic runtime (Node, Bun, Go)?"
---

# Úkol

Vytvoř jednoduchou Vue 3 + Vite + TypeScript SPA nasazenou na Zerops jako
static site.

Aplikace:

- Landing page se 3 cards (`Bootstrap`, `Develop`, `Deploy`) — každá karta
  má nadpis a krátký popis (~2 věty), styling libovolný.
- Routing přes Vue Router (3 routes: `/`, `/about`, `/credits`).
- Žádná databáze, žádné API volání — čistě client-side SPA.
- Build-time env var `VITE_APP_ENV` zobrazený někde v UI (např. footer).

Verify: `GET /` na public subdomain URL vrátí HTTP 200 s renderovanou
HTML (skutečný `<div id="app">` content, ne jenom prázdný shell).

Bootstrap + develop flow standardní cestou (recipe route je preferovaná —
existuje vue-static-hello-world template).
