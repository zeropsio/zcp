---
id: greenfield-laravel-weather
description: Laravel + Postgres weather dashboard — greenfield, two-step bootstrap discovery → recipe/classic commit, develop scaffolds + first-deploys
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
    - zerops_deploy
    - zerops_verify
  workflowCallsMin: 9
  mustEnterWorkflow:
    - bootstrap
    - develop
  requiredPatterns:
    # Both patterns must appear in at least one tool call input. JSON keys
    # serialize in alphabetical order, so don't assume "action":"start"
    # sits adjacent to "workflow":"bootstrap" — they're separated by
    # "intent" / "recipeSlug" / "route". Check each fragment
    # independently; in practice they only coexist in a bootstrap start.
    - '"workflow":"bootstrap"'
    - '"route":"'
  forbiddenPatterns:
    - "app-<projectId>"
    - "${projectId}.prg1.zerops.app"
  requireAssessment: true
  finalUrlStatus: 200
  finalUrlHostname: appstage
followUp:
  - "Jak vypadal tvůj první `zerops_workflow action=\"start\" workflow=\"bootstrap\"` call — s jakým `route` parametrem? A co ti to vrátilo?"
  - "Zvolil jsi route=recipe nebo route=classic? Pokud recipe, který slug a proč ten a ne jiný kandidát?"
  - "Kdy jsi poprvé zavolal zerops_deploy — uvnitř bootstrap session, nebo až v develop? Proč to tak musí být?"
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

Bootstrap flow (důležité):

- **První `start` call bez `route` parametru je discovery** — vrátí
  `routeOptions[]` (ranked list: recipe kandidáti / classic / adopt /
  resume). Žádná session se nevytváří.
- **Druhý call musí mít `route="<zvolený>"` (recipe vyžaduje `recipeSlug`)**
  — teprve tohle commitne session.
- Bootstrap **nikdy nedeployí**. Provisionuje services, mount, env var
  discovery — a končí.
- **Develop** scaffolduje `zerops.yaml`, napíše aplikaci a spustí první
  deploy. Passing verify překlopí `deployed=true` v envelope (Services
  block).
