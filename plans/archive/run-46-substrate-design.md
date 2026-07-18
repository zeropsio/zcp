# Run-46 substrate design — first-principles fixes to push to production-ready

> **Status**: validated by codex (`a70769eba948de0dd`) + fresh-eyes reviewer (`a0a53ef7a87687cc0`) on 2026-05-13. Scope tightened from 5 items to 7 (Item 2 dropped; Items 6 + 7 added; Items 1/3/4/5 reshaped per reviewer feedback). All items survive the no-monkey-patch test.

---

## Context — five runs of refinement-2 failing in different ways

| Run | refinement-2 failure mode |
|---|---|
| 41 | IG-coverage scope gap |
| 42 | Missed self-inflicted, X-Cache cross-origin, semantic-lie execOnce, cross-surface deferrals |
| 43 | 12 IG citations missed — scope gap, different surface count |
| 44 | G2/G5/G6/G7 all failed at audit-emit — 4 of 8 G-fixes didn't move authoring |
| 45 | Pillar B walked ZERO codebase surfaces — dispatch-discovery / disposition failure |

**Meta-pattern**: each prior fix has been "more prose in the brief." Five runs. Same sub-agent. Five distinct failure modes in the same spot. **Critical fresh-eyes finding**: in run-45 the refinement-2 brief ALREADY rendered absolute paths to `/var/www/zcprecipator/nestjs-showcase/apidev/README.md` etc — the sub-agent had the right paths and STILL didn't walk them. The failure mode isn't "wrong wording"; it's "sub-agent treats brief as advisory and walks partial state." Path-correction would have monkey-patched. **The structural fix is forcing the walk by handing structured data + requiring a walked-ledger receipt the close-gate verifies.**

**Production-ready means**: a porter reading the deliverable end-to-end would not block on any single finding. (Reframed from "≤1 minor finding" per fresh-eyes — counting findings ignores porter-blockingness and the ~10% stochastic drift floor.)

---

## The principle (unchanged)

**No monkey-patching.** Acceptable fix shapes:
- Removing a class of failure by construction
- Authoring-time structural gate enforcing invariants stochastic authors don't reliably reproduce
- Real validator bugs (existing validator misses a documented case)
- Engine-side safety primitives extended symmetrically

**Rejected**: "add more brief text," "add worked examples for things already taught," "test that hand-typed strings match reality," "tighten a derived rule to catch one more case."

---

## Scope — 7 items

### Item 1 — Surface-manifest-as-JSON-file + walked-ledger receipt

**Class of failure eliminated**: refinement-2 sub-agent failing to walk all codebase surfaces (run-41 → run-45 pattern). Run-45's specific evidence: brief already rendered absolute paths to codebase READMEs; sub-agent still walked partial state and emitted 0 findings on S4/S5/S7 across all 3 codebases.

**Change**: `build-subagent-prompt briefKind=refinement2` renders a structured manifest to `<outputRoot>/.refinement-2-manifest.json` and the brief instructs the sub-agent to Read it first. Manifest enumerates every item across every in-scope surface across all codebases as **one comparable corpus** (so the cross-surface uniqueness pass has structured input):

```json
{
  "tier_yaml_comments": {
    "0": [{"service": "db", "body": "...", "porter_tunable_directives": ["minContainers", "verticalAutoscaling.minRam"], "topic_candidates": ["env-var-model"]}, ...],
    "1": [...], ..., "5": [...]
  },
  "codebase_ig": {
    "api": [{"slot": 2, "body": "...", "topic_candidates": ["http-support"]}, ...],
    "app": [...], "worker": [...]
  },
  "codebase_kb": {
    "api": [{"stem": "...", "body": "...", "topic_candidates": ["managed-services-nats"]}, ...],
    "app": [...], "worker": [...]
  },
  "codebase_zerops_yaml": {
    "api": {"body": "...", "per_field_comments": [{"field": "minContainers", "value": "1", "comment_lines": [...], "porter_tunable": true}, ...]},
    "app": {...}, "worker": {...}
  }
}
```

