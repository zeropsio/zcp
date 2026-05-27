---
id: discover-adoption-state-adopted-no-rebootstrap
description: |
  "Můžeš mi shrnout, co tu mám nainstalováno a v jakém stavu?" —
  natural-Czech state-summary scenario testing that the v9.101.x
  adoptionState enum on `zerops_discover` correctly reports
  fully-adopted services + that the agent reads the per-service
  state instead of re-bootstrapping.

  Pre-state: fixture deploys appdev/appstage/db standard pair; preseed
  plants a COMPLETE pair-keyed ServiceMeta for appdev (with
  stageHostname=appstage, bootstrappedAt + firstDeployedAt set,
  primarySetupName/stageSetupName populated). Both pair halves resolve
  to this single meta via ManagedRuntimeIndex.

  Surfaces the agent must navigate:

   1. adoptionState classification round-trip — zerops_discover output
      MUST carry `adoptionState:"adopted"` on appdev + appstage (via
      pair-keyed meta lookup) and `adoptionState:"managed-dep"` on db.
      Agent reads these directly; doesn't derive from other fields.
   2. No warnings on fully-adopted state — Warnings array empty;
      agent doesn't see "Services with adoptionState=adoptable" or
      "resumable" since neither bucket has entries. If agent reports
      a missing adopt warning, that's confused reading.
   3. No re-bootstrap reflex — agent MUST NOT call `zerops_workflow
      action="start" workflow="bootstrap" route="adopt"` against
      already-adopted services. If agent attempts, ADOPT_REQUIRED
      doesn't fire (services have complete metas), but the call still
      opens an unnecessary session.
   4. discover-first, no list inflation — agent reports project state
      from one discover call. Doesn't loop discover-per-service or
      mix in zerops_env/zerops_verify for state summary.

seed: deployed
fixture: fixtures/nodejs-standard-deployed.yaml
preseedScript: preseed/discover-adopted-pair-meta.sh
tags: [discover, adoption-state, adopted, state-summary, czech-prompt, real-life]
area: discover
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
    - re-bootstrap
    - re-adopt
    - "had to bootstrap again"
userPersona: |
  Jsi vývojář a vrátil ses k Zerops projektu po pár dnech. Pamatuješ
  si že tam máš `appdev`/`appstage` pair (Node.js) + `db` (Postgres)
  a všechno běží — bootstrap byl proveden, deploy taky. Teď chceš
  jen rychle vidět, v jakém stavu projekt je.

  Tvoje preference:
   - Akceptuj agent's discover-based state summary.
   - Pokud agent navrhne re-bootstrap nebo re-adopt, odmítni: "ne,
     služby jsou už adoptované — vidíš to v adoptionState. Jen mi
     shrň aktuální stav."
   - Pokud agent zkouší další workflow start, odmítni: "ne, jen
     summary, žádné nové akce."
   - Pokud agent správně přečte adoptionState=adopted + managed-dep,
     akceptuj.
   - Pokud agent správně reportuje "žádný adopt potřeba, žádný
     resume needed", akceptuj.

  Co odmítneš:
   - Agent zkusí `workflow="bootstrap" route="adopt"` → "ne, vidíš
     v adoptionState že jsou adopted."
   - Agent zkusí `workflow="develop"` start → "ne, jen mi shrň co
     tu mám, neotevírej novou session."
   - Agent zkusí ručně iterovat přes services jednotlivě discover-em
     → "discover bez filtru ti dal vše najednou."

  Co očekáváš na konci:
   - Agent pochopil + reportoval adoptionState=adopted pro appdev+
     appstage
   - Reportoval adoptionState=managed-dep pro db (a pochopil že to
     nepotřebuje adopt)
   - Žádný nový bootstrap/adopt/develop session-start call
   - Krátký factual summary, ne planning

notableFriction:
  - id: adoptionstate-as-per-service-flag
    description: |
      Agent must read `adoptionState` per service from discover
      response, not derive from other fields (managedByZcp doesn't
      exist anymore; isInfrastructure alone is not sufficient).
      Surfaces whether atom guidance gives agents a stable hook for
      the new field.
  - id: no-rebootstrap-on-adopted-services
    description: |
      Pre-state has complete metas. Agent MUST NOT propose / call
      bootstrap route=adopt against services already showing
      adoptionState=adopted. Surfaces whether atom prose + warning
      absence successfully telegraphs "no action needed".
  - id: pair-keyed-meta-lookup-consistency
    description: |
      Both appdev and appstage resolve to same complete meta via
      pair-key. Both should report adoptionState=adopted in
      discover. Surfaces whether pair-keyed index works correctly
      end-to-end on the new field.
---

Můžeš mi prosím rychle shrnout, co tu mám nainstalováno v Zerops projektu a v jakém je to stavu? Jen krátký přehled, nic neměnit.
