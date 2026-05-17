---
id: launch-failure-build-stuck
description: |
  "Source pair má rozbitý zerops.yaml (build:base nepoužitelný),
  zkus to deploynout do produkce — pak diagnostikuj a navrhni fix" —
  natural-Czech failure-recovery scenario. Pre-state: dev/stage pair
  exists with a deliberately-broken `setup: prod` (e.g. `build.base:
  bogus@99` or unsatisfiable buildCommands). Agent launches via
  LaunchKey; CreateAndImportProject succeeds (services created);
  first deploy poll catches FAILED build → launch-production
  surfaces first-deploy-failed blocker with the recovery hints from
  S2.2.2 (retry-via-push + dashboard URL + delete fallback).

  Surfaces the agent must navigate:

   1. Recognize structured failure response — blocker `id=first-deploy-failed`
      carries `Recovery`-style guidance (retry-via-push + dashboard URL)
      in the message. Agent must READ the guidance instead of
      interpreting "FAILED" as a generic catastrophe.
   2. Diagnostic gather BEFORE retry — agent should pull build logs
      (via `zerops_logs source=build` if reachable, OR via deep-link
      dashboard inspection), surface what failed (build pipeline,
      bogus base detected, etc.) to the user, propose a fix.
   3. Orphan-project cleanup — same constraint as #9: agent must
      delete the half-launched prod project (zcli + token swap) so
      Muad org doesn't accumulate broken targets.
   4. Retry-via-push pattern verification — if agent applies fix to
      source yaml + pushes, the platform picks up new ref and
      re-triggers build. Verify this loop is articulable from the
      atom guidance.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: scripts/break-prod-setup.sh
tags: [launch-production, failure-recovery, czech-prompt, token-injected, real-life, requires-cleanup]
area: launch-production-recovery
requiredEnvVars:
  - ZCP_E2E_LAUNCH_KEY
retrospective:
  promptStyle: briefing-future-agent
verification:
  noFailedProcesses: false
  retrospectiveMustNotMention:
    - YJQTh.
userPersona: |
  Máš funkční dev/stage pair na Zerops (`appdev` + `appstage` + `db`).
  POZOR: zerops.yaml na source pair má vědomě zpackaný `setup: prod`
  blok (build pipeline neproveditelný — neexistující build.base nebo
  podobné). Tohle je úmyslné — chceš si vyzkoušet failure-recovery
  flow: launch-production → build FAILED → diagnostic + fix proposal.

  LaunchKey je v env var `$ZCP_E2E_LAUNCH_KEY` — získej přes Bash.

  Tvoje preference:
   - Production project name: "eval-failure-test" nebo cokoli
     short-lived.
   - Akceptuj defaults pro env-classification.
   - Pokud agent nechce launch protože "zerops.yaml má problém",
     řekni: "spustíme to schválně — chci vidět recovery flow."

  Po failure:
   - Agent musí surface failure attribution (build vs runtime, base
     unknown vs network, atd.).
   - Agent musí navrhnout fix (`build.base` na existující runtime,
     etc.).
   - Smaž ten prod projekt: "není potřeba ho mít, recovery jsme si
     vyzkoušeli."

  Co odmítneš:
   - Agent retries blindly without diagnostic.
   - Agent skipne cleanup (orphan projekt v org).

  Co očekáváš na konci:
   - Prod projekt vytvořený + první build vstoupil do FAILED stavu
   - Response carries first-deploy-failed blocker s retry guidance
   - Agent jasně diagnostikoval příčinu (build.base, syntax, atd.)
   - Agent navrhl konkrétní fix
   - Prod projekt smazaný

notableFriction:
  - id: first-deploy-failed-blocker-readability
    description: |
      Blocker message obsahuje retry-via-push příkaz + dashboard URL.
      Agent by měl prezentovat tuhle guidance user-friendly, ne jen
      "deploy failed" prose. Surfaces whether S2.2.2 enhanced message
      actually lands in agent's response.
  - id: diagnostic-before-retry
    description: |
      Agent nesmí blindly retry. Musí pull build logs / events nebo
      navrhnout dashboard inspect. Surfaces whether failure-recovery
      atoms (post-S2.2.2) telegraph "diagnose first" pattern.
  - id: orphan-cleanup-after-failure
    description: |
      Stejné jako #9 — agent musí delete prod projekt. Failure scenario
      činí cleanup ještě důležitější (org se zaplní rozbitými targety).
  - id: fix-proposal-actionable
    description: |
      Agent by měl navrhnout KONKRÉTNÍ změnu (edit `setup: prod`
      build.base na `nodejs@22`, atd.), ne obecné "fix the yaml".
      Surfaces whether agent's diagnostic skills extend to fix
      proposal vs. handing back to user.
---

Mám na Zerops Node.js dev/stage pair (`appdev` + `appstage` + `db`). Source zerops.yaml ale má vědomě rozbitý `setup: prod` blok — chci si vyzkoušet failure-recovery flow. Spusť launch-production přes LaunchKey (`$ZCP_E2E_LAUNCH_KEY` přes Bash), nech to FAILED na build kroku, pak diagnostikuj a navrhni mi fix. Až mi řekneš příčinu + návrh, ten prod projekt smaž — je to jen test.
