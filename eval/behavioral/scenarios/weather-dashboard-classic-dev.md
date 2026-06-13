---
id: weather-dashboard-classic-dev
description: |
  Greenfield custom dynamic app with NO managed dependency, dev-mode,
  via classic bootstrap. A weather dashboard (fetches a public weather
  API, renders it) is a real custom app — NOT a hello-world scaffold —
  so the agent should NOT recipe-match a {runtime}-hello-world and should
  instead take the classic route and AUTHOR a plan from scratch. Purpose:
  exercise the classic plan-AUTHORING path (agent writes the nested
  `[{"runtime":{devHostname,type,bootstrapMode:"dev"}}]` plan) for a
  single dynamic dev container with no DB — the single-dev shape. Used to
  prove the zerops_workflow Plan-field inline-examples trim does not
  degrade first-try plan authoring (the bootstrap discover-step atoms own
  the example shapes; the inline schema examples were removed).

  Surfaces the agent must navigate:

   1. Classic route, not recipe — a weather dashboard is custom; no
      hello-world recipe fits. Agent should go classic and author the
      plan rather than scaffold a {runtime}-hello-world.
   2. No managed dep — weather comes from an external public API; the
      agent must NOT add postgres/redis "for safety". Plan has a single
      runtime, zero dependencies.
   3. Dev mode — "chci na tom iterovat" → bootstrapMode=dev (single
      mutable dev container), NOT simple/standard. Single-dev plan shape.
   4. Plan nesting first-try — bootstrapMode must nest inside runtime;
      the agent authors a valid nested plan without inline schema
      examples (the discover-step atom carries the shape).
seed: empty
tags: [bootstrap, classic-route, dev-mode, dynamic, no-managed-deps, plan-authoring, czech-prompt]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
userPersona: |
  Jsi vývojář co si chce rychle hodit na Zerops malou vlastní appku a
  iterovat na ní. Žádná databáze — data o počasí taháš z veřejného API.
  Tvoje preference:
   - Accept defaults pokud má agent jasný důvod.
   - Chceš na kódu iterovat (live), ne immutable push-only.
   - Pokud agent navrhne přidat databázi, řekni: "ne, žádnou databázi
     nepotřebuju, počasí beru z veřejného API."
notableFriction:
  - id: classic-vs-recipe-for-custom-app
    description: |
      A custom weather dashboard has no matching {runtime}-hello-world
      recipe. Surfaces whether the agent correctly goes classic and
      authors a plan, vs reflexively recipe-matching a hello-world for
      the chosen runtime (which dominated greenfield single-runtime runs).
  - id: single-dev-plan-authoring
    description: |
      The classic dev-only plan is a single nested runtime target with
      bootstrapMode=dev and zero dependencies. Surfaces whether the agent
      authors valid nested plan JSON first-try from the discover-step
      atom alone (the zerops_workflow Plan-field inline examples were
      trimmed; the atom is the owner).
---

Hoď mi na Zerops jednoduchý dashboard s počasím — vezme data z nějakého veřejného weather API a zobrazí je. Chci na tom pak iterovat na kódu.
