---
id: git-push-setup-empty-remote
description: |
  "Mám Node standard pair na Zerops, nastav mi git push deploy na čerstvě
  založený GitHub repo" — the `git-push-setup-then-actions` sibling for the
  OTHER remote shape: a brand-new, genuinely EMPTY repository (zero commits,
  `auto_init: false`) the farm creates per run and deletes after, instead of
  the shared repo `git-push-setup-then-actions` resets to a one-commit
  baseline. "The user just created a repo" — the first push from appdev must
  be a plain fast-forward, never the rebase/merge/replace-remote decision
  the shared-repo sibling's non-fast-forward friction forces.

  Pre-state: standard pair seeded outside ZCP (via fixture, same as
  git-push-setup-then-actions). Agent walks user through GitHub PAT
  requirements, persona reveals the (admin-scoped, run-unique) PAT via Bash
  from `$ZCP_E2E_GITHUB_PAT_ADMIN`, agent runs git-push-setup, verifies.

  Surfaces the agent must navigate:

   1. PAT requirements — same enumeration discipline as the shared-repo
      sibling (Contents/Secrets/Workflows write), even though this run's
      PAT happens to carry broader (admin) scopes too.
   2. Adopt-first vs bootstrap — services already exist; agent runs
      adopt → ServiceMeta, then proceeds to git-push-setup.
   3. Persona-reveals-token-via-Bash pattern, same as the sibling.
   4. git-push-setup, then ONE plain `zerops_deploy strategy=git-push` so
      the empty remote actually receives appdev's history — no
      build-integration (neither actions nor webhook; CI wiring is the
      sibling's job, and the persona refuses both).
   5. The FIRST push must be a clean fast-forward: the repo is genuinely
      empty (no baseline commit, unlike gitRepoReset's shared repo), so
      zcp's own pre-push classification (the `remote` block on the
      git-push-setup success result) must read `state: empty`, and the
      agent must never see, let alone resolve, a non-fast-forward
      decision.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
tags: [adopt, git-push-setup, empty-remote, node, czech-prompt, token-injected]
area: develop-cicd
requiredEnvVars:
  - ZCP_E2E_GITHUB_PAT_ADMIN
gitRepoCreate: krls2020
retrospective:
  promptStyle: briefing-future-agent
verification:
  mode: required
  spec: spec-workflows.md §4.3
  meta:
    - {hostname: appdev, field: gitPushState, expect: "configured"}
  containerCheck:
    # The push landed: origin's `main` exists on the per-run repo. Read
    # through the container's own GIT_TOKEN — the value never leaves it.
    - {service: appdev, cmd: "url='{{gitRepoURL}}'; curl -sf -H \"Authorization: Bearer $GIT_TOKEN\" \"https://api.github.com/repos/${url#https://github.com/}/branches/main\" | grep -o '\"name\": *\"main\"'", match: "main"}
  toolResult:
    - {tool: zerops_workflow, contains: "\"state\":\"empty\""}
  toolArg:
    - {always: "zerops_deploy{strategy=git-push}"}
    - {never: "zerops_deploy{strategy≠git-push}"}
    - {max: 0, call: "zerops_workflow{action=build-integration}"}
  never: ["zerops_import{override=true}", "zerops_delete"]
  retrospectiveMustNotMention:
    - ghp_
    - hand-edited token
userPersona: |
  Máš funkční Node.js standard pair na Zerops (`appdev` + `appstage` +
  `db` postgresql). Chceš si na to nastavit deploy přes git push na
  ČERSTVĚ založený GitHub repo — žádná historie, žádný commit.

  Repo je `{{gitRepoURL}}` — už existuje (založil jsi ho těsně předtím),
  je prázdné. Máš k němu Personal Access Token s právy Contents (write),
  Secrets (write), Workflows (write) — token je v env var
  `$ZCP_E2E_GITHUB_PAT_ADMIN`. Když agent požádá o token, pošli mu BASH
  PRÍKAZ aby ho přečetl: `echo $ZCP_E2E_GITHUB_PAT_ADMIN`. NIKDY token
  nepíš sám v textu (zůstane v transcriptu).

  Tvoje preference:
   - Akceptuj agent's adopt-route návrh — služby už existují, jen jim
     chybí ZCP metadata.
   - Pokud agent navrhne přejmenování hostnames, odmítni.
   - Pokud agent navrhne založení DALŠÍHO nového GitHub repa, odmítni:
     "ne, repo už existuje na {{gitRepoURL}}, jen tam ještě nic není."
   - Žádné CI: pokud agent nabídne build-integration (GitHub Actions
     NEBO webhook), odmítni: "žádné Actions ani webhook, chci jen
     nastavit git push a hned tam pushnout aktuální stav appdev."
   - Když je git-push-setup hotový, chceš, aby agent rovnou udělal jeden
     git push deploy (`zerops_deploy strategy=git-push`), aby v repu byl
     kód. Bez toho úkol není hotový.
   - Pokud agent požaduje token v textu, řekni: "získej ho přes Bash,
     `echo $ZCP_E2E_GITHUB_PAT_ADMIN`."

  Co odmítneš:
   - Agent chce promote do produkce → "tohle je jen CI setup, ne
     production launch."
   - Agent se zeptá, jak naložit s "existující historií" na remote
     (rebase / merge / non-fast-forward) → to je matoucí otázka, repo je
     prázdné, řekni: "tam nic není, prostě to tam pushni."

  Co očekáváš na konci:
   - ServiceMeta pro appdev má GitPushState=configured +
     RemoteURL={{gitRepoURL}}
   - Na `{{gitRepoURL}}` existuje větev `main` s kódem appdev (jeden
     git push deploy proběhl)
   - Žádné nové služby vytvořené, žádné Actions/webhook wiring

notableFriction:
  - id: empty-remote-never-non-fast-forward
    description: |
      The repo the farm creates for this run has ZERO commits
      (auto_init:false) — unlike git-push-setup-then-actions's shared repo
      (which the farm resets to a one-commit README baseline, forcing a
      non-fast-forward decision on the FIRST push), the first push here has
      nothing to diverge from. zcp's own pre-push classification (the
      git-push-setup success result's `remote` block) should read
      `state: empty`, never `ahead`/`diverged`/`unrelated`, and the agent
      should never surface — let alone attempt to resolve — a
      GIT_PUSH_NON_FAST_FORWARD rebase/merge/replace-remote decision.
      There is no `verification.never`/`askWhen` assertion for this error
      code not occurring — `never` matches tool CALL shapes, not error
      codes, and no toolResult-absent assertion form exists — so this stays
      an observational note for the local session reading self-review.md
      and the transcript, not an automated gate.
  - id: pat-permissions-enumeration
    description: |
      Agent musí enumerovat PŘESNÉ permissions požadované GitHub PAT
      (Contents+Secrets+Workflows write) PŘED tím než požádá user o
      token — stejná disciplína jako u git-push-setup-then-actions, i
      když tenhle token má navíc admin scopes (Administration atd.) pro
      farm-side create/delete, co agent nikdy nepoužije.
  - id: bash-token-fetch-pattern
    description: |
      Persona předává token přes `Bash echo $ZCP_E2E_GITHUB_PAT_ADMIN`, ne
      literal. Agent musí navrhnout tenhle pattern aby token nezůstal
      v transcript prose.
  - id: no-second-repo-created
    description: |
      Repo už existuje (farm ho založilo před seedem) — agent nesmí
      navrhnout "založím ti nový repo" ani volat cokoliv, co by repo
      znovu vytvořilo. Persona to explicitně odmítne, pokud se agent
      zeptá.
---

Mám na Zerops Node.js standard pair (`appdev` + `appstage` + `db`) co byl postavený mimo ZCP. Nastav mi ho do ZCP — adopt, pak nastavení git push deploy na `{{gitRepoURL}}` (čerstvě založený, prázdný repo) a rovnou tam jedním git push deployem pushni aktuální stav appdev. Žádné CI (Actions ani webhook) teď nechci. Mám PAT v env var `$ZCP_E2E_GITHUB_PAT_ADMIN` (Contents+Secrets+Workflows write na ten repo). Až bude push hotový, řekni mi co se nastavilo.
