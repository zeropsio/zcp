# Plan: Engine improvements for atom rendering + adjacent UX (2026-04-27)

> **Reader contract.** Engine-side backlog complementary to the
> atom-corpus-hygiene cycles. Tickets are PROBLEM STATEMENTS +
> APPROACH PROPOSALS, not phased exec plans. Each ticket is
> independently shippable.
> Sister plans:
> - `plans/atom-corpus-hygiene-2026-04-26.md` (cycle 1)
> - `plans/archive/atom-corpus-hygiene-followup-2026-04-27.md`
>   (cycle 2; SHIP-WITH-NOTES; surfaced 2 Phase-8+ engine tickets)
> - `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` (cycle 3
>   in-flight; surfaces 1 additional engine ticket via Finding 6)

## 1. Problem

The atom-corpus-hygiene cycles 1+2+3 push corpus-quality work as
far as content edits can. Several SHIP-blocking or near-blocking
issues are NOT corpus-level — they're engine-level (rendering,
state management, error-response shape, lint tooling). Without
engine work, future hygiene cycles will hit the same structural
ceilings.

This plan consolidates 4 engine tickets surfaced across cycles
1+2+3:

1. **Multi-service single-render atom support** (combines
   cycle 2 Phase-8+ ticket #1 — two-pair structural Redundancy
   fail — and cycle 3 Finding 2 — 1-line per-service `-cmds`
   atoms).
2. **`zerops_deploy` error-response enrichment** (cycle 3
   Finding 3 deeper principle — agent shouldn't need zcli
   internals to triage failures).
3. **Auto-handle orphan metas** (cycle 3 Finding 6 — drop
   `idle-orphan-cleanup` atom; engine handles transparently).
4. **K/L/M (and N) lint enforcement** in
   `internal/content/atoms_lint.go` (cycle 2 Phase-8+ ticket #2
   — lint-rule additions to catch drift in future atom edits;
   cycle 3 adds Axis N to the same enforcement surface).

## 2. Goal

Each ticket is shippable independently. None block the others.
The engine plan does NOT supersede or block the corpus-hygiene
cycles — it runs on its own cadence. Cycle 4+ corpus work would
benefit from these engine tickets but doesn't require them.

Acceptance is per-ticket; this plan provides the technical
proposals + estimated blast radius + risks to inform sequencing
decisions.

## 3. Tickets

### Ticket E1 — Multi-service single-render atom support  *(SHIPPED 2026-04-27)*

> **Outcome.** Engine + corpus migration shipped. New scalar axis
> `multiService: aggregate` + body directive `{services-list:TEMPLATE}`
> implemented in `internal/workflow/{atom,synthesize}.go`; three atoms
> migrated (`develop-first-deploy-execute`, `develop-first-deploy-verify`,
> `develop-first-deploy-promote-stage`); two `-cmds` split atoms
> deleted. Two-pair fixture Redundancy 1 → 2; SHIP-WITH-NOTES on the
> two-pair shape now CLEAN-SHIP. Probe pinned by
> `internal/workflow/aggregate_render_probe_test.go`. Post-rescore at
> `plans/audit-composition-v3/post-e1-rescore-2026-04-27.md`.
>
> The fourth atom mentioned below (`develop-dynamic-runtime-start-
> container`) was NOT migrated — its body has no per-host placeholder so
> the existing post-substitution dedup in `Synthesize` already collapses
> it to 1×; the plan's "add per-service start invocations table" was
> editorial content-add, scoped out of E1.

**Problem**: when an atom has a per-service axis
(`modes`, `runtimes`, `strategies`, etc., service-scoped) AND
the envelope has multiple matching services, `Synthesize`
renders the atom ONCE PER MATCHING SERVICE. This duplicates the
atom body N times with hostname substitution.

For multi-service envelopes (e.g.
`develop_first_deploy_two_runtime_pairs_standard` with
`appdev` + `apidev` + `appstage` + `apistage`), atoms like
`develop-dynamic-runtime-start-container` and
`develop-first-deploy-promote-stage` render TWICE with different
hostnames. The §6.2 redundancy rubric counts these as restated
facts; cycle 2's two-pair fixture stuck at Redundancy=1 because
of this structural duplication.

Cycle 3 Finding 2 surfaces a related symptom: the
`develop-first-deploy-execute-cmds` and
`develop-first-deploy-verify-cmds` atoms are 1-line per-service
templates EXACTLY because of this rendering model. They exist to
emit one explicit command per service. Without engine support
for "iterate over services in single render", the split is
architecturally forced.

