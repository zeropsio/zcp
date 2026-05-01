---
id: delivery-git-push-actions-e2e
description: Real delivery flow after a working deploy — switch simple PHP app to closeMode=git-push, configure GitHub Actions on reusable repo krls2020/eval2, push, observe async build, record-deploy, verify URL.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/github-actions-delivery.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_deploy
    - zerops_events
    - zerops_verify
  workflowCallsMin: 6
  requiredPatterns:
    - '"action":"status"'
    - '"action":"close-mode"'
    - '"git-push"'
    - '"action":"git-push-setup"'
    - 'https://github.com/krls2020/eval2.git'
    - '"action":"build-integration"'
    - '"integration":"actions"'
    - '.github/workflows/zerops.yml'
    - '"strategy":"git-push"'
    - '"action":"record-deploy"'
  forbiddenPatterns:
    - '"action":"strategy"'
    - '"workflow":"cicd"'
    - 'github_pat_'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Jak bylo repo `krls2020/eval2` připravené pro opakované běhy a co jsi do něj pushnul?"
  - "Jaké GitHub secrets jsi nastavil a odkud jsi vzal `ZEROPS_TOKEN`?"
  - "Jak jsi poznal, že push s buildIntegration=actions nestačí jako deploy proof a že musíš čekat na build + zavolat record-deploy?"
  - "Které datum/čas a status posledního appVersion eventu jsi použil jako důkaz před record-deploy?"
---

# Úkol

V projektu už běží PHP služba `app` s databází `db`. Aplikace je funkční a
má ověřený direct deploy. Teď chci nastavit delivery pro další změny přes
GitHub Actions na repozitáři:

`https://github.com/krls2020/eval2.git`

Eval runner už před startem scénáře udělal bezpečný reset repozitáře `eval2`
tak, aby `main` odpovídal aktuálnímu baseline kódu v runtime containeru, a
nastavil `GIT_TOKEN` do env služby `app`. Tokeny jsou dostupné jen přes runtime
env (`GIT_TOKEN`) a proces env (`GH_TOKEN` pro `gh` CLI). Nikdy je nevypisuj,
neechoj a nevkládej do odpovědi.

Úkol:

1. Začni `zerops_workflow action="status"`.
2. Přepni close-mode pro `app` na `git-push`.
3. Proveď `git-push-setup` pro `app` s remote URL
   `https://github.com/krls2020/eval2.git`.
4. Nastav `build-integration` na `actions`.
5. Z response `build-integration` zapiš workflow soubor do mountu aplikace na
   cestu, kterou response vrátí (`.github/workflows/zerops.yml`).
6. Přes `gh secret set` nastav secrets z response. Pro `ZEROPS_TOKEN` použij
   stejný Zerops token jako `ZCP_API_KEY`; v containeru je dostupný jako env
   proměnná. Pro GitHub autentizaci použij existující `GH_TOKEN` env, bez
   vypsání hodnoty.
7. Udělej malou aplikační změnu: na root page přidej text
   `GitHub Actions delivery OK`.
8. Commitni změnu v runtime git repo a spusť `zerops_deploy` se
   `strategy="git-push"`.
9. Protože GitHub Actions build běží asynchronně, sleduj `zerops_events`, dokud
   poslední appVersion pro `app` nebude `ACTIVE`.
10. Teprve potom zavolej `zerops_workflow action="record-deploy"` pro `app`.
11. Ověř public URL `app` a potvrď, že stránka obsahuje nový text.

Tento scénář netestuje Zerops dashboard webhook OAuth. Testuje jen git-push
capability a GitHub Actions delivery.
