---
id: launch-to-existing-prod-project
description: |
  "Mám už předpřipravený produkční Zerops projekt, deploy do něj
  current dev/stage" — natural-Czech scenario testing the Phase 2c
  existing-project launch path. Persona reveals
  `$ZCP_E2E_EXISTING_PROJECT_ID` + `$ZCP_E2E_EXISTING_PROD_TOKEN` via
  Bash. Agent recognizes that ExistingProjectID + ExistingProdToken
  (not LaunchKey) drives the existing-project mutation path.

  Surfaces the agent must navigate:

   1. Existing-project path recognition — phrases like "mám už
      předpřipravený produkční projekt", "deploy do existujícího",
      "existing prod" must route to `ExistingProjectID` +
      `ExistingProdToken` inputs (NOT `LaunchKey`).
   2. Token-pair Bash fetch — both `ZCP_E2E_EXISTING_PROJECT_ID` and
      `ZCP_E2E_EXISTING_PROD_TOKEN` revealed via `Bash echo $VAR`.
      Persona never embeds literals.
   3. Scope-token-permission gate — `ExistingProdToken` is project-
      scoped; ZCP validates scope before mutation. If the token's
      project ID doesn't match `ExistingProjectID`, the gate refuses
      with a structured error.
   4. Service-only import on existing project — uses
      `PostProjectServiceStackImport` (rejects yaml with `project:`
      block). Bundle composer skips the project block; agent should
      not be confused if the compose preview elides it.
   5. No new project created — `productionProjectId` in the response
      matches the user-supplied `ExistingProjectID`. Agent must NOT
      treat this as a fresh project provisioning.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, existing-project, node, czech-prompt, token-injected, real-life]
area: launch-production
requiredEnvVars:
  - ZCP_E2E_EXISTING_PROJECT_ID
  - ZCP_E2E_EXISTING_PROD_TOKEN
retrospective:
  promptStyle: briefing-future-agent
verification:
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      type: nodejs@*
    - hostname: appstage
      status: [ACTIVE]
      type: nodejs@*
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*
  noFailedProcesses: true
  retrospectiveMustNotMention:
    - YJQTh.
    - github_pat_
    - ghp_
userPersona: |
  Máš funkční Node.js dev/stage pair (`appdev`, `appstage`, `db`) na
  Zerops. Cíl: deploy current source do JIŽ EXISTUJÍCÍHO produkčního
  Zerops projektu (ne vytvořit nový). Pre-state je nastavený mimo
  scenario — produkční projekt už existuje v Muad org s prázdnou
  service-stack listou.

  Máš tyhle credentials připravené:
   - `$ZCP_E2E_EXISTING_PROJECT_ID` — UUID existujícího produkčního
     projektu
   - `$ZCP_E2E_EXISTING_PROD_TOKEN` — project-scoped Zerops API
     token pro ten konkrétní projekt (generated v dashboardu →
     Settings → Access Tokens Management → "scoped to this project")

  Když agent požádá o ProjectID nebo Token, pošli BASH PŘÍKAZ:
  `echo $ZCP_E2E_EXISTING_PROJECT_ID` nebo `echo $ZCP_E2E_EXISTING_PROD_TOKEN`.
  NIKDY tokens nepiš v textu.

  Tvoje preference:
   - Akceptuj defaults pro env-classification (auto-secret pro keys,
     infrastructure pro DB_* refs).
   - Pokud agent navrhne nový projekt (LaunchKey path), odmítni:
     "ne, projekt už existuje, použij ExistingProjectID + 
     ExistingProdToken."
   - Pokud agent zmate scope token s account-wide LaunchKey, odmítni:
     "token je SCOPED na ten konkrétní projekt — nepoužívej ho jako
     account-wide LaunchKey."

  Co odmítneš:
   - Agent navrhne LaunchKey (account-wide) místo ExistingProdToken
     (project-scoped) → "ne, mám už projekt, scoped token na něj."
   - Agent požaduje tokeny v textu → "získej přes Bash, prosím."

  Co očekáváš na konci:
   - Služby importované do existujícího projektu
   - `productionProjectId` v response = `$ZCP_E2E_EXISTING_PROJECT_ID`
   - První build na prod-side aspoň startuje (past WAITING_TO_BUILD)
   - Žádný nový projekt nevznikl v org

notableFriction:
  - id: existing-vs-new-project-routing
    description: |
      Phrase "mám už předpřipravený produkční projekt" musí dispatch
      na `ExistingProjectID` + `ExistingProdToken` path, ne na
      `LaunchKey` path (která vytvoří nový projekt). Surfaces whether
      launch-production atoms telegraph the two-path distinction
      based on user phrasing.
  - id: scope-token-mismatch-error
    description: |
      Pokud token nematchuje project ID, ZCP odmítne s structured
      error. Agent musí pochopit error message a požádat o správný
      pár, ne re-pokoušet stejné credentials.
  - id: post-import-no-project-create
    description: |
      Agent musí pochopit že happy-path response je "services
      imported into existing project" — žádný nový projekt vytvořený.
      Surfaces whether `productionProjectId` field carries the
      EXISTING projectID a agent tomu rozumí.
---

Mám už předpřipravený produkční Zerops projekt — UUID v env var `$ZCP_E2E_EXISTING_PROJECT_ID`, project-scoped token v `$ZCP_E2E_EXISTING_PROD_TOKEN`. Mám taky funkční Node.js dev/stage pair (`appdev` + `appstage` + `db`) v eval-zcp source projektu. Deploy current source do toho existujícího prod projektu — nech tam vytvořit služby a vyhotov první deploy. Až bude prod-side build startovat, řekni mi to. Tokens si získej přes Bash.
