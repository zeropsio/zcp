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
   4. Two-step config — `git-push-setup` provisions remote + writes
      GIT_TOKEN as a service-scope secret consumed by the container's git
      credential helper; `build-integration=actions` wires the CI handoff.
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
gitRepoReset: https://github.com/krls2020/eval2
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.3
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
  meta:
    - {hostname: appdev, field: gitPushState, expect: "configured"}
  containerCheck:
    # The workflow's home is ORIGIN, not the dev container: the Actions run
    # this push triggers redeploys appdev from the GitHub runner with a
    # deployFiles-filtered artifact, so /var/www/.github may legitimately be
    # gone afterwards (observed gf-cargo-6). Read origin through the
    # container's own GIT_TOKEN — the value never leaves the container.
    - {service: appdev, cmd: "curl -sf -H \"Authorization: Bearer $GIT_TOKEN\" https://api.github.com/repos/krls2020/eval2/contents/.github/workflows | grep -o '\"name\": *\"[^\"]*\\.ya\\?ml\"'", match: "\\.ya?ml"}
  toolArg:
    - {never: "zerops_deploy{strategy≠git-push}"}
  askWhen: [GIT_TOKEN_MISSING, GIT_PUSH_NON_FAST_FORWARD]
  never: ["zerops_import{override=true}", "zerops_delete"]
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
   - Repo na GitHubu má jen jeden placeholder commit (README), který
     nemá nic společného s tvým kódem. Když se agent zeptá, jak
     naložit s historií na remote (rebase / merge / replace-remote,
     "non-fast-forward", "remote ahead", "unrelated"), odpověz:
     "replace-remote — ten README commit klidně zahoď, chci tam svůj
     appdev kód." Odpověz hned, je to tvoje rozhodnutí; agent ho bez
     tebe udělat nesmí.

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
  - id: remote-history-is-not-zcps-to-discard
    description: |
      The farm resets eval2 to a single unrelated baseline commit before
      seed, so the first push from appdev is non-fast-forward — the shape
      of a fresh GitHub repo created with a README. zcp classifies this
      (GF-11: rebase, merge or replace-remote) and never force-pushes or
      merges on its own; an agent that runs `git push --force` or a merge
      without the user choosing is off-contract (gf-cargo-6 forced,
      gf-cargo-3 merged — both improvised).
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
      project state untouched. On success it writes sensitive GIT_TOKEN as
      a service-scope secret, syncs origin + the git credential helper on
      /var/www/.git, then verifies a FRESH SSH session authenticates with
      the just-written secret before stamping GitPushState=configured (no
      container restart — fresh sessions read the live env). The agent
      should NOT separately call zerops_env to write GIT_TOKEN —
      git-push-setup owns it.
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
