# Real-life test matrix — minimum covering set

**Origin**: Karel 2026-05-16. Phase 2 (`v9.92`) push paused until each
of ZCP's core flows is verified by **agent-driven life tests against the
real Zerops platform** — natural-language user prompts → Claude
headless agent loop → ZCP MCP server → real platform mutation →
outcome verified on platform → cleanup. Karel's first scenario
(kanban-laravel-minimal-dev-only) exposed the routing trap on `recipe`
keyword and revealed that current behavioral evals reach call-shape
conformance but not full mutation cycle.

## Why a matrix is needed

ZCP exposes a large user-choice surface — each axis can vary
independently:

| Axis | Values (representative) |
|---|---|
| Pre-state | fresh / adopted-no-meta / bootstrapped / +git-push / +CICD-actions / +CICD-webhook / +launched-to-prod |
| Workflow entry | bootstrap / develop / launch-production / export / recipe-author |
| Route (bootstrap) | classic / recipe / adopt / resume |
| Topology | simple / dev-only / standard-pair / local-only |
| Runtime stack | PHP/Laravel / Node / Python / Go / Static / Bun / Rust |
| Managed deps | none / db only / db+cache / db+storage / multi |
| Deploy mode | auto-zcli / git-push / manual |
| Git push state | unconfigured / configured |
| CICD | none / webhook / actions |
| Launch target | new-project / existing-project |
| Failure mode | happy / build-stuck / source-drift / token-scope-mismatch / hostname-conflict |
| User language | EN / CS / mixed |

Naive product: 7×5×4×4×7×5×3×2×3×2×5×3 ≈ 7.8 million combinations.
Most are nonsense (e.g. `local-only` × `CICD-actions` makes no sense)
or covered by structural constraints (e.g. recipe topology pre-defined).
Real-world space is ~10²-10³ meaningful combinations.

## Minimum covering principle

Pairwise (every 2-axis-value pair appears once) gives ~25-30 scenarios.
But Karel asked for "realistic situations" — frequency-weighted
coverage trumps combinatorial completeness. Strategy:

1. **Top-frequency journeys first** — recipe → develop → launch is the
   most-traveled path; covered explicitly.
2. **Constraint-respecting bundles** — Karel's "už předtím dělal CICD
   setup, pak chce production" is a sequence, not a permutation; test
   as a single chained scenario (not 6 separate ones).
3. **One representative per stack family** — Laravel covers PHP +
   web-fw + managed-deps; Node covers JS + simple; Static covers
   no-managed; that's 80% of real-world stacks in 3 scenarios.
4. **Failure-recovery tested separately** — failure paths cluster on
   diagnostic surface (logs, events, recovery hints), not on stack
   variety; one or two scenarios per failure class is enough.

## 12-scenario covering set

Each scenario lists what axes it pins. Together they hit every Tier-1
axis value at least once + chain through realistic transitions.

### Tier 1 — Critical greenfield paths (5)

| # | Scenario | Workflow | Route | Topology | Stack | Notes |
|---|---|---|---|---|---|---|
| 1 | **kanban-laravel-minimal-dev-only** | bootstrap | recipe | dev-only | Laravel+pg | First real-life test (run 2026-05-16); recipe routing fix in progress |
| 2 | **kanban-laravel-standard-pair** | bootstrap | recipe | standard | Laravel+pg+cache | Recipe + stage promotion path |
| 3 | **node-postgres-classic-dev-only** | bootstrap | classic | dev-only | Node+pg | Classic-scaffold flow, no recipe |
| 4 | **static-nginx-simple** | bootstrap | classic | simple | Static | Minimal stack, immutable runtime |
| 5 | **adopt-existing-standard-pair** | bootstrap | adopt | standard | Node+pg | Pre-existing services on Zerops, ZCP integration only |

### Tier 2 — Operational flows (4)

| # | Scenario | Pre-state | Intent | Notes |
|---|---|---|---|---|
| 6 | **develop-loop-after-bootstrap** | bootstrapped standard pair | code edit → cross-deploy | Develop workflow happy path |
| 7 | **git-push-setup-then-actions** | bootstrapped | configure CI/CD | Karel's flow (b) — token + repo + atom; chains git-push-setup → build-integration=actions |
| 8 | **export-buildfromgit-self-snapshot** | bootstrapped dev | re-import bundle | Export workflow; verifies F19/F20/F21 on real source |
| 9 | **launch-production-from-standard** | bootstrapped + CICD-actions | promote to new prod project | Karel's "už předtím dělal CICD setup" — launch with pre-existing CICD wiring |

### Tier 3 — Production variants + failure-recovery (3)