Sub-agent walks the manifest, applies each surface's single-question test per-item, emits findings + a `walked` ledger listing the item IDs it actually evaluated.

**S6 (CLAUDE.md) explicitly NOT in the manifest** — repo-local, not published, refinement effort wasted. See "Substrate contract update" section below for the corresponding `audit_checklist.md` edit.

**Walked-ledger receipt** (codex Item 6a — folded into Item 1): close-gate refuses if `walked` ledger doesn't cover every manifest entry. Zero-finding allowed ONLY when walked ledger proves the item was evaluated; "didn't dispatch" and "walked-but-found-nothing" become distinguishable failure modes.

**Why structural, not monkey-patch**: removes filesystem walk from the sub-agent's responsibility. Eliminates the class of "audit failed because sub-agent didn't walk X." The walked-ledger closes the loop on "sub-agent emitted findings on a subset and stopped."

**Pin tests**:
- `TestRefinement2Manifest_EnumeratesEveryFragment` — given a fixture plan, manifest contains every fragment from the fragment store.
- `TestRefinement2CloseGate_RefusesIncompleteWalkedLedger` — close fails when ledger covers <100% of manifest.
- Existing `briefs_rendered_substrate_pin_test.go` extended to assert the brief's "Read the manifest first" instruction.

---

### ~~Item 2 — DISCARD-class authoring gate~~ — DROPPED

**Both reviewers verified** the codebase-content brief (`synthesis_workflow.md` L90-96 + L626-708) already surfaces all 6 candidateClass options including DISCARD classes with KEEP/DISCARD worked examples. The run-45 misclassification (`search-reindex-on-boot` as `intersection` despite self-evident self-inflicted "why" text) isn't a missing-knowledge gap; it's a disposition gap.

**Fresh-eyes argument**: requiring `classificationReasoning` as a four-field struct teaches disposition through a JSON wrapper. The sub-agent that mis-classifies one field today will write four equally-superficial answers tomorrow.

**Correct structural placement**: classification at authoring is best-effort; refinement-2 audit (Item 1) is authoritative. The `audit_checklist.md` already has "DROP wins" for DISCARD-class facts AND the per-surface S5 test would catch apidev KB #6 (framework-quirk) and #7 (self-inflicted) by principle if the audit walked them. With Item 1 forcing the walk, the existing DROP machinery is the structural answer. No new authoring gate needed.

---

### Item 3 — Extend snapshot/restore wrapper to env/import-comments fragments

**Class of failure eliminated**: validator-breaking refinement ACTs surfacing only at close-time (run-45 TY5 canonical-block strip → 6 missing-priority-justification-block at close → re-record cycle).

**Reframed per both reviewers**: the wrapper at `handlers.go::wrapRefinement` IS active for `PhaseRefinement && mode=replace` — but only when `codebaseHostFromFragmentID` resolves (codebase fragments). **Tier `env/<N>/import-comments/*` fragments are explicitly best-effort with no wrapper at all**, which is exactly the surface where run-45's TY5 cycle landed.

**Change**: extend `wrapRefinement` to also fire when fragment ID matches `env/<N>/import-comments/...` with a tier-aware `refinementPreCheckScoped` variant. One-line conditional change to the resolution logic; tier-scoped validators run before/after the replace.

**Why structural, not monkey-patch**: closes an asymmetry that was an oversight (codex-verified: codebase fragments transactional, env fragments explicitly "no snapshot/restore gate yet"). Same wrapper, broader fragment-ID coverage.

**Pin tests**:
- `TestWrapRefinement_TierImportCommentsWrapped` — TY5-breaking replace reverts via snapshot/restore.

---

### Item 4 — F-FRIENDLY-AUTH gate at codebase-content close, floor proportional to porter-tunable count

**Class of failure eliminated**: codebase yaml friendly-authority adapt-path coverage being author-stochastic (run-44 author = ~7 hits across 3 yamls; run-45 author = ~2). Substrate F6 teaches the pattern but doesn't enforce.

