---
id: greenfield-laravel-weather
description: Baseline Laravel + Postgres weather dashboard greenfield bootstrap
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_verify
  workflowCallsMin: 12
  mustEnterWorkflow:
    - bootstrap
    - develop
  forbiddenPatterns:
    - "app-<projectId>"
    - "${projectId}.prg1.zerops.app"
followUp:
  - "Použil jsi develop workflow pro code changes? Proč ano/ne?"
  - "Jak jsi určil formát URL pro subdomain (co jsi přesně dosadil za placeholdery)?"
---

# Úkol

Vytvoř Laravel aplikaci s jednoduchým dashboardem počasí. Uživatel si vybere
město (alespoň Praha, Brno, Ostrava), aplikace zobrazí aktuální teplotu
získanou z Open-Meteo API (bez API klíče).

Požadavky:

- Laravel 11+ s php-nginx runtime.
- PostgreSQL jako závislost (v budoucnu pro cache měst — pro teď stačí connection check).
- Viditelný výsledek na veřejné subdomain URL (200 OK).
- Sloučená prod + dev setup v `zerops.yaml`.

Verify: otevři root URL a potvrď, že se zobrazí teploty pro vybraná města.
