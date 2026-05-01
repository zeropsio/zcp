---
id: close-mode-git-push-setup
description: Simple-mode app is deployed with closeMode=auto; user asks to switch to closeMode=git-push via the per-axis decomposition — close-mode picks the close behaviour, git-push-setup provisions GIT_TOKEN/.netrc/remote, build-integration wires the GitHub Actions workflow YAML.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/close-mode-git-push-setup.sh
expect:
  mustCallTools:
    - zerops_workflow
  # workflowCallsMin counts only zerops_workflow calls. Realistic floor
  # is 4: action=status + action=close-mode + action=git-push-setup +
  # action=build-integration. Each per-axis action synthesizes the
  # axis-specific setup atom in one call. Agent's other work (env set,
  # discover, SSH for deploy.yml) goes through different tool names.
  workflowCallsMin: 4
  requiredPatterns:
    # Per-axis decomposition since deploy-strategy-decomposition (2026-04-28).
    # The agent must invoke each axis action — proves they chose the
    # canonical per-axis flow, not the retired single action="strategy" entry.
    - '"action":"close-mode"'
    - '"git-push"'
    - '"app"'
    - '"action":"git-push-setup"'
    - '"action":"build-integration"'
    # GIT_TOKEN is the credential the per-axis git-push-setup atom names —
    # agent must have seen / set it before the build-integration call lands.
    - 'GIT_TOKEN'
  forbiddenPatterns:
    # action="strategy" is retired in favour of the per-axis decomposition;
    # workflow=cicd is similarly gone.
    - '"action":"strategy"'
    - '"workflow":"cicd"'
  # requireAssessment NOT set — the load-bearing invariant is "agent uses
  # the three per-axis entries for git-push setup", asserted by required
  # patterns. End-to-end SUCCESS in assessment would require a real GitHub
  # repo + gh CLI auth + GitHub secret set, which are out-of-scope
  # externalities. Agent correctly reports PARTIAL when those user-side
  # actions are intentionally skipped per prompt rules.
followUp:
  - "Co konkrétně vrátil `zerops_workflow action=\"git-push-setup\" service=\"app\" remoteUrl=\"...\"` call? Jakou guidance ti vrátil pro GIT_TOKEN setup?"
  - "Po `git-push-setup` jsi volal `action=\"build-integration\"` s integration=\"actions\" nebo \"webhook\"? Proč?"
  - "Jaké permissions musí mít GIT_TOKEN (GitHub fine-grained token / GitLab access token)? A kde jsi to získal z atomu?"
  - "Kdyby uživatel zmínil legacy `action=\"strategy\"` přístup — co bys mu odpověděl?"
---

# Úkol

V projektu běží Laravel služba `app` (php-nginx, simple mode) s databází `db`
(postgres). App už je adoptovaná v ZCP a má confirmed close-mode `auto`.
První deploy proběhl přes buildFromGit.

Přepni close-mode na **git-push** + buildIntegration **actions** tak, aby
to bylo CI/CD přes GitHub Actions — chci, aby každý push na `main` branch
v GitHubu automaticky spustil deploy na Zerops.

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti aktuální stav.
- Per-axis flow:
  1. `action="close-mode" closeMode={"app":"git-push"}` — set close behaviour.
  2. `action="git-push-setup" service="app" remoteUrl="..."` — provision
     GIT_TOKEN / .netrc / remote URL. Setup atom se vrátí jako response —
     projdi ho a vykonej kroky v pořadí.
  3. `action="build-integration" service="app" integration="actions"` —
     wire the GitHub Actions workflow YAML.
- Repo URL když nebudeš vědět: `https://github.com/example/weather-app`
  (repo je prázdný — žádné README, žádný .gitignore).
- GIT_TOKEN: `ghp_FAKE_TOKEN_FOR_EVAL_PURPOSES_xyz789` (neukládej ho jinam
  než do project env přes `zerops_env action="set"`).
- ZEROPS_TOKEN pro GitHub Actions: použij existující ZCP_API_KEY jako
  deploy token (řekni userovi, že ho přečteš z `.mcp.json`).
- Setup projdi aspoň do bodu, kdy víš přesně jaký by byl next step —
  finální push dělat nemusíš (prázdný repo), ale `.github/workflows/deploy.yml`
  by měl být zapsaný na kontejner (`/var/www/.github/workflows/deploy.yml`).

Verify: ověř, že na `app` je v project env nastavený `GIT_TOKEN` (přes
`zerops_env action="list"` nebo `zerops_discover includeEnvs=true`) a že
na mountu existuje `.github/workflows/deploy.yml`.