**Revised per both reviewers**: "≥1 per yaml" is below the goldens (jetstream zerops.yaml has 2 hits, showcase has 1). It would also fail run-45's workerdev (0 hits) for the wrong reason — workerdev legitimately has no porter-tunable directives beyond `minContainers` (which is tier-yaml territory, not codebase-yaml territory).

**Correct shape**: gate counts porter-tunable directives in each codebase yaml and requires adapt-path coverage **proportional to count**. Suggested formula: `floor = max(0, ceil(porter_tunable_directives_count / 3))`. Yamls with zero porter-tunables pass naturally. Yamls with 6 porter-tunables need ≥2 adapt-paths.

**Porter-tunable detection**: engine has authoritative knowledge via:
- yaml schema (`zerops-yaml-json-schema.json`) for which fields accept values vs are required-shape
- static allowlist of porter-tunable field families: `minContainers`, `maxContainers`, `verticalAutoscaling.*`, `ports[].port` (not for required ports like NestJS 3000), `objectStorageSize`, `priority`, env-var values matching custom-domain/secret/url patterns

**Adapt-path detection**: regex match on `if you`, `once you`, `bump`, `feel free to`, `swap to`, `rotate`, `tune`, `customize`, `change.*if`, `raise.*if`.

**Why structural, not monkey-patch**: enforces an existing F6 invariant via close-gate machinery (same shape as slot-shape refusal, named-constant drift, etc.). Doesn't add brief text. Doesn't depend on author discipline.

**Pin tests**:
- `TestFriendlyAuthGate_ProportionalFloor` — yaml with N porter-tunables, M adapt-paths, M < ceil(N/3) → close fails.
- `TestFriendlyAuthGate_NoTunablesPassesNaturally` — workerdev-shape yaml with zero porter-tunables → close passes.
- `TestFriendlyAuthGate_GoldensPass` — jetstream + showcase goldens pass the gate (regression fixture).

---

### Item 5 — G1 citation validator: display-text ↔ URL ↔ topic agreement

