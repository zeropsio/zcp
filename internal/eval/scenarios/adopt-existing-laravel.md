---
id: adopt-existing-laravel
description: Laravel + Postgres pre-seeded (buildFromGit, mimo ZCP) — LLM musí rozpoznat adopt z discovery response, ne re-provisionovat
seed: deployed
fixture: fixtures/laravel-existing-buildfromgit.yaml
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_discover
  workflowCallsMin: 9
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # Discovery must precede commit, and commit must be route=adopt.
    # If the LLM blindly route=classic over existing services, it would
    # collide on hostnames and we'd want that to fail.
    - '"action":"start","workflow":"bootstrap"'
    - '"route":"adopt"'
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
