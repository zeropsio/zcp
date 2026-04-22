---
id: adopt-existing-laravel
description: Laravel + Postgres pre-seeded (buildFromGit, mimo ZCP) — LLM musí rozpoznat adopt z discovery response, ne re-provisionovat
seed: deployed
fixture: fixtures/laravel-existing-buildfromgit.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 7
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # JSON keys serialize alphabetically. Check each fragment separately;
    # route=adopt proves the LLM saw the discovery adopt option and
    # didn't blindly classic-bootstrap over existing services.
    - '"workflow":"bootstrap"'
    - '"route":"adopt"'
    # scope is REQUIRED for develop start since cb63bf3 ("workflow(develop):
    # explicit scope + new-intent autoclose + close-always-wins"). Without
    # this pattern the agent either skipped scope (ErrInvalidParameter) or
    # we missed the workflow entry entirely.
    - '"scope":['
    - '"app"'
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: app
followUp:
  - "Kolik services bylo v projektu na začátku a v jakém stavu?"
  - "První `zerops_workflow action=\"start\" workflow=\"bootstrap\"` call — bez `route` — co ti vrátil? Objevila se tam adopt option s jakými `adoptServices`?"
  - "Proč jsi zvolil route=adopt a ne route=classic? Co by se stalo, kdybys šel classic?"
  - "Co jsi musel/a udělat, než jsi mohl/a začít s vývojem? Proč?"
---

# Úkol

Vytvoř jednoduchý dashboard s počasím — pro Prahu, Brno a Ostravu
zobraz aktuální teplotu z Open-Meteo API (bez API klíče). Výsledek
musí být dostupný na veřejné subdomain URL (HTTP 200).
