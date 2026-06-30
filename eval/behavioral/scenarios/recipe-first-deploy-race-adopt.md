---
id: recipe-first-deploy-race-adopt
description: |
  "Vytvořil jsem v Zerops projekt z recipe, napoj mi na to ZCP" — the
  recipe-first-deploy RACE. The project is seeded with seed=building: the
  fixture imports a buildFromGit Node.js pair (appdev/appstage) + Postgres
  and the harness boots the agent WHILE the first build is still in flight.
  So the agent's first `zerops_discover` lands on services that are actively
  building yet read status=READY_TO_DEPLOY.

  Pre-fix, discover steered "adopt now" on READY_TO_DEPLOY runtimes and the
  agent adopted mid-build (premature — the first deploy is still running).
  Post-fix, discover carries a per-service `activity` object while a build/
  deploy is live, the adopt-candidate warning becomes a "wait until it
  settles, THEN adopt" steer, and route=adopt HARD-REFUSES a busy target.

  Surfaces the agent must navigate:

   1. Reads `activity`, not just status — appdev/appstage read
      status="READY_TO_DEPLOY" but carry activity={action:"build"|"deploy",
      status:"BUILDING"|"DEPLOYING", processId:...}. The agent must treat a
      service carrying `activity` as NOT idle, even though its status looks
      adoptable.
   2. Waits instead of adopting mid-build — on the busy steer the agent
      should re-run `zerops_discover` (or watch `zerops_events
      serviceHostname=appdev`) until activity clears + status reaches ACTIVE,
      THEN adopt. An immediate adopt is the bug.
   3. Adopt gate is a backstop, not the plan — if the agent tries to adopt
      while busy anyway, route=adopt refuses with ADOPT_TARGET_BUSY naming the
      processId + the wait/cancel escape. The agent should read the refusal
      and wait, not fight it (no override reflex, no zcli escape).
   4. Adopts the settled pair correctly — once both halves are ACTIVE, the
      agent adopts appdev/appstage as a standard pair (explicit plan=[...] for
      the same-stack pair) + db as EXISTS. No new services, no redeploy.
   5. NEVER escapes to zcli / raw API — the entire point is that ZCP carries
      the live-activity fact. Any `zcli` / direct-API fallback is a FAILURE to
      root-cause, regardless of whether the task completed.

seed: building
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [bootstrap, adopt-route, activity, build-in-flight, race, node, postgres, czech-prompt, real-life]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář. Před chvílí jsi v Zerops dashboardu vytvořil nový projekt
  z recipe (Node.js appdev/appstage + Postgres) a zrovna se to poprvé
  nasazuje. Chceš na ten projekt napojit ZCP, ať ho máš pod správou a
  můžeš pak normálně vyvíjet.

  Tvoje preference:
   - Necháváš agenta řídit postup; akceptuj jeho defaulty tam, kde dávají
     smysl.
   - Pokud agent zjistí, že se služby zrovna buildí/deployují a navrhne
     POČKAT než to doběhne — souhlas: "jasně, počkej až to dojede, pak
     to napoj."
   - Pokud agent chce adoptovat hned a ty máš pocit, že se to ještě
     nasazuje, řekni: "ještě se to snad nasazuje, ne? počkej až bude
     hotovo."
   - Hostnames `appdev`/`appstage`/`db` ti vyhovují — nepřejmenovávat.
   - Nechceš nové služby ani redeploy — kód běží z recipe, jen to napojit.
   - Na produkci/launchKey se neptej — tohle je jen napojení dev projektu.

  Co odmítneš:
   - Agent sáhne po `zcli` / přímém volání API místo ZCP toolů → "ne,
     používej prosím ty Zerops nástroje, co máš, ne zcli."
   - Agent navrhne smazat / re-importovat služby pro "čistý start" → "ne,
     nic nemaž, ten build zrovna běží."

  Co očekáváš na konci:
   - Agent počkal, než první deploy doběhl (služby ACTIVE), a TEPRVE PAK
     adoptoval.
   - appdev/appstage/db mají ZCP ServiceMeta (isExisting), žádné nové
     služby, žádný redeploy.
   - Develop workflow je použitelný pro další iteraci.
   - Nikde se neobjevil `zcli` ani přímé volání platform API.

notableFriction:
  - id: reads-activity-over-status
    description: |
      appdev/appstage read status="READY_TO_DEPLOY" while building. The fix
      adds a per-service `activity` object discover carries whenever a build/
      deploy is live. Surfaces whether the agent reads `activity` and treats
      the service as busy, or trusts the resting status and adopts mid-build.
  - id: waits-then-adopts
    description: |
      The busy adopt-candidate steer says "wait until RUNNING/ACTIVE (re-run
      discover / watch events), then adopt." Surfaces whether the agent
      actually polls to settle before adopting, or adopts on the first
      discover. The correct loop is bounded by the build completing.
  - id: adopt-gate-not-fought
    description: |
      If the agent adopts while busy, route=adopt returns ADOPT_TARGET_BUSY
      naming the live processId + the watch/cancel escape. Surfaces whether
      the agent reads the refusal and waits, vs. reaching for override /
      confirmDestructive / zcli to force past it.
  - id: no-zcli-escape
    description: |
      The whole feature exists so ZCP carries the live-activity fact through
      the zerops_* tools. Any fallback to `zcli` / direct platform API is a
      ZCP gap to root-cause — a louder failure than the task failing cleanly.
  - id: pair-adopt-after-settle
    description: |
      Once settled, appdev+appstage are a same-stack pair → discover steers an
      explicit plan=[...] adopt (not a bare scope). Surfaces whether the agent
      submits the pair plan + db-as-EXISTS, creating no new services.
---

Vytvořil jsem si teď v Zerops dashboardu nový projekt z recipe — Node.js (`appdev`/`appstage`) a Postgres (`db`). Napoj mi na ten projekt prosím ZCP, ať to mám pod správou a můžu pak normálně vyvíjet. Žádné nové služby nevytvářej. Až to bude hotové, dej vědět.