**Current state**:
- `internal/workflow/synthesize.go:Synthesize` walks atoms;
  for each atom with service-scoped axes, it iterates matching
  services and emits one rendered body per service.
- Hostname substitution is done via the placeholder replacer
  (`{hostname}`, `{stage-hostname}`, etc.).
- The atom body is REPLICATED per service; no aggregation form
  exists.

**Proposed approach (single-render iteration template)**:

Add an OPTIONAL frontmatter axis `multiService: aggregate`
(or similar) to atoms that should render ONCE with a service
list aggregated inline. The atom body uses a new placeholder
syntax — e.g. `{services-list:hostname}` — that the renderer
expands into a markdown table or bullet list of all matching
services.

Example atom body (post-engine-change):

```markdown
### First deploy — per-service execute

Run `zerops_deploy` for each runtime service that hasn't
been deployed:

{services-list:hostname:zerops_deploy targetService="{}"}
```

Renderer expands the `{services-list:hostname:...}` directive
into:

```markdown
- `zerops_deploy targetService="appdev"`
- `zerops_deploy targetService="apidev"`
```

(or as a code block if format=code, etc.)

Atoms that opt-in via `multiService: aggregate` render ONCE
regardless of service count. The hostname-iteration is
delegated to the renderer.

**Blast radius**:
- `internal/workflow/synthesize.go` — add aggregate-mode branch
  in the per-service iteration loop.
- `internal/workflow/atom.go` — add `multiService` axis parsing.
- `internal/workflow/replacer.go` (or similar) — add
  `{services-list:...}` placeholder template syntax.
- 4-6 atoms migrate to aggregate mode:
  - `develop-first-deploy-execute-cmds` → merged into
    `develop-first-deploy-execute` (delete `-cmds`).
  - `develop-first-deploy-verify-cmds` → merged into a new
    `develop-first-deploy-verify` (or existing).
  - `develop-dynamic-runtime-start-container` → aggregate (one
    table of dev-server start invocations per service).
  - `develop-first-deploy-promote-stage` → aggregate.
- Composition tests: re-render the two-pair fixture; expect
  Redundancy to move 1 → 2 (the atoms render once each, not
  per-service).

**Risks**:
- Aggregate render loses per-service contextual prose
  ("for `appdev`, ..." sections). Mitigation: the placeholder
  template is rich enough to embed contextual fragments.
- Atom AST tests may need new pin patterns for aggregate atoms.
- Some atoms genuinely benefit from per-service render
  (e.g. `develop-checklist-dev-mode` per dev service); not all
  service-scoped atoms convert.

**Estimated effort**: ~3-5 days of engine work + corpus
migration. Test impact: re-render all 5 fixtures; composition
re-score; G3 strict-improvement should close on two-pair
(Redundancy 1 → 2).

**Closes**:
- Cycle 2 Phase-8+ ticket #1 (two-pair structural).
- Cycle 3 Finding 2 (1-line `-cmds` atom split).

---

### Ticket E2 — `zerops_deploy` error-response enrichment

**Problem**: when `zerops_deploy` fails, the response carries
generic fields (`status`, `failedPhase`, `buildLogs`,
`runtimeLogs`). The agent has to interpret raw logs to find the
issue. The corpus partially compensates by teaching the agent
about dispatch-layer specifics (e.g. `zcli push` errors,
push-dev SSH failure modes, push-git credential issues).

Cycle 3 Finding 3 surfaces this principle: the agent shouldn't
need to know `zcli push` exists; it should call `zerops_deploy`
and reason about ENRICHED error fields that point to the actual
issue.

**Current state**:
- `internal/tools/deploy.go` (and `internal/ops/deploy.go`)
  return raw platform errors + log streams.
- `platform.APIError.Code` is sometimes set (e.g.
  `GIT_TOKEN_MISSING`, `PREREQUISITE_MISSING`) but not
  consistently across all failure modes.
- The corpus fills the gap with prose like "Read
  `platform.APIError.code` for...".

**Proposed approach**: add a `DeployFailureClassification` type
to `ops.DeployResult` that the deploy handler populates BEFORE
returning. The classifier reads:
- `failedPhase` (build / push / start / verify).
- Build/runtime log tails for known patterns (port-already-in-use,
  module-not-found, missing-env-var, healthcheck-timeout, etc.).
