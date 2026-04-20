---
id: bootstrap-user-forces-classic
description: User explicitly refuses recipe templates — agent must honor the override and pick route=classic even though the intent scores high against a recipe match. Directive prompt is intentional (testing the override path).
seed: empty
expect:
  mustCallTools:
    - zerops_workflow
    - zerops_import
  workflowCallsMin: 5
  mustEnterWorkflow:
    - bootstrap
  requiredPatterns:
    # route=classic is the whole test. Forcibly picking classic means the
    # agent read the user's refusal, saw recipe options in discovery, and
    # ignored them.
    - '"route":"classic"'
  forbiddenPatterns:
    # A recipe-route commit here would be ignoring the user's direct
    # instruction. Classic override is not optional when the user demands it.
    - '"route":"recipe"'
  requireAssessment: true
followUp:
  - "Zavolal jsi nejdřív discovery (start bez route)? Jaké všechny options ti vrátila?"
  - "Nabídla discovery response některé recipe kandidáty? Kolik a které?"
  - "Proč je OK, že jsi ty recipe návrhy odmítl? Co by se stalo, kdyby je agent prosadil i přes explicitní 'nepoužívej recipe' v promptu?"
---

# Úkol

Potřebuju Laravel app s PostgreSQL a počasím z Open-Meteo (Praha, Brno,
Ostrava).

**Důležité**: nepoužívej žádný recipe/šablonu. Chci to naplánovat ručně,
ať přesně vím, které služby v projektu budou a proč. Projdi infrastrukturu
krok po kroku.
