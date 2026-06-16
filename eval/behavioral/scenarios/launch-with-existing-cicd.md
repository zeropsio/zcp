---
id: launch-with-existing-cicd
description: |
  "Mám funkční dev/stage pair s nastaveným git push + GitHub Actions,
  udělej z toho produkční projekt — chci stejný Actions setup pro prod"
  — natural-Czech scenario testing the launch-production NEW-project path
  with a source pair that already declares BuildIntegration=actions. The
  primary ask: the agent recognizes launch-production intent (not
  bootstrap), pulls the launchKey via Bash from `$ZCP_E2E_LAUNCH_KEY`,
  hands it to ZCP ONCE on the mutation call, and reads the DERIVED Actions
  delivery family from the launched response rather than expecting a
  delivery-method prompt.

  Surfaces the agent must navigate:

   1. Launch-production intent recognition — phrases like "udělej z toho
      produkční projekt", "go live", "promote to production" must
      route to `workflow=launch-production`, NOT a fresh bootstrap.
   2. Derived Actions delivery family — the source pair carries
      BuildIntegration=actions in ServiceMeta, so the launched response
      DERIVES deliveryFamily=actions and emits the prodCd actions track
      (`.github/workflows/zerops-prod.yml`, tag-triggered raw `zcli push`,
      `ZEROPS_TOKEN_PROD` repo secret). The agent reads the derived family
      — there is NO delivery-method menu to pick from.
   3. Pipeline-first prod import — the composed prod import is pipeline-first
      (P-LP-10): promoted runtimes carry startWithoutCode:true and NO
      buildFromGit. The launched runtimes are ACTIVE-EMPTY; the prod build
      does NOT start at launch. The FIRST production build arrives as the
      first release tag through the prod pipeline (`git tag v0.1.0 &&
      git push --tags`). The agent must NOT treat the empty runtimes as a
      failed/stuck build.
   4. Single-token lifecycle — the launchKey crosses the conversation ONCE
      (on the mutation call). ZCP stages it as the prod push service's
      `ZCP_LAUNCH_TOKEN` secret; `prodCd.secret.command` conveys it into the
      `ZEROPS_TOKEN_PROD` GitHub repo secret secret-to-secret over ssh — the
      user does NOT paste the prod token a second time.
   5. Cleanup on completion — the new prod project is a separate Zerops
      project the eval cleanup hook (services in eval-zcp only) does NOT
      manage. The scenario MUST end by tearing down that prod PROJECT via
      the launch lifecycle's own reset — `zerops_workflow
      workflow=launch-production action=reset` (deletes the prod project +
      clears launch state, using the staged launch token), or `zcli project
      delete <productionProjectId>`. NOT `zerops_delete` — that deletes a
      single SERVICE by hostname, not a project. Otherwise the org
      accumulates dead prod projects.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, launch-production, cicd-actions, pipeline-first, single-token, node, czech-prompt, token-injected, real-life, requires-cleanup, P-LP-10, P-LP-14]
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
  udělat produkční — nový samostatný Zerops projekt, stejný source code,
  a stejná Actions CICD wiring pro prod.

  LaunchKey je v env var `$ZCP_E2E_LAUNCH_KEY` (account-wide Zerops API
  token s project-creation permission, generovaný v dashboardu →
  Settings → Access Tokens). Když agent požádá o LaunchKey, pošli BASH
  PŘÍKAZ: `echo $ZCP_E2E_LAUNCH_KEY`. NIKDY token nepiš v textu. Token
  předáváš JEDNOU — na launch (mutation) call; pak ho ZCP stage-uje jako
  `ZCP_LAUNCH_TOKEN` secret na prod push service pro zbytek launch okna.

  Tvoje preference:
   - Production project name: "eval-prod-test" nebo cokoli krátkého.
   - Region: default (eu-central).
   - Akceptuj defaults pro HA promotion managed deps (postgres → HA).
   - Pokud agent navrhne keep db NON_HA, akceptuj — je to dev/stage
     postgres, prod můžeš začít na NON_HA.
   - Pokud agent navrhne fresh bootstrap místo launch-production,
     odmítni: "ne, mám už dev/stage pair, chci to PROMOTOVAT, ne
     postavit znovu."
   - Source pair má BuildIntegration=actions, takže čekáš že ZCP
     ODVODÍ Actions delivery pro prod (prodCd actions track) — NEbudeš
     vybírat delivery method z menu. Pokud se ptá na method, odmítni:
     "source má Actions, prod delivery se z toho má odvodit."
   - Nechceš pastovat prod token podruhé pro repo secret —
     `prodCd.secret.command` ho převede z `ZCP_LAUNCH_TOKEN` do
     `ZEROPS_TOKEN_PROD` secret-to-secret přes ssh.
   - **DŮLEŽITÉ**: Po dokončení launch a verify, požaduj smazání
     prod projektu přes launch lifecycle reset: "ten prod projekt je
     jen test, smaž ho prosím hned — `zerops_workflow
     workflow=launch-production action=reset` (smaže prod projekt +
     vyčistí launch state) nebo `zcli project delete`. Pozor: ne
     `zerops_delete`, ten maže jednu SERVICE podle hostname, ne celý
     projekt."

  Co odmítneš:
   - Agent zapomene na cleanup nového prod projektu → "smaž ten prod
     projekt, jen jsem to chtěl ozkoušet."
   - Agent požaduje token v textu, ne přes Bash → "získej přes
     `echo $ZCP_E2E_LAUNCH_KEY`."
   - Agent chce launch-production bez source git push setup →
     "source pair MÁ git push setup, nebij na fresh bootstrap."
   - Agent tvrdí že prod build "se rozjel" hned po launch → "ne,
     prod runtimes jsou ACTIVE-empty, první build je až první release
     tag — to je pipeline-first."
   - Agent chce po mně launchKey podruhé → "token jsem dal jednou,
     je staged jako `ZCP_LAUNCH_TOKEN`."

  Co očekáváš na konci:
   - Nový prod projekt vytvořený (např. `eval-prod-test`) s
     analogickou pair shape
   - Prod runtimes jsou ACTIVE-EMPTY (startWithoutCode:true, žádný
     buildFromGit) — žádný prod build se NEspustil při launch; první
     build přijde jako první release tag přes prod pipeline
   - Launched response nese DERIVED deliveryFamily=actions + prodCd
     actions track (zerops-prod.yml, ZEROPS_TOKEN_PROD repo secret) +
     mandatory launch-delete-key atom (zavření launch okna přes
     confirm-production)
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
      transcript prose, a předává se JEDNOU na mutation call. Surfaces
      same pattern enforcement plus the single-token discipline.
  - id: derived-actions-delivery-family
    description: |
      Source pair carries BuildIntegration=actions, so the launched
      response DERIVES deliveryFamily=actions and emits the prodCd
      actions track — no delivery-method prompt. Surfaces whether the
      agent reads the derived family + prodCd steps or stalls waiting
      for a choice menu.
  - id: pipeline-first-no-build-at-launch
    description: |
      The prod import is pipeline-first (P-LP-10): runtimes start
      startWithoutCode:true with NO buildFromGit, so they are
      ACTIVE-EMPTY and NO prod build runs at launch — the first build
      is the first release tag. Surfaces whether the agent accepts the
      empty-runtime shape or wrongly reports/awaits a "first build
      starting".
  - id: single-token-no-repaste
    description: |
      The launchKey crosses the conversation once. ZCP stages it as
      `ZCP_LAUNCH_TOKEN`; `prodCd.secret.command` conveys it into the
      `ZEROPS_TOKEN_PROD` repo secret secret-to-secret over ssh.
      Surfaces whether the agent re-asks the user to paste the token
      for the repo secret or understands the staged-secret conveyance.
  - id: orphan-project-cleanup
    description: |
      Eval framework cleanup hook deletes services in eval-zcp ONLY.
      The new prod project is a separate Zerops project that the
      framework doesn't manage. Scenario MUST end with explicit
      delete; otherwise the Muad org accumulates dead prod projects.
      Surfaces whether agent recognizes this org-hygiene gap and
      proposes cleanup proactively.
---

Mám na Zerops Node.js dev/stage pair (`appdev` + `appstage` + `db`) s nastaveným git push deploy na `https://github.com/krls2020/eval2` a GitHub Actions buildy. Teď z toho chci udělat produkční projekt — samostatný nový Zerops projekt, stejný source repo, a Actions delivery pro prod. LaunchKey mám v `$ZCP_E2E_LAUNCH_KEY` (account-wide, getči ho přes Bash, dám ti ho jednou). Až bude prod projekt vytvořený, řekni mi jak se prod bude doručovat a jak zavřu launch okno. Pak ten prod projekt rovnou smaž — je to jen test.