- `platform.APIError.Code` if set.
- Strategy (push-dev / push-git / manual) for credential-class
  failures.

And emits a structured `DeployFailureClassification` with:
- `category` (BUILD_FAILED / RUNTIME_FAILED / CREDENTIAL_MISSING
  / CONFIG_INVALID / NETWORK_TIMEOUT / etc.).
- `likelyCause` (one-sentence diagnosis).
- `suggestedAction` (next tool call or YAML edit).

The agent reads `result.failureClassification` instead of
parsing logs.

**Blast radius**:
- New type in `internal/ops/deploy.go` or
  `internal/platform/...`.
- Classifier function — pattern library across the known failure
  modes (build / push / start / verify per strategy).
- `internal/tools/deploy.go` populates the field.
- Atoms drop the "read buildLogs / failedPhase" prose; replace
  with "read failureClassification" cross-link.

**Risks**:
- Classifier maintenance: new failure modes need new patterns.
  Mitigation: classifier is best-effort — when no pattern
  matches, emit `category: UNKNOWN` and the agent falls back to
  log inspection.
- Test surface: per-classification unit tests + integration
  tests against eval-zcp.
- Drift: classifier patterns can drift from log message format
  changes upstream (Zerops platform). Mitigation: classifier
  patterns reviewed quarterly; failure-mode catalog committed.

**Estimated effort**: ~5-10 days. Pattern library is the bulk;
type + integration + corpus updates are tractable.

**Closes**:
- Cycle 3 Finding 3 deeper principle.
- Reduces `zcli push` mention surface in corpus by ~60-70%
  (most remaining mentions are tool-chain literal commands the
  agent writes — those KEEP).

---

### Ticket E3 — Auto-handle orphan metas

**Problem**: cycle 3 Finding 6 — the
`internal/content/atoms/idle-orphan-cleanup.md` atom is
bookkeeping noise. The detection (`computeOrphanMetas`) is
mechanical; the cleanup is one command (`zerops_workflow
action="reset" workflow="bootstrap"`); the LLM adds zero
judgment. Surfacing this to the LLM burns a turn for an
operation the engine can do transparently.

**Current state**:
- `internal/workflow/compute_envelope.go:92` computes orphans
  via `computeOrphanMetas(services, metas, alivePIDs,
  sessionByID)`.
- `internal/workflow/build_plan.go:199` — when bootstrap=0 +
  adoptable=0 + orphans > 0 + no live → primary action =
  `resetOrphanMetasAction()`.
- The atom fires on `idleScenarios: [orphan]` and instructs
  `zerops_workflow action="reset" workflow="bootstrap"`.

**Proposed approach** — Option B from cycle 3 Finding 6
analysis: auto-prune as a side-effect of `zerops_workflow
action="start" workflow="bootstrap"`.

When bootstrap-start runs and orphan metas exist, the engine:
1. Detects orphans via the existing `computeOrphanMetas` path.
2. Deletes the orphan meta files transparently.
3. Includes a `cleanedUpOrphanMetas: ["{hostname1}",
   "{hostname2}"]` field in the bootstrap-start response shape.
4. Proceeds with bootstrap as normal.

The agent calling `start` gets:
- The normal bootstrap-discovery response.
- An informational note: "cleaned up N orphan metas before
  starting".

The `idle-orphan-cleanup` atom is dropped.
`build_plan.go:resetOrphanMetasAction()` is removed; the
orphan-only idle scenario routes to the standard
"start a bootstrap" primary action.

**Blast radius**:
- `internal/workflow/build_plan.go` — drop
  `resetOrphanMetasAction()`; orphan-only scenario routes to
  `start bootstrap` primary action.
- `internal/workflow/compute_envelope.go` — IdleScenario
  derivation: orphan-only no longer surfaces; collapses to
  `IdleEmpty` or `IdleAdopt` based on remaining state.
- `internal/tools/workflow.go` (bootstrap-start handler) — add
  pre-start orphan-cleanup side-effect; populate
  `cleanedUpOrphanMetas` response field.
- `internal/content/atoms/idle-orphan-cleanup.md` DELETED.
- Tests: corpus-coverage update; bootstrap-start integration
  test asserting orphan cleanup happens.

**Risks**:
- Stateless STDIO invariant: bootstrap-start gains a
  side-effect (file deletion). Mitigation: side-effect IS state
  CORRECTION (deleting files that point to nothing), not state
  CREATION; documented in tool description; surfaced in response.
