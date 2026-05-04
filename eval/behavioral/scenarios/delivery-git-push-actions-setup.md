---
id: delivery-git-push-actions-setup
description: |
  Working PHP app has confirmed direct deploy. The agent must move future
  delivery toward git-push + GitHub Actions after the working deploy, using
  the per-axis delivery setup flow instead of legacy strategy shortcuts.
seed: deployed
fixture: fixtures/laravel-dev-deployed.yaml
preseedScript: preseed/close-mode-git-push-setup.sh
tags: [delivery, git-push, github-actions, close-mode, deployed-service, php]
area: delivery
retrospective:
  promptStyle: briefing-future-agent
notableFriction:
  - id: delivery-after-working-deploy
    description: |
      The app is already deployed and verified in state. The agent should
      start from status, then adjust delivery; it should not redo bootstrap
      or treat delivery setup as proof of app behavior.
  - id: per-axis-git-push-flow
    description: |
      Correct flow is close-mode selection, git-push capability setup, then
      build integration. Legacy single-action strategy or workflow=cicd
      paths should not appear.
  - id: actions-proof-boundary
    description: |
      Without a real GitHub secret or repo write, the valuable behavior is a
      clear setup boundary and next proof step, not pretending a push/build
      happened.
---

The PHP app is already working; set future delivery to GitHub Actions from `main` using `https://github.com/example/weather-app`. I just need the setup path made clear and ready as far as ZCP can take it without a real GitHub secret.
