---
id: strategy-push-git-setup
description: App is deployed with push-dev strategy; user asks to switch to push-git via the action=strategy central deploy-config entry (b76aa49) — the retired workflow=cicd setup flow now runs inside one atom-synthesized response.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/strategy-push-git-setup.sh
expect:
  mustCallTools:
    - zerops_workflow
  # workflowCallsMin counts only zerops_workflow calls. Realistic floor
  # is 2: action=status + action=strategy (which synthesizes the full
  # setup atom in one call). Agent's other work (env set, discover,
  # SSH for deploy.yml) goes through different tool names. The
  # requiredPatterns below do the heavy lifting on the load-bearing
  # strategy call content.
  workflowCallsMin: 2
  requiredPatterns:
    # Central deploy-config entry since b76aa49. The agent must invoke the
    # action=strategy path with push-git — proves the agent chose the
    # canonical configure-then-execute flow, not the retired cicd route.
    - '"action":"strategy"'
    - '"push-git"'
    - '"app"'
    # The strategy-push-git atom renders as the response body; these
    # substrings are load-bearing task references the agent must have
    # seen (tasks 3, 5, 6, 8 of the atom). GIT_TOKEN is the credential
    # the agent needs to set even in push-only sub-mode. ZEROPS_TOKEN
    # is the signal the agent entered the CI/CD sub-mode at minimum
    # awareness level — the atom body contains it whether the agent
    # executes it or just reports it to the user.
    - 'GIT_TOKEN'
  forbiddenPatterns:
    # workflow=cicd is retired — if the agent tries to start it they got
    # stuck in an outdated mental model.
    - '"workflow":"cicd"'
  # requireAssessment NOT set — the load-bearing invariant is "agent
  # uses action=strategy central entry for push-git setup", asserted
  # by required patterns. End-to-end SUCCESS in assessment would require
  # a real GitHub repo + gh CLI auth + GitHub secret set, which are
  # out-of-scope externalities. Agent correctly reports PARTIAL when
  # those user-side actions are intentionally skipped per prompt rules.
followUp:
  - "Co konkrétně vrátil `zerops_workflow action=\"strategy\" strategies={\"app\":\"push-git\"}` call? Byl tam ten 12-task checklist s Option A/B (push-only vs CI/CD)?"
  - "Zvolil jsi push-only nebo full CI/CD (GitHub Actions / webhook)? Proč?"
  - "Jaké permissions musí mít GIT_TOKEN (GitHub fine-grained token / GitLab access token)? A kde jsi to získal z atomu?"
  - "Kdyby uživatel v minulosti používal `workflow=cicd` přístup — co bys mu odpověděl?"
---

# Úkol

V projektu běží Laravel služba `app` (php-nginx, dev mode) s databází `db`
(postgres). App už je adoptovaná v ZCP a má confirmed deploy strategy
`push-dev`. První deploy proběhl přes buildFromGit.

Přepni deploy strategy na **push-git** tak, aby to bylo CI/CD přes GitHub
Actions — chci, aby každý push na `main` branch v GitHubu automaticky
spustil deploy na Zerops.

Pravidla:

- Začni `zerops_workflow action="status"` — zjisti aktuální stav.
- Centrální konfigurační bod pro deploy strategy je
  `action="strategy" strategies={"app":"push-git"}`. Setup flow se vrátí
  jako response atomu — projdi ho a vykonej kroky v pořadí.
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
