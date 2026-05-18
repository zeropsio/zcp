# Schema service-type form migration — audit + design plan

**Status:** draft for repo-owner review
**Date:** 2026-05-17
**Inputs:** `/tmp/zcp-schema-audit-explore.md` (Explore agent footprint
audit) + `/tmp/zcp-schema-audit-codex.md` (Codex independent design
opinion)

---

## What's going on

Zerops platform supports two service-type forms — **bare** (`nodejs@22`,
`postgresql@18`) and **verbose** (`ubuntu/nodejs@22`,
`postgresql:single@18`). Platform JSON schema declares verbose only;
platform API accepts both. ZCP's embedded schema was frozen on bare-only
(April 2026), discover output (from SDK) returns verbose, and a recent
fix landed a union-merge (bare aliases derived for every verbose entry)
so the embedded validator accepts both forms — same posture as live
API.

The union merge unblocks `launch-production` end-to-end (verified by
third flow-eval run, agent reached `status="launched"` atomically). But
it only fixed **validator permissiveness**. Several semantic-level
issues remain unaddressed:

- **`topology.RuntimeClassFor("ubuntu/php-nginx@8.4")` does not detect
  implicit-web runtime class** — prefix matcher looks for `php-nginx`
  in raw string, but the verbose form starts with `ubuntu/`.
  Same for `RuntimeClassFor("alpine/static")` → static-class miss.
  Real Discover data exposes this today.
- **`internal/tools/workflow_checks.go::checkServiceType` does
  raw-string equality** — a bare plan vs verbose API service flags as
  mismatch even though the platform considers them the same type.
- **All test fixtures use bare form**; real API returns verbose. Mocks
  diverge from live behavior.
- **No drift detection** — embedded schema can fall arbitrarily far
  behind live without any signal short of a flow-eval failure.

Plus the recent schema sync was done via a one-shot `/tmp/` Python
script. Repo is Go-only; tooling needs Go equivalent.

---

## Codex's net recommendation (and where I agree)

> "ZCP should mirror platform API compatibility at acceptance points,
> but take the stronger position that **ZCP-owned generated output is
> verbose**."

This is the cleanest split I've seen articulated:

- **Acceptance points** (`schema.ValidateImportYAML`, `topology.IsManagedService`,
  bootstrap-plan validation, semantic equality checks) — accept both
  forms. ZCP mirrors what the platform actually does.
- **ZCP-authored output** (composer emit to import.yaml, scaffolded
  zerops.yaml templates, recipe import YAML where ZCP owns the
  artifact) — verbose canonical. We don't generate legacy form
  ourselves.

This sidesteps the symmetric-helper trap (`BareServiceType` /
`VerboseServiceType`) that reverted earlier. There's no directional
normalizer — there's a typed value with parser + equality.

### Boundary choice — source-read, via topology parser

Codex argues for **(a) source-read canonicalize + topology parser** over
the alternatives. I agree. The reasoning:

| Option | Problem |
|---|---|
| (b) composer-emit | Every composer becomes a policy owner — re-creates the BareServiceType class of patch |
| (c) validator-only | Solves API compat, doesn't fix `checkServiceType` / `RuntimeClassFor` semantic bugs |
| (d) platform-layer normalize | Hides drift; violates `internal/platform/` raw-API-only contract |

The new type lives in `internal/topology/` (zero non-stdlib imports per
the layering rule pinned in `architecture_test.go`). Shape:

```
type ServiceType struct {
    Family   string  // "nodejs", "postgresql", "static"
    Version  string  // "22", "18", "" for unversioned
    OSPrefix string  // "ubuntu", "alpine", "" for managed deps
    Variant  string  // "single", "ha", "" for runtimes
}

func ParseServiceType(s string) (ServiceType, error)
func (s ServiceType) String() string         // canonical (verbose) form
func (s ServiceType) BareString() string     // legacy form
func (s ServiceType) Equal(other ServiceType) bool
```

Predicates in `topology/predicates.go` and `runtime_class.go` parse
first, then dispatch on `.Family`. Equality on `.Family + .Version`,
ignoring prefix/variant.

---

## What Codex pushed back on from the audit

1. **"Topology prefix matching is form-agnostic"** — false. Audit's
   Section A claimed `runtime_class.go` handles either form; Codex
   verified raw-string prefix check fails on `ubuntu/php-nginx@8.4` and
   `alpine/static`. Audit was light here.
2. **"Rewrite all test fixtures"** wasn't in audit explicitly, but a
   blanket sweep would be wrong. Primary Discover/launch fixtures →
   verbose (match live); keep targeted bare fixtures as API-compat
   coverage.
3. **`develop-env-var-model.md` doesn't mention service types** — audit
   listed atom-corpus mentions but didn't field-segregate. Field-aware
   sweep only: atoms showing `import.yaml services[].type` → verbose;
   atoms showing `zerops.yaml build.base` → either (both still valid
   per live schema).
