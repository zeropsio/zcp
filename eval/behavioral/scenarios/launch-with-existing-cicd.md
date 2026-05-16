---
id: launch-with-existing-cicd
description: |
  "Mám funkční dev/stage pair s nastaveným git push + GitHub Actions,
  udělej z toho produkční projekt — chci stejný Actions setup pro prod"
  — natural-Czech scenario testing the launch-production happy path with
  pre-existing CICD. Karel's primary ask: agent should recognize
  launch-production intent (not bootstrap), pull LaunchKey via Bash
  from `$ZCP_E2E_LAUNCH_KEY`, compose the launch bundle, mutate via
  real platform, and emit cicd handoff atoms for prod-side Actions
  setup.

  Surfaces the agent must navigate:

   1. Launch-production intent recognition — phrases like "udělej z toho
      produkční projekt", "go live", "promote to production" must
      route to `workflow=launch-production`, NOT a fresh bootstrap.
   2. Source pair shape — source dev/stage pair carries
      BuildIntegration=actions in ServiceMeta (from prior CICD setup).
      Agent surfaces this as "existing CICD will be replicated for prod"
      rather than treating it as new infrastructure.
   3. LaunchKey via Bash pattern — same persona pattern as PAT scenario:
      agent calls `Bash echo $ZCP_E2E_LAUNCH_KEY` when reaching the
      ready-to-launch → launching transition. Token never in persona
      literal.
   4. Source-control gate — source zerops.yaml needs `setup: prod`
      block (or ProdSetupNameOverride). For this scenario the seed
      fixture's source is the `zerops-recipe-apps/nodejs-hello-world-app`
      repo whose zerops.yaml has both `setup: dev` and `setup: prod` —
      gate should pass cleanly.
   5. Cleanup on completion — scenario must delete the new prod project
      at teardown. The eval cleanup hook only wipes services in the
      SOURCE project (eval-zcp); the new prod project is independent.
      Scenario MUST flag this for operator cleanup or include explicit
      delete in the agent's final actions.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, launch-production, cicd-actions, node, czech-prompt, token-injected, real-life, requires-cleanup]
area: launch-production
requiredEnvVars:
  - ZCP_E2E_LAUNCH_KEY
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
    - hand-edited launch-key
userPersona: |
  Máš funkční Node.js dev/stage pair na Zerops s nastaveným git push
  deploy + GitHub Actions buildy (předchozí scenario `git-push-setup-
  then-actions` to nastavil — předpokládáme že source ServiceMeta nese
  GitPushState=configured + BuildIntegration=actions). Teď to chceš
  udělat produkční — nový samostatný Zerops projekt s production-shaped
  buildy, stejným source code + stejnou CICD wiring.

  LaunchKey je v env var `$ZCP_E2E_LAUNCH_KEY` (account-wide one-shot
  Zerops API token, generovaný v dashboardu → Settings → Access
  Tokens). Když agent požádá o LaunchKey, pošli BASH PŘÍKAZ:
  `echo $ZCP_E2E_LAUNCH_KEY`. NIKDY token nepiš v textu.

  Tvoje preference:
   - Production project name: "eval-prod-test" nebo cokoli krátkého.
   - Region: default (eu-central).
   - Akceptuj defaults pro HA promotion managed deps (postgres → HA).
   - Pokud agent navrhne keep db NON_HA, akceptuj — je to dev/stage
     postgres, prod můžeš začít na NON_HA.
   - Pokud agent navrhne fresh bootstrap místo launch-production,
     odmítni: "ne, mám už dev/stage pair, chci to PROMOTOVAT, ne
     postavit znovu."
   - Pokud agent po launch chce nastavit Actions na prod (Phase 6b
     cicd handoff), akceptuj — to je čisté pokračování source CICD.
   - **DŮLEŽITÉ**: Po dokončení launch a verify, požaduj smazání
     prod projektu: "ten prod projekt je jen test, smaž ho prosím
     hned (`zerops_delete` nebo přes dashboard) — nechci ho mít
     trvale viset v org."

  Co odmítneš:
   - Agent zapomene na cleanup nového prod projektu → "smaž ten prod
     projekt, jen jsem to chtěl ozkoušet."
   - Agent požaduje token v textu, ne přes Bash → "získej přes
     `echo $ZCP_E2E_LAUNCH_KEY`."
   - Agent chce launch-production bez source git push setup →
     "source pair MÁ git push setup, nebij na fresh bootstrap."

  Co očekáváš na konci:
   - Nový prod projekt vytvořený (např. `eval-prod-test`) s
     analogickou pair shape
   - Prod runtime BUILD pipeline alespoň startuje (build process
     past WAITING_TO_BUILD)
   - cicd handoff atom vidí v guidance (info pro prod-side Actions
     setup) NEBO SkipPipelineSetup=true pokud user opted out
   - **Prod projekt smazaný** před dokončením scenario — žádný
     orphan v Muad org

notableFriction:
  - id: launch-production-intent-recognition
    description: |
      Phrase "udělej z toho produkční projekt" musí dispatchovat na
      `workflow=launch-production`, ne `workflow=bootstrap` nebo
      `recipe`. Surfaces whether the routing-table atom telegraphs
      the trigger phrases (Czech included).
  - id: launchkey-bash-fetch-pattern
    description: |
      Stejně jako GitHub PAT v sister scenario, LaunchKey se předává
      přes Bash `echo $ZCP_E2E_LAUNCH_KEY`. Token nesmí být v
      transcript prose. Surfaces same pattern enforcement.
  - id: cicd-handoff-atom-emit
    description: |
      Po `launched` status by guidance měla obsahovat cicd handoff
      atom pro prod-side Actions setup — buď "configure-dashboard"
      blocker s deeplinkem, nebo "configured" pokud platform sama
      detekuje source integration. Surfaces whether
      pipeline-blocker atoms reach the agent.
  - id: orphan-project-cleanup
    description: |
      Eval framework cleanup hook deletes services in eval-zcp ONLY.
      The new prod project is a separate Zerops project that the
      framework doesn't manage. Scenario MUST end with explicit
      delete; otherwise the Muad org accumulates dead prod projects.
      Surfaces whether agent recognizes this org-hygiene gap and
      proposes cleanup proactively.
  - id: source-shape-preservation
    description: |
      Source pair carries BuildIntegration=actions in ServiceMeta;
      launch-production should EITHER replicate (recommend Actions
      for prod-side too) OR ack the source state in its guidance.
      Surfaces whether the launch atoms read source ServiceMeta vs.
      operating as if source had no CICD configured.
---

Mám na Zerops Node.js dev/stage pair (`appdev` + `appstage` + `db`) s nastaveným git push deploy na `https://github.com/krls2020/eval2` a GitHub Actions buildy. Teď z toho chci udělat produkční projekt — samostatný nový Zerops projekt, stejný source repo, ideálně stejný Actions setup pro prod. LaunchKey mám v `$ZCP_E2E_LAUNCH_KEY` (account-wide, getči ho přes Bash). Až bude prod projekt vytvořený a první build běží, řekni mi to. Pak ten prod projekt rovnou smaž — je to jen test.