- Loss of audit trail: agent never sees "you had 3 orphan
  metas". Mitigation: response field
  `cleanedUpOrphanMetas` provides post-fact transparency.
- Edge case: the existing
  `zerops_workflow action="reset" workflow="bootstrap"` command
  becomes redundant for the orphan case. KEEP the command — it
  has other uses (manual reset of any active bootstrap session).

**Estimated effort**: ~1-2 days. Small surface; test+integration
straightforward.

**Closes**:
- Cycle 3 Finding 6.

---

### Ticket E4 — K/L/M/N axis lint enforcement

**Problem**: cycles 2+3 introduce 4 content-quality axes:
- Axis K (abstraction-leak) — cycle 2 §3
- Axis L (title-over-qualified) — cycle 2 §3
- Axis M (terminology-drift) — cycle 2 §3
- Axis N (universal-atom-carries-per-env-detail) — cycle 3 §3

These axes are AUTHOR-FACING rules currently; the
atom-corpus-hygiene cycles audit them via Codex CORPUS-SCAN
rounds. There's NO LINT enforcement to catch drift in future
atom edits.

Cycle 2 explicitly noted: "These are author-facing rules, NOT
lint-enforced (yet)". Cycle 3 inherits that note and adds
Axis N to the same surface.

Future atom edits (by humans or LLMs) can re-introduce drift,
silently, without any test catching it. The hygiene work is
preserved by the LACK of regressions, but there's no mechanical
guardrail.

**Current state**:
- `internal/content/atoms_lint.go` enforces:
  - Spec invariant IDs (DM-*, E*, O*, etc.) — forbidden.
  - Handler-behavior verbs ("auto-stamps", "activates", etc.)
    — forbidden.
  - Invisible-state field names — forbidden.
  - Plan-doc paths — forbidden.
- Test: `TestAtomAuthoringLint`.

**Proposed approach** — add 4 new lint rule families:

**Axis K lint** (`atomLintAxisK`):
- Detect "no SSHFS" / "no dev container" / "no X here" patterns
  AS LEAK CANDIDATES (not forbidden — they may be guardrails).
