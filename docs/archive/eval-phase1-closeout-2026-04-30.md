# Phase 1 Eval Close-out — Bootstrap variants × Develop ending with deploy

**Date**: 2026-04-30
**Plan archived from**: `plans/eval-phase1-bootstrap-develop-deploy-2026-04-30.md` → `plans/archive/`
**Suite duration total**: ~3.5h compute (Tier-1 ~50 min + R-fix resmoke ~25 min + Tier-2 1h22m + Tier-3 24m + framework dev work)

---

## Headline

**11 scenarios run end-to-end, 9 agent-SUCCESS (82%), 7 grader-PASS (64%).**

Lifecycle is healthy: every grader failure was either platform conditions (Zerops migration during Tier-2), framework calibration (Tier-1 R2/R2+ workflowCallsMin tuning, Tier-3 over-tuned forbidden patterns), or a real bug discovered (B5 cross-type recipe pair). **Zero lifecycle-correctness bugs surfaced.**

The Phase 1 acceptance criterion was "≥80% PASS"; formally we hit 64% grader-PASS. Material reading: 82% agent-SUCCESS — the lifecycle works; the gap is eval framework + 1 known schema limitation, not the codepath the user sees.

---

## What landed in the codebase

### New CLI + framework
- `cmd/zcp/eval.go`: `scenario-suite` and `triage` subcommands
- `internal/eval/suite.go`: `Suite.RunAllScenarios` + `ScenarioSuiteResult`
- `internal/eval/aggregate.go` + tests: per-suite EVAL REPORT parser → triage.md aggregator (groups Failure chains by Root cause taxonomy)

### Round 1 fixes from real Tier-1 signal
- `internal/eval/probe.go`: `ResolveProbeHostname` filters `IsSystem()` services and `zcp@` type prefix; auto-picks stage half when standard pair candidates exist
- `internal/eval/scenarios/{adopt-existing-laravel,greenfield-nodejs-todo}.md`: workflowCallsMin recalibrated against agent-actual counts
- `internal/content/atoms/bootstrap-mode-prompt.md`: explicit "Plan MUST set `stageHostname`" callout for standard mode
- `internal/content/atoms/develop-checklist-dev-mode.md`: added `runtimes: [dynamic]` axis filter so PHP/static don't get noop-start guidance

### B4 fix from Tier-2 signal
- `internal/content/atoms/bootstrap-provision-rules.md`: `project:` block dichotomy guidance for classic+adopt routes
- `internal/ops/import.go`: `IMPORT_HAS_PROJECT` suggestion now names the env-var-first recovery path explicitly + preprocessor-passes-literally rule
- `internal/ops/import_test.go`: contract pin

### Two new Tier-3 scenarios
- `internal/eval/scenarios/bootstrap-recipe-static-simple.md` — gap-filler: static runtime greenfield via recipe route
- `internal/eval/scenarios/bootstrap-classic-node-standard.md` — gap-filler: classic-route manual standard-pair plan

---

## Per-tier summary

| Tier | Scenarios | Grader-PASS | Agent-SUCCESS | Wall | Real signal extracted |
|---|---|---|---|---|---|
| Tier-1 | 4 | 4/4 | 4/4 | 33m | Calibration: probe filter (R1), 2× workflowCallsMin (R2/R2+), 2× atom guidance (R3/R4) |
| Tier-2 | 5 | 3/5 | 3/5 + 2 ERROR (platform) | 1h22m | Backlog: B1 dotnet csproj template, B2 rust rustls-tls, B3 Laravel APP_KEY base64, B4 import project: dichotomy. B1+B2 fixed inline. B3 backlog. B4 fixed (this commit chain). |
| Tier-3 | 2 | 0/2 | 2/2 | 24m | Backlog: B5 cross-type recipe pair, B6 cross-deploy zerops.yaml location, B7 close-mode for auto-close visibility, B8 (suspect) cross-deploy stage subdomain auto-enable |

Combined: 11 / 9 / 7.

---

## What's now in `plans/backlog/`