| # | Scenario | Notes |
|---|---|---|
| 10 | **launch-to-existing-prod-project** | Phase 2c path — ExistingProdToken + hostname preflight; verifies token-scope refuse |
| 11 | **launch-failure-build-stuck** | Source repo build fails (e.g. broken composer.json); verifies agent pulls build logs + suggests retry via recovery hint |
| 12 | **resume-after-compaction** | Mid-bootstrap session, simulate compaction (`/status` recovery); verifies state envelope tells agent next step |

## Cross-cutting requirements

### Real-platform mutation
Every scenario hits real Zerops API. New project provisioning uses
real LaunchKey (env: `ZCP_E2E_LAUNCH_KEY`). Source repo URLs are
public github recipes — no GitHub PAT needed for read-only clones.
Tests #7 + #9 may need writable GitHub PAT (env: `ZCP_E2E_GITHUB_PAT`)
if they exercise `gh secret set` post-build-integration confirm.

### Post-run platform verification (framework gap)
Current `eval/behavioral/flow-eval.sh` runs agent → retro → cleanup.
Outcome verification is the retrospective only — agent self-reports
success. Real-life test requires **programmatic platform query
before cleanup**:

- `ListServices(targetProjectID)` — assert expected service names
  exist with `Status=ACTIVE` (or `READY_TO_DEPLOY` for compiled-base
  no-init).
- `HTTP probe` on subdomain (if `SubdomainAccess=true` on runtime
  service) — assert 2xx.
- `LogQuery` on build pipeline — assert no `FAILED` processes (or
  on failure scenarios, assert expected failure shape).
- Audit-log scan — assert classifications applied, no token leak.

Framework change: add `verification:` block to scenario YAML
frontmatter:

```yaml
verification:
  services:
    - hostname: appdev
      expectStatus: [ACTIVE, READY_TO_DEPLOY]
      subdomainProbe: 2xx
  noFailedProcesses: true
  noTokenLeak: true
```

`eval/behavioral/flow-eval.sh` invokes verification BEFORE cleanup
hook fires. Pass/fail joins retrospective.

### Cleanup discipline
Each scenario MUST clean up everything it provisions. Failure to clean
leaves residue in eval-zcp + spends Karel's Muad-org quota. Cleanup
hooks run regardless of pass/fail outcome.

### Wall-clock budget
- Tier 1 scenario: 8-15 min (real build + deploy)
- Tier 2 scenario: 5-10 min
- Tier 3 scenario: 5-12 min (some include intentional failures)

Full matrix: ~2-3 hours total wall clock. Acceptable for nightly /
pre-release runs.

## Routing fix (Karel's first ask)

Karel's first scenario showed agent went into `zerops_recipe` MCP tool
(AUTHOR scope) instead of `zerops_workflow workflow=bootstrap
route=recipe` (user-deploy scope). Root cause: the routing table in
`internal/content/templates/claude_shared.md` had a row matching
"recipe keyword OR slug" → recipe tool. Too broad — caught both
authoring AND deploy intents.

Fix landed 2026-05-16: row reworded to tighten the AUTHORING trigger
to phrases like "contribute a recipe", "create a new recipe for X",
and added explicit redirect telling agent that "deploy from existing
recipe" goes through bootstrap workflow. The "Don't" column now
explicitly forbids `zerops_recipe` direct invocation for deploys.

Re-run of kanban-laravel-minimal-dev-only after the fix is the gate
for whether the routing trap is closed end-to-end.

## Implementation phasing

**Pre-push (this session/follow-up)**:
1. Land routing fix ✅
2. Re-run scenario 1 to verify routing closes
3. Write scenarios 2, 3, 9 (the Karel-specific real-life shapes)
4. Add `verification:` framework hook (post-run platform query)
5. Run scenarios 1-3 + 9 + verify

**Post-push (v9.92.x)**:
6. Write scenarios 4-8, 10-12
7. Full nightly matrix run
8. Triage failures, fix, re-run

**Out of scope this round** (recipe-authoring tool description fix):
The `zerops_recipe` MCP tool's own Description still says generic
"recipe engine" without AUTHOR-only flag. Editing that text is in
Aleš's scope (`internal/recipe/handlers.go`). My routing fix above
mitigates by steering the agent BEFORE it reaches for the tool. A
parallel handoff to Aleš to add the AUTHOR-only marker on the tool
description would be defense-in-depth.

## What this enables

- Confidence that v9.92 push doesn't ship a regression in any common
  user journey (verified by Tier 1 + 2 scenarios)
- Reproducible test set Karel can re-run as the project evolves
- Concrete artifacts (`eval/behavioral/runs/<suite>/<scenario>/...`)
  for each scenario — transcripts, retros, verification logs
- Foundation for Phase 6b atom-update validation (when atoms change,
  run the matrix; if scenarios fail, atoms regressed)
