---
id: launch-failure-build-stuck
description: |
  "Source pair má rozbitý zerops.yaml (build:base nepoužitelný),
  zkus to deploynout do produkce — pak diagnostikuj a navrhni fix" —
  natural-Czech recovery scenario. Pre-state: dev/stage pair exists
  with a deliberately-broken `setup: prod` build.base (preseed script
  rewrites it to `nodejs@99-deliberately-bogus` between fixture seed
  and agent run).

  **Scenario name is historical.** The original design assumed
  `launch-production` would auto-build at launch time and the broken
  yaml would FAIL the build → trigger S2.2.2 `first-deploy-failed`
  blocker. **launch-production v1 (Path B) doesn't auto-build** —
  the first prod build only fires once the user wires up CD in the
  dashboard and pushes a tag (spec §4.5). So this scenario can NEVER
  fire S2.2.2 live; the retry-via-push surface stays unit-tested
  only (`TestLaunchFirstDeployFailedResponse_EmbedsRetryGuidance`).

  What this scenario ACTUALLY exercises (and what the agent in suite
  20260517-074452 successfully demonstrated):

   1. **Diagnostic-from-source-state** — agent reads broken yaml
      directly from source `/var/www/zerops.yaml` (after bootstrap-
      adopt completes and SSHFS mount materializes). The broken
      `build.base: nodejs@99-deliberately-bogus` in `setup: prod`
      is observable without ever triggering a build.
   2. **Concrete fix proposal** — agent surfaces the specific yaml
      change needed (`base: nodejs@22` or whichever real runtime),
      not a vague "fix the yaml".
   3. **Cleanup of orphan target project** — even when the launch
      "succeeds" without ever building, the new target project
      exists in the org and must be deleted (zcli auth-swap dance).
   4. **Expectation calibration** — agent should NOT promise "I'll
      notify you when the build fails" because launch-production
      returns synchronously after `status=launched` without ever
      attempting a build. Honest framing for the user is "the
      failure would surface on the first tag-push after dashboard
      pipeline setup, not now".

  When Path A launch-production with auto-build lands (future), this
  scenario can be promoted back to its original "build FAILED → S2.2.2
  retry guidance" shape. Until then it's a diagnostic-skills test.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: scripts/break-prod-setup.sh
tags: [launch-production, diagnostic-skills, czech-prompt, token-injected, real-life, requires-cleanup, design-mismatch-historical]
area: launch-production-recovery
requiredEnvVars:
  - ZCP_E2E_LAUNCH_KEY
retrospective:
  promptStyle: briefing-future-agent
verification:
  noFailedProcesses: true
  retrospectiveMustNotMention:
    - YJQTh.
    - github_pat_
    - ghp_
userPersona: |
  Máš funkční dev/stage pair na Zerops (`appdev` + `appstage` + `db`).
  POZOR: zerops.yaml na source pair má vědomě zpackaný `setup: prod`
  blok (build.base je `nodejs@99-deliberately-bogus`). Tohle je
  úmyslné — chceš si vyzkoušet diagnostic flow.

  LaunchKey je v env var `$ZCP_E2E_LAUNCH_KEY` — získej přes Bash.

  Tvoje očekávání ohledně failure timing:
   - Víš že launch-production v1 NEDĚLÁ auto-build. Build pipeline
     vystartuje až po push tagu po dashboard CD setup. Takže
     "FAILED build během launch call" se tady NESTANE.
   - Místo toho chceš aby agent diagnostikoval rozbitý yaml ze
     source state PŘED launch-em nebo z dashboard inspekce PO
     launch-em a navrhl konkrétní fix.
   - Pokud agent slíbí "dám ti vědět až build fails", oprav ho:
     "launch-production v1 nedělá auto-build, build se spustí až
     po tag push s CD wired; chci jen diagnostiku rozbitého yaml."

  Tvoje preference:
   - Production project name: "eval-failure-test" nebo cokoli
     short-lived.
   - Akceptuj defaults pro env-classification.
   - Pokud agent navrhne launch i přes rozbitý yaml, fajn — chceš
     uvidět celý flow + cleanup. Pokud agent odmítne launch protože
     "yaml je broken", taky fajn — chceš slyšet diagnostiku +
     návrh fixu.

  Co odmítneš:
   - Agent slíbí "počkám až build fails" → "launch-production
     nedělá build, vidíme rozbitý yaml přímo, dej mi diagnostiku."
   - Agent skipne cleanup orphan projektu.
   - Agent navrhne generic "fix the yaml" bez konkrétního pojmenování
     které pole je rozbité.

  Co očekáváš na konci:
   - Agent přečetl `setup: prod` blok ze source zerops.yaml
   - Agent surfacoval konkrétní problém: `build.base:
     nodejs@99-deliberately-bogus` neexistuje, potřebuje to být
     `nodejs@22` (nebo jiný supported runtime)
   - Agent buď udělal launch (a target project potom smazal) NEBO
     odmítl launch s návrhem fix-first
   - Žádný orphan target projekt v org

notableFriction:
  - id: source-state-detect-broken-yaml
    description: |
      Agent musí přečíst `/var/www/zerops.yaml` (po bootstrap-adopt,
      který materializuje SSHFS mount) a parsovat `setup: prod`
      blok. Surfaces whether atom guidance telegraphs "read source
      yaml before launch" jako diagnostic step.
  - id: expectation-calibration-no-auto-build
    description: |
      Agent NESMÍ slíbit "dám vědět až build fails" — launch-production
      v1 nedělá auto-build. Surfaces whether agent comprehends the
      spec (§4.5: prod runtime startWithoutCode:true, no buildFromGit,
      build fires on tag push post-CD-setup) or naively assumes
      auto-build like Heroku-style platforms.
  - id: concrete-fix-proposal
    description: |
      "fix the yaml" je generic. Agent by měl říct: "v `setup: prod`
      bloku je `build.base: nodejs@99-deliberately-bogus`, změnit
      na `nodejs@22`" — konkrétně. Surfaces whether agent reads
      what's broken vs just relays "broken yaml".
  - id: orphan-cleanup-after-cosmetic-launch
    description: |
      I když launch "uspěje" (status=launched), broken-yaml target
      projekt je nefunkční a zaplňuje org. Agent must propose
      cleanup proactively. Same constraint as #9 + #10 — cross-
      project cleanup needs zcli auth-swap dance.
  - id: diagnose-before-launch-vs-during-launch
    description: |
      Two valid agent paths: (a) refuse launch + diagnose source
      yaml first, (b) launch + diagnose post-launch via source read.
      Both end at "broken build.base, here's the fix". Surfaces
      agent's strategic judgment (don't burn a launch call when
      cheaper to read source first).
---

Mám na Zerops Node.js dev/stage pair (`appdev` + `appstage` + `db`). Source zerops.yaml ale má vědomě rozbitý `setup: prod` blok (build.base je `nodejs@99-deliberately-bogus`) — chci si vyzkoušet diagnostic flow. **Vím že launch-production v1 nedělá auto-build, takže "FAILED build během launch" se tady nestane.** Místo toho chci abys přečetl ten source yaml a řekl mi konkrétně co je rozbité a jak to fixnout. Pokud chceš pro úplnost spustit launch-production přes LaunchKey (`$ZCP_E2E_LAUNCH_KEY` přes Bash) a uvidíš celý flow, ok — pak ale ten prod projekt smaž, je to jen test.