**Class of failure eliminated**: wrong-URL form-(b) citations passing because URL canonical-matches SOME guide even when display text named a different topic (run-45 apidev IG #4: rolling-deploys friendly text + init-commands canonical URL).

**Change** in the citation validator (`validators_codebase.go` per codex, or wherever the form-(b) anchored matcher lives):

```go
// existing: URL host+path matches some entry in CitationGuideURL
matchedGuideID := lookupGuideIDFromURL(url)

// NEW: display text must be the friendly display name for matchedGuideID
expectedDisplay := FriendlyDisplayName(matchedGuideID)
if displayText != expectedDisplay {
  return fmt.Errorf("citation display text %q does not match friendly name %q for the URL's matched guide %q", displayText, expectedDisplay, matchedGuideID)
}

// AND: when bullet topic is unambiguously detectable, URL must match canonical
bulletTopic := DetectBulletTopic(bulletBody)  // best-effort, returns "" on ambiguity
if bulletTopic != "" && CitationGuideURL[bulletTopic] != url {
  return fmt.Errorf("citation URL %s does not match canonical for bullet topic %q (canonical is %s)", url, bulletTopic, CitationGuideURL[bulletTopic])
}
```

Both reviewers: `DetectBulletTopic` best-effort (multi-topic bullets legit; only hard-fail on unambiguous topic detection).

**Land alongside Item 1's PR** — both share citation-map lookups.

**Pin tests**:
- `TestCitationGuideURL_DisplayTextAgreement` — run-45 apidev IG #4 case as failing fixture.
- `TestDetectBulletTopic_AmbiguousReturnsEmpty` — multi-topic body doesn't trigger hard-fail.

---

### Item 6 — Cross-codebase uniqueness pass gate (the remainder of codex's Item 6a after Item 1 absorbed walked-ledger)

**Class of failure eliminated**: cross-codebase KB duplications shipping without cross-reference (run-45: apidev IG #5 + worker KB #4 both teach APP_SECRET shadow; apidev KB #3 + worker KB #5 both teach https-Meilisearch; neither cross-references the other).

**Change**: Item 1's manifest already structures items as one comparable corpus. Add close-gate assertion: the sub-agent must emit a `crossSurfaceUniquenessReport: {scanned: <count>, duplicates_found: [<pair-references>], duplicates_resolved_in_findings: [...]}` ledger alongside the walked ledger. Close-gate refuses if `scanned < total-manifest-items` (sub-agent stopped before the cross-surface pass).

**Why structural, not monkey-patch**: existing audit_checklist.md cross-surface uniqueness pass becomes verifiable instead of trusted. Engine confirms the pass ran across the corpus; sub-agent can't silently skip it.

**Pin tests**:
- `TestRefinement2CloseGate_RequiresCrossSurfaceUniquenessReport` — close fails when sub-agent emits findings but no uniqueness report.

---

### Item 7 — Self-referential-naming linter at `record-fragment` for codebase KB (NEW per fresh-eyes)

**Class of failure eliminated**: recipe-internal symbols (filenames, class names, helper names, UI feature names) leaking into KB bullet bodies inside otherwise-legitimate intersection teachings. Run-45 apidev KB #5 (`X-Cache`, `X-Cache-Elapsed-Ms`, `X-Cache-Key` recipe-stamped headers + "cache demo"); KB #6 (`cache.module.ts`, `CacheService`, `CacheController`, `REDIS_CLIENT`, `cache.tokens.ts`); KB #7 (`SearchIndexer`, `ItemsService.onModuleInit`, "Search card", "Items card"). 0/3 caught by run-45 audit despite spec §"Self-referential decoration prohibition" being in the brief.

**Change**: regex-based linter at `record-fragment` time for fragment IDs matching `codebase/<host>/knowledge-base`. Detects backticked tokens in bullet bodies that resolve to:
- File paths from the codebase's `src/` glob
- Class/symbol names from the codebase's `package.json` + `src/` exports
- Module file names (`<x>.module.ts`, `<x>.service.ts`, `<x>.controller.ts` patterns)

Engine already reads `src/` glob and `package.json` for IG #1 rendering, so the data source exists. Refuse with redirect: "rewrite at the principle level or move to code comments. Spec §S5 self-referential decoration prohibition."

**Why structural, not monkey-patch**: same shape as named-constant-drift validator already in the engine. The principle is in the brief; the brief teaches it; goldens conform; stochastic authors don't reproduce reliably. Real Validator Bug per the plan's own taxonomy.

**Pin tests**:
- `TestSelfReferentialNamingLinter_RecipeSymbolsRefuse` — run-45 apidev KB #6 body as failing fixture.
- `TestSelfReferentialNamingLinter_AllowsGenericTokens` — `X-Cache` header NAME is platform-spec content (not recipe-internal); allow.
- `TestSelfReferentialNamingLinter_GoldensPass` — jetstream + showcase KB bodies pass the linter.

---

## Substrate contract update (S6 out-of-scope)

`internal/recipe/content/briefs/refinement2/audit_checklist.md` L26-48 currently says *"This audit walks S3, S4, S5, S6, S7."* Per user direction (CLAUDE.md is repo-local, not published, refinement effort wasted) and codex's contract-drift warning, update the substrate contract to:

- Mark S6 explicitly out-of-scope with rationale (repo-local, not published, refinement effort wasted).
- Update L162 walk loop: `For SURFACE in {S3, S4, S5, S7}` (drop S6).
- Update L189 surface-caps table: remove S6 row or annotate "out of scope for refinement-2 audit; CLAUDE.md authoring gates at codebase-content close."

Pin: `briefs_rendered_substrate_pin_test.go` extended to assert S6-out-of-scope text in rendered brief.

---

## Dropped (monkey-patches)

| Original item | Why dropped |
|---|---|
| ~~Item 2 — DISCARD-class authoring gate~~ | Brief already surfaces all 6 classes with worked examples. JSON-wrapper reasoning = teaching disposition through a struct = monkey-patch. Item 1 + existing audit DROP machinery is the structural answer. |
| ~~Item 7 (old) — Substrate-text → filesystem-path consistency test~~ | Subsumed by Item 1 (no paths in brief = no consistency to test). |
| ~~Item 8 — Audit checklist tunable-field enumeration~~ | S7 test already says "bare mechanism on porter-tunable values" is a fail. Listing tunable fields = teaching disposition. Item 4 (authoring-time gate) closes the gap upstream. |
| ~~Item 9 — Triage REWRITE-with-adapt-path scope~~ | Only relevant if Item 8 stays. With Item 4 closing the gap at authoring, triage doesn't need new scope teaching. |

---

## Production-ready bar (reframed per fresh-eyes)

**"A porter would not block on any single finding"** — not finding-count-based. ~10% stochastic floor is in play per project memory; counting beyond that is fiction. Porter-blockingness is the semantic that matters.

**Necessary conditions** (run-45 missed at least 4):
- Every codebase IG item is a Zerops-forced porter change (✓ run-45)
- Every codebase KB bullet passes the S5 single-question test (run-45: 2 hard-fail; Items 1 + 7 address)
- Every KB/IG bullet that names a citation-map topic carries a citation with display↔URL↔topic agreement (run-45: 4/14 KB, 1/10 IG with one wrong-URL; Items 1 + 5 address)
- Every codebase yaml has friendly-authority adapt-path coverage proportional to its porter-tunable count (run-45: 2 hits across 3 yamls; Item 4 addresses)
- Zero cross-codebase KB duplications without cross-reference (run-45: 2 ship without cross-ref; Items 1 + 6 address)
- Zero cross-surface duplications (run-45: closed on tier yamls; not walked on codebase surfaces; Items 1 + 6 address)
- Zero recipe-internal naming in published bullet stems or bodies (run-45: 3 bullets violate; Item 7 addresses structurally; Item 1 addresses via audit walk)
- Zero refinement-close validator failures requiring re-record (run-45: TY5 cycle; Item 3 addresses)
- Zero classification rejections at main-agent ACT path (run-45: ✓ 0)
- Zero refinement-replace-reverted on URL composition (run-45: ✓ 0)

---

## Phase ordering (both reviewers converge)

1. **Item 1 + Item 5 + S6 contract update together** — they share citation-map and topic-detection lookups; landing as one PR keeps test fixtures consistent. Item 1's manifest infrastructure is the foundation for Item 6's gate.
2. **Item 7 (self-referential-naming linter)** — independent, smallest validator addition, closes the highest-visibility surface defect class.
3. **Item 3 (snapshot/restore for env fragments)** — one-line conditional, independent.
4. **Item 4 (friendly-authority gate)** — requires plan.json metadata extension (porter-tunable directive enumeration) so lands after the smaller items.
5. **Item 6 (cross-surface uniqueness ledger)** — layered on top of Item 1's manifest infrastructure.

If only one item lands: **Item 1**. Without it, refinement-2 will continue to fail on codebase surfaces in some new way per the 5-run pattern.

---

## TDD discipline (per CLAUDE.md)

Every item: RED → GREEN → REFACTOR. Pin tests at every affected layer:
- Topology/types changes → unit tests
- Engine logic → tool-layer tests
- New MCP action surface (Item 1's manifest endpoint, if exposed) → integration tests
- End-to-end → e2e (-tags e2e) where the change is observable cross-process

Brief-content edits → extend `briefs_rendered_substrate_pin_test.go` to pin the new text. Don't ship brief edits without the pin extension (Pillar A invariant).

---

## Reviewer agentIds

- Codex (no-monkey-patch validation): `a70769eba948de0dd`
- Fresh-eyes (production-ready validation): `a0a53ef7a87687cc0`

---

## Implementation tracking

(To be populated as PRs land.)

| Item | Implementation commit | Codex review | Status |
|---|---|---|---|
| 1 + 5 + S6 contract | | | pending |
| 7 | | | pending |
| 3 | | | pending |
| 4 | | | pending |
| 6 | | | pending |
