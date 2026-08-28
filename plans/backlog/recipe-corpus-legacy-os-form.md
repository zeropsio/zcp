# Recipe corpus still authors the legacy OS form (`base: nodejs@22` + `os: ubuntu`)

**Surfaced**: 2026-08-28, OS-axis migration (branch `feat/os-axis-migration`) —
flow-eval `adopt-ubuntu-node-first-deploy-keeps-os`, runs `20260828-083747` (main)
and `20260828-085843` (after the content migration). In both runs the agent's
zerops.yaml came verbatim from `zerops_knowledge recipe="nodejs-hello-world"`:
`build.base: nodejs@22` (bare → alpine build container) + `run.base: nodejs@22` +
`os: ubuntu` (legacy sibling) on an `ubuntu/nodejs@22` service — a mixed-OS
build that the run-OS oracle cannot see. The recipe was the ONLY tool result in
the transcript carrying the legacy form; every ZCP-owned surface showed
`ubuntu/nodejs@22`.

Census (synced corpus, gitignored): 30/47 recipe `.md` carry `os: ubuntu`, 1
`os: alpine`, 0 use the composite prefix; 45/47 tracked `*.import.yml` carry a
bare runtime `type:` (materializes as ubuntu at import — see
`docs/spec-knowledge-architecture.md` §3.1 for the live-verified defaults).

**Why deferred**: ZCP-owned content, catalog and the envelope service line now
state the rule at the point of contact (the service line carries "write this
prefix on both bases; legacy X + os: Y ≡ Y/X"), which is the minimal fix that
does not depend on upstream. Rewriting recipes is either ~30 PRs to recipe app
repos + Strapi (`zcp sync push recipes <slug>`) or a serve-time normalization
in `zerops_knowledge recipe=` — both are a separate decision (docs upstream is
itself mid-migration: only the Ruby pages use the composite form as of docs
`4899cf0b`).

**Trigger to promote**: a GREEN eval run where the agent STILL copies the
recipe's legacy shape despite the service-line hint; or the docs team finishing
the composite migration (then `zcp-recipe-patch` + `zcp sync push recipes` is
the mechanical follow-through).

## Sketch

- **A — migrate at source**: extend `cmd/zcp-recipe-patch` to rewrite
  `base: X` + sibling `os: Y` → `base: Y/X` and bare `base: X` → `alpine/X`
  (faithful to platform resolution), then `zcp sync push recipes` per slug.
  `TestRecipeLint` gains "OS explicit (prefix)".
- **B — normalize when serving**: the same rewrite applied to fenced yaml when
  `zerops_knowledge recipe=` renders a recipe body; deterministic, table-tested,
  recipes untouched. Makes a recipe's hidden mixed-OS build visible
  (`build.base: alpine/nodejs@22` next to `run.base: ubuntu/nodejs@22`).

## Refs

- `internal/knowledge/recipes/nodejs-hello-world.md:107-110` (legacy `os:` sibling)
- `internal/content/atoms/bootstrap-provision-rules.md` — the single "Legacy forms" block
- `internal/content/agent_facing_type_form_lint_test.go` — lint scope deliberately excludes recipes
- `eval/behavioral/scenarios/adopt-ubuntu-node-first-deploy-keeps-os.md`
