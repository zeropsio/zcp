---
id: recipe-laravel-minimal-standard
description: |
  Greenfield Laravel via recipe route — agent should match `laravel-minimal`
  recipe and consume its import yaml. Standard pair (dev/stage). Contrast
  to classic-route scenarios: the recipe path supplies the plan template,
  the agent narrows to the chosen recipe and proceeds. Tests the recipe-
  matcher path + recipe-bound plan submission shape.
seed: empty
tags: [bootstrap, recipe-route, standard-pair, implicit-webserver, php, laravel, mysql, recipe-match]
area: bootstrap
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: recipe-vs-classic-pick
    description: |
      Discover step returns multiple route options (recipe with confidence
      score, classic fallback). Agent must pick the recipe when the
      framework is named explicitly. Surfaces whether the route-pick atom
      telegraphs the recipe-confidence threshold.
  - id: recipe-import-yaml-shape
    description: |
      Recipe route supplies an import yaml that includes `project:` block —
      agent must strip it before zerops_import (project already exists).
      Surfaces whether the provision atom flags IMPORT_HAS_PROJECT
      pre-emptively.
  - id: laravel-app-key
    description: |
      Laravel needs APP_KEY at project scope (preprocessor directive).
      Surfaces whether the recipe + provision atoms wire it without
      hand-edit.
---

Set up a Laravel app for me on Zerops. I want a dev environment to iterate on plus a staging slot to validate builds before promoting.