4. **Recipes are not pre-migrated.** Audit noted recipes gitignored;
   Codex pulled and confirmed `internal/knowledge/recipes/*.import.yml`
   carry bare form. Code change must accept both — downstream recipe
   PR is separate work.

---

## Key decisions repo owner must confirm before Phase 1

1. **Canonical form for ZCP-authored output: verbose.** Composer emits
   `ubuntu/nodejs@22`, not `nodejs@22`. Even when source-side reports
   bare (rare but possible). Confirm? *(Recommended: yes.)*

2. **Acceptance: both forms, semantically equal.** `RuntimeClassFor`,
   `checkServiceType`, etc. parse and compare canonical identity, not
   raw string. Bare and verbose with same `(Family, Version)` resolve
   identically. Confirm? *(Recommended: yes.)*

3. **Schema sync = Go tool, committed JSON.** `cmd/schema-sync/main.go`,
   `make schema-sync` target, no runtime fetch in validators. Replaces
   the `/tmp/merge_schema_bare.py` ad-hoc script. Confirm? *(Recommended:
   yes.)*

4. **Drift CI = scheduled `ZCP_SCHEMA_LIVE=1` test.** Public schema URLs
   don't need a token; daily/weekly cron fetches live, regenerates
   merged JSON, byte-compares to committed. PR if drift detected.
   Confirm pattern? *(Recommended: yes.)*

5. **Phase stop/review after Phase 3.** Code understands both forms
   semantically by then. Before Phase 4 starts changing generated
   outputs + atom/recipe surfaces. Confirm? *(Recommended: yes.)*

6. **Atom/recipe sweep ownership.** Phase 6 touches
   `internal/content/atoms/` (in repo) and `internal/knowledge/recipes/`
   (gitignored, synced from `zeropsio/docs`). Atoms are mine to edit;
   recipe content is Aleš's pipeline. Phase 6 will: (a) edit atoms
   where `services[].type` examples appear, (b) flag recipes for Aleš's
   sync push. Confirm scope split?

---

## Transition plan — 6 phases

Per CLAUDE.local.md "5-file phase caps are about phase verifiability,
not total work" — phase counts are about review-ability per step.

### Phase 1 — Go schema-sync (smallest viable change)

**Files:**
- new `cmd/schema-sync/main.go` (~80 lines)
- new `internal/schema/sync.go` (fetch+merge helper, ~120 lines)
- new `internal/schema/sync_test.go` (idempotent merge, deterministic
  output)
- regenerated `internal/schema/testdata/import_yml_schema.json` +
  `zerops_yml_schema.json`
- `Makefile` — add `schema-sync` target

**Replaces:** `/tmp/merge_schema_bare.py` (already gone, conceptually).

**Gate:** `go test ./internal/schema -short` green; `make schema-sync`
on freshly-stashed JSON produces byte-identical output to what was
already committed; `go test ./internal/ops -run LaunchBundle -short`
green (composer unaffected — schema permissive both forms).

### Phase 2 — Topology service-type parser

**Files:**
- new `internal/topology/service_type.go` (~150 lines — struct +
  parser + equality + canonical/bare emit)
- new `internal/topology/service_type_test.go` (~100 lines table-
  driven, covering OS prefix / scope variant / unversioned / managed
  / runtime, plus equality matrix bare↔verbose)

**No call-site migration yet.** Pure value type + tests.

**Gate:** `go test ./internal/topology -short`.

### Phase 3 — Classification & equality migration

**Files (~5):**
- `internal/topology/runtime_class.go` — parse first, dispatch on
  `.Family`
- `internal/topology/predicates.go` — `IsManagedService`,
  `IsRuntimeType`, etc. parse first
- `internal/workflow/<service-type readers>` — relevant per audit
  Section A; up to 2 files
- `internal/tools/workflow_checks.go::checkServiceType` — use
  `ServiceType.Equal` for identity
- existing test fixtures stay bare-form (still pass — both equivalent
  under the parser)

**STOP / REVIEW with repo owner here.** Code understands both forms
semantically by end of Phase 3 without changing any generated output.
This is the safest checkpoint — verify behavior against eval-zcp
before risking Phase 4+.

**Gate:** `go test ./internal/topology ./internal/workflow ./internal/tools -short`;
`make lint-local`; one flow-eval scenario re-run on a scenario that
exercises classification (e.g.
`existing-standard-appdev-only-reminders`).

### Phase 4 — Canonical ownership boundaries

**Files (~5):**
- `internal/tools/launch_source_read.go` — `LaunchSourceState.ServiceType`
  documented as canonical verbose; managed entries normalized via
  `collectManagedServices` to canonical.
- `internal/ops/bundle/inputs.go` — `BundleInputs.ServiceType` doc
  updated.
- `internal/ops/bundle/inputs_test.go` and `internal/ops/bundle/export_test.go`
  — primary path uses verbose source data; **keep** targeted
  bare-form fixtures asserting compat.
