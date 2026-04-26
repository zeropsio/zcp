# Run 14 readiness — implementation plan

Run 13 (`nestjs-showcase`, 2026-04-26) was the first dogfood after the
post-run-13-readiness engine. The TEACH-side wins of run 13 — §Q (template
strip), §T (tier capability matrix), §F (showcase scenario), §N
(init-commands distinct keys), §U (alias resolution timing), §B2
(engine-composed dispatch prompt) — all reached the deliverable
cleanly. Wrapper share dropped from run-12's 28-38% to **5.7-6.8%**;
six SPA panels shipped against the §F mandate; the run-12 init-commands
key collision and cross-service alias build-time race were prevented at
compose time, not rediscovered at deploy time. Apps-repo READMEs lifted
from A-/B+ → A.

But the run did not close cleanly. Eleven of run-13's 21 defect entries
share a single architectural root cause: **the engine grew positive
shapes without auditing the I/O boundary that materializes them**. The
§3 auto-stitch landed correctly in design; the validator's same-handler
disk-read against an SSHFS-coherent filesystem did not. Once the engine
read a 0-byte view of a 14746-byte file, every downstream phase blocked.
Finalize never dispatched. Tier READMEs collapsed from 52-60 lines
(run-12) to 9 lines each (run-13). Tier yaml comments are absent. The
content grade dropped from 7/10 (run-12) to **6.5/10** — not because
content quality regressed, but because content authored downstream of
the blocked phase gate never authored at all.

Run 14's bar is structural recovery + fix-the-fix-discipline. Five
clusters, smaller surface than run-13, each addressing one root cause:

- **Cluster A** — engine I/O coherence (stitch ↔ disk ↔ validator read
  + symmetric platform-state materialization for subdomain). This is
  the gating cluster; without it, no other workstream can be validated.
- **Cluster B** — engine reserved-semantics surfacing (`mode=replace`
  semantics, `${...}` token-clash on agent-recorded fragments,
  recipe-slug enumeration).
- **Cluster C** — session-state survival across defensive re-dispatch
  + compaction.
- **Cluster D** — operational preempts that recurred for the third run
  in a row (Vite host-allowlist, git-identity, scaffold close
  ordering, browser-walk staleness, watcher PID volatility).
- **Cluster E** — content-discipline tighteners (porter-audience rule
  reach, IG-scope sweet-spot clarity, retiring `verify-subagent-dispatch`
  now that §B2 makes it redundant).

Five clusters, fewer workstreams, sharper specs. Run-13-readiness was
2157 lines; this plan targets ≤ 1800.

Reference material:

- [docs/zcprecipator3/runs/13/ANALYSIS.md](../runs/13/ANALYSIS.md) —
  run-13 forensic, R-13-1..R-13-21 with root-cause synthesis at §6.
- [docs/zcprecipator3/runs/13/CONTENT_COMPARISON.md](../runs/13/CONTENT_COMPARISON.md) —
  surface-by-surface vs `/Users/fxck/www/laravel-showcase-app/`.
  Honest aggregate **6.5/10**.
- [docs/zcprecipator3/runs/13/PROMPT_ANALYSIS.md](../runs/13/PROMPT_ANALYSIS.md) —
  turn-by-turn timeline, dispatch sizing, R-13-P-1..R-13-P-22 evidence.
- [docs/zcprecipator3/system.md](../system.md) §1 (audience model)
  + §4 (TEACH/DISCOVER line + verdict table).
- [docs/spec-workflows.md §4.8](../../spec-workflows.md) — subdomain
  L7 activation as deploy-handler concern.
- [docs/zcprecipator3/plans/run-13-readiness.md](run-13-readiness.md) —
  prior plan; Q/T/V/F/N/U/I-feature/W/G2/Y2D/B2 all shipped (some
  with semantic gaps R-13-2/-3 surface).
- [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) top entry
  "2026-04-26 — run-13 readiness: tier-fact + showcase scenario +
  per-codebase scoping + dispatch wrapper".

---

## 0. Preamble — context a fresh instance needs

### 0.1 What run 13 produced and what it missed

Produced (TEACH-side wins held end-to-end):
- §Q template strip — three published CLAUDE.md files have header +
  service-facts + notes only. Zero template-injected `## Zerops dev
  loop` block.
- §T tier capability matrix — tier 5 README claims `minContainers: 2`
  not "three replicas"; meilisearch correctly NON_HA at tier 5.
- §F showcase scenario — six SPA panels (Items / Cache / Queue /
  Storage / Search / Status); six browser-verification facts. The
  Queue panel visibly demonstrates the publish→worker→`item_events`
  round-trip run 12 missed.
- §N + §U traps prevented at compose time, not rediscovered at deploy.
  No `execOnce-key-collision` fact; no `cross-service-alias-resolution-timing`
  fact.
- §B2 engine-composed dispatch — wrapper share **5.7-6.8%** across all
  four dispatches; the largest surface-reduction-per-LoC win since v3.

