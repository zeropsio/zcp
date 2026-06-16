---
id: launch-to-existing-prod-project
description: |
  "Mám už předpřipravený produkční Zerops projekt, deploy do něj
  current dev/stage" — natural-Czech, live-token-injected scenario testing
  the existing-project launch path. Distinct from
  `launch-production-existing-project-token` (English/SRE persona, no live
  injection): here the persona reveals `$ZCP_E2E_EXISTING_PROJECT_ID` +
  `$ZCP_E2E_EXISTING_PROD_TOKEN` via Bash so the run hits the REAL scope-
  validation gate against a real token, and the Czech routing phrasing is
  exercised. The agent recognizes that ExistingProjectID + ExistingProdToken
  (not LaunchKey) drives the existing-project mutation by INPUT PRESENCE —
  there is no "New vs Existing" project-mode menu.

  Surfaces the agent must navigate:

   1. Existing-project path by input presence — phrases like "mám už
      předpřipravený produkční projekt", "deploy do existujícího",
      "existing prod" map to supplying `ExistingProjectID` +
      `ExistingProdToken` (NOT `LaunchKey`); the two paths are mutually
      exclusive.
   2. Token-pair Bash fetch — both `ZCP_E2E_EXISTING_PROJECT_ID` and
      `ZCP_E2E_EXISTING_PROD_TOKEN` revealed via `Bash echo $VAR`.
      Persona never embeds literals.
   3. Scope-validation gate (P-LP-12, BEFORE any mutation) — ZCP validates
      `ExistingProdToken` via `GetUserInfo`, requires it to scope to EXACTLY
      1 project, and requires that project to equal `ExistingProjectID`. A
      multi-project token, no-access, or scope mismatch refuses with a
      structured error and NO mutation. The agent must read the refusal and
      ask for a correctly scoped token, not retry the same one.
   4. Services-only import on existing project — the composed yaml carries
      NO `project:` block (the services-only endpoint rejects one — the
      project already exists). The agent should not be confused if the
      compose preview omits the project block.
   5. Pipeline-first, no project create — promoted runtimes are pipeline-
      first (P-LP-10): startWithoutCode:true, NO buildFromGit, so they land
      ACTIVE-EMPTY and NO prod build runs at launch (the first build is the
      first release tag). `productionProjectId` in the response equals the
      user-supplied `ExistingProjectID` — no fresh project is provisioned.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [launch-production, existing-project, token-scope-gate, services-only-import, pipeline-first, node, czech-prompt, token-injected, real-life, P-LP-10, P-LP-12]
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
   - Token předáváš JEDNOU; ZCP ho stage-uje jako `ZCP_LAUNCH_TOKEN`
     secret na source push service pro zbytek launch okna. Nechceš ho
     pastovat podruhé pro pozdější window ops.

  Co odmítneš:
   - Agent navrhne LaunchKey (account-wide) místo ExistingProdToken
     (project-scoped) → "ne, mám už projekt, scoped token na něj."
   - Agent požaduje tokeny v textu → "získej přes Bash, prosím."
   - Agent tvrdí že prod build "se rozjel" hned po importu → "ne,
     prod runtimes jsou ACTIVE-empty (pipeline-first), první build je
     až první release tag."

  Co očekáváš na konci:
   - Služby importované do existujícího projektu (services-only import,
     žádný `project:` block)
   - `productionProjectId` v response = `$ZCP_E2E_EXISTING_PROJECT_ID`
   - Prod runtimes jsou ACTIVE-EMPTY (startWithoutCode:true, žádný
     buildFromGit) — žádný prod build se NEspustil při launch
   - Žádný nový projekt nevznikl v org

notableFriction:
  - id: existing-vs-new-project-routing
    description: |
      Phrase "mám už předpřipravený produkční projekt" musí mapovat
      na supplying `ExistingProjectID` + `ExistingProdToken` (input
      presence), ne na `LaunchKey` path (která vytvoří nový projekt).
      Surfaces whether launch-production atoms telegraph the two-path
      distinction based on user phrasing.
  - id: scope-token-mismatch-error
    description: |
      Pokud token scope-uje na víc projektů nebo na jiný projekt než
      `ExistingProjectID`, ZCP odmítne s structured error PŘED mutací
      (P-LP-12). Agent musí pochopit error message a požádat o správný
      pár, ne re-pokoušet stejné credentials.
  - id: services-only-import
    description: |
      Existing-project import yaml nese NO `project:` block (services-
      only endpoint ho odmítne — projekt už existuje). Surfaces whether
      the agent is confused if the compose preview omits the project
      block, or treats its absence as a bug.
  - id: pipeline-first-no-build-at-launch
    description: |
      Promoted runtimes are pipeline-first (P-LP-10): startWithoutCode:
      true, NO buildFromGit → ACTIVE-EMPTY, no prod build at launch
      (first build = first release tag). Surfaces whether the agent
      accepts the empty-runtime shape or wrongly reports/awaits a "first
      build starting".
---

Mám už předpřipravený produkční Zerops projekt — UUID v env var `$ZCP_E2E_EXISTING_PROJECT_ID`, project-scoped token v `$ZCP_E2E_EXISTING_PROD_TOKEN`. Mám taky funkční Node.js dev/stage pair (`appdev` + `appstage` + `db`) v eval-zcp source projektu. Deploy current source do toho existujícího prod projektu — nech tam vytvořit služby. Pokud můj token scope-uje na něco jiného než ten projekt, odmítni — vygeneruju nový. Až bude import hotový, řekni mi jak se prod bude doručovat. Tokens si získej přes Bash.
