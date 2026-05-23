---
id: git-push-setup-then-actions
description: |
  "Mám Node standard pair na Zerops, nastav mi git push deploy + GitHub
  Actions na ten dev service" — natural-Czech scenario testing the
  git-push-setup → build-integration=actions chain. Pre-state: standard
  pair seeded outside ZCP (via fixture). Agent walks user through
  GitHub PAT requirements, persona reveals PAT via Bash from
  `$ZCP_E2E_GITHUB_PAT`, agent runs the two-step ZCP setup, verifies.

  Surfaces the agent must navigate:

   1. PAT requirements — fine-grained PAT needs Contents (write),
      Secrets (write), Workflows (write) on the target repo. Agent
      must enumerate these before asking user to fetch the token.
   2. Adopt-first vs bootstrap — services already exist; agent runs
      adopt → ServiceMeta, then proceeds to git-push-setup.
   3. Persona-reveals-token-via-Bash pattern — persona never embeds
      the token literal; agent calls `Bash echo $ZCP_E2E_GITHUB_PAT`
      when it explicitly needs the value. Transcript stays clean
      (Bash tool result shows it briefly; persona doesn't).
   4. Two-step config — `git-push-setup` provisions remote + GIT_TOKEN
      env + .netrc; `build-integration=actions` wires the CI handoff.
      Both must complete before considering done.
   5. Verify ServiceMeta updates — both ServiceMeta entries (or one
      pair-keyed entry) reflect GitPushState=configured + RemoteURL,
      plus BuildIntegration=actions.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, git-push-setup, build-integration, github-actions, node, czech-prompt, token-injected, real-life]
area: develop-cicd
requiredEnvVars:
  - ZCP_E2E_GITHUB_PAT
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
    - ghp_
    - hand-edited token
userPersona: |
  Máš funkční Node.js standard pair na Zerops (`appdev` + `appstage` +
  `db` postgresql). Chceš si na to nastavit deploy přes git push, plus
  GitHub Actions buildy. Cílový repo je `https://github.com/krls2020/eval2`
  — máš k němu fine-grained Personal Access Token s právy Contents
  (write), Secrets (write), Workflows (write).

  Token je v env var `$ZCP_E2E_GITHUB_PAT`. Když agent požádá o token,
  pošli mu BASH PRÍKAZ aby ho přečetl: `echo $ZCP_E2E_GITHUB_PAT`.
  NIKDY token nepíš sám v textu (zůstane v transcriptu).

  Tvoje preference:
   - Akceptuj agent's adopt-route návrh — služby už existují, jen jim
     chybí ZCP metadata.
   - Pokud agent navrhne přejmenování hostnames, odmítni.
   - Pokud agent navrhne vytvoření nového GitHub repa, odmítni:
     "ne, repo už existuje na https://github.com/krls2020/eval2."
   - Pokud agent navrhne nastavit Actions na appstage místo appdev,
     odmítni: "chci Actions na appdev — testuju build pipeline."
   - Pokud agent požaduje token v textu, řekni: "získej ho přes
     Bash, `echo $ZCP_E2E_GITHUB_PAT`."

  Co odmítneš:
   - Agent chce promote do produkce → "tohle je jen CI setup, ne
     production launch."
   - Agent zapomene na build-integration=actions a označí task jako
     hotový → "ještě GitHub Actions wiring chybí, dokonči to."

  Co očekáváš na konci:
   - ServiceMeta pro appdev má GitPushState=configured +
     RemoteURL=https://github.com/krls2020/eval2
   - ServiceMeta pro appdev má BuildIntegration=actions
   - V GitHub repo (přes API) by měl být GIT_TOKEN secret nastaven
     + příslušný workflow YAML committed
   - Žádné nové služby vytvořené, žádný redeploy

notableFriction:
  - id: pat-permissions-enumeration
    description: |
      Agent musí enumerovat PŘESNÉ permissions požadované GitHub PAT
      (Contents+Secrets+Workflows write) PŘED tím než požádá user
      o token. Surfaces whether git-push-setup atom telegraphs the
      scope-fine-grained-pat requirements clearly.
  - id: bash-token-fetch-pattern
    description: |
      Persona předává token přes `Bash echo $ZCP_E2E_GITHUB_PAT`, ne
      literal. Agent musí navrhnout tenhle pattern aby token nezůstal
      v transcript prose. Surfaces whether agent reflexively asks for
      the literal value (transcript leak) or routes via Bash.
  - id: probe-first-single-call-verify
    description: |
      `git-push-setup` confirm call probes (remoteUrl, gitToken) against
      the remote BEFORE writing any project state — failed probe leaves
      project state untouched. On success it writes sensitive GIT_TOKEN,
      restarts the push-source so $GIT_TOKEN is live, syncs origin, then
      stamps GitPushState=configured. The agent should NOT separately
      call zerops_env to write GIT_TOKEN — git-push-setup owns it.
      `build-integration=actions` runs AFTER setup to wire the CI
      handoff (workflow YAML + gh secret set commands). Agent who skips
      build-integration leaves the wiring partial.
  - id: service-meta-pair-key
    description: |
      Standard pair → one pair-keyed ServiceMeta entry (dev half holds
      `Stage` field pointing at stage's hostname). git-push-setup +
      build-integration both write to the DEV-half ServiceMeta.
      Surfaces whether the agent walks the pair-keyed shape correctly
      vs. setting flags on appstage independently.
---

Mám na Zerops Node.js standard pair (`appdev` + `appstage` + `db`) co byl postavený mimo ZCP. Nastav mi ho do ZCP — adopt, pak nastavení git push deploy na `https://github.com/krls2020/eval2` a GitHub Actions buildy na ten dev service (appdev). Mám fine-grained PAT v env var `$ZCP_E2E_GITHUB_PAT` (Contents+Secrets+Workflows write na ten repo). Až bude wiring komplet, řekni mi co se nastavilo.
