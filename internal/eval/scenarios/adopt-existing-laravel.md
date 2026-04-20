---
id: adopt-existing-laravel
description: Laravel + Postgres pre-seeded (buildFromGit, mimo ZCP) — LLM dostane čistý úkol bez hintů o stavu
seed: deployed
fixture: fixtures/laravel-existing-buildfromgit.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 8
  mustEnterWorkflow:
    - bootstrap
followUp:
  - "Kolik services bylo v projektu na začátku a v jakém stavu?"
  - "Co jsi musel/a udělat, než jsi mohl/a začít s vývojem? Proč?"
  - "Jakou workflow route (bootstrap a jeho route / develop) jsi použil/a a v jakém pořadí?"
---

# Úkol

Vytvoř jednoduchý dashboard s počasím — pro Prahu, Brno a Ostravu
zobraz aktuální teplotu z Open-Meteo API (bez API klíče). Výsledek
musí být dostupný na veřejné subdomain URL (HTTP 200).