Missed (R-13-1's blocking blast radius):
- Finalize phase never entered. Tier READMEs are 9 lines each. Tier
  yaml comments are absent. Whatever §V validates couldn't fire because
  there was no prose to validate. §Y2D dedupe had nothing to dedupe.
  Acceptance criterion 28 (finalize wrapper within 10% of brief) was
  unmeasurable.
- Run 13's content grade dropped from run-12's 7/10 to 6.5/10 — not a
  regression in authored content (the codebase READMEs and apps-repo
  zerops.yamls lifted to A) but a structural absence of the finalize
  surfaces.

### 0.2 The audit that run-13-readiness did not do

Run-13's §3 auto-stitch made `complete-phase` automatically call
`stitchCodebases` (eliminating the "remember to call stitch first"
ritual). That's a positive shape. But the same handler invocation
that wrote the codebase README/CLAUDE.md to disk then re-read those
files for validator input — and the disk in question is an SSHFS
mount whose write-back coherence is FUSE-page-cache-bound, not
local-filesystem-bound. The handler read out of the page cache before
the kernel flush sequence completed; the validator saw 0 bytes for a
file that `wc -c` independently confirmed at 14746 bytes.

The run-13-readiness plan's §3 cleanup entry described the win
("complete-phase should be a single semantic transition") but did
not audit what happens when the writer is a Go `os.WriteFile` call,
the reader is the Go `os.ReadFile` call 50 microseconds later, and
the boundary between them is a network filesystem.

The meta-fix for run-14 readiness: **trace every engine-as-deterministic-emit-shape
claim to the I/O boundary that materializes it.** If the
materialization crosses a filesystem-coherence boundary, the
validator must not treat that materialization as ground truth — the
in-memory body is ground truth. This is encoded in the §6 risks
section as a recurring watch.

### 0.3 The R-13-12 symmetric defect

R-13-12 (subdomain not auto-enabled on dev-container deploy) is
structurally R-13-1's twin. Both share *engine has the truth (yaml
declares intent / fragment body in memory), runtime doesn't
materialize it, agent has to operationally trigger materialization*.
R-13-1's truth is in fragment memory; R-13-12's truth is in yaml's
`enableSubdomainAccess: true`. R-13-1's runtime gap is the SSHFS read
boundary; R-13-12's runtime gap is the deploy handler's `meta == nil`
short-circuit at
[`internal/tools/deploy_subdomain.go:38-42`](../../../internal/tools/deploy_subdomain.go#L38-L42)
— recipe-authoring deploys go through `zerops_import content=<yaml>`,
which doesn't write the per-PID `workflow.ServiceMeta` files the
auto-enable path expects. Auto-enable is silently skipped for every
recipe-authoring deploy.

Both belong to Cluster A. Both fixes are TEACH-side per system.md §4
— engine resolves runtime state by construction, agent doesn't have
to operationally trigger it.

### 0.4 Workstream legend

| Letter | Cluster | Tranche | Type |
|---|---|---|---|
| **A** | Engine I/O coherence (stitch ↔ disk ↔ validator + subdomain materialization) | T1 | engine (~80-120 LoC) |
| **B** | Engine reserved-semantics surfacing (replace, `${...}`, slug catalog) | T2 | engine (~30 LoC) + brief (~25 lines) |
| **C** | Session-state survival across re-dispatch | T2 | engine (~30-50 LoC) + brief (~10 lines) |
| **D** | Operational preempts (Vite, git-identity, ordering, browser staleness, PID volatility) | T3 | content-only (~60 lines) |
| **E** | Content-discipline tighteners (porter-audience reach, IG-scope clarification, retire verify-subagent-dispatch) | T3 | content (~15 lines) + 5 LoC engine retire |

T1 unblocks every downstream surface. T2 closes the engine reserved-
semantics gaps and the session-state gap. T3 is content-only operational
hygiene. Total: ≤ 200 LoC engine work + ~110 lines of brief content +
one CHANGELOG sign-off.

### 0.5 What the discipline of this plan looks like

- **Cluster, not symptom.** Run-13 produced 21 R-13-N entries; 11 of
  them collapse into Cluster A's single architectural defect. The
  workstream count is 5, not 21.
- **TEACH/DISCOVER classification per workstream.** Every workstream
  states which side it lives on and why. Cluster A is uniformly TEACH
  (engine resolves materialization by construction). Cluster B's
  borderline cases (`${...}` token catalog enumeration vs allowing
  literal-`${...}` in fragment bodies) are redesigned to TEACH where
  possible, demoted to Notice where not.
- **Audience-model check per workstream.** Cluster D's atom extensions
  are positive shapes (set this Vite knob, run this git config) — not
  catalog bans. Cluster E's R-13-5 is reframed as a porter-audience
  rule extension, not a `zcli` ban-list extension.
- **Cite, don't re-quote.** Evidence already cited in
  [ANALYSIS.md §3](../runs/13/ANALYSIS.md) is linked, not repeated.

---

## 1. Goals for run 14

A `nestjs-showcase` (or fresh slug) recipe run that, compared to
run 13:

1. **Phase=feature complete-phase returns ok:true on first call after
   feature sub-agents return.** R-13-1's stitch race no longer fires;
   validators read fragment bodies in memory, not from SSHFS-coherent
   disk reads. (Cluster A)

2. **Per-codebase complete-phase verdicts are equivalent to the
   matching slice of full-phase complete-phase.** Sub-agent's scoped
   close passing implies main's full-phase close passes for that
   codebase's content. The §G2 actor-mismatch closes for real. (Cluster A)

3. **Subdomain is auto-enabled on every recipe-authoring deploy whose
   service has `httpSupport: true` AND `enableSubdomainAccess: true`.**
   Stage subdomains (apistage / appstage / workerstage) materialize
   without an explicit `zerops_subdomain action=enable` call. (Cluster A)

4. **Tier READMEs and tier import.yaml comments are authored by
   finalize.** Each per-tier README ≥ 40 lines (the run-12 ladder
   structure), each tier yaml carries multi-line causal comments.
   §T's tier-fact table feeds the finalize sub-agent's prose;
   §V's `tier-prose-*-mismatch` validator fires zero blocking
   violations and < 5 notices. (Cluster A — bound, validates §T+§V)

5. **`record-fragment mode=replace` does not silently clobber
   scaffold-authored content.** Either the brief teaches the
   wholesale-overwrite semantic loud-and-clear, or the engine returns
   the prior fragment body in the action's response so the agent
   replaces from a known baseline. (Cluster B)

6. **Agent-recorded fragments may carry literal `${...}` tokens
   inside fenced code examples.** The pre-processor's reserved-token
   check distinguishes fragment-body literals from
   template-side substitutions; engine error message includes the
   offending fragment id when the check fires. (Cluster B)

7. **Recipe-slug guesses don't burn turns.** `zerops_recipe action=
   start` returns the canonical reachable recipe-slug list in its
   response, OR scaffold brief carries it. (Cluster B)

8. **Defensive feature re-dispatch does not re-walk the phase
   transitions.** Either main is taught not to re-dispatch after
   `complete-phase phase=feature` returns `ok:true`, or the engine
   `start` action accepts `attach=true` to align session state with
   on-disk state without phase re-walk. (Cluster C)

9. **No SSH_DEPLOY_FAILED loop on git-identity.** Phase-entry scaffold
   carries the git-identity preamble; agents set `git config
   user.name/email` on the dev container before first deploy. (Cluster D)

10. **Vite host-allowlist trap captured at compose time, not
    rediscovered for the fourth run in a row.** Scaffold brief for
    frontend-role codebases on a Vite/Webpack base teaches the
    `allowedHosts: true` knob positively. (Cluster D)

11. **Browser-walk verification doesn't burn 3 minutes on stale
    element refs.** §F's showcase scenario atom extends with a "Stable
    selectors" subsection naming `data-feature` / `data-test` patterns.
    (Cluster D)

12. **Apps-repo CLAUDE.md zero `zcli`-anywhere occurrences.** R-13-5
    fixed structurally — porter-audience rule taught positively, not
    as ban-list extension. (Cluster E)

---

## 2. Workstreams

### 2.0 Guiding principles

1. **No new architecture.** Each workstream is small (~5-100 LoC).
   Cluster A is the largest at ~80-120 LoC because it touches the
   validator-input plumbing across multiple validators; even so, the
   surface change is bounded.

2. **Cluster A is the gate.** Without A, run 14 hits the same wall
   immediately. Tranche 1 ships A alone; if A doesn't close cleanly,
   abort the dogfood and revisit.

3. **TEACH side stays positive.** Per system.md §4. Cluster A's two
   fixes are uniformly positive shapes (engine produces materialized
   state by construction). Cluster B's three fixes are positive shapes
   where possible (engine returns prior body, engine relaxes literal
   `${...}` in fragment bodies). Cluster D's atom extensions are
   positive shapes (set this knob, run this command). Catalog-shaped
   teaching is rejected at design time, not authored and demoted.

4. **Audience rules at every layer.** Per system.md §1. Cluster E's
   R-13-5 is the rule that surfaced — every fix that touches a
   published surface preserves the porter-audience rule. Authoring-
   voice tools (`zcli *`, `zerops_*`, `zcp *`) leak when the layer
   that authored the content didn't internalize "the reader is the
   porter, not the agent."

5. **Don't add an engine extension without auditing its I/O boundary.**
   Run-13's run-13-§3 auto-stitch was correct in design; the audit of
   its filesystem-coherence dependency was not done. Run-14 risks
   carry this as a recurring watch — every engine extension a future
   readiness plan ships must list its I/O boundary explicitly.

### 2.A — Engine I/O coherence: stitch ↔ disk ↔ validator + subdomain materialization

#### What run 13 showed

Cluster A binds the bulk of run-13's blocking surface. Eleven defect
entries share the architectural shape **engine has truth in memory or
yaml; runtime doesn't materialize it; validator/agent observes the
divergence**. Symptoms:

- [`R-13-1`](../runs/13/ANALYSIS.md#r-13-1) — auto-stitch + same-handler
  disk read races SSHFS write-back; validator reads 0 bytes.
- [`R-13-2`](../runs/13/ANALYSIS.md#r-13-2) — per-codebase scoped
  complete-phase passes when full-phase complete-phase fails on the
  same codebase's content; the two paths read different snapshots.
- [`R-13-6`](../runs/13/ANALYSIS.md#r-13-6) — tier READMEs collapse
  to 9 lines because finalize never dispatches downstream of R-13-1's
  block.
- [`R-13-7`](../runs/13/ANALYSIS.md#r-13-7) — tier import.yaml comments
  absent for the same reason.
- [`R-13-9`](../runs/13/ANALYSIS.md#r-13-9) + [`R-13-10`](../runs/13/ANALYSIS.md#r-13-10)
  — symptoms of R-13-2 (hand-edits, source-comment voice-leak missed
  at scoped pass).
- [`R-13-12`](../runs/13/ANALYSIS.md#r-13-12) — subdomain not
  auto-enabled on dev-container deploy because the auto-enable path
  short-circuits when `workflow.FindServiceMeta` returns nil, and
  recipe-authoring deploys (via `zerops_import`) don't write meta.

#### TEACH/DISCOVER classification

**TEACH** uniformly. Both fixes resolve runtime state by construction:
the engine produces validator inputs from the in-memory fragment map
(no disk round-trip); the deploy handler enables the L7 subdomain
based on the service's actual platform mode + httpSupport, not on
meta-presence. Per system.md §4, "knowledge that is the same for every
recipe regardless of framework, language, or scenario, AND can be
expressed as a positive rule (a shape the engine produces or
requires)" — both fixes hit both criteria.

#### Audience-model check

The fixes are below the published-surface layer; they don't author
content directly. A.1's secondary effect on the porter is positive —
finalize phase entered, tier READMEs authored at the run-12 ladder
density, tier yaml comments restored. A.2's effect on the porter is
also positive — published-recipe stage subdomains materialize without
the porter ever needing to know about `zerops_subdomain action=enable`
(O3 in spec-workflows.md holds end-to-end).

#### Mechanism

**A.1 — Decouple validators from disk read.** Today's flow:
1. Sub-agent records fragment via `record-fragment` → engine writes to
   `Plan.Fragments` map in memory.
2. Sub-agent calls `complete-phase phase=scaffold codebase=<host>`.
3. Handler at
   [`internal/recipe/handlers.go:419-424`](../../../internal/recipe/handlers.go#L419-L424)
   calls `stitchCodebases(sess)` → `writeCodebaseSurfaces(plan)` →
   `os.WriteFile` for `<SourceRoot>/README.md` + `<SourceRoot>/CLAUDE.md`.
4. Handler then calls `sess.CompletePhaseScoped(CodebaseGates(),
   in.Codebase)`.
5. `gateCodebaseSurfaceValidators` →  `runSurfaceValidatorsForKinds` →
   for each surface kind, resolves the on-disk path and `os.ReadFile`s
   it (handlers.go gates.go:147-184).

The kernel/FUSE write-back coherence between step 3 and step 5 is the
defect. Same Go process, same handler call, different page-cache views.

The fix shape: validator inputs flow through an in-memory body
extracted from the just-stitched fragment map, not through a disk
round-trip. The validator's signature `ValidateFn(ctx, path, body
[]byte, inputs)` already accepts a body — the issue is that
`runSurfaceValidatorsForKinds` reads body from disk via
`os.ReadFile(p)` instead of from the assembler's output.

**A.2 — Subdomain auto-enable for recipe-authoring deploys.** Today's
flow at [`internal/tools/deploy_subdomain.go:30-46`](../../../internal/tools/deploy_subdomain.go#L30-L46):
1. After successful deploy, `maybeAutoEnableSubdomain` calls
   `workflow.FindServiceMeta(stateDir, targetService)`.
2. If meta is nil, returns. (Comment: "Not ZCP-managed (managed
   services have no meta per spec E6; agent-owned services without
   bootstrap also absent). Skip.")
3. Recipe-authoring services don't go through `bootstrap` /
   `zsc bootstrap` paths. They're created via `zerops_import
   content=<yaml>` in workspace-yaml shape. No meta is ever written.
4. Auto-enable is silently skipped for every recipe-authoring dev /
   stage runtime that declares `httpSupport: true` +
   `enableSubdomainAccess: true`.

The fix shape: the meta-nil short-circuit was conservative for
non-ZCP-managed services. But the eligibility decision can be made
from the deployed service's actual platform state (mode +
httpSupport + the yaml's `enableSubdomainAccess`) without requiring
meta. ops.Subdomain's internal check-before-enable handles
idempotency; the auto-enable path can call it whenever the deployed
service's platform mode is in the eligible set, regardless of meta.

#### Fix direction

**A.1 — In-memory validator inputs.**

Two viable surfaces (run-13-readiness §6 risks named "either (a) Sync()
between stitch and read, OR (b) decouple validators from disk read";
(b) is the architecturally clean fix):

(b1) Refactor `runSurfaceValidatorsForKinds` to take a `bodyByPath
map[string]string` argument instead of reading from disk. The caller
(`gateCodebaseSurfaceValidators`, `gateEnvSurfaceValidators`) computes
the body map by calling the assembler functions directly:

```go
// internal/recipe/gates.go (new helper, near line 100-115)
func collectCodebaseBodies(plan *Plan) (map[string]string, error) {
    bodies := map[string]string{}
    for _, cb := range plan.Codebases {
        readme, _, err := AssembleCodebaseREADME(plan, cb.Hostname)
        if err != nil {
            return nil, fmt.Errorf("assemble %s README: %w", cb.Hostname, err)
        }
        bodies[filepath.Join(cb.SourceRoot, "README.md")] = readme
        claude, _, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
        if err != nil {
            return nil, fmt.Errorf("assemble %s CLAUDE.md: %w", cb.Hostname, err)
        }
        bodies[filepath.Join(cb.SourceRoot, "CLAUDE.md")] = claude
        // zerops.yaml is file-only (not a fragment) — fall through to
        // disk read for that surface, which is fine because the
        // sub-agent ssh-edits it (no race).
    }
    return bodies, nil
}

func gateCodebaseSurfaceValidators(ctx GateContext) []Violation {
    bodies, err := collectCodebaseBodies(ctx.Plan)
    if err != nil {
        return []Violation{{Code: "validator-prep-failed", Message: err.Error()}}
    }
    return runSurfaceValidatorsForKinds(ctx, codebaseSurfaceKinds, bodies)
}
```

`runSurfaceValidatorsForKinds` takes the body map; if a path is not in
the map (e.g. `<SourceRoot>/zerops.yaml`), it falls back to
`os.ReadFile(path)` for surfaces backed by a file that is NOT in the
fragment map. This preserves the codebase-zerops-comment validator's
behavior (zerops.yaml is sub-agent ssh-edited, no race).

The stitch step still runs (so the on-disk artifact is current for
publish), but it's no longer in the validator's critical path. If the
on-disk write completes after the validator returns, the deliverable
is still correct — only the validator's view changes.

(b2) Source-comment validator (`gateSourceCommentVoice`) reads source
files from `<SourceRoot>` directly. Source files are agent-authored
through Write/Edit tools, not through the fragment map. Keep the
disk-read for source-comment scanning (no fragment-side coherence
issue exists; the sub-agent owns the file and writes are
direct-mounted not stitched). R-13-10's missed-at-scoped-pass surfaces
once R-13-2 is closed by (b1) — see below.

(b3) `runSurfaceValidatorsForKinds` plumbing change is ~30-40 LoC
(callers compute the body map, the helper switches its read source).
`collectCodebaseBodies` and an analogous `collectEnvBodies` for
finalize add ~30 LoC.

**Acceptance-equivalence between scoped and full-phase passes (R-13-2):**
Once both passes consume the same `bodies` map computed from the same
`Plan` object, the verdict equivalence is structural:
`CompletePhaseScoped` filters `Plan.Codebases` to one host; the
fragment map and assembler outputs derive deterministically from that
filtered Plan. If the scoped pass runs against a Plan where api is
the only codebase, `collectCodebaseBodies` returns api's bodies only;
the validators run those bodies; the verdict is equivalent to "what
api would contribute to a full-phase pass." The implicit assumption
"if every per-codebase pass returns ok:true, then the full-phase pass
returns ok:true on the same content" holds by construction.

There remains a per-validator audit task: a small number of validators
in `validators_codebase.go` may iterate `inputs.Plan.Codebases` rather
than scoping to the file under inspection. These should be fixed to
read from the path's hostname (extracted from the path or carried via
`inputs.Codebase` if needed). Run an audit pass; if any validator
needs adjustment, fix in the same commit.

**A.2 — Decouple subdomain auto-enable from meta presence.**

Edit `maybeAutoEnableSubdomain`:

```go
// internal/tools/deploy_subdomain.go
func maybeAutoEnableSubdomain(
    ctx context.Context,
    client platform.Client,
    httpClient ops.HTTPDoer,
    projectID, stateDir string,
    targetService string,
    result *ops.DeployResult,
) {
    // Read the deployed service's actual platform mode from the
    // platform's GetService response — this is REST-authoritative
    // (spec-workflows.md O3) and doesn't require ZCP-managed meta.
    svc, err := client.GetService(ctx, projectID, targetService)
    if err != nil {
        // Soft-fail: agent's manual zerops_subdomain remains valid.
        return
    }
    if !modeEligibleForSubdomain(svc.Mode) {
        return
    }
    if !svc.HTTPSupport {
        return
    }
    // Now call ops.Subdomain (idempotent via check-before-enable).
    subRes, err := ops.Subdomain(ctx, client, projectID, targetService, "enable")
    // ... existing handling
}
```

Two things change vs today's path:
1. Source of mode/httpSupport: platform `GetService` instead of
   `workflow.FindServiceMeta`. The platform is REST-authoritative;
   meta tracks ZCP-bootstrap intent, not deploy-time platform state.
2. The eligibility predicate uses platform-state `Mode` and
   `HTTPSupport` directly. No meta dependency.

This brings recipe-authoring deploys into the auto-enable path without
breaking the existing meta-keyed code: bootstrap-managed services
still pass through (they have meta AND meet the platform-state
predicates); recipe-authoring services now also pass through.

Cost: ~10-15 LoC engine + 1 test exercising the meta-nil path.

#### Tests

```go
// internal/recipe/gates_test.go
func TestCodebaseSurfaceValidators_UsesInMemoryBodies(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.Fragments = map[string]string{
        "codebase/api/integration-guide": "## Integration\n\n### 1. ...\n",
        "codebase/api/knowledge-base":    "## Knowledge\n\n",
        "codebase/api/claude-md/service-facts": "- Hostname: apidev\n",
        "codebase/api/claude-md/notes":         "- Dev loop: ...\n",
        "codebase/api/intro": "Some intro.\n",
    }
    // Don't write any file to disk — validator must work in-memory.
    sess := &Session{Plan: &plan, OutputRoot: t.TempDir()}
    blocking, _, err := sess.CompletePhase(CodebaseGates())
    if err != nil { t.Fatal(err) }
    // Specific assertion: no `validator-read-failed` from os.ReadFile
    // (the historical run-13 race shape).
    for _, v := range blocking {
        if v.Code == "validator-read-failed" {
            t.Errorf("validator attempted disk read despite in-memory bodies present: %+v", v)
        }
    }
}

func TestCompletePhaseScoped_VerdictEquivalentToFullPhaseSlice(t *testing.T) {
    plan := syntheticShowcaseWithViolations(t) // populated to fail one rule on api
    full := *plan
    sess := &Session{Plan: &full, OutputRoot: t.TempDir()}
    fullBlocking, _, _ := sess.CompletePhase(CodebaseGates())

    scoped := *plan
    sessScoped := &Session{Plan: &scoped, OutputRoot: t.TempDir()}
    scopedBlocking, _, err := sessScoped.CompletePhaseScoped(CodebaseGates(), "api")
    if err != nil { t.Fatal(err) }

    apiSubsetOfFull := violationsForCodebase(fullBlocking, "api")
    if !sameViolationSet(scopedBlocking, apiSubsetOfFull) {
        t.Errorf("scoped pass for api ≠ full-phase api subset:\n  scoped: %v\n  full(api): %v",
            scopedBlocking, apiSubsetOfFull)
    }
}
```

```go
// internal/tools/deploy_subdomain_test.go
func TestMaybeAutoEnable_NoMeta_StillRunsForPlatformEligibleService(t *testing.T) {
    fakeClient := platformClientStub{
        getService: func(_ context.Context, _, host string) (*platform.Service, error) {
            return &platform.Service{Mode: topology.PlanModeDev, HTTPSupport: true}, nil
        },
    }
    httpClient := stubReady{}
    result := &ops.DeployResult{}
    // stateDir without meta — recipe-authoring scenario
    stateDir := t.TempDir()
    maybeAutoEnableSubdomain(ctx, fakeClient, httpClient,
        "proj-1", stateDir, "apidev", result)
    if !result.SubdomainAccessEnabled {
        t.Errorf("auto-enable did not fire on platform-eligible recipe-authoring service")
    }
}

func TestMaybeAutoEnable_PlatformIneligibleMode_Skips(t *testing.T) {
    fakeClient := platformClientStub{
        getService: func(_ context.Context, _, _ string) (*platform.Service, error) {
            return &platform.Service{Mode: topology.PlanModeProd, HTTPSupport: true}, nil
        },
    }
    result := &ops.DeployResult{}
    maybeAutoEnableSubdomain(ctx, fakeClient, stubReady{},
        "proj-1", t.TempDir(), "apiprod", result)
    if result.SubdomainAccessEnabled {
        t.Errorf("auto-enable fired on prod-mode service (eligibility check failed)")
    }
}
```

#### Acceptance

- Run-14 `complete-phase phase=feature` (no codebase, after feature
  sub-agents return) returns `ok:true` on first call.
- Run-14 finalize phase enters; per-tier READMEs ≥ 40 lines each;
  every tier import.yaml carries multi-line causal comments.
- Run-14 `complete-phase phase=scaffold codebase=<host>` for every
  scaffold sub-agent returns `ok:true`; `complete-phase phase=scaffold`
  (main, no scope, after sub-agents return) also returns `ok:true`
  on first call. Zero main-agent `Edit` calls during scaffold-close.
- Run-14 stage subdomains (`apistage`, `appstage`, etc.) materialize
  without the agent calling `zerops_subdomain action=enable`.
  Browser-walk subdomains reachable on first attempt.

#### Cost / Value

- Engine: ~80 LoC (40 in gates.go for body map plumbing, 30 in
  collectors, 15 in deploy_subdomain.go). 4 tests.
- Brief / atom changes: zero (engine resolution; agent surface
  unchanged).
- Value: structural — closes 11 of run-13's 21 defect entries.
  Without this, no other workstream can be validated.

### 2.B — Engine reserved-semantics surfacing

#### What run 13 showed

The engine has reserved semantics that the brief doesn't surface;
agents discover them via failure rather than from the brief:

- [`R-13-3`](../runs/13/ANALYSIS.md#r-13-3) — `record-fragment
  mode=replace` overwrites the entire fragment body. The §W brief
  teaching extends the action to feature-phase corrections of
  scaffold-authored fragments. The semantic is wholesale-overwrite
  per the run-12 §R design intent, but the brief teaching doesn't
  surface this loud-and-clear; features-1 sub-agent replaced one IG
  section, lost the other five, spent ~1 minute reconstructing from
  working memory.
- [`R-13-19`](../runs/13/ANALYSIS.md#r-13-19) — engine pre-processor
  rejects fragment bodies containing `${HOSTNAME}` literal (a code
  example in a worker fragment). The error message doesn't locate the
  offending fragment id; agent burned ~1m38s isolating it across four
  stitch-content failures.
- [`R-13-21`](../runs/13/ANALYSIS.md#r-13-21) — scaffold-app guessed
  at `svelte-ssr-hello-world`, got `INVALID_PARAMETER` with the
  canonical list. Recovered in one retry; cost ~10s. Recurring class:
  the engine knows the canonical reachable recipe-slug list; the brief
  doesn't surface it.

#### TEACH/DISCOVER classification

**Mostly TEACH.** Three positive shapes the engine can produce or require:
- B.1: engine returns the prior fragment body in the response of
  `record-fragment` (the agent has the prior body in hand before the
  next replace).
- B.2: pre-processor allows literal `${...}` inside fenced code blocks
  in fragment bodies (positive shape — engine relaxes a check), AND
  emits the offending fragment id when the check still fires.
- B.3: engine emits canonical recipe-slug list in the `start` action
  response (positive shape — engine emits known truth).

The borderline case run-13-readiness raised was B.2's "engine
reserves these tokens" enumeration. The TEACH framing avoids the
catalog: the engine doesn't enumerate "tokens it bans"; it relaxes
the check on fenced-block content (positive structural rule on what
fragment bodies may contain) and improves the error when the relaxed
check still fires (engine produces a clear locator). Catalog-shaped
teaching ("don't write `${HOSTNAME}` in a fragment unless escaped")
is rejected — the engine fix removes the need for that teaching.

#### Audience-model check

B.1's secondary effect: the agent doesn't have to grep / git-log /
read-and-reconstruct between replaces. The published apps-repo IG
content is whatever the agent intended; no scaffold-authored sections
silently drop. Zero porter-audience risk.

B.2's secondary effect: agents can author fragments containing code
examples that demonstrate the engine's substitution behavior. The
fenced-block predicate is structural; it doesn't constrain published
content beyond what fenced markdown already does.

B.3's secondary effect: the brief / start response is internal to the
authoring run; doesn't reach a published surface.

#### Mechanism

**B.1 — `record-fragment` returns the prior body.** Today's
`RecipeResult` already has `FragmentID`, `BodyBytes`, `Appended`
fields populated for record-fragment success
([handlers.go:150-152](../../../internal/recipe/handlers.go#L150-L152)).
Add a `PriorBody string` field to the response when the action
processes a `mode=replace` call; the body is whatever was in
`Plan.Fragments[in.FragmentID]` before the replace. Agents that use
`mode=replace` to extend a fragment can call status / read prior_body
first, OR receive it in the response of a previous record-fragment
call. The brief teaches the read-then-replace workflow positively.

**B.2 — Fenced-block `${...}` literal.** The pre-processor (location:
follow `stitch-content`'s emit/scan path → check
`yaml_emitter.go` and `assemble.go` for `${...}` rejection logic
— mark as **spec-only — implementation surface needs audit before
commit** if the exact site is non-obvious; cluster B's B.2 fix
direction is sound but the surface needs verification at commit
time). Add a fenced-block-aware skip: characters inside a fenced
markdown code block (`` ``` `` or backtick-delimited inline) are
allowed to contain literal `${...}` without rejection. Engine error
when the check fires outside a fenced block: include `fragment id =
<id>` and an excerpt naming the line.

