---
id: export-deployed-service
description: Deployed Laravel service `app` + managed `db` in the project; user asks to export this infra into a reusable import.yaml in the repo (via the new immediate workflow=export introduced in 09ae4df). Tests that the agent enters the export workflow, calls zerops_export + zerops_discover to gather raw state, and produces an import.yaml with buildFromGit / zeropsSetup / priority:10 for the managed service.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/export-deployed-service.sh
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_export
    - zerops_discover
  # workflowCallsMin counts only zerops_workflow calls. Realistic floor
  # is 2: action=status + workflow=export (immediate, stateless — one
  # call returns the full 11-task checklist). Everything else (export,
  # discover, SSH writes) uses different tools covered by mustCallTools.
  workflowCallsMin: 2
  requiredPatterns:
    # workflow=export is the immediate (stateless) workflow introduced in
    # 09ae4df. Agent may invoke it via action="start" workflow="export" OR
    # via the bare workflow="export" form — both forms serialize this
    # substring, so this single pattern catches either.
    - '"workflow":"export"'
    # The final import.yaml shape — load-bearing fragments the export atom
    # tells the agent to emit. Grader scans tool-call Input+Result only
    # (not prompt text), so these patterns match from the Bash / Write
    # tool Input where the agent writes the YAML to the mount — or from
    # the zerops_export tool Response. priority:10 on managed services
    # is the start-order invariant; buildFromGit+zeropsSetup tell Zerops
    # to clone the repo and pick the right `setup:` block.
    - 'buildFromGit'
    - 'zeropsSetup'
    - 'priority'
  forbiddenPatterns:
    # workflow=cicd is retired since b76aa49 — the export atom delegates
    # strategy switches to action="strategy" (task 10), never to cicd.
    - '"workflow":"cicd"'
  requireAssessment: true
followUp:
  - "Jaké služby jsi do `import.yaml` zahrnul a jaké jsi vynechal? Proč (podle task 3 atomu)?"
  - "Jaká pole jsi přidal na runtime služby (`app`)? Proč právě `buildFromGit` + `zeropsSetup` a ne build/run pipeline z `zerops.yaml`?"
  - "Proč `db` dostane `priority: 10` a runtime `app` nedostane? Co by se stalo, kdyby `db` neměla prioritu?"
  - "Našel jsi v envech nějaký APP_KEY / SECRET_* co by měl být nahrazen `<@generateRandomString(...)>` preprocessorem? Pokud ano, kde a proč?"
---

# Úkol

V projektu běží Laravel služba `app` (php-nginx dev) + managed `db`
(postgres). Už jsme to nějakou dobu rozvíjeli přes `push-dev` a teď bych
rád, aby se tahle infra dala snadno nahodit v jiném projektu — potřebuju
si to **exportovat** do gitového repa jako `import.yaml` spolu s
`buildFromGit` odkazem, ať si to můžu kdykoli `zcli project project-import
import.yaml` provolat v čistém projektu.

Repo URL na kterém aplikace sedí: `https://github.com/example/laravel-app`.

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti stav projektu.
- Použij nový immediate workflow `workflow="export"` — vrací ti procedurální
  checklist. Projdi ho úkol po úkolu.
- Konkrétně potřebuju v `import.yaml`:
  - `app` (runtime php-nginx) s `buildFromGit` + `zeropsSetup` + případně
    `enableSubdomainAccess` pokud je to teď zapnuté,
  - `db` (managed postgres) s `priority: 10`, bez `envSecrets`
    (managed credentials regeneruje platforma),
  - žádné `envVariables:` na service level (atom říká: "import API je
    silently drops").
- Výsledný soubor ulož na mount: `/var/www/import.yaml` (ssh app).
- Nemusíš nic pushovat do GitHubu — stačí aby soubor existoval na
  kontejneru a byl validní Zerops import-project-yml.

Verify: ověř, že na mountu je `/var/www/import.yaml` a že jeho obsah
zahrnuje `buildFromGit:` + `zeropsSetup:` pro `app` a `priority: 10`
pro `db`.