- `internal/tools/workflow_export_probe.go::collectManagedServices` —
  parse + canonical emit.
- pin test: existing-project mutation with `discover` returning verbose
  → composer emits verbose → schema validates → mock platform import
  accepts.

**Gate:** `go test ./internal/tools ./internal/ops -run 'Launch|Bootstrap|Export' -short`;
flow-eval `launch-to-existing-prod-project`.

### Phase 5 — Live drift CI hook

**Files (~2):**
- new `internal/schema/live_drift_test.go` — fetches live, runs same
  alias expansion as `cmd/schema-sync`, byte-compares to embedded JSON.
  Also runs workflow-shaped probes (launch import with
  `ubuntu/nodejs@22`, managed `postgresql:ha@18`, bare aliases
  `nodejs@22` / `postgresql@18`).
- gating: `testing.Short()` AND `os.Getenv("ZCP_SCHEMA_LIVE") == "1"`
  → run; otherwise skip with diagnostic message.
- CI/cron documentation update (which file owns scheduled-run config —
  TBD, depends on existing CI setup).

**Gate:** `go test ./internal/schema -short` still passes
(test skips); manual `ZCP_SCHEMA_LIVE=1 go test ./internal/schema`
passes against current live schema.

### Phase 6 — Downstream sweep

**Files (likely 5-10 + recipe sync):**
- `internal/content/atoms/bootstrap-classic-plan-dynamic.md` — worked
  example `services[].type` → verbose
- `internal/content/atoms/bootstrap-recipe-match.md` — same
- `internal/content/atoms/scaffold-zerops-yaml.md` — check if
  `services[].type` appears; if only `zerops.yaml build.base`, leave
  bare
- recipe import YAML — flag for Aleš's sync push; document in
  `plans/backlog/` what needs upstream-side update
- run `flow-eval launch-to-existing-prod-project` final clean

**Gate:** `make lint-local`; `go test ./internal/content ./internal/workflow -short`;
flow-eval green.

---

## Open questions / risks

1. **`platform.ServiceStackTypeVersionName` field naming.** This is the
   SDK-bridge field. Once we have `topology.ServiceType`, do we rename
   downstream fields (`LaunchSourceState.ServiceType` is fine, but
   `BundleInputs.ServiceType` is a string — promote to typed?). Cost
   vs clarity. *Default: keep strings at API boundaries, typed inside
   business logic.*

2. **`zerops.yaml build.base` / `run.base` forms.** Live `zerops_yml_schema.json`
   accepts both forms via union (per current state). Atoms teach bare.
   Should `scaffold-zerops-yaml.md` change canonical recommendation?
   *Codex: stays bare-acceptable for `build.base`/`run.base` because
   the platform sees these as runtime hints, not service identity.
   Defer until Phase 6 review.*

3. **Recipe pipeline coordination.** `internal/knowledge/recipes/` is
   `zerops_sync_push`'d from `zeropsio/docs` upstream. Recipe import
   YAML changes need to happen there, not here. Coordination question
   for Aleš.

4. **Codex's "live API platform API accepts both" claim** is unverified
   programmatically. Phase 5 drift test should include explicit
   round-trip: `POST` an import with bare `nodejs@22`, assert success;
   same with verbose. If platform deprecates bare server-side in the
   future, drift test catches it.

5. **Disagreement Codex flagged with audit.** Audit Section A claimed
   topology predicates are "form-agnostic." This is not currently true
   for verbose runtimes. Phase 3 fixes it; until then, eval scenarios
   that exercise verbose runtime classification (anything with non-`db`
   non-bare hostname pair) may misclassify. Workaround: continue with
   bare-form scenarios until Phase 3 ships.

---

## What lands when

| Phase | Risk | Files | Verification | Owner |
|---|---|---|---|---|
| 1 — schema-sync Go tool | low | ~5 | unit | me |
| 2 — topology parser | low | ~2 | unit | me |
| 3 — classification migration | medium | ~5 | unit + 1 flow-eval | me |
| **(stop / review)** | — | — | repo-owner judgment | — |
| 4 — canonical ownership | medium | ~5 | unit + flow-eval | me |
| 5 — live drift CI | low | ~2 | gated test + cron doc | me |
| 6 — downstream sweep | high (atoms/recipe) | ~5-10 | lint + flow-eval | me + Aleš |

---

## References

- Explore audit (footprint map): `/tmp/zcp-schema-audit-explore.md`
- Codex opinion (architectural design): `/tmp/zcp-schema-audit-codex.md`
- Recent work that informs this: 5 review bugs fix session
  (uncommitted), schema union-merge (committed-pending), flow-eval
  runs `20260517-172802`, `20260517-174900`, `20260517-181141`,
  `20260517-185653`.
- Layering pin: `internal/topology/architecture_test.go`
- Spec: `docs/spec-architecture.md`,
  `docs/spec-knowledge-distribution.md`