**B.3 — Recipe-slug enumeration in `start` response.** Today's `start`
returns a research-phase guidance string + the parent recipe inline
([handlers.go:170+](../../../internal/recipe/handlers.go#L170)). The
mount root scan (via `Resolver{MountRoot: s.mountRoot}`) walks the
recipes mount; the canonical reachable slugs are derivable. Add a
`ReachableSlugs []string` field to `RecipeResult` populated on `start`,
listing every recipe slug whose `<mountRoot>/<slug>/import.yaml`
exists. Agent calling `zerops_knowledge query=svelte-ssr-hello-world`
gets `INVALID_PARAMETER` once; on retry, the agent has the canonical
list from the start response and picks the right slug deterministically.

Alternative for B.3: include the list in the scaffold brief composer
(under a `## Recipe-knowledge slugs you may consult` section). The
brief composer already has `Resolver` access. Cheaper than adding a
RecipeResult field; agent's first scaffold sub-agent dispatch carries
the list from the brief.

#### Fix direction

**B.1**:
```go
// internal/recipe/handlers.go::RecipeResult
type RecipeResult struct {
    // ... existing fields
    PriorBody string `json:"priorBody,omitempty"` // run-14 §B.1
}

// internal/recipe/handlers_fragments.go::recordFragment (or wherever
// the action is dispatched)
func recordFragment(sess *Session, in RecipeInput, r RecipeResult) RecipeResult {
    // ... existing logic
    if in.Mode == "replace" {
        r.PriorBody = sess.Plan.Fragments[in.FragmentID]
    }
    // ... write new body, populate FragmentID/BodyBytes/Appended
    return r
}
```

Brief teaching update for `briefs/feature/content_extension.md`:

```markdown
## `mode=replace` — wholesale overwrite

`record-fragment mode=replace` overwrites the ENTIRE fragment body. To
extend an existing scaffold-authored fragment (add a new IG section,
add a new KB bullet), include the prior content verbatim plus your
additions in the new body.

If you need the prior body, the engine returns it in the response of
the previous record-fragment call (`response.priorBody` field) or you
can read it from `zerops_recipe action=status` snapshot's
`Plan.Fragments[<id>]`. Don't reconstruct from working memory or grep
the on-disk README — both lose fragment fidelity.
```

**B.2**: spec-only — implementation surface needs audit before commit.
Direction: locate the `${...}` rejection in stitch-content's emit
pipeline (probably in the YAML pre-processor) and add the
fenced-block predicate. Brief teaching is unchanged — the fix removes
the failure mode.

**B.3** (atom-extension variant — preferred for cost):

```markdown
## Recipe-knowledge you may consult

When calling `zerops_knowledge runtime=<runtime>` or
`zerops_knowledge recipe=<slug>`, these are the canonical reachable
recipes:

- nestjs-hello-world
- nodejs-hello-world
- svelte-hello-world
- ... (engine emits the actual list at compose time from the
  recipes mount)

Use `runtime=<runtime>` to fetch managed-service connection idioms;
use `recipe=<slug>` only for slugs in this list.
```

The composer reads the resolver's enumerable slug list and emits the
bullets. Frontend / api / worker scaffolds all benefit equally;
no role conditional needed.

#### Tests

```go
func TestRecordFragment_ReplaceReturnsPriorBody(t *testing.T) {
    sess := freshSessionWithFragment(t, "codebase/api/integration-guide", "ORIGINAL\n")
    in := RecipeInput{
        Action: "record-fragment", Slug: "x",
        FragmentID: "codebase/api/integration-guide",
        Fragment:   "REPLACED\n",
        Mode:       "replace",
    }
    r := dispatch(ctx, sess.Store(), in)
    if r.PriorBody != "ORIGINAL\n" {
        t.Errorf("priorBody = %q, want %q", r.PriorBody, "ORIGINAL\n")
    }
}

func TestStitchContent_FencedBlockTokenAllowed(t *testing.T) {
    plan := syntheticPlan()
    plan.Fragments["codebase/worker/integration-guide"] = "Example:\n\n```\nworker-${HOSTNAME}-${pid}\n```\n"
    // Should not return rejection error.
    sess := &Session{Plan: &plan, OutputRoot: t.TempDir()}
    if _, err := stitchContent(sess); err != nil {
        t.Errorf("stitchContent rejected fenced-block ${...} literal: %v", err)
    }
}

func TestStitchContent_UnfencedTokenErrorIncludesFragmentID(t *testing.T) {
    plan := syntheticPlan()
    plan.Fragments["codebase/worker/integration-guide"] = "Bare ${HOSTNAME} reference outside any fence.\n"
    sess := &Session{Plan: &plan, OutputRoot: t.TempDir()}
    _, err := stitchContent(sess)
    if err == nil || !strings.Contains(err.Error(), "codebase/worker/integration-guide") {
        t.Errorf("error %v should name the offending fragment id", err)
    }
}

func TestBuildScaffoldBrief_CarriesReachableSlugList(t *testing.T) {
    plan := syntheticPlan()
    cb := plan.Codebases[0]
    brief, err := BuildScaffoldBrief(plan, cb, syntheticParent())
    if err != nil { t.Fatal(err) }
    mustContain(t, brief.Body, "## Recipe-knowledge you may consult")
    mustContain(t, brief.Body, "- nestjs-hello-world")
    // Negative: a guess like svelte-ssr-hello-world should NOT be in the list
    mustNotContain(t, brief.Body, "svelte-ssr-hello-world")
}
```

#### Acceptance

- Run-14 features sub-agent's `mode=replace` calls don't lose
  scaffold-authored sections (no replay of R-13-3's reconstruction
  loop). Zero `record-fragment` calls followed by a recovery `replace`
  to restore lost content.
- Run-14 fragment bodies containing fenced `${...}` examples don't
  trip stitch-content rejection. Zero stitch-content failures
  attributable to fenced-block literal tokens.
- Run-14 scaffold sub-agents don't burn turns on
  `zerops_knowledge recipe=<guess>` `INVALID_PARAMETER` errors. Zero
  invalid-recipe-slug retries.

#### Cost / Value

- Engine: ~30 LoC (B.1: ~10, B.2: ~10, B.3: ~10 for resolver list
  emission). 4 tests.
- Brief / atom: ~25 lines (mode=replace teaching + recipe-slug list).
- Value: medium — closes 3 recurring trap classes; B.1 alone saved
  ~1 minute in run 13 and is a permanent fix-class.

### 2.C — Session-state survival across re-dispatch

#### What run 13 showed

- [`R-13-4`](../runs/13/ANALYSIS.md#r-13-4) — features-1 returned
  cleanly at 18:35:32 with `complete-phase phase=feature` `ok:true`.
  Main was idle 4m04s. At 18:39:36 main re-dispatched the same prompt
  to a fresh sub-agent. The re-dispatched sub-agent saw a fresh
  session ("session not open"), re-walked through `start` /
  `update-plan` / `complete-phase` for research / provision / scaffold
  to align engine state with on-disk state — burning ~50 seconds. Then
  hit R-13-1's stitch race and never recovered.
- [`R-13-18`](../runs/13/ANALYSIS.md#r-13-18) — same shape; 7
  sequential phase-state-realignment calls.

#### TEACH/DISCOVER classification

**Mostly TEACH.** Two positive shapes:
- C.1: engine `start` action accepts `attach=true` flag; when the
  recipe slug has on-disk state (Plan, Fragments, Phase) and the
  caller asserts attach intent, the engine reads that state into the
  session and skips re-walking.
- C.2: dispatch prompt's `## Closing notes from the engine` section
  carries the current phase from `Plan.CurrentPhase` so a defensive
  re-dispatch sub-agent reads "phase=feature already; do not
  re-establish state."

The borderline case: "main agent should not re-dispatch the feature
sub-agent after `complete-phase phase=feature` returns `ok:true`" is
TEACH-able as a positive shape (phase-entry teaching prescribes the
order: complete-phase → enter-phase → not re-dispatch). The
phase-entry feature.md atom adds a `## After complete-phase` section.

#### Audience-model check

Below the published-surface layer; no porter impact.

#### Mechanism

**C.1 — `start attach=true`.** Today's `start` always returns the
research-phase guidance and the freshly-resolved parent recipe. With
`attach=true`, the engine:
1. Reads `<outputRoot>/manifest.json` (or whatever the persistence
   surface is named — verify at commit time; mark **spec-only —
   implementation surface needs audit before commit** if persistence
   is currently in-memory only).
2. Loads the persisted Plan + Fragments + Completed map.
3. Sets `sess.Current` to the most-recent completed phase + 1.
4. Returns `ParentStatus`, the loaded Plan, and a phase-resume
   guidance string instead of research-phase guidance.

If on-disk state is absent, the engine returns an error
("attach=true but no on-disk state for slug=<slug>; call start
without attach to begin a fresh run"). The agent picks one path
explicitly.

If session state is currently in-memory only (no persistence), C.1's
fix shape changes: persist Plan + Fragments to
`<outputRoot>/recipe-state.json` on every `update-plan` /
`record-fragment` / `complete-phase`; load on `start attach=true`.
~30 LoC for the persister; ~15 LoC for the loader.

**C.2 — Dispatch prompt phase awareness.** Edit
[`briefs_subagent_prompt.go::writePromptCloseFooter`](../../../internal/recipe/briefs_subagent_prompt.go#L179)
to include the current phase explicitly:

```go
case BriefFeature:
    fmt.Fprintf(b, "Note: when this dispatch fires, the recipe session is at\n")
    fmt.Fprintf(b, "phase=%s. If you join an existing session at a later\n", currentPhase)
    fmt.Fprintf(b, "phase, do NOT re-walk research/provision/scaffold —\n")
    fmt.Fprintf(b, "the engine will refuse the transitions, and the on-disk\n")
    fmt.Fprintf(b, "state is already correct. Resume work at the current phase.\n\n")
    // ... existing close-notes
```

`currentPhase` flows from `buildSubagentPrompt(plan, parent, in)` (the
`in` carries it via the `RecipeInput.Phase`, OR the engine reads
`sess.Current` and passes it to the composer).

**C.3 — Phase-entry teaching.** Edit `phase_entry/feature.md` (and
finalize.md) to add a `## After complete-phase phase=feature` section:

```markdown
## After complete-phase phase=feature

When `complete-phase phase=feature` returns `ok:true`, the engine has
recorded the phase as completed AND it has set the next phase. Your
next action is `enter-phase phase=finalize`, NOT a defensive re-dispatch
of the feature sub-agent. The work is done; re-dispatch only re-walks
state and risks compounding session-loss artifacts.
```

#### Tests

```go
func TestStart_AttachLoadsOnDiskState(t *testing.T) {
    outputRoot := t.TempDir()
    // Pre-populate state on disk
    persistedState := SessionStateOnDisk{
        Plan: syntheticPlan(),
        Completed: map[Phase]bool{PhaseResearch: true, PhaseProvision: true, PhaseScaffold: true, PhaseFeature: true},
    }
    writePersistedState(t, outputRoot, persistedState)

    in := RecipeInput{Action: "start", Slug: "test", OutputRoot: outputRoot, Attach: true}
    store := NewStore("/tmp")
    r := dispatch(ctx, store, in)
    if !r.OK { t.Fatalf("attach=true failed: %s", r.Error) }
    if r.Status.CurrentPhase != PhaseFinalize {
        t.Errorf("attach=true should have set phase to next-after-completed (finalize), got %q", r.Status.CurrentPhase)
    }
}

func TestStart_AttachWithoutOnDiskStateFails(t *testing.T) {
    in := RecipeInput{Action: "start", Slug: "test", OutputRoot: t.TempDir(), Attach: true}
    r := dispatch(ctx, NewStore("/tmp"), in)
    if r.OK { t.Errorf("attach=true should fail when no on-disk state exists") }
    if !strings.Contains(r.Error, "no on-disk state") {
        t.Errorf("error message should distinguish missing state from generic failure: %s", r.Error)
    }
}

func TestSubagentPrompt_FeatureCarriesCurrentPhase(t *testing.T) {
    plan := syntheticPlan()
    plan.CurrentPhase = "feature"
    in := RecipeInput{BriefKind: "feature"}
    prompt, err := buildSubagentPrompt(plan, nil, in)
    if err != nil { t.Fatal(err) }
    mustContain(t, prompt, "phase=feature")
    mustContain(t, prompt, "do NOT re-walk")
}
```

#### Acceptance

- Run-14 main agent calls `start` once on entry; if a defensive
  re-dispatch fires (e.g. after compaction), the sub-agent's prompt
  carries the current phase + main reads on-disk state via attach.
- Zero `~50s of phase-realignment` re-walk loops in run-14 sub-agent
  jsonls.
- If C.1's `attach=true` is impractical at commit time (because
  session state is in-memory only and the persistence surface needs
  more design), at minimum C.2 + C.3 ship — those alone reduce the
  re-walk cost by guiding the re-dispatched sub-agent to a faster
  recovery.

#### Cost / Value

- Engine: ~30-50 LoC (C.1 attach + state persistence; C.2 prompt
  composer change is ~5 LoC).
- Brief / atom: ~10 lines (phase_entry/feature.md).
- Value: medium — closes a class of recovery loops that compounds
  whenever R-13-1-class issues fire. Even after Cluster A closes
  R-13-1, defensive re-dispatch can fire from compaction; this is
  the structural backstop.

### 2.D — Operational preempts

#### What run 13 showed

Six recurring framework / platform / ordering traps the
phase-entry / atom layer should preempt:

- [`R-13-13`](../runs/13/ANALYSIS.md#r-13-13) — git-identity (×2,
  ~3 min): dev container has no git identity by default; SSH-deploy
  fails until `git config user.name/email` is set.
- [`R-13-14`](../runs/13/ANALYSIS.md#r-13-14) — `zcli env get` inside
  the dev container fails (host-side tool used in container, ~1 min).
- [`R-13-15`](../runs/13/ANALYSIS.md#r-13-15) — Vite host-allowlist
  (third recurrence; ~13s in run 13). Per
  [run-12 facts.jsonl:1](../runs/12/environments/facts.jsonl#L1) and
  earlier; the trap recurs every run with a Vite frontend.
- [`R-13-16`](../runs/13/ANALYSIS.md#r-13-16) — browser publish-click
  silent-fail (~3m13s): stale per-call element refs across browser
  snapshots produce silent no-op clicks.
- [`R-13-17`](../runs/13/ANALYSIS.md#r-13-17) — worker pidfile
  `running:false` confusion (~30s): nest-watcher PID rotates on
  rebuild; `kill -0` against the old pid returns false.
- [`R-13-20`](../runs/13/ANALYSIS.md#r-13-20) — scaffold complete-phase
  pre-deploy ordering (~13s): main called `complete-phase
  phase=scaffold` before deploy → verify; the gate requires deployed +
  verified.

Combined wall-time burn: ~6-7 minutes per run.

#### TEACH/DISCOVER classification

**Mostly TEACH.** Six positive shapes:
- D.1: phase-entry scaffold §M extends with `## Git identity on the
  dev container` subsection prescribing `git config user.name "ZCP
  agent"` + `git config user.email "agent@zerops.io"` before first
  deploy. Positive shape (run this command).
- D.2: `principles/mount-vs-container.md` adds `## zcli scope`
  subsection naming `zcli env get` as a host-side tool not available
  in the container. Positive shape (this tool's scope).
- D.3: scaffold brief role contract for `RoleFrontend` adds a
  `## Build-tool host-allowlist` section when the codebase's runtime
  base implies Vite/Webpack/Rollup. Positive shape (set this knob).
  Run-13's framing was right: not "ban this string"; "Vite has a
  config knob; here's how to set it."
- D.4: `briefs/feature/showcase_scenario.md` extends with a `### Stable
  selectors` block naming `data-feature` / `data-test` patterns for
  browser-walk verification. Positive shape (use these selectors).
- D.5: `principles/dev-loop.md` (or workerdev/CLAUDE.md template)
  notes nest-watcher PID volatility. Positive shape (the watcher
  rotates the PID on rebuild; rely on the listening port, not the
  pidfile).
- D.6: `phase_entry/scaffold.md` makes the close ordering explicit:
  *"deploy → verify → complete-phase (no codebase) is the main-agent
  close sequence."* Positive shape (action ordering).

The borderline case for D.3: the third recurrence raises the question
of whether brief-side teaching is appropriate (per system.md §4
DISCOVER side: framework-specific). The TEACH framing positions the
content as positive shape: Vite has a config knob; the brief teaches
*the knob*, not "don't ship `client.proxy.host: undefined`." When
the framework changes (Webpack, Rollup), the atom extends with the
analogous knob, not a new ban.

#### Audience-model check

These are agent-facing brief / phase-entry / atom extensions, not
published surfaces. The atom voice already targets the agent's
working context. D.3 is the most porter-relevant — once the agent
sets the Vite config, the published recipe doesn't need any porter-
facing teaching about it; the Vite config is just correct.

#### Mechanism

Pure content additions to existing atoms / phase-entry files. No engine
change.

#### Fix direction

```markdown
# phase_entry/scaffold.md (insert after § Mount state preamble)

## Git identity on the dev container

The dev container has no git identity by default; the SSH-deploy
sequence runs git operations (push, commit) and fails until identity
is set. Before the first deploy in any codebase:

ssh <hostname>dev "git config --global user.name 'zerops-recipe-agent' \
  && git config --global user.email 'recipe-agent@zerops.io'"

This is one-time per dev container; subsequent deploys reuse the
configured identity.
```

```markdown
# principles/mount-vs-container.md (append)

## zcli scope

`zcli` is a host-side tool. Inside the dev container (over `ssh
<host>`) the binary is not available — DO NOT use `zcli env get`,
`zcli vpn`, or other zcli verbs in container-side commands. Use
the platform-injected env vars (`$DB_PASSWORD`, `$NATS_USER`, etc.)
which are present in the container by construction.

If you need to fetch a project-level secret from outside the
container, run `zcli` on the host shell, not over `ssh`.
```

```markdown
# briefs/scaffold/content_authoring.md (new section, role-conditional
# loaded by composer when role=frontend AND base=nodejs@*)

## Build-tool host-allowlist (Vite / Webpack / Rollup)

Modern bundler dev servers reject requests whose Host header is not in
their allowlist. Zerops dev / stage subdomains are dynamic; the
default allowlist (`localhost`, `127.0.0.1`) does not include them.

For Vite, set `server.allowedHosts: true` in `vite.config.{js,ts}`
to allow any host. For Webpack-Dev-Server, set
`devServer.allowedHosts: 'all'`. For Rollup-based dev servers, follow
the equivalent knob.

This is a positive Vite/Webpack config knob — the bundler's intended
extension point for hosted dev environments. Set it once in the
config; it doesn't need per-tier overrides.
```

```markdown
# briefs/feature/showcase_scenario.md (new subsection in browser-verification block)

### Stable selectors for browser-walk verification

Per-snapshot DOM element refs go stale across `zerops_browser` calls.
A click against a previous-snapshot ref produces a silent no-op (no
error; nothing happens). Use stable attribute selectors instead:

- Add `data-feature="<name>"` to interactive elements you intend to
  exercise (publish triggers, search inputs, upload buttons).
- Add `data-test="<name>"` to result-display elements (search results,
  X-Cache badges, queue-feed entries).
- In `zerops_browser` calls, target by attribute (`[data-feature=
  "publish"]`) not by per-snapshot ref.

If a click appears to do nothing, suspect a stale ref; re-query the
DOM via the data attribute and retry.
```

```markdown
# principles/dev-loop.md (append)

## Nest watcher PID volatility

`nest start --watch` rotates its child process on rebuild. A pidfile
captured at first run is stale after any source-change rebuild.
Rely on the listening port (`netstat`, `ss -lnt`, or the dev-server
status endpoint) for liveness, not on `kill -0 <pid>` against a saved
pidfile.

Other watch-loop dev servers (vite, webpack-dev-server) generally
keep a stable parent process — pidfile-based liveness works there.
The nest-watcher pattern is the outlier; treat watcher PIDs as
ephemeral by default.
```

```markdown
# phase_entry/scaffold.md (extend close-sequence subsection)

## Scaffold close — main-agent action sequence

After all scaffold sub-agents have terminated:

1. `zerops_deploy` for each codebase (cross-deploy dev → stage).
2. `zerops_verify` for each cross-deployed service.
3. `zerops_recipe action=complete-phase phase=scaffold` (no codebase).
   The gate requires every codebase deployed + verified on dev + stage
   before it returns `ok:true`. Calling complete-phase before deploy +
   verify wastes a turn.

The per-codebase pre-termination self-validate (sub-agent's call
during scaffold) is a different action — the sub-agent already
self-validates before terminating. Main's no-codebase call is the
final phase-advance gate.
```

#### Tests

```go
func TestPhaseEntry_ScaffoldCarriesGitIdentitySection(t *testing.T) {
    body := loadAtom(t, "content/phase_entry/scaffold.md")
    mustContain(t, body, "## Git identity on the dev container")
    mustContain(t, body, "git config --global user.name")
}

func TestPrinciples_MountVsContainerCarriesZcliScope(t *testing.T) {
    body := loadAtom(t, "content/principles/mount-vs-container.md")
    mustContain(t, body, "## zcli scope")
}

func TestScaffoldBrief_FrontendCarriesBuildToolHostAllowlist(t *testing.T) {
    plan := syntheticPlan()
    var frontend Codebase
    for _, cb := range plan.Codebases {
        if cb.Role == RoleFrontend && strings.HasPrefix(cb.BaseRuntime, "nodejs") {
            frontend = cb; break
        }
    }
    brief, err := BuildScaffoldBrief(plan, frontend, nil)
    if err != nil { t.Fatal(err) }
    mustContain(t, brief.Body, "## Build-tool host-allowlist")
    mustContain(t, brief.Body, "allowedHosts: true")
}

func TestShowcaseScenarioAtom_CarriesStableSelectors(t *testing.T) {
    body := loadAtom(t, "content/briefs/feature/showcase_scenario.md")
    mustContain(t, body, "### Stable selectors")
    mustContain(t, body, "data-feature")
}
```

#### Acceptance

- Run-14 zero `SSH_DEPLOY_FAILED: ... default identity` failures.
- Run-14 zero `zcli env get` calls inside dev containers.
- Run-14 zero `vite-host-allowlist` rediscovery facts; the trap
  doesn't fire because the agent set the knob at scaffold time.
- Run-14 browser-walk loops don't burn > 30 seconds on stale-ref
  diagnostics; agent uses `data-feature` selectors from the start.
- Run-14 main agent's first `complete-phase phase=scaffold` (no
  codebase) is preceded by `zerops_deploy` + `zerops_verify`. Zero
  pre-deploy complete-phase wasted calls.

#### Cost / Value

- Engine: 0 LoC.
- Brief / atom / phase-entry: ~60 lines content.
- Tests: ~5 atom-content tests.
- Value: ~6-7 minutes saved per dogfood (~10% of run-13 wall time).
  Highest content-only ROI surface.

### 2.E — Content-discipline tighteners

#### What run 13 showed

- [`R-13-5`](../runs/13/ANALYSIS.md#r-13-5) — `zcli vpn` reference in
  agent-authored CLAUDE.md notes ([apidev/CLAUDE.md:32](../runs/13/apidev/CLAUDE.md#L32)).
  Agent honored the §C ban for the dev-loop content (line 26 reads
  `npm run start:dev`); slipped on a "hitting localhost from your
  laptop" tangential mention.
- [`R-13-8`](../runs/13/ANALYSIS.md#r-13-8) — `verify-subagent-dispatch`
  is a no-op now that §B2 ships engine-composed prompts byte-identical.
  Run 13 had zero verify calls; phase-entry teaching still says to
  call it. Future agents may waste a turn.
- [`R-13-11`](../runs/13/ANALYSIS.md#r-13-11) — apidev IG has 9
  numbered items; §I sweet-spot target is 4-7. Run-12 had 7. Items
  2-9 are all platform-mechanics-relevant; the IG inflation is
  partially explained by deeper showcase scope.

#### TEACH/DISCOVER classification

**Mixed.**

- E.1 (R-13-5): the existing §C teaching is catalog-shaped (lists
  tool names not to mention). Per system.md §4, the right TEACH
  framing is the porter-audience rule positively: "CLAUDE.md describes
  what the porter does in their codebase, with framework-canonical
  commands." If the brief teaches the rule, the agent doesn't need a
  zcli ban-list at all. Reframing E.1 as the porter-audience rule
  reach is the TEACH-side fix; extending the catalog (3 more lines)
  is catalog drift.

- E.2 (R-13-8): retire `verify-subagent-dispatch` from phase-entry
  teaching. §B2 makes paraphrase mathematically zero (main dispatches
  byte-identical from `build-subagent-prompt`'s response). The verify
  action stays in the engine for defensive use but the phase-entry
  no longer prescribes it. TEACH side — engine produces the prompt,
  no verify needed.

- E.3 (R-13-11): clarify the §I IG-scope rule. The 4-7 ceiling target
  is a per-codebase soft target, NOT recipe-wide. A showcase recipe
  with 5 managed-service categories may legitimately produce 8-10
  items per codebase (one per category × per platform mechanic).
  The brief teaches: 4-7 is the sweet spot for minimal recipes; for
  showcase recipes scoped at 5 categories × 2-3 platform-mechanic
  facets, expect 7-10 items. Above 12 is bloat; below 4 likely
  missed something.

#### Audience-model check

E.1 is the audience-model fix proper. The reframing makes the rule
explicit and the catalog redundant.

#### Mechanism

E.1: edit `briefs/scaffold/content_authoring.md` to add a positive
porter-audience rule near the top of the CLAUDE.md authoring
section. The existing "What does NOT go here" list can stay (it's
already there as catalog), but the load-bearing rule is the positive
one. Eventually the catalog can be deleted; for run-14 the positive
rule lands and the catalog stays as reinforcement (not the load-
bearing teaching).

E.2: edit `phase_entry/scaffold.md` + `feature.md` + `finalize.md`
to remove the "call verify-subagent-dispatch" step from the dispatch
flow. The action stays in the engine for explicit recovery; the
prescribed flow uses `build-subagent-prompt` → dispatch with
`prompt=<response.prompt>` directly.

E.3: edit `briefs/scaffold/content_authoring.md`'s §I section to
clarify the per-recipe-scope multiplier.

#### Fix direction

```markdown
# briefs/scaffold/content_authoring.md (top of CLAUDE.md authoring section)

## CLAUDE.md is for the porter

CLAUDE.md guides the porter (or the porter's AI agent) working in the
cloned apps repo. The reader has framework experience but is new to
*this* codebase. Voice rule: describe what the porter does in their
own codebase, with framework-canonical commands. Don't mention
authoring tools (`zcli *`, `zerops_*`, `zcp *`) — those are how the
recipe was BUILT, not how the porter USES it.

This rule is unconditional: it applies to dev-loop content, runbook
notes, debugging tips, port-forwarding hints, "hitting localhost from
your laptop" guidance, ANY tangential mention. If you'd write
`zcli vpn` to give the porter a tip, you're describing an authoring
tool; rewrite as a framework-canonical command (an `npm` / `composer` /
`cargo` invocation, an `ssh` for remote-access, etc.) or skip the
tangential tip entirely.
```

```markdown
# briefs/scaffold/content_authoring.md (§I IG-scope rule clarification)

## IG scope (4-7 sweet spot, scope-dependent)

The Integration Guide carries platform-mechanics-only content as
numbered items. For minimal recipes (1-2 managed services), 4-7
items per codebase is the target. For showcase recipes (5 managed-
service categories), expect 7-10 items per codebase — each platform
mechanic relevant to a category gets its own item.

If you ship more than 12 IG items, audit for: items duplicating prose
in KB (move to KB), items describing framework configuration not
platform mechanics (move to source comments or KB), items claiming
multiple concerns in one (split or consolidate). Below 4 likely
missed at least one platform mechanic.
```

```markdown
# phase_entry/scaffold.md (replace verify-subagent-dispatch step)

## Scaffold dispatch

For each codebase scaffold:

1. `zerops_recipe action=build-subagent-prompt briefKind=scaffold codebase=<host>`
2. Dispatch the sub-agent with `prompt=<response.prompt>` byte-identical.

The engine composes the full dispatch prompt (recipe-level wrapper +
brief body + close criteria) deterministically from Plan +
Research.Description. There is no separate verify step — the prompt
IS the engine output.
```

#### Tests

```go
func TestContentAuthoring_TeachesPorterAudienceRule(t *testing.T) {
    body := loadAtom(t, "content/briefs/scaffold/content_authoring.md")
    mustContain(t, body, "CLAUDE.md is for the porter")
    mustContain(t, body, "framework-canonical commands")
}

func TestContentAuthoring_IGScopeClarifiesShowcaseMultiplier(t *testing.T) {
    body := loadAtom(t, "content/briefs/scaffold/content_authoring.md")
    mustContain(t, body, "showcase recipes")
    mustContain(t, body, "7-10 items")
}

func TestPhaseEntry_ScaffoldDoesNotPrescribeVerifyDispatch(t *testing.T) {
    body := loadAtom(t, "content/phase_entry/scaffold.md")
    mustNotContain(t, body, "verify-subagent-dispatch")
    mustContain(t, body, "build-subagent-prompt")
}
```

#### Acceptance

- Run-14 published apps-repo CLAUDE.md files have zero `zcli` /
  `zerops_*` / `zcp *` occurrences anywhere in the body — service-
  facts, notes, runbooks, tangential tips. Strict criterion:
  `grep -c 'zcli\|zerops_\|zcp ' apps-repo/*/CLAUDE.md` returns 0.
- Run-14 main agent does not call `verify-subagent-dispatch` during
  the run.
- Run-14 IG item counts per codebase are within 4-10 (sweet spot
  expanded for showcase scope); zero codebases with >12 items.

#### Cost / Value

- Engine: ~5 LoC (retire phase-entry teaching).
- Brief / atom / phase-entry: ~15 lines.
- Tests: ~3 atom-content tests.
- Value: low-medium. E.1 is the structural audience-rule reach (the
  long-term displacement of catalog-shaped teaching). E.2 retires a
  no-op. E.3 clarifies a sweet-spot the previous run mis-applied.

---

## 3. Tranche ordering + commits

### Tranche 1 — engine I/O coherence (must-ship gate)

Cluster A is the gate. Without it, no other workstream's effects can
be validated end-to-end. Ship A first; validate it on a fast-path
synthetic run; only then proceed.

1. **commit 1** — Cluster A: validator in-memory plumbing
   ([gates.go::collectCodebaseBodies](../../../internal/recipe/gates.go),
   [gates.go::collectEnvBodies](../../../internal/recipe/gates.go),
   [gates.go::runSurfaceValidatorsForKinds](../../../internal/recipe/gates.go#L147)
   signature change accepting `bodies map[string]string`). Validator
   audit pass: ensure every codebase-validator scopes to the path's
   hostname not to `inputs.Plan.Codebases[*]` indiscriminately.
   Tests: `TestCodebaseSurfaceValidators_UsesInMemoryBodies`,
   `TestCompletePhaseScoped_VerdictEquivalentToFullPhaseSlice`.

2. **commit 2** — Cluster A: subdomain auto-enable for recipe-authoring
   deploys
   ([deploy_subdomain.go::maybeAutoEnableSubdomain](../../../internal/tools/deploy_subdomain.go#L30)).
   Tests: `TestMaybeAutoEnable_NoMeta_StillRunsForPlatformEligibleService`,
   `TestMaybeAutoEnable_PlatformIneligibleMode_Skips`.

### Tranche 2 — engine reserved-semantics + session-state

3. **commit 3** — Cluster B.1: `record-fragment mode=replace` returns
   prior body
   ([handlers.go::RecipeResult.PriorBody](../../../internal/recipe/handlers.go#L128),
   `handlers_fragments.go::recordFragment`). Brief teaching update at
   `briefs/feature/content_extension.md`.
   Tests: `TestRecordFragment_ReplaceReturnsPriorBody`.

4. **commit 4** — Cluster B.2: fenced-block `${...}` literal allowed +
   error names fragment id. **spec-only — implementation surface
   needs audit before commit.** Locate the pre-processor rejection
   site in stitch-content's emit pipeline; add the fenced-block
   predicate; include fragment id in error.
   Tests: `TestStitchContent_FencedBlockTokenAllowed`,
   `TestStitchContent_UnfencedTokenErrorIncludesFragmentID`.

5. **commit 5** — Cluster B.3: scaffold brief carries reachable
   recipe-slug list. Brief composer reads from `Resolver`. Tests:
   `TestBuildScaffoldBrief_CarriesReachableSlugList`.

6. **commit 6** — Cluster C: `start attach=true` + dispatch prompt
   carries current phase + phase-entry feature.md adds `## After
   complete-phase phase=feature` section. **spec-only —
   implementation surface needs audit before commit.** If session
   state is in-memory only, persistence design needs attention before
   landing C.1; ship C.2 + C.3 unconditionally either way.
   Tests: `TestStart_AttachLoadsOnDiskState` (gated on C.1 landing),
   `TestStart_AttachWithoutOnDiskStateFails`,
   `TestSubagentPrompt_FeatureCarriesCurrentPhase`.

### Tranche 3 — operational preempts + content discipline

7. **commit 7** — Cluster D: phase-entry scaffold §M git-identity
   subsection +  scaffold close-sequence subsection +
   principles/mount-vs-container.md zcli-scope subsection +
   scaffold brief frontend-conditional build-tool host-allowlist
   subsection + showcase_scenario.md stable-selectors block +
   principles/dev-loop.md nest-watcher PID volatility note.
   Tests: `TestPhaseEntry_ScaffoldCarriesGitIdentitySection`,
   `TestPrinciples_MountVsContainerCarriesZcliScope`,
   `TestScaffoldBrief_FrontendCarriesBuildToolHostAllowlist`,
   `TestShowcaseScenarioAtom_CarriesStableSelectors`.

8. **commit 8** — Cluster E: porter-audience rule reach in
   content_authoring.md + IG-scope sweet-spot clarification +
   phase-entry retire `verify-subagent-dispatch` from prescribed
   flow.
   Tests: `TestContentAuthoring_TeachesPorterAudienceRule`,
   `TestContentAuthoring_IGScopeClarifiesShowcaseMultiplier`,
   `TestPhaseEntry_ScaffoldDoesNotPrescribeVerifyDispatch`.

### Tranche 4 — CHANGELOG + verdict-table sign-off

9. **commit 9** — CHANGELOG + system.md verdict-table updates
   - New CHANGELOG entry summarizing Cluster A-E fixes with file:line
     for key changes.
   - system.md §4 verdict-table additions:
     - Validator in-memory plumbing (Cluster A.1) — TEACH (engine
       resolves materialization by construction)
     - Recipe-authoring subdomain auto-enable (Cluster A.2) — TEACH
       (engine resolves runtime state per spec-workflows §4.8 O3)
     - `record-fragment` priorBody return (Cluster B.1) — TEACH
       (engine produces the read-then-replace baseline)
     - Pre-processor fenced-block predicate (Cluster B.2) — TEACH
       (engine relaxes a structural rule on what fragment bodies may
       contain)
     - Build-tool host-allowlist atom (Cluster D.3) — TEACH (positive
       knob shape, not a phrase ban)

### Fast-path

If time pressure forces a partial run-14 dogfood: **Tranche 1 alone**
is viable. Cluster A unblocks finalize, restores the §T / §V / §F /
§Y2D run-13 workstreams that couldn't be validated, and immediately
lifts content grade toward 8-8.5/10 by letting downstream content
author. The remaining tranches polish; without them, the run still
closes, just with the run-13 operational-burn pattern persisting.

**Tranche 1 + 2** is the recommended must-ship.

**Tranche 3 + 4** are strongly recommended polish.

---

## 4. Acceptance criteria for run 14 green

### Inherited from run 13 (continue to hold; numbers continue from run-13's 28)

29. All five phases close `ok:true`. (Cluster A enables this.)
30. Three sub-agents in parallel for scaffold (single-message Agent
    dispatch).
31. Per-codebase apps-repo content lands at `<cb.SourceRoot>/`.
32. Per-codebase `.git/` initialized with at least one scaffold commit.
33. Recipe-root README templated; per-tier READMEs ≥ 40 lines (now
    actually authored, vs run-13's 9 lines).
34. Workspace yaml inline-imported; deliverable yamls written at
    `<outputRoot>/`.
35. Apps-repo `zerops.yaml run.envVariables` declares own-key aliases;
    code reads `process.env.<OWN_KEY>` (run-12 §E held).
36. Zero `https://${<host>_zeropsSubdomain}` source occurrences (run-12
    §A held).
37. Apps-repo IG focuses on platform mechanics; numbered items 4-10
    per codebase (run-12 §I held; run-14 §E.3 expanded sweet-spot for
    showcase scope).
38. Engine-composed dispatch wrappers < 15% (run-13 §B2 held).

### New for run 14

39. **`complete-phase phase=feature` (main, no scope, after sub-agents
    return) returns `ok:true` on first call.** Cluster A.1.

40. **`complete-phase phase=scaffold` (main, no scope) returns
    `ok:true` on first call after deploy + verify.** Zero main-agent
    `Edit` calls during scaffold-close. Cluster A.1.

41. **Per-codebase scoped close + full-phase close are verdict-
    equivalent for that codebase's content.** No defects flagged at
    full-phase that the per-codebase pass missed. Cluster A.1.

42. **Stage subdomains materialize without explicit
    `zerops_subdomain action=enable`.** apistage / appstage /
    workerstage are HTTP-reachable on first browser-walk. Cluster A.2.

43. **Tier yaml comments factually match emit fields.** Zero
    `tier-prose-*-mismatch` blocking violations; notice count < 5.
    (Re-validation of run-13 §V — now testable because finalize
    runs.)

44. **Apps-repo CLAUDE.md zero `zcli`/`zerops_*`/`zcp ` occurrences
    anywhere in the body, agent-authored or template-injected.**
    Cluster E.1 + run-13 §Q held.

45. **Feature sub-agent's `mode=replace` calls don't lose scaffold-
    authored fragment content.** Zero recovery loops to reconstruct
    lost sections. Cluster B.1.

46. **Fragment bodies containing fenced `${...}` examples don't trip
    stitch-content rejection.** Zero stitch-content failures
    attributable to fenced-block literal tokens. Cluster B.2.

47. **No SSH_DEPLOY_FAILED on git-identity.** Zero
    `default identity` failures. Cluster D.1.

48. **No Vite host-allowlist fact recorded; trap prevented at compose
    time** for fourth straight run trend. Cluster D.3.

49. **No defensive feature re-dispatch followed by re-walk.** If a
    re-dispatch fires (compaction-driven), the sub-agent reads
    `phase=feature` from the prompt + does not re-walk. Cluster C.

### Stretch criteria

50. **Recipe content grade 8.5/10 vs reference.** Lifts from run-13's
    6.5 — gating on Cluster A landing, finalize authoring tier
    READMEs at the run-12 ladder density.

51. **Recipe content grade 9/10 vs reference.** Adds: every Cluster D
    + E item lands cleanly; zero operational-burn rediscoveries;
    per-codebase IG counts within sweet-spot.

---

## 5. Non-goals for run 14

- **No re-design of the phase state machine.** 5 phases stay; phase
  entry / exit guards stay; per-codebase scope stays self-validate-
  only (no phase-advance from per-codebase form).
- **No re-architecture of the surface taxonomy.** spec-content-surfaces.md
  authoritative; run-14 enforces it more thoroughly with the §V
  validator now actually firing.
- **No new fragment ids.** zerops.yaml stays a file (not a fragment).
  Sub-agents ssh-edit for yaml-comment fixes (per run-13 §G2 design).
- **No new validator catalogs.** Per system.md §4: catalog-shaped
  teaching is rejected at design time. Cluster D's atom extensions
  are positive shapes, not phrase bans.
- **No re-promotion of previously-demoted notice validators.** §V
  stays Notice; no V-3 / V-4 / O-2 / P-3 re-promotion.
- **No publish-path changes.** `zcp sync recipe publish` stays out
  of scope.
- **No multi-codebase recipe-shape changes.** 3-codebase showcase
  shape stays.
- **No engine-side persistence rework beyond Cluster C's
  `attach=true` minimum.** If session state needs structural
  redesign for full attach functionality, defer to run-15.

---

## 6. Risks + watches

### Risk 1: I/O boundary recurrence (the meta-pattern)

The single highest-priority lesson from run 13 is that engine extensions
shipped without auditing the I/O boundary they depend on. The §3
auto-stitch was correct in design; the audit of its filesystem-
coherence dependency was not done.

**Mitigation**: every engine workstream in this plan (A.1, A.2, B.1,
B.2, B.3, C.1, C.2) explicitly states its I/O surface in the
mechanism block. Reviewer's job is to confirm each surface's
read/write coherence model.

**Watch**: every future readiness plan that proposes an engine
extension must list its I/O boundary. If a workstream adds a state-
read in a path that crosses a network filesystem, an inter-process
boundary, or a kernel page-cache boundary, the plan must state how
coherence is achieved (in-memory body, fsync, ordering, retries).
The signature-failure of run 13 was not the bug itself; it was the
absence of this audit.

### Risk 2: Session-state-loss-on-compaction recurrence

Run 13's features-2 re-dispatch happened because main's reasoning
between features-1 termination and re-dispatch suggests a context-
compaction event that lost the "feature is done; move to finalize"
intent.

**Mitigation**: Cluster C.2 + C.3 land unconditionally (the dispatch
prompt carries phase; phase-entry teaching prescribes "do not re-
dispatch after ok:true"). Cluster C.1 (`attach=true`) is the
structural backstop; it can defer if the persistence surface needs
more design, but C.2 + C.3 must ship.

**Watch**: monitor run-14's main-session jsonl for re-dispatch
patterns. If a re-dispatch fires AND the sub-agent re-walks phase
state despite the prompt teaching, the prompt teaching needs to be
strengthened or main-side teaching needs additional emphasis.

### Risk 3: Content-quality-regression-on-incomplete-finalize

Run 13's content grade dropped from 7/10 to 6.5 not because authored
content regressed (apps-repo content lifted to A) but because content
authored downstream of the blocked phase gate never authored at all.
A future run can hit the same shape if any new gate-blocking surface
emerges.

**Mitigation**: Cluster A.1 closes the specific blocking surface that
manifested in run 13. Cluster A's TEACH classification (engine
resolves materialization by construction) is the structural defense
— if validators consume in-memory bodies, the dependency on disk
coherence is gone.

**Watch**: monitor run-14's `complete-phase phase=feature` (main, no
scope) call. If it doesn't return `ok:true` on first call after
sub-agents return, flag immediately as A.1 partial.

### Risk 4: Cluster A.1's body-map plumbing has hidden disk reads

`runSurfaceValidatorsForKinds` switches its read source; per-validator
`ValidateFn` signature is unchanged. Risk: a validator that today
calls `os.ReadFile(otherPath)` internally (e.g. cross-file references)
still hits disk despite the body-map plumbing.

**Mitigation**: at commit 1, audit every codebase-validator for
internal `os.ReadFile`. Run the full test suite (`go test
./internal/recipe/... -count=1 -race`). Any failure indicates a
validator with a disk-state dependency the audit missed; surface for
redesign before continuing.

### Risk 5: Cluster D.3's role-conditional brief teaching trips on non-Vite Node frontends

D.3 loads the build-tool-host-allowlist content when the frontend
codebase's runtime base is `nodejs@*`. Not every Node-frontend uses
Vite (could be Webpack, Rollup, or no bundler at all).

**Mitigation**: the atom names Vite + Webpack + Rollup variants
explicitly. Agents authoring against a different bundler get the
shape (a host-allowlist knob exists; configure it) without needing
the engine to enumerate every bundler.

**Watch**: run-14's dogfood is `nestjs-showcase` (Vite-on-Svelte
frontend). If a future run uses a different bundler and the agent
mis-applies the teaching, expand the atom.

---

## 7. Open questions

1. **Does Cluster A.1's body-map plumbing extend to env validators
   (root README, env READMEs, env import-comments)?** Today's
   `gateEnvSurfaceValidators` reads from disk via the same path. The
   stitch-vs-read race manifested at codebase surfaces; whether env
   surfaces have the same race depends on whether env stitching also
   crosses the SSHFS boundary. Run-14's A.1 implementation should
   extend the body-map approach to env surfaces by symmetry; if env
   surfaces don't write to SSHFS (they write to `<outputRoot>` which
   is NOT SSHFS-mounted in current runs), the disk read might be
   safe — but consistency favors the in-memory approach uniformly.

2. **Does Cluster C.1 require new on-disk persistence, or does
   `<outputRoot>/manifest.json` already capture session state?** Audit
   what's currently persisted; if Plan + Fragments + Phase already
   land on disk via existing engine paths, `attach=true` is just a
   read+restore. If session state is purely in-memory, C.1 ships the
   persister + loader together — bigger surface but still small (~50
   LoC).

3. **Should `verify-subagent-dispatch` be removed from the engine
   action list entirely, or kept for explicit recovery?** Run-14 §E.2
   retires it from phase-entry teaching but keeps it in the action
   list. If subsequent runs show it never gets called, consider a
   future deletion; for run-14 the cost of keeping it is just the
   schema.

4. **Does Cluster B.2's fenced-block predicate handle inline backtick
   spans as well as triple-backtick fences?** Markdown allows both
   ` ``` ` blocks and `` `inline` `` spans. The pre-processor rejection
   should treat both as fragment-body literal contexts. Run-14
   implementation should handle both; tests cover both.

5. **Is the showcase scenario panel mandate (§F) extending to
   future-recipe categories the engine doesn't yet teach about (e.g.
   message-broker, mail, mqtt)?** If a recipe ships kafka or rabbitmq
   as a queue, the §F mandate's "queue / broker" panel is correct.
   If a future recipe ships a category not in the §F enumeration
   (e.g. mail-only), the mandate's panel-per-category may not apply.
   Defer to run-15+; not blocking.

---

## 8. After run 14 — what's next

If run 14 closes green on criteria 39-49:

- Run-14's content quality should be **8.5/10 vs reference** (run 13:
  6.5/10, run 12: 7/10, run 11: 6/10). Cluster A's stitch-race fix
  re-enables finalize authoring, lifting tier READMEs from D-mid to
  A and tier yaml comments from F to B+. Cluster B's reserved-
  semantics fixes eliminate ~3 minutes of recurring fragment-recovery
  loops. Cluster D's atom extensions eliminate ~6-7 minutes of
  recurring operational rediscovery. Cluster E's audience-rule reach
  closes the last template-vs-agent voice slip.

- If criteria 50-51 (stretch — 9/10 vs reference) ALSO holds: the
  recipe-authoring engine has reached a content-quality plateau;
  remaining work is around reach (more recipes, parent-recipe chain
  validation) and engine breadth (additional managed-service
  categories).

- Run-15 readiness focuses on:
  - Audit of remaining catalog-shaped validators (V-3, V-4, O-2,
    P-3, kbCitedGuideBoilerplateRE, kbSelfInflictedVoiceRE, etc.) —
    do the underlying lessons still bite, or has brief teaching
    fully replaced them? Delete or re-promote on §4 grounds.
  - Recipe-root README cross-codebase runbook content (open question
    from run 11 + 12; nothing currently lands in recipe-root README
    beyond engine-templated intro + tier links).
  - Showcase scenario typed Plan field (`Plan.ShowcasePanels
    []string`) for richer per-recipe scenario shape.
  - Catalog of non-Vite-frontend bundler knobs in D.3 (Webpack,
    Rollup, Parcel, esbuild) — extend per recipe-frontend variety
    encountered in dogfoods.

If run 14 closes RED on any of 39-49:

- ANALYSIS will name the structural cause. Most likely places it
  goes wrong:
  - **Cluster A.1 partial** — a validator still reads from disk
    despite the body-map plumbing; the audit pass missed a site.
    Re-audit the validator inventory; pin the disk-read sites with
    a test that fails on `os.ReadFile` calls inside any
    `ValidateFn`.
  - **Cluster A.2 partial** — `GetService` returns mode/httpSupport
    differently from the yaml's intent; the auto-enable path enables
    when it shouldn't. Add a diagnostic log showing the platform-
    state read; reconcile against the yaml intent.
  - **Cluster B.1 partial** — agent receives priorBody but doesn't
    use it; brief teaching needs to be more explicit about the
    workflow. Strengthen the brief content; OR add a positive engine
    enforcement (refuse `mode=replace` with a body shorter than
    `len(priorBody) * 0.5` unless `acknowledge=truncating-deliberately`
    is set).
  - **Cluster D.x partial** — atom teaching doesn't reach the agent's
    decision moment; needs role-conditional or earlier-phase loading.

The whole-engine path forward stays:
- Tighter audience boundary (porter vs authoring agent) per system.md §1.
- TEACH-side positive shapes per system.md §4.
- Engine pushes resolved truth into briefs; agent authors against
  truth, not mental models.
- Sub-agent self-validate before terminating; main only handles
  phase-state transitions.
- **New**: every engine extension audited against its I/O boundary
  before ship.

---

## 9. Pre-flight verification checklist

Before run-14 dogfood:

- [ ] All 9 commits land cleanly (Tranche 1-4).
- [ ] `make lint-local` passes (full lint, not lint-fast).
- [ ] `go test ./internal/recipe/... -count=1` passes.
- [ ] `go test ./internal/recipe/... -count=1 -race` passes.
- [ ] `go test ./internal/tools/... -count=1` passes (covers
      deploy_subdomain.go).
- [ ] No `replace` directives in `go.mod`.
- [ ] CHANGELOG entry summarizes Cluster A-E with file:line for key
      changes.
- [ ] system.md §4 verdict-table updated with Cluster A.1, A.2, B.1,
      B.2, D.3 entries.
- [ ] Manual sanity check: `runSurfaceValidatorsForKinds` no longer
      calls `os.ReadFile` for paths present in the bodies map.
      (Pinning test: a validator that touches a path WITHOUT a
      bodies-map entry still reads from disk; one WITH an entry
      reads from the map. Both behaviors covered.)
- [ ] Manual sanity check: `maybeAutoEnableSubdomain` runs the
      eligibility check from `GetService` not from `FindServiceMeta`.
- [ ] Manual sanity check: `record-fragment mode=replace` response
      carries `priorBody`.
- [ ] Manual sanity check: scaffold brief for a frontend codebase on
      `nodejs@22` runtime carries the `## Build-tool host-allowlist`
      subsection.
- [ ] Manual sanity check: `phase_entry/scaffold.md` no longer
      prescribes `verify-subagent-dispatch`.
- [ ] **I/O boundary audit (per Risk 1)**: for each Cluster A
      workstream, list the read source and write destination. Confirm
      no read crosses a network filesystem from a fresh write.
- [ ] **Session-state audit (per Risk 2)**: for Cluster C.1, confirm
      `attach=true` reads on-disk state OR document why it ships
      C.2+C.3 only.

When all green: dogfood `nestjs-showcase` (replay) — replay isolates
engine changes from research-phase variability.

If any check fails or surfaces new questions, surface immediately —
do not paper over with workaround code. Run-13's R-13-1 was
discoverable in design review; the readiness plan didn't surface it
because the I/O boundary audit step didn't exist. Run-14 readiness
encodes it as a checklist item; run-15 readiness should do the same
for whatever the next architectural blind spot turns out to be.
