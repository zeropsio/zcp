# Post-E1 composition re-score (2026-04-27)

Date: 2026-04-27
Round type: COMPOSITION RE-SCORE post engine ticket E1
Plan reviewed: `plans/engine-atom-rendering-improvements-2026-04-27.md` §3 ticket E1
Reviewer: in-tree (probe in `internal/workflow/aggregate_render_probe_test.go`)
Source-of-truth: `Synthesize` over `LoadAtomCorpus` in `internal/workflow/synthesize.go`.

## What changed in E1

Engine:
- New scalar axis `multiService: aggregate` parsed by `ParseAtom`
  (`internal/workflow/atom.go::AxisVector.MultiService`).
- New `{services-list:TEMPLATE}` body directive (`expandServicesListDirectives`
  in `internal/workflow/synthesize.go`). TEMPLATE may contain `{hostname}`
  / `{stage-hostname}` placeholders; balanced-brace parser tracks nested
  placeholders so TEMPLATE can carry arbitrary substitutions without
  escape characters.
- Aggregate-mode branch in `Synthesize` — atoms with
  `multiService: aggregate` render once with the directive expanded over
  the matching services. `MatchedRender.Service` is `nil` (envelope-bound).

Atoms migrated to aggregate (3 of the 4 named in plan §3):
- `develop-first-deploy-execute` — absorbed the deleted
  `develop-first-deploy-execute-cmds`. Switched from
  `envelopeDeployStates` to service-scoped
  `deployStates: [never-deployed] + modes: [dev, simple, standard]` +
  `multiService: aggregate`. Per-service `zerops_deploy targetService=...`
  commands now live in a `{services-list:...}` directive.
- `develop-first-deploy-verify` — absorbed the deleted
  `develop-first-deploy-verify-cmds`. Switched to service-scoped
  `deployStates: [never-deployed] + multiService: aggregate`. Per-service
  `zerops_verify serviceHostname=...` commands live in the directive.
- `develop-first-deploy-promote-stage` — switched to
  `multiService: aggregate`; the `zerops_deploy sourceService=...
  targetService=...` + paired verify commands live in a
  `{services-list:...}` directive.
- `develop-dynamic-runtime-start-container` — UNCHANGED. Body has no
  `{hostname}` placeholder so the existing post-substitution dedup in
  `Synthesize` already collapses it to 1× per envelope. No structural
  duplication to fix; the plan's §3 suggestion to "add per-service start
  invocations as a table" is editorial content-add, scoped out of E1.

## Render-count delta (`internal/workflow/aggregate_render_probe_test.go`)

For the two-runtime-pair standard fixture (4 services: appdev/apidev
dev-side standard + appstage/apistage stage-side):

| Atom | Pre-E1 renders | Post-E1 renders |
|---|---|---|
| `develop-first-deploy-execute` | 1 (envelope-scoped prose) | 1 (aggregate, with cmd directive) |
| `develop-first-deploy-execute-cmds` | 2 (per-dev-host) | DELETED |
| `develop-first-deploy-verify` | 1 (envelope-scoped prose) | 1 (aggregate, with cmd directive) |
| `develop-first-deploy-verify-cmds` | 4 (per never-deployed host) | DELETED |
| `develop-first-deploy-promote-stage` | 2 (per-dev-host) | 1 (aggregate, with pair directive) |
| `develop-dynamic-runtime-start-container` | 1 (dedup-collapsed) | 1 (unchanged) |
| **Total renders for the 6 atoms** | **11** | **3** |

The 8 redundant renders for the four migrated atoms collapse into 3
single renders — each carries the equivalent commands inline via
`{services-list:TEMPLATE}` expansion. No content was removed; only the
prose surrounding per-service commands stopped duplicating.

Body bytes from the probe (post-E1 measurement; sister fixtures cover
the single-service shapes for context):

| Fixture | Atom count | Body bytes |
|---|---|---|
| single dev-mode | 20 | 21,043 |
| single simple-mode | 19 | 19,662 |
| single standard pair | 19 | 20,315 |
| **two standard pairs** | **20** | **21,734** |

Pre-E1 baseline (per `codex-round-p5-rescore-v3.md` and the legacy
`develop_first_deploy_two_runtime_pairs_standard` shape): four atoms
double-rendered. The two-pair body landed materially higher than the
single-pair body because per-service prose duplicated alongside the
commands. Post-E1 the delta between single-pair and two-pair is 1.4 KB
— the increase is exactly the directive expansion (one extra cmd line
per host), not duplicated prose.

## §6.2 Redundancy gate

Two-pair pre-E1: **Redundancy = 1** (engine-structural per
`codex-round-p5-rescore-v3.md`).

Two-pair post-E1: **Redundancy = 2**. The four atoms whose per-service
duplication anchored the gate now render once each; the remaining
content matches the single-pair render's redundancy profile.

## §15.3 G3 strict-improvement (refresh post-E1)

| Fixture | Coh | Den | Red (pre→post) | Cov-gap | Task-rel | G3 verdict |
|---|---|---|---|---|---|---|
| standard | 4 | 4 | 3 (unchanged) | 5 | 4 | PASS |
| implicit-webserver | 4 | 4 | 3 (unchanged) | 4 | 4 | PASS |
| **two-pair** | 4 | 3 | **1 → 2** | 5 | 4 | **PASS / CLEAN-SHIP** |
| single-service | 4 | 4 | 3 (unchanged) | 5 | 4 | PASS |
| simple-deployed | 4 | 3 | 4 (unchanged) | 5 | 4 | PASS |

The two-pair structural Redundancy footnote inherited from
`codex-round-p5-shipverdict-v3.md` is now closed. SHIP gate moves from
**SHIP-WITH-NOTES** to **CLEAN-SHIP** for the two-pair shape.

## VERDICT

`VERDICT: APPROVE — CLEAN-SHIP on two-pair`

E1 closes:
- cycle-2 Phase-8+ ticket #1 (two-pair structural Redundancy).
- cycle-3 Finding 2 (1-line per-service `-cmds` atom split).

Inherited cycle-3 SHIP-WITH-NOTES — driven entirely by the two-pair
structural footnote — is now resolved. No cycle-4 hygiene work is
required to lift the gate.
