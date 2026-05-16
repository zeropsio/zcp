---
id: adopt-existing-standard-pair
description: |
  "Mám standard pair appdev/appstage/db v Zerops dashboardu, nastav mi
  ZCP integration" — natural-Czech adopt-route scenario. Project
  already carries a deployed Node.js standard pair seeded outside ZCP
  (no ServiceMeta yet). Agent runs bootstrap → discovers the existing
  services → picks `route="adopt"` → stamps ServiceMeta with
  `isExisting=true` and `FirstDeployedAt` from the current ACTIVE
  status. No new services created, no infra change.

  Surfaces the agent must navigate:

   1. Discover-route surfaces adopt option — services exist on the
      platform but lack ZCP ServiceMeta files. Discovery atom must
      surface `route="adopt"` with the per-service adoptServices[]
      list. Agent picks adopt, not classic or recipe.
   2. isExisting=true semantics — adopt stamps ServiceMeta with
      isExisting=true so develop workflows know NOT to treat the
      first push as a from-scratch deploy. Verify scope: every
      adopted service ends with isExisting=true in its meta.
   3. FirstDeployedAt timestamp — the platform's
      `serviceStack.lastDeployedAt` becomes ZCP's `FirstDeployedAt`
      stamp. Agent should not synthesize a current-time stamp.
   4. No infra change — adopt MUST NOT call zerops_import,
      zerops_env at project scope, or any mutation. The adopt path
      is metadata-only.
   5. Standard-pair topology preserved — appdev + appstage + db
      stays paired in ZCP's view post-adopt; develop workflows can
      then issue cross-deploy / verify against either half.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [bootstrap, adopt-route, standard-mode, node, postgres, czech-prompt, real-life]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář a v Zerops dashboardu už máš standard pair postavený
  ručně — `appdev` (nodejs@22, zeropsSetup:dev), `appstage` (nodejs@22,
  zeropsSetup:prod) a `db` (postgresql@18). Obě runtime služby běží,
  databáze taky. Ale ZCP o tom ještě "neví" — žádné ServiceMeta,
  agent by jinak myslel že je projekt prázdný.

  Tvoje preference:
   - Akceptuj defaulty agenta tam, kde je adopt-flow přirozený.
   - Pokud agent navrhne classic-route nebo recipe-route místo
     adopt, odmítni: "ne, ty služby už existují — adopt route,
     prosím."
   - Pokud agent navrhne přejmenování hostnames, odmítni: "nech
     to jak je, `appdev`/`appstage`/`db` mi vyhovují."
   - Pokud agent chce mazat / re-importovat služby pro "čistý
     start", odmítni: "ne, jenom mi tam nastav metadata, kód běží
     v pořádku."
   - Pokud se agent ptá na git remote pro adopt phase, řekni
     "později — teď chci jen metadata nastavit."

  Co odmítneš:
   - Agent zkusí `zerops_workflow workflow="develop"` aniž by
     dokončil adopt → "musíš nejdřív dokončit adopt, ať mají služby
     ServiceMeta."
   - Agent požaduje launchKey / prod token → "toto je jen adopt,
     žádná produkce."

  Co očekáváš na konci:
   - Všechny tři služby (appdev, appstage, db) mají ZCP
     ServiceMeta s `isExisting: true`.
   - `appdev` a `appstage` (runtime) mají `FirstDeployedAt`
     timestamp z `lastDeployedAt` platformy.
   - Žádné nové služby nevzniknou, žádný redeploy.
   - Develop-workflow je teď použitelný pro další iteraci na
     `appdev` (live editing přes SSH mount).

notableFriction:
  - id: discover-surfaces-adopt-option
    description: |
      Discovery atom musí dispatch agenta na `route="adopt"` když
      objeví služby bez ServiceMeta. Recipe-match / classic-route
      jsou alternativy ale adopt je správná volba pro tohle pre-state.
      Surfaces whether discovery atom prioritizes adopt over
      recipe-fit when services already exist.
  - id: isExisting-stamp-semantics
    description: |
      Adopt MUST stamp ServiceMeta s `isExisting: true` aby develop
      workflow věděl že první push není from-scratch deploy. Bez
      téhle značky se develop chová jako po greenfield bootstrap →
      špatně klasifikuje first-deploy semantics.
  - id: first-deployed-at-from-platform
    description: |
      Adopt by měl číst `serviceStack.lastDeployedAt` z platformy a
      uložit do ServiceMeta.FirstDeployedAt. Synthetic "now()" stamp
      by skreslil historii deploys pro pozdější diagnostiku
      (orphan-deploy detection, drift-against-baseline checks).
  - id: no-mutation-on-adopt
    description: |
      Adopt path MUSÍ být read-only proti platformě — žádné import,
      žádné env set, žádné scale. Agent by neměl volat mutating
      tools dokud uživatel explicitně nepožádá. Surfaces whether
      adopt atom telegraphs the metadata-only nature clearly.
  - id: pair-topology-detected
    description: |
      Standard-pair: appdev + appstage by se měly objevit jako
      jednna pair-keyed entry v adoptServices[], ne jako dvě
      nezávislé runtime. Surfaces whether adopt discovery
      respects pair semantics nebo seznam ploše vypisuje obě
      služby jako "standalone".
---

V Zerops projektu (UUID waAzEFn6SBaysG4YE4rv7A) už mám rozjetý standard pair — `appdev` a `appstage` (nodejs@22), plus `db` (postgresql@18). Všechno bylo postavené ručně přes dashboard, takže ZCP o tom zatím neví. Nastav mi prosím ZCP integration — adopt-route — ať tam mají ty služby ServiceMeta a můžu pak normálně používat develop workflow. Žádné nové služby nevytvářej, žádný redeploy. Až bude hotovo, řekni mi že je adopt dokončený.
