# Live-eval runs — atom corpus goldens

**Protocol**: see `_live-eval-protocol.md`.
**Owner**: `@krls2020` (initial assignment).
**CODEOWNERS**: `/internal/workflow/testdata/atom-goldens/_live-eval-runs.md @krls2020` (see `.github/CODEOWNERS`).

This file is an append-only log of live-eval runs against eval-zcp.
Each run captures the date, scenarios walked, services used,
divergence summary, and disposition. Quarterly cadence per protocol
§5; ad-hoc runs welcome when production friction surfaces.

---

## 2026-05-02 — Q1 2026 live-eval (first run, post-merge cross-check)

**Owner**: `@krls2020`
**Scope**: 1 of 5 protocol scenarios verifiable against current eval-zcp
state (`idle/adopt-only`); 1 negative-path verification (export handler
validation gate); plus 1 deployment-state observation (production
zcp is pre-Phase-0b-migration, MCP restart needed for new paths).
**Services provisioned**: none — eval-zcp's existing `zcp` service
(type `zcp@1`, status ACTIVE, not bootstrapped, subdomain enabled)
already matches `idle/adopt-only` fixture shape.
**Disposition**: clean for verifiable subset; remaining 4 scenarios
require provisioning + state-driving (regular cadence work).

### Findings

#### 1. `idle/adopt-only` fixture-vs-production shape — **PASS**

Production envelope from `zerops_workflow action="status"`:
- `Phase: idle` ✓ (matches fixture `PhaseIdle`)
- `IdleScenario: adopt` ✓ (matches fixture `IdleAdopt` — derived
  from "1 unbootstrapped runtime, no metas, no resumable")
- `Services`: 1 runtime (`zcp` / `zcp@1` / status `ACTIVE` /
  `bootstrapped: false`) ✓ structurally matches fixture's single-
  runtime shape

Atom fire-set:
- `bootstrap-route-options` ✓ (present in production response)
- `idle-adopt-entry` ✓ ("Adopt unmanaged runtimes" guidance fires —
  matches the golden's expected atom IDs at
  `internal/workflow/testdata/atom-goldens/idle/adopt-only.md`)

Differences (cosmetic, not divergence):
- Service hostname: live=`zcp`, fixture=`appdev` (placeholder
  substitution, irrelevant).
- Type-version: live=`zcp@1` (the ZCP runtime container itself),
  fixture=`nodejs@22`. Different runtime classes (ZCP runtime vs
  generic dynamic) but RuntimeClass classification doesn't gate
  fire-set for these two atoms.

**Conclusion**: fixture-vs-production cross-check PASSES for
`idle/adopt-only`. The fixture envelope shape derives cleanly from
production; the atoms render exactly as the golden expects.

#### 2. Production zcp is pre-Phase-0b-migration — **EXPECTED**

`zerops_workflow action="start" workflow="export"` against eval-zcp
returned:
```json
{
  "guidance":"Pick the runtime service to export. Pass targetService=<hostname> on the next call.",
  "phase":"export-active",
  "runtimes":["zcp"],
  "status":"scope-prompt"
}
```

The `guidance` field is a single inline string — the **pre-migration
shape**. After Phase 0b refactor, the response's `guidance` field
should be atom-rendered output from `export-intro` + `export-scope-
prompt` (multi-paragraph "three-call narrowing" framing).

**This confirms the deployed zcp is pre-migration code.** The MCP
server running `.mcp.json`-registered local binary started BEFORE
this branch landed; an MCP server restart will pick up the new code
paths. Per Phase 0b commit message (commit `7776cc7d`), this was
explicitly documented as expected:

> Live-platform smoke against eval-zcp is pending an MCP server
> restart so the new code paths run; deferred to Phase 5 documented
> live-eval protocol per plan section 5.5.

**No fixture-reality drift here** — production behavior matches
pre-migration corpus state, which is consistent. Once MCP restarts,
the next quarterly run can verify the migrated atom-rendered output
matches the new goldens (the `export/scope-prompt`,
`export/variant-prompt`, etc. set introduced in Phase 0b).

#### 3. Export handler validation gate — **PASS**

`zerops_workflow action="start" workflow="export" targetService="zcp"`
returned the structured error:
```json
{
  "code":"SERVICE_NOT_FOUND",
  "error":"Service \"zcp\" has no bootstrapped meta — export needs
   the topology.Mode (dev / standard / stage / simple / local-stage
   / local-only) to resolve variant",
  "suggestion":"Run bootstrap first: zerops_workflow action=\"start\"
   workflow=\"bootstrap\". Or adopt the service via adopt-local.",
  "recovery":{"tool":"zerops_workflow","action":"status"}
}
```

The error message and recovery shape match my refactored handler
exactly (verified in `internal/tools/workflow_export.go::handleExport`
line 109-112). Validation logic is identical pre/post migration —
the Phase 0b refactor only moved the success-path guidance into atom
rendering; error paths were unchanged. **Cross-check PASS for
service-not-found error path.**

### Scenarios NOT verified this run

The following 4 protocol scenarios require service provisioning +
state-driving on eval-zcp (estimated 4-8 hours per protocol §5):

| Scenario | Why deferred this run |
|---|---|
| `bootstrap/recipe/provision` | Needs a recipe-route bootstrap on a fresh `nodejs@22` runtime; observe at provision step. |
| `develop/first-deploy-dev-dynamic-container` | Needs dev-mode `nodejs@22` with `startWithoutCode: true`; never deployed; in-container. |
| `develop/standard-auto-pair` | Needs standard pair (`appdev`+`appstage`), close-mode auto, both deployed. Two-deploy minimum. |
| `strategy-setup/configured-build-integration` | Needs single runtime, GitPushState=configured, BuildIntegration=none. |
| `export/publish-ready` | Needs full export workflow driven to publish-ready (zerops.yaml + git remote + envClassifications). |

**These deferrals are expected** for the first quarterly run
(post-merge follow-up) per plan §5.5: "first live-eval run is
post-merge follow-up — NOT a pre-merge gate". Subsequent quarterly
runs will pick up these scenarios incrementally.

### Follow-ups

1. **MCP server restart** to load new code paths — agent action
   when convenient. Subsequent live-eval runs against eval-zcp will
   verify the migrated atom-rendered output (Phase 0b export
   workflow).

2. **Provisioning sequence** for next quarterly run: file a
   sub-plan (`plans/atom-corpus-quarterly-eval-2026-Q2.md` or
   similar) listing the 4 deferred scenarios with provisioning
   recipes. Recommended order: bootstrap/recipe/provision (sets up
   a service), develop/first-deploy-dev-dynamic-container (uses the
   service), strategy-setup/configured-build-integration (configures
   git-push), export/publish-ready (full workflow). standard-auto-
   pair needs separate provisioning of the pair.

3. **Fixture refinements from this run**: NONE — the one fixture
   shape we cross-checked (`idle/adopt-only`) matches production
   exactly. No edits to `scenarios_fixtures_test.go`.

### Cleanup

No services provisioned this run; no cleanup needed. eval-zcp
remains at its pre-run state (single `zcp` service).

---

(End of Q1 2026 entry. Next entry: Q2 2026 quarterly run, owner-
scheduled.)