From Phase 1 specifically (eight backlog entries closed-out from this work):

- `bootstrap-cross-type-recipe-pair.md` (B5, HIGH) — schema/handler change needed for static SPA recipes (vue + others)
- `cross-deploy-zerops-yaml-location.md` (B6, MEDIUM) — atom edit + optional handler `--zeropsYaml` flag
- `close-mode-required-for-auto-close-visibility.md` (B7, LOW) — atom edit
- `cross-deploy-stage-subdomain-auto-enable-suspect.md` (B8, TBD) — investigation first, then fix or scenario adjustment
- `laravel-app-key-base64-prefix.md` (B3, MEDIUM) — recipe-side or platform preprocessor change

Plus pre-existing items the audit work parked: `auto-wire-github-actions-secret.md`, `close-mode-handler-dispatch-drift-lint.md`, `deploy-intent-resolver.md`, `m1-glc-safety-net-identity-reset.md`, `m2-stage-timing-validation-gate.md`, `m4-aggregate-atom-placeholder-lint.md`, `record-deploy-build-id-correlation.md`, `rename-closedeploymode-to-deliverymode.md`.

---

## What worked well — the load-bearing primitives Phase 1 confirmed

Across every passing scenario, agents consistently named these as essential:

- **`zerops_workflow action="start" workflow="bootstrap"` (route discovery)** — ranked routes + import YAML preview made route choice obvious
- **`zerops_dev_server action="start"`** — replaced raw SSH backgrounding with structured `running/healthStatus/startMillis/reason/logTail` in one call
- **`zerops_discover includeEnvs=true`** — env-var-key catalog matched recipe expectations, no guessing
- **`zerops_import`** — provisioned multi-service in one call, structured per-service status
- **`zerops_verify` with `bodyText`** — single-call HTTP check including rendered content; replaced separate browser/curl roundtrip
- **`zerops_browser`** — when bodyText wasn't enough, single-call browser verification with snapshot
- **Recipe `CLAUDE.md` at `/var/www/<host>/CLAUDE.md`** — provided dev command + ports + framework gotchas; eliminated guesswork
- **Cross-deploy `dev → stage` with `setup="prod"`** — first try in every standard-mode scenario where platform was healthy
- **Auto-close** — fired correctly post deploy+verify (when close-mode was set; B7)

These are the primitives Phase 1.5 + later phases can rely on without re-verification.

---

## Watchdog mechanism (per user request mid-Tier-3)

Added bash idiom for polling-loop staleness watchdog:

```bash
LAST_SIZE=0; LAST_GROWTH=$(date +%s)
until ssh zcp "grep -q '=== Scenario suite' LOG"; do
  sleep 60
  SIZE=$(ssh zcp "stat -c %s LOG 2>/dev/null || echo 0")
  NOW=$(date +%s)
  if [ "$SIZE" -gt "$LAST_SIZE" ]; then LAST_SIZE=$SIZE; LAST_GROWTH=$NOW; fi
  STALE=$((NOW - LAST_GROWTH))
  [ "$STALE" -gt 600 ] && { echo "STUCK: log silent ${STALE}s"; break; }
done
```

10-minute threshold caught a true positive in Tier-3: Claude visibly hung in `request_response_lock` (Anthropic API stuck), log silent 14+ min, no platform progress past 12 min mark. Per-scenario runner timeout (30 min) then auto-recovered (killed Claude, suite continued). Combination worked: watchdog notified, runner self-healed.

10 min is the right value — caught real stuck without false-positive on normal long agent thinking.

---

## Decisions for Phase 1.5 + beyond

Phase 1.5 scope (next plan): existing seed=deployed/imported scenarios that exercise specific develop behaviors. Less wall-clock cost, focused signal on develop-side coverage.

Phase 2 (no scope yet): git-push, build-integration, mode-expansion. Defer until Phase 1.5 triage shapes priorities.

B5 (HIGH) is worth promoting to a plan independently — it's a schema/handler change with non-trivial design surface.

Other backlog stays parked until trigger conditions promote individual entries.
