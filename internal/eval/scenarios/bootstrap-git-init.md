---
id: bootstrap-git-init
description: Greenfield Node.js hello-world — agent must never run mount-side `git init`; container-side bootstrap init + deploy safety-net carry the .git lifecycle
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
  workflowCallsMin: 7
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    - '"workflow":"bootstrap"'
    - '"workflow":"develop"'
  forbiddenPatterns:
    # GLC-1 violation surface: any mount-side `git init` poisons .git/objects
    # with root-owned dirs (zembed SFTP MKDIR bug). The atom tells the agent
    # not to; eval verifies the agent actually complied. The regex covers
    # the common shapes: cd into the mount folder then git init, or invoking
    # git in the mount path directly.
    - "cd /var/www/.+ && git init"
    - "git init /var/www/"
    # Downstream error strings that would appear in tool output if the agent
    # managed to poison .git/ anyway. These are what a failed-to-deploy run
    # would surface — pinning them as forbidden catches a regression where
    # the deploy safety-net wasn't enough.
    - "insufficient permission for adding an object"
    - "fatal: empty ident name"
    - "dubious ownership"
  requireAssessment: true
  finalUrlStatus: 200
followUp:
  - "Nastala potřeba někdy vytvořit `.git/` ručně v `/var/www/{hostname}/` přes mount? Pokud ano, co tě k tomu vedlo, pokud ne, proč ne?"
  - "Kdy (v jakém kroku workflow) existuje `.git/` container-side uvnitř dev service — a kdo ho tam vytvořil?"
  - "Pokud by `.git/` neexistoval (scénář: migrace starší service), co by se stalo při prvním `zerops_deploy`? Jak to zachytí safety-net?"
---

# Úkol

Vytvoř jednoduchý Node.js HTTP server, který na `/` vrátí JSON `{ "ok": true }`,
a nasaď ho na Zerops.

Požadavky:

- Node.js 22+ runtime.
- Single dev service, subdomain enabled.
- Start command spouští produkční entry point (long-running process).

Verify: `GET /` vrátí `200` s body obsahujícím `"ok":true`.

# Pozadí (ne-instrukce, jen kontext)

Container mode flow:

- Bootstrap provisionuje dev service a automaticky mountuje ho k ZCP přes SSHFS.
- Post-mount (autoMountTargets) běží `ops.InitServiceGit` container-side přes
  SSH exec — `/var/www/.git/` existuje, owned by `zerops:zerops`, identity
  nastavená na `agent@zerops.io` / `Zerops Agent` ještě než zapíšeš první řádek
  aplikačního kódu.
- První `zerops_deploy` jen přidá soubory (`git add -A`) a pushne. Žádný
  další `git init` od agenta není potřeba.
- Safety-net v `buildSSHCommand` ošetří edge cases (migrace, recovery) atomic
  OR branchem — init + identity v jedné atomické sekvenci.

Mount-side `git init` (`cd /var/www/{hostname}/ && git init` z ZCP hosta)
je **zakázaný** — zembed SFTP MKDIR by vytvořil `.git/objects/` owned by
root, což zablokuje následný container-side `git add`. Recovery:
`ssh {hostname} "sudo rm -rf /var/www/.git"` a nechat safety-net re-initnout.
