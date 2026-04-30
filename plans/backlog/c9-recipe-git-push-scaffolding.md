# Recipe-side git-push scaffolding (R12 from deploy-strategy-decomposition)

**Surfaced**: 2026-04-29 — `docs/audit-prerelease-internal-testing-2026-04-29.md`
finding C9. R12 was already deferred from the deploy-strategy-decomposition
plan (archived 2026-04-28). Confirmed still empty: `grep -rn 'git-push\|GitPushState\|setup-git-push\|configureGit' internal/recipe/` returns zero hits in non-test code.

**Why deferred**: the audit's pre-internal-testing fix bundle stayed scoped
to platform-mechanics fixes. Recipe-side scaffolding for `closeMode=git-push`
(pre-staging a remote URL placeholder, mentioning `setup-git-push-{container,local}`
atoms in recipe README, walking the `action="git-push-setup"` path) is a
recipe-content concern that doesn't block first-day testing — agents can
follow the post-bootstrap atom guidance reactively when they switch
close-mode.

**Trigger to promote**: live-agent feedback during internal testing that
agents stumble on the git-push path during recipe-bootstrap because the
recipe README doesn't mention it, OR a recipe-author asks for a curated
template for git-push-on-day-one.

## Sketch

- Recipe corpus generator (`internal/recipe/...`) gains an OPTIONAL
  `git-push-setup` post-bootstrap section that the recipe authoring
  pipeline emits when `gitPushSetup: true` in recipe metadata.
- Generated section walks the per-axis flow: `action="close-mode"` →
  `action="git-push-setup" remoteUrl="..."` → optional
  `action="build-integration"`.
- Update one or two reference recipes (`laravel-minimal`,
  `nestjs-minimal`) with the metadata flag turned on so authors have a
  template.
- Authoring contract: when `gitPushSetup: true` is set, recipe authoring
  also requires `recommendedRemoteHost: github.com|gitlab.com|...` so
  the generated guidance picks the right `gh secret set` /
  GitLab-CI snippet.

**Linked backlog entry**: see `plans/backlog/auto-wire-github-actions-secret.md`
— the GitHub-API zero-touch secret-creation Codex flagged. Both are about
"close the manual-step gap on the git-push path" but at different layers
(C9 = recipe guidance; auto-wire = ZCP-side automation).

## Risks

- Recipe content drift: every git-push-setup generated section duplicates
  knowledge that lives in `setup-git-push-*` atoms. Need a single source
  of truth — either reference the atoms by name only, or extract a
  shared template.
- Recipes ship as static markdown; users running an old recipe miss any
  scaffolding improvements. The atom corpus is the live channel; recipe
  scaffolding is the one-time onboarding signal.

## Refs

- Audit C9 + R12 history.
- Existing atoms covering the current flow:
  `internal/content/atoms/setup-git-push-container.md`,
  `setup-git-push-local.md`, `setup-build-integration-webhook.md`,
  `setup-build-integration-actions.md`.
- Linked backlog: `plans/backlog/auto-wire-github-actions-secret.md`.