- Surface as INFO/WARN: list candidate leaks per atom with
  signal-check classifier (does any of #1-5 apply?).
- Author MUST add a comment marker `<!-- axis-k-keep:
  signal-#N -->` or `<!-- axis-k-drop -->` to indicate they
  reviewed.
- Lint fails on uncommented leak candidates.

**Axis L lint** (`atomLintAxisL`):
- Walk atom titles + H1/H2/H3 for env-only qualifier suffixes
  (`(container)`, `(local)`, `— container`, etc.).
- HARD-FORBID env-only qualifiers; allow mode/runtime/strategy
  distinguishers per the cycle-2 worked examples.
- Mechanism payload (`GIT_TOKEN + .netrc`, `user's git`) detected
  via heuristic — preserve.
- Lint fails on env-only qualifier in title/headers.

**Axis M lint** (`atomLintAxisM`):
- Detect canonical-violations per cluster:
  - Cluster #1 (container): forbidden bare "the container" /
    "service container" without `dev|runtime|build|new`
    prefix.
  - Cluster #3 (Zerops/ZCP): forbidden bare "the platform" /
    "the tool" without context disambiguator.
  - Cluster #5 (agent): forbidden "the agent" / "the LLM" in
    new atoms (existing atoms grandfathered via allowlist).
- Allowlist for legitimate exceptions (e.g.
  `develop-verify-matrix` "agent" = sub-agent).

**Axis N lint** (`atomLintAxisN`):
- For each atom WITHOUT `environments:` axis restriction (or
  with both env values), grep body for env-specific tokens:
  "locally", "your machine", "SSHFS", "/var/www/{hostname}",
  "container env", "local env".
- Surface as candidate leak; require `<!-- axis-n-... -->`
  marker.

**Blast radius**:
- `internal/content/atoms_lint.go` — 4 new lint families.
- `internal/content/atoms_lint_test.go` — 4 new test functions
  + golden-file failures.
- 79 atoms — initial allowlist for grandfathered legitimate
  exceptions; review per atom; lint passes after allowlist seed.
- New comment-marker convention for atom authors.

**Risks**:
- False positives on Axes K + N (heuristic detection of "leaks");
  marker-convention adds author burden. Mitigation: HARD-FORBID
  only the unambiguous patterns (Axis L env-only suffixes); use
  WARN/marker pattern for ambiguous (Axis K, Axis N).
- Allowlist drift: grandfathered exceptions accumulate
  technical debt. Mitigation: allowlist entries require
  rationale comment + audit cadence (e.g. each hygiene cycle).
- Codex / LLM edits may not understand the marker convention.
  Mitigation: document in `docs/spec-knowledge-distribution.md
  §11.5/§11.6` + atom author guide.

**Estimated effort**: ~5-7 days. Bulk is the allowlist seed +
per-atom audit (one pass corpus-wide). Lint code itself is
straightforward.

**Closes**:
- Cycle 2 Phase-8+ ticket #2.
- Future cycle drift prevention.

## 4. Suggested execution order

These tickets are independent. Suggested order optimizes for:
- Earliest impact on hygiene-cycle structural blockers.
- Smallest first ticket as engine warm-up.

**Recommended order**:

1. **E3 — Auto-handle orphan metas** (smallest; ~1-2 days). Easy
   warm-up; immediate corpus reduction (drop 1 atom).
2. **E4 — K/L/M/N lint enforcement** (~5-7 days). Locks in the
   hygiene-cycle work; prevents future drift. Independent of
   render-engine changes.
3. **E1 — Multi-service single-render** (~3-5 days). Closes
   the two-pair structural Redundancy gate inherited from
   cycle 2 SHIP-WITH-NOTES; closes Finding 2.
4. **E2 — `zerops_deploy` error-response enrichment** (~5-10
   days). Most ambitious; benefits the dispatch-layer cleanup
   from cycle 3 Finding 3 (Phase 2). Can ship before E1+E3 if
   prioritized.

Total estimated engine effort: ~15-25 days across 4 tickets.

## 5. Cross-references

| Engine ticket | Closes hygiene-cycle artifact |
|---|---|
| E1 multi-service single-render | cycle 2 Phase-8+ ticket #1 (two-pair structural) + cycle 3 Finding 2 (1-line `-cmds` atoms) |
| E2 deploy error enrichment | cycle 3 Finding 3 deeper principle |
| E3 auto-handle orphan metas | cycle 3 Finding 6 |
| E4 K/L/M/N lint enforcement | cycle 2 Phase-8+ ticket #2 |

After E1 ships, cycle 4 hygiene work could re-score the two-pair
fixture and (likely) move it from G3-STRUCTURAL-FAIL to G3-PASS.
After E3 ships, the `idle-orphan-cleanup` atom drops from the
corpus; aggregate atom count decreases.

After E2 ships, cycle 3 Phase 2 (zcli-push cleanup) is
reinforced — the dispatch-layer mentions become explicitly
unnecessary because the agent has structured failure data.

After E4 ships, future hygiene cycles can rely on lint rather
than Codex CORPUS-SCAN to catch most drift.

## 6. Out of scope

- **Render-engine performance optimization** — not a hygiene
  concern; tracking surface for P95 STDIO response latency in
  a separate engine doc.
- **Atom storage refactor** (e.g. moving atoms to a database)
  — overkill; current `//go:embed` filesystem approach is the
  right granularity.
- **LLM-specific prompt engineering** — atoms are model-agnostic
  by design; specific Claude/GPT/etc. tuning belongs in
  `claude_*.md` boot-shim files.

## 7. Provenance

Drafted 2026-04-27 alongside cycle 3 hygiene mini-plan. Cycle 2
identified 2 Phase-8+ engine tickets at SHIP-WITH-NOTES
disposition; cycle 3 surfaced 2 more during user audit. This
plan consolidates all 4 into a single engine backlog with
technical proposals.

The hygiene cycles have hit the corpus-quality ceiling — further
significant gains require engine work. This plan provides the
roadmap.

Tickets are intentionally PROBLEM/PROPOSAL shape, not phased
exec plans, because:
- Each is small enough (1-10 days) that phased planning is
  overkill.
- The technical proposal IS the action plan.
- Engine work cadence is independent of hygiene-cycle cadence;
  prioritization is per-ticket.

## 8. Tracking

Each ticket gets:
- A GitHub issue (or equivalent) with this plan as design link.
- A feature branch.
- A standard PR review cycle.
- A post-merge test confirming the closed hygiene-cycle artifact
  (e.g. E1 confirms two-pair Redundancy moves to 2 via
  `post-followup-scores.md` re-score).

After all 4 tickets ship, this plan archives to
`plans/archive/engine-atom-rendering-improvements-2026-04-27.md`.
