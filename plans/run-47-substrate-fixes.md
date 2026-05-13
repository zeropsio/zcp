# Run-47 substrate fixes — root-cause-driven implementation plan

> **Status**: codex-validated 2026-05-14 (`a0aa7572ae48c5e49`). Original 9 items scoped down to 7 — **Item F REJECTED as monkey-patch** ("do not look at disk" brief clause violates the no-monkey-patch principle the plan itself carries forward); **Item E DROPPED as scope-creep** (substring-matching prose feature names teaches fuzzy content preference rather than removing a failure class by construction; the 4 prose-level carryovers don't block a porter). Items A, C, G, H, I had touch-point corrections applied per codex's code-references review.
>
> Derived from `plans/run-46-validation.md` ITERATE-TO-47 verdict. Each item names a ROOT CAUSE (not a symptom), the smallest code diff that closes the class, and the pin tests that prove it. No monkey-patches — same discipline as run-46-substrate-design.md.

---

## Root-cause inventory (mapped from run-46 verdict)

| # | Symptom in run-46 | Root cause |
|---|---|---|
| **R1** | 4-refusal close-gate retry loop on walked-ledger; sub-agent emitted free-form idKeys (`S5:codebase/api/knowledge-base/<name>`) instead of manifest grammar (`codebase_kb:api:<empty>`); bypass via iterative empty-findings retries | **idKey-format validation fires at the wrong layer** (refinement-close, post-audit) and the ledger is latest-wins so retries CAN bypass. Sub-agent never `Read` the manifest to learn the grammar. |
| **R2** | Item 5 citation validator refused 4 legitimate `#deployfiles-` cites; triage HOLD'd finding #15 | **`lookupGuideIDFromURL` strips URL fragments before path-match** (`validators_codebase.go:615-623`), collapsing `#initcommands-` / `#deployfiles-` / `#envvariables-` into one canonical-path entry. Real engine bug. |
| **R3** | `crossSurfaceUniquenessReport` emitted as flat int + sibling array, not the spec'd nested object; gate accepted weak shape | **Item 6 gate validates presence, not shape**. Sub-agent's emission grammar is under-specified; engine reads the flat int as scanned-count without verifying the structural envelope. |
| **R4** | Item 7 self-referential linter missed prose-level recipe-internal naming ("Cache card", "cache panel", "cache demo"); 4 carryovers in run-46 deliverable | **Linter scope is backticked-tokens + class-symbol-from-AST only**; prose-level recipe-specific UI feature names have no detection path. |
| **R5** | Items 3 / 4 / 7 unverified-in-production at run-46 surface; refusal counts invisible in main-session | **Engine-side gate refusals don't surface to main-session telemetry**. Sub-agent boundary swallows the refusal + auto-fixes; main agent has no observability into how many times each gate fired. Can't distinguish "gate worked silently" from "gate had nothing to do". |
| **R6** | Refinement-1 sub-agent ran `find / -path "*/jetstream*"` searching disk for goldens; substrate brief already embeds golden anchors | **Brief doesn't tell sub-agent "the goldens are embedded, do NOT look at disk"**. Sub-agent has implicit prior to seek source-of-truth on disk when uncertain. Brief is "what to do"; missing "what NOT to do" guard-rail. |
| **R7** | Engine auto-DROPed worker IG #2 (`framework-quirk` candidateClass → DROP); main agent HELD per surface test | **`candidateClass` is assigned at scaffold-time fact-recording and never re-evaluated**. Platform-invariant nature can emerge AFTER scaffold (e.g., the standalone-app-context being Zerops-rolling-deploys-forced isn't obvious until refinement). Engine auto-DROP is too aggressive when the class is brittle. |
| **R8** | Refinement-2 audit output 58.7 KB exceeded inline cap; main agent did Python re-extraction from tool-results file | **Audit emits monolithic JSON** — findings + walked-ledger + cross-surface-uniqueness all in one block. No chunking primitive; no per-surface emission shape. |
| **R9** | Main agent called `enter-phase phase=refinement` after env-content self-advanced; engine refused adjacent-forward | **Phase-state is shared across main + sub-agents** but the protocol assumes single-actor phase advance. Sub-agents that self-close phases leave the main agent's state stale. |
| **R10** | Walked count 92 ≠ scanned count 95 inside audit's own emission; decomposition `60+14+15+3=92` contradicts narrated "95 items" | **Sub-agent computes ledger + scan-count from different code paths**; no engine-side cross-check. Symptom of R3's under-specified shape. |

R10 is downstream of R3; R5 amplifies R3 + Items 3 / 4 / 7 visibility cost. R1 is the dominant blocker.

---

## The principle (carry-forward from run-46)

**No monkey-patching.** Acceptable fix shapes:
- Removing a class of failure by construction
- Moving validation to the layer where the failure originates
- Real engine bugs (validator misses a documented case)
- Telemetry/observability primitives that let the next iteration's design see the truth

**Rejected**: "add more brief text", "tighten a regex to catch one more name", "add a manifest-format hint to the audit checklist", "improve sub-agent disk-search behavior with a hint".

---

## Items — priority ordered

### Item A — idKey-format validation at `enrich-findings` boundary (closes R1)

**Class of failure eliminated**: format-mismatch retry loop. Sub-agent learns the manifest's idKey grammar at the audit boundary, not via reverse-engineering 4 close-gate refusal messages.

**Codex-corrected touch-points**:
- Handler is `enrichFindingsAction` at `internal/recipe/handlers.go:708` (not `handleEnrichFindings`).
- No `sess.Refinement2Manifest` field exists; rebuild via `BuildRefinement2Manifest(sess.Plan)` at `internal/recipe/refinement_manifest.go:160` and call `AllKeys()` (or add a new `AllKeysSet()` helper) on the result.
- Ledger mutation at `internal/recipe/handlers.go:735` (`sess.Refinement2Ledger = &Refinement2Ledger{Walked: append([]string(nil), in.Walked...)}`) is latest-wins; validation MUST run BEFORE this assignment, otherwise a refused call still corrupts the close-gate's ledger state.

**Diff (revised)**: at the top of `enrichFindingsAction`, before the existing classification enrich and BEFORE `sess.Refinement2Ledger` is mutated, build the manifest from the session's plan and validate every `walked[i]` against `manifest.AllKeys()`. Refuse with an error message that echoes the manifest's idKey grammar + lists 3-5 valid examples.

```go
// At enrichFindingsAction top, before any sess mutation:
if len(in.Walked) > 0 {
    manifest := BuildRefinement2Manifest(sess.Plan)
    validKeys := manifest.AllKeysSet()  // new helper; returns map[string]bool
    var invalid []string
    for _, key := range in.Walked {
        if !validKeys[key] {
            invalid = append(invalid, key)
        }
    }
    if len(invalid) > 0 {
        head := invalid
        if len(head) > 5 {
            head = head[:5]
        }
        r.Error = fmt.Sprintf(
            "enrich-findings: %d walked-ledger key(s) do not match the manifest idKey grammar. "+
            "Valid idKey shapes: codebase_ig:<host>:<slot>, codebase_kb:<host>:<empty>, "+
            "codebase_zerops_yaml:<host>, tier_yaml_comments:<N>:<key>. "+
            "Read .refinement-2-manifest.json end-to-end to enumerate every valid idKey. "+
            "Invalid keys received (first 5): %v", len(invalid), head)
        return r  // refuse BEFORE sess.Refinement2Ledger mutation
    }
}
```

When sub-agent emits a non-empty `walked` array, the refusal-on-format-mismatch implicitly proves the sub-agent did NOT `Read` the manifest end-to-end. Refusal message says so explicitly.

**Why structural, not monkey-patch**: validates the contract at the layer where the contract is first observable. Manifest defines the grammar; `enrich-findings` is the first call where the sub-agent declares its walk; refusal there is symmetric to existing classification + suggestedReplacement validations.

**Pin tests**:
- `TestEnrichFindings_RejectsInvalidWalkedIDKey` — fixture: manifest has `codebase_kb:api:<empty>`; sub-agent emits `S5:codebase/api/knowledge-base/nats-auth-violation` → refuse with grammar echoed.
- `TestEnrichFindings_AcceptsValidWalkedIDKey` — fixture: all walked keys match manifest grammar → enrich succeeds.
- `TestEnrichFindings_RefusalMessageNamesManifestPath` — refusal message names `.refinement-2-manifest.json` so sub-agent knows where to look.
- `TestEnrichFindings_RefusalDoesNotCorruptPriorLedger` (codex-added) — given a session with a valid prior ledger, an invalid `walked` array refused → `sess.Refinement2Ledger` is unchanged from its prior state. Pins that refusal happens BEFORE ledger overwrite.

---

### Item B — Preserve URL fragment in `lookupGuideIDFromURL` (closes R2)

**Class of failure eliminated**: legitimate `#deployfiles-` / `#envvariables-` / `#initcommands-` citations refused as wrong-guide. Engine bug; fragment-distinguished topics on the same canonical path collapse.

**Diff**: `internal/recipe/validators_codebase.go:615-623`. Stop the fragment-strip. Match `host + path + fragment` against `CitationGuideURL` table — table already has fragment-distinguished entries for all three topics.

```go
// Existing (bug):
// pathPartLower := strings.ToLower(strings.SplitN(stripped, "#", 2)[0])  // strips fragment
// for canonGuideID, canonURL := range CitationGuideURL {
//     canonPath := canonicalURLPath(canonURL)
//     if strings.EqualFold(canonPath, pathPartLower) { ... }
// }

// Fix:
fullPathWithFrag := strings.ToLower(stripped)  // keep fragment
for canonGuideID, canonURL := range CitationGuideURL {
    canonPathWithFrag := canonicalURLPathWithFragment(canonURL)
    if strings.EqualFold(canonPathWithFrag, fullPathWithFrag) {
        return canonGuideID
    }
}
```

`canonicalURLPathWithFragment` is the new helper; the existing `canonicalURLPath` stays for callers that explicitly want fragment-less matching.

**Why structural, not monkey-patch**: fixes a single engine bug at its root site. The citation map (`CitationGuideURL`) already distinguishes fragments; the lookup just throws away the information.

**Pin tests**:
- `TestLookupGuideIDFromURL_PreservesFragmentAnchor` — three sub-cases: `/zerops-yaml/specification#initcommands-` → `init-commands`; `#deployfiles-` → `deploy-files`; `#envvariables-` → `env-variables`. All three must resolve to distinct guide IDs.
- `TestLookupGuideIDFromURL_NoFragmentReturnsFamily` — `/zerops-yaml/specification` with no fragment returns the path-family default (probably init-commands, matching prior behavior for fragment-less URLs) or an explicit ambiguous-no-match signal.
- `TestKBCitationDisplayMismatch_DeployFilesURLAccepted` — end-to-end: KB body with `[deploy files](https://docs.zerops.io/zerops-yaml/specification#deployfiles-)` no longer triggers `kb-citation-display-mismatch` revert.

---

### Item C — Cross-surface uniqueness receipt shape + counter consistency (closes R3 + R10)

**Class of failure eliminated**: walked vs scanned counter mismatch never surfaces; the close-gate accepts a weak shape because the input type is too forgiving.

**Codex-corrected scope**: `RecipeInput` already carries `CrossSurfaceUniquenessScanned int` and `Duplicates []string` as discrete fields [`handlers.go:221`]; there is no nested `CrossSurfaceUniquenessReport` object input. The close gate at `handlers.go:1088` checks `scanned < total`, not object shape. So Item C is NOT "refuse a flat shape" (the input was always flat) — it's **enforcing counter consistency** between walked-ledger and scanned-count, and refusing when the sub-agent's internal numbers contradict.

**Diff (revised)**: in the refinement-close gate at `internal/recipe/handlers.go:~1088`, add a counter-consistency check:
1. Compute `expectedCount := len(BuildRefinement2Manifest(sess.Plan).AllKeys())`.
2. Refuse if `in.CrossSurfaceUniquenessScanned != expectedCount` (current gate refuses `<`, which over-counted 95 vs 71 silently passed).
3. Refuse if `len(sess.Refinement2Ledger.Walked) != in.CrossSurfaceUniquenessScanned` — walked-vs-scanned counter mismatch from R10 (audit's own 92 vs 95).
4. Refusal message names BOTH numbers so the sub-agent can reconcile.

```go
// In refinement-close gate, after Item A's idKey validation has already vetted the ledger:
manifest := BuildRefinement2Manifest(sess.Plan)
expected := len(manifest.AllKeys())
if in.CrossSurfaceUniquenessScanned != expected {
    return refuseWithMessage(fmt.Sprintf(
        "complete-phase: phase=refinement crossSurfaceUniquenessScanned=%d does not equal manifest total entries=%d. "+
        "The uniqueness pass must walk every manifest entry; partial or over-counted scans indicate the sub-agent did not run the pass against the full corpus.",
        in.CrossSurfaceUniquenessScanned, expected))
}
if walked := len(sess.Refinement2Ledger.Walked); walked != in.CrossSurfaceUniquenessScanned {
    return refuseWithMessage(fmt.Sprintf(
        "complete-phase: phase=refinement walked-ledger length=%d does not equal crossSurfaceUniquenessScanned=%d. "+
        "Same corpus must be walked for findings + uniqueness pass; sub-agent's internal counters contradict each other.",
        walked, in.CrossSurfaceUniquenessScanned))
}
```

(Optional follow-on, not in this item's scope: add a structured `Duplicates` parser that requires each entry to name the chosen canonical surface + the dropped duplicate's surface — currently `[]string` is opaque.)

**Why structural, not monkey-patch**: tightens an existing gate from `<` to `==` and adds the cross-counter consistency check the run-46 audit's own emission violated (92 walked vs 95 scanned in the same JSON block). No new prose; same `RecipeInput` shape.

**Pin tests**:
- `TestRefinement2CloseGate_RefusesOverScanned` — fixture: manifest=71, scanned=95 → refuse with both numbers named.
- `TestRefinement2CloseGate_RefusesWalkedScannedMismatch` — walked=92, scanned=95 → refuse.
- `TestRefinement2CloseGate_AcceptsConsistentCounters` — walked=71, scanned=71, manifest.AllKeys=71 → accept.

---

### Item D — Engine-emitted telemetry for gate refusals (closes R5)

**Class of failure eliminated**: gates that fire at sub-agent boundaries leave no main-session evidence; impossible to distinguish "gate fired and authoring corrected" from "gate had nothing to do".

**Diff**: every engine-side gate refusal (Item 3 snapshot/restore wrapper, Item 4 friendly-auth gate, Item 7 self-referential linter, Item B citation validator) writes a structured entry to a per-session ledger at `<outputRoot>/.gate-refusals.jsonl`. One line per refusal:

```json
{"ts":"2026-05-13T22:21:14Z","gate":"friendly-auth-floor","phase":"codebase-content","codebase":"apidev","fragment":"codebase/apidev/zerops-yaml","reason":"porter_tunable_count=6 requires floor=2 adapt-paths; found 1"}
{"ts":"2026-05-13T22:23:08Z","gate":"self-referential-naming","phase":"codebase-content","codebase":"apidev","fragment":"codebase/apidev/knowledge-base","reason":"backticked token `cache.module.ts` matches nest-module suffix"}
{"ts":"2026-05-13T22:42:11Z","gate":"refinement-replace-revert","phase":"refinement","fragment":"codebase/app/integration-guide/4","reason":"kb-citation-display-mismatch: display \"deployment lifecycle\" vs URL's matched guide \"rolling-deploys\""}
```

`<engine-detail> status` action gets a new `gateRefusals` field summarizing counts per gate per phase. Validation runs read the ledger to verify each substrate item fired (or didn't need to) end-to-end.

**Why structural, not monkey-patch**: observability primitive. Doesn't change gate behavior; makes gate behavior visible. Future validation runs can answer "did Item N fire?" deterministically. Substrate-design feedback loop closes.

**Pin tests**:
- `TestGateRefusalLedger_WritesEntryOnFriendlyAuthRefusal`
- `TestGateRefusalLedger_WritesEntryOnSelfReferentialRefusal`
- `TestGateRefusalLedger_WritesEntryOnRefinementReplaceRevert`
- `TestStatusAction_ReportsGateRefusalCounts`

---

### ~~Item E~~ — DROPPED per codex review

**Codex verdict**: SCOPE-CREEP. Substring-matching prose feature names teaches a fuzzy content preference, not a hard substrate invariant. Drifts toward run-46's dropped Item 2 pattern (teaching disposition rather than removing a failure class by construction). High false-positive risk on legitimate domain nouns. Run-46 validation itself called this marginal — 4 prose-level carryovers ("Cache card" / "cache panel" / "cache demo" / etc.) don't block a porter's read.

**Decision**: drop. If prose-level recipe-internal naming creep becomes a porter-blocking class in a future run, revisit with a structural (not regex-substring) approach — e.g., enforcing at codebase-content authoring that KB bodies don't reference UI feature names that exist only in the recipe's own scaffold output, via fact-evidence cross-referencing.

---

### ~~Item F~~ — REJECTED per codex review

**Codex verdict**: REJECT — mostly monkey-patch. The "do not look at disk" clause is explicitly the kind of brief-prose addition that the plan's own carry-forward principle from `run-46-substrate-design.md:31` rejects. Adding a rendered-text pin only proves the prose exists; it doesn't enforce sub-agent behavior. Brief path in the original plan was also wrong (refinement uses `phase_entry/refinement.md` + `briefs/refinement/*`, not the proposed path).

**Decision**: drop. The behavior signal is real (refinement-1 sub-agent searched disk for goldens despite the brief embedding them) but a brief-text addition won't change sub-agent disposition. If this recurs in a future run as a porter-blocking class, the structural answer is a sub-agent boundary that doesn't have access to broader filesystem search — but that's a tool-allowlist change to the dispatch shape, not a brief edit.

---

### Item G — Refuse refinement-2 auto-override when classification is ambiguous (closes R7)

**Codex-revised scope**: original framing risked replacing one brittle classifier with another. Codex correctly noted: "potentially structural, but current heuristic is under-specified — risks replacing one brittle classifier with another." Also flagged conflict with run-46's rationale for dropping the DISCARD-class authoring gate at `run-46-substrate-design.md:79` (classification disposition is brittle).

**Class of failure eliminated**: scaffold-time `candidateClass=framework-quirk` assignment ages poorly; engine auto-override (REWRITE → DROP) fires on stale classification even when main agent's surface test PASSES. Worker IG #2 case in run-46: scaffold-time framework-quirk class drove auto-DROP; main agent HELD per S4 surface test (createApplicationContext is Zerops-rolling-deploys-forced).

**Diff (revised — ambiguity-refuses-not-promotes)**: at `internal/recipe/enrich_findings.go:~288-363`, where auto-override fires based on `factRef.CandidateClass`, add a guard: if the fact has cross-surface evidence (cited by IG AND yaml during scaffold+feature, OR matches a citation-map topic), the engine REFUSES to auto-override and emits the finding as RECORDED-AS-IS with `originalMode` + a `classificationAmbiguous: true` flag. Main agent then decides on the surface test, not the engine.

The point: ambiguity refuses, doesn't promote. The engine never decides "this framework-quirk fact is actually intersection" — it decides "this fact's classification is unreliable; let the main-agent surface test settle it."

```go
// In enrich-findings auto-override path:
factRef := lookupFact(finding.FactID)
if factRef.CandidateClass == "framework-quirk" && hasCrossSurfaceEvidence(factRef, plan) {
    // Refuse to auto-override; emit the finding as-is with ambiguity flag.
    finding.ClassificationAmbiguous = true
    finding.AutoOverrideRefused = "framework-quirk class has cross-surface evidence; main-agent S4 surface test settles disposition"
    // Do NOT change finding.Mode.
    continue
}
// existing auto-override logic when class is unambiguous
```

`hasCrossSurfaceEvidence` looks at the fact's appearances in IG + KB + yaml fragments; deterministic; doesn't try to re-classify.

**Why structural, not monkey-patch**: refuses on ambiguity, doesn't try to be smarter about the class. Same principle as run-46's dropped Item 2: don't teach disposition via heuristics; refuse when the signal is unreliable and let the human-in-loop (main agent) settle it.

**Pin tests**:
- `TestEnrichFindings_RefusesAutoOverrideOnAmbiguousClass` — fact tagged framework-quirk + cited in IG + KB + yaml → auto-override refused; finding mode unchanged; `ClassificationAmbiguous: true`.
- `TestEnrichFindings_AutoOverridesOnUnambiguousClass` — fact tagged framework-quirk, only appears in one surface → auto-override fires (existing behavior preserved).
- `TestEnrichFindings_HasCrossSurfaceEvidence_Deterministic` — fixture: walks fact-ID across plan fragments; returns identical result across runs.

---

### Item H — Typed multi-batch `enrich-findings` API (closes R8)

**Codex-revised scope**: original framing ("brief instructs sub-agent to emit 4 blocks") was prose; codex correctly noted current parser only accepts a single `FindingsEnvelope` at `enrich_findings.go:86` and a single `findings/findingsJson` pair on `RecipeInput` at `handlers.go:213`. Brief-instruction alone doesn't change parser shape.

**Class of failure eliminated**: monolithic 58.7 KB audit output overflows inline cap; main agent does Python re-extraction.

**Diff (revised — typed API, not brief prose)**: extend the engine's `enrich-findings` to accept a `batchID` + `totalBatches` pair on input. Sub-agent emits per-surface (or per-N-findings) JSON blocks and calls `enrich-findings` once per batch. Engine aggregates across batches in the session state; close-gate validates aggregated walked+scanned against manifest. Backward compat: a single call with no batchID is equivalent to `batchID=1, totalBatches=1`.

The walked-ledger aggregation must NOT use latest-wins semantics (codex flagged the interaction with Item A's [`handlers.go:735`] mutation). Add a `BatchedLedger` accumulator that appends across batches and resets per-session.

```go
// New input fields on RecipeInput:
type RecipeInput struct {
    // ...existing fields...
    BatchID       int `json:"batchId,omitempty"`        // 1..totalBatches; 0 == single-batch mode
    TotalBatches  int `json:"totalBatches,omitempty"`   // 0 or 1 == single-batch mode
}

// In enrichFindingsAction, after Item A validation:
if in.TotalBatches > 1 {
    if sess.BatchedRefinement2Ledger == nil {
        sess.BatchedRefinement2Ledger = &BatchedLedger{Walked: []string{}}
    }
    sess.BatchedRefinement2Ledger.Walked = append(sess.BatchedRefinement2Ledger.Walked, in.Walked...)
    sess.BatchedRefinement2Ledger.ReceivedBatches[in.BatchID] = true
    if len(sess.BatchedRefinement2Ledger.ReceivedBatches) < in.TotalBatches {
        // Incomplete batch set; return OK without locking ledger.
        r.OK = true
        return r
    }
    // All batches received; promote to sess.Refinement2Ledger.
    sess.Refinement2Ledger = &Refinement2Ledger{Walked: sess.BatchedRefinement2Ledger.Walked}
    sess.BatchedRefinement2Ledger = nil
} else {
    // Single-batch path: existing latest-wins assignment.
    sess.Refinement2Ledger = &Refinement2Ledger{Walked: append([]string(nil), in.Walked...)}
}
```

Brief is then UPDATED to instruct chunking — but the brief edit is downstream of the typed API change, not the load-bearing diff.

**Why structural, not monkey-patch**: changes the transport contract, not the content. Same shape as MCP's existing chunked-result primitives. Closes the 58.7 KB overflow class by construction (each batch sized to fit the inline cap).

**Pin tests**:
- `TestEnrichFindings_AggregatesBatches` — 4 batches with disjoint walked entries; final aggregated ledger has the union.
- `TestEnrichFindings_BatchedLedger_NoLatestWinsLoss` — codex requirement: confirms walked entries from batch 1 aren't overwritten by batch 4.
- `TestEnrichFindings_SingleBatchBackwardCompat` — call with TotalBatches=0 or 1 uses existing semantics.
- `TestRefinement2CloseGate_RefusesIncompleteBatchSet` — close-gate refuses when `len(ReceivedBatches) < TotalBatches`.

---

### Item I — Phase-state reconciliation on stale forward `enter-phase` (closes R9)

**Codex-revised scope**: original framing "current=finalize, enter=refinement" was wrong — that transition is adjacent-forward in engine semantics at `workflow.go:172,175`, not "already past". The actual run-46 failure: env-content sub-agent self-closed its phase; main agent's subsequent `enter-phase phase=refinement` was refused as non-adjacent-forward (state had already advanced past refinement-position).

Codex correctly noted: "too permissive as phrased — allowing 'already past' could mask real skipped-phase errors unless completed-state is checked carefully."

**Class of failure eliminated**: stale main-agent `enter-phase` calls after a sub-agent self-advanced phase.

**Diff (revised — narrower scope)**: `EnterPhase` at `internal/recipe/workflow.go:~172` becomes idempotent ONLY when (a) the requested phase is in the session's `Completed` list (i.e., already done, not skipped) AND (b) the request is from a main-agent context (not a phase-advance attempt that would skip uncompleted work). The check guards against silently masking skipped phases.

```go
// In EnterPhase, before existing adjacency check:
if slices.Contains(sess.Completed, in.Phase) {
    // Phase already completed (likely by sub-agent self-close); accept as no-op.
    r.OK = true
    r.Notice = fmt.Sprintf("session phase=%s already in completed list; enter-phase accepted as no-op. Likely sub-agent self-closed this phase.", in.Phase)
    return r
}
// existing adjacency logic for genuinely new transitions
```

The key difference vs original: only phases already in `sess.Completed` are accepted; arbitrary "in the past" transitions still refuse. Skipped-phase errors stay caught.

**Why structural, not monkey-patch**: engine has all the information to recover from a sub-agent self-close; the refusal helps no one. Narrows the original Item I's "at-or-past" to "in completed-list" so the genuine "skipped a phase" failure class stays caught.

**Pin tests**:
- `TestEnterPhase_IdempotentOnCompletedPhase` — `sess.Completed = [scaffold, feature, codebase-content, env-content]`, `enter-phase phase=env-content` → OK with notice.
- `TestEnterPhase_RefusesSkippedPhase` — `sess.Completed = [research, provision]`, `enter-phase phase=refinement` → still refuses (would skip scaffold/feature/etc).
- `TestEnterPhase_AdjacentForwardStillWorks` — existing semantics preserved for new transitions.

---

## Phase ordering (codex-validated)

Items E and F dropped. 7 items remain. Codex's priority order:

1. **Item B** (URL fragment fix) — smallest correct-existing-bug diff; lands first as standalone PR. Independent.
2. **Item A** (idKey-format validation at `enrich-findings`) — load-bearing diff. Must precede C + H because they share the ledger mutation point at `handlers.go:735`.
3. **Item C** (counter consistency) — depends on Item A's validated ledger. Same PR as A or immediate follow-up.
4. **Item D** (telemetry) — observability unlock; independent, lands as separate PR.
5. **Item I** (`EnterPhase` idempotency on completed phase) — tiny independent diff.
6. **Item H** (typed multi-batch `enrich-findings`) — touches the same transport contract as A + C; lands after A's structure stabilizes.
7. **Item G** (auto-override refuse on ambiguous class) — touches `enrich_findings.go:288-363`; lands last to avoid interaction with A's pre-mutation validation.

If only one diff lands: **Item A**. Without it, every run-47 will hit the same 4-refusal close-gate loop.

If two land: **Item A** + **Item B**. Closes the two run-46 substrate-frontier defects that produced observable retries.

---

## Production-ready bar (unchanged from run-46)

**A porter would not block on any single finding.** ~10% stochastic floor in play.

**Necessary conditions still in force**:
- Every codebase IG item is a Zerops-forced porter change (run-46 ✓)
- Every codebase KB bullet passes the S5 single-question test (run-46 ✓, modulo 4 prose-level recipe-internal carryovers Item E addresses)
- Every KB/IG citation with a topic carries display↔URL↔topic agreement (run-46 ✓, modulo legitimate `#deployfiles-` cite refused by R2 — Item B addresses)
- Codebase yaml friendly-authority adapt-path coverage proportional to porter-tunable count (run-46 ✓ at 9 hits)
- Zero cross-codebase KB duplications without cross-reference (run-46 ✓)
- Zero recipe-internal naming in published bullet stems/bodies (run-46 partial — Item E addresses prose-level)
- Zero refinement-close validator failures requiring re-record (run-46 ✓ on env fragments; 4 reverts on S4 citation surface — Item B addresses)
- Zero classification rejections at main-agent ACT path (run-46 ✓)
- Zero refinement-replace-reverted on URL composition (run-46 4 reverts — Item B addresses)
- **(new)** Zero close-gate retry loops (run-46 4-refusal loop — Item A addresses)
- **(new)** Every substrate-item refusal observable in `<engine-detail> status` (Item D addresses)

---

## Out of scope for run-47

- **CLAUDE.md voice** — deliberate divergence from goldens (S6 stays out-of-scope; CLAUDE.md is repo-local, not published; goldens aren't authoritative on every dimension). No fix needed.
- **Item 4 / Item 7 verification-in-production via deliberate failing fixture** — Item D's telemetry surfaces the firing-count organically next run; no need for a separate verification pass.
- **Engine `framework-quirk` auto-DROP teardown** — Item G's re-evaluation pass is the right scope; full teardown of the auto-DROP heuristic is broader than this run warrants.

---

## TDD discipline (per CLAUDE.md)

Every item: RED → GREEN → REFACTOR. Pin tests at every affected layer:
- Topology/types changes → unit tests (`internal/topology/`, `internal/recipe/types`)
- Engine logic → tool-layer tests (`internal/tools/`, `internal/recipe/`)
- New MCP action surface (Item D's `.gate-refusals.jsonl`, Item H's chunked emission) → integration tests
- End-to-end → e2e (-tags e2e) where observable cross-process

Brief-content edits → extend `briefs_rendered_substrate_pin_test.go` to pin new text. Don't ship brief edits without the pin extension.

Each item carries pin tests; codex review per item before merge (mandatory per project memory `feedback_codex_validation.md`).

---

## Reviewer + validation protocol

Same as run-46-substrate-design.md:
1. Codex review (no-monkey-patch validation) per item before merge.
2. Fresh-eyes review on the assembled PR set before run-47 dogfood.
3. Dogfood on run-47; validation report graded against this plan's pin tests + production-ready bar.

If any item fails its pin test in dogfood (gate did NOT fire when it should have, or fired falsely), name the root cause explicitly in the run-47 validation and propose the smallest counter-diff for run-48.

---

## Implementation tracking

(To be populated as PRs land.)

| Item | Description | Impl commit | Codex review | Status |
|---|---|---|---|---|
| A | idKey-format validation at `enrich-findings` (pre-mutation) | — | a0aa7572ae48c5e49 (plan-level) | pending |
| B | URL fragment preservation in `lookupGuideIDFromURL` | — | a0aa7572ae48c5e49 (plan-level) | pending |
| C | Counter consistency on cross-surface uniqueness receipt | — | a0aa7572ae48c5e49 (plan-level) | pending |
| D | Gate refusal telemetry ledger (`.gate-refusals.jsonl`) | — | a0aa7572ae48c5e49 (plan-level) | pending |
| ~~E~~ | ~~Self-referential linter extension~~ | DROPPED — scope-creep | — | dropped |
| ~~F~~ | ~~Refinement-1 brief disk-search guard-rail~~ | REJECTED — monkey-patch | — | dropped |
| G | Refuse auto-override on ambiguous classification | — | a0aa7572ae48c5e49 (plan-level) | pending |
| H | Typed multi-batch `enrich-findings` API | — | a0aa7572ae48c5e49 (plan-level) | pending |
| I | `enter-phase` idempotency on already-completed phase | — | a0aa7572ae48c5e49 (plan-level) | pending |
