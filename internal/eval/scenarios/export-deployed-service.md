---
id: export-deployed-service
description: |
  Deployed Laravel service `app` (php-nginx, mode=dev) + managed `db`
  (postgresql) in the project; user asks to export this infra into a
  re-importable single-repo bundle (zerops-project-import.yaml plus
  zerops.yaml plus buildFromGit) via the new multi-call export flow
  that landed in plan/export-buildfromgit-2026-04-28. Tests that the
  agent walks the four-step narrowing (scope-prompt → classify-prompt
  → publish-ready) and produces a bundle whose import.yaml carries
  buildFromGit + zeropsSetup + mode NON_HA for the runtime, plus the
  managed db with priority 10.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/export-deployed-service.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 3
  requiredPatterns:
    - '"workflow":"export"'
    - 'buildFromGit'
    - 'zeropsSetup'
    - 'NON_HA'
    - 'classify-prompt'
  forbiddenPatterns:
    # cicd workflow retired in b76aa49 — must NOT surface anywhere.
    - '"workflow":"cicd"'
    # The legacy zerops_export standalone tool is orthogonal to the new
    # multi-call flow per plan §4 X9. Either the agent uses workflow="export"
    # (ideal) or, if they call zerops_export directly, that's not a Phase 8
    # failure — but the new flow MUST be exercised, so the workflow=export
    # invocation is required (positive pattern above) without forbidding
    # zerops_export. The mode: dev / mode: simple grader false-positive
    # was dropped: the source ServiceMeta.Mode is legitimately ModeDev for
    # the fixture's `app` service, and `zerops_workflow action="status"`
    # surfaces that value as `mode=dev` in its response — scanning the
    # full log catches that legit echo. The required pattern `NON_HA`
    # below proves the bundle emits the platform scaling enum correctly.
  requireAssessment: true
followUp:
  - "Kolik volání `zerops_workflow workflow=\"export\"` jsi udělal a v jakém pořadí (scope-prompt / classify-prompt / publish-ready)? Co řídilo stav narrowingu?"
  - "Které envy jsi zařadil do bucketu `infrastructure` (drop) a které do `auto-secret` / `external-secret` / `plain-config`? Jak ses to rozhodl — co tě navedlo na klasifikaci?"
  - "Proč `services[].mode` v import.yaml musí být `NON_HA` a ne `dev` / `simple`? (Nápověda: §3.3 plánu po Phase 5 amendmentech.)"
  - "Pokud `bundle.errors` přijde nepráznde, jaký status handler vrátí místo `publish-ready`? Co bys udělal jako agent?"
  - "Pokud `bundle.warnings` obsahuje M2 indirect-reference upozornění, co znamená a jak to opraví — reklasifikací nebo úpravou zerops.yaml?"
---

# Úkol

V projektu běží Laravel služba `app` (php-nginx, mode=dev) + managed
`db` (postgres). Už jsme ji nějakou dobu rozvíjeli a teď bych rád, aby
se tahle infra dala znovu nahodit v čistém projektu — potřebuju ji
**exportovat** do gitového repa jako self-referential bundle, ať si to
můžu kdykoli `zcli project project-import zerops-project-import.yaml`
provolat v novém projektu.

Cílový repozitář pro budoucí push: `https://github.com/krls2020/eval1`
(z preseedu už máš `meta.gitPushState=configured` + `meta.remoteUrl`
nastavený, takže workflow nebude chainovat na `git-push-setup`).

V této úloze ti **stačí dorazit k `status="publish-ready"`** s validním
bundlem — fyzický push na GitHub Phase 8 eval testovat nemusí. Cíl je
ověřit, že:

1. Multi-call narrowing funguje (scope → classify → publish).
2. Final bundle má správný shape (buildFromGit, zeropsSetup, NON_HA,
   priority:10 na managed db).
3. Klasifikační protokol je správně aplikovaný (`infrastructure` envy
   se dropnou, `auto-secret` dostanou `<@generateRandomString>`, atd.).

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav projektu.
- Pak invokuj nový multi-call workflow `zerops_workflow workflow="export"`.
  - **První volání** (bez `targetService`) ti vrátí `scope-prompt` se
    seznamem runtimů. Vyber `app`.
  - **Druhé volání** (s `targetService="app"`, bez `envClassifications`)
    ti vrátí `classify-prompt` s tabulkou envů ke klasifikaci. Pro každý
    env si lookup-ni hodnotu přes `zerops_discover` (s `includeEnvs=true
    includeEnvValues=true`) a zařaď ho podle plan §3.4 do jednoho z buckets:
    `infrastructure` (drop), `auto-secret` (random), `external-secret`
    (REPLACE_ME), `plain-config` (verbatim).
  - **Třetí volání** (s `envClassifications` populovaným) ti vrátí
    `publish-ready` s `bundle.importYaml` + `bundle.zeropsYaml` +
    `nextSteps` (write yamls, commit, push).
- Bundle by měl obsahovat:
  - `app` (runtime php-nginx) s `buildFromGit` + `zeropsSetup` +
    `enableSubdomainAccess: true` (subdomain je zapnutý) + `mode: NON_HA`
    (Zerops platform scaling enum, NE `dev`/`simple` — to je ZCP
    topology, ne zerops.yaml mode).
  - `db` (managed postgres) s `priority: 10`, bez `envSecrets`
    (managed credentials regeneruje platforma).
- Pokud `bundle.errors` přijde neprázdné, handler vrátí
  `validation-failed` místo `publish-ready` — schema validation flagla
  něco rozbitého. Oprav source a zopakuj export.
- Pokud `bundle.warnings` obsahuje M2 indirect-reference upozornění
  (Infrastructure-classified env je referencovaný v zerops.yaml),
  reclassify daný env jako PlainConfig a re-call.
- Když dorazíš k `status="publish-ready"`, **neexekvuj** SSH writes /
  commit / push — pro Phase 8 eval stačí dosáhnout publish-ready
  responsu a vidět správný bundle shape v něm.

Verify: V publish-ready responsu bude `bundle.importYaml` obsahující
`buildFromGit:` + `zeropsSetup: app` + `mode: NON_HA` pro runtime,
plus managed db s `priority: 10`. `bundle.zeropsYaml` zrcadlí
upstream `/var/www/zerops.yaml` z runtime kontejneru.
