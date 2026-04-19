# v8.98 — Simulation-surfaced gaps in v8.97

**Intended reader**: a fresh Opus 4.7 instance. This doc is self-contained.

**Prerequisite read**: [docs/implementation-v8.97-masterclass.md](implementation-v8.97-masterclass.md), especially its §"Shipped (v8.97)" preamble and Fixes 2, 4, 5.

**Goal**: close three gaps that a v33 dry-simulation surfaced after v8.97 landed. None of them invalidate v8.97's fixes — each is an interaction v8.97 didn't explicitly spec. Three small changes; one new test file; no new checks; no schema changes.

---

## 1. What the simulation surfaced

Walking a Nest.js showcase run through v8.97's code paths exposed three points where the spec is right in isolation but the run-time flow has no named owner:

| Gap | What breaks | Where |
|---|---|---|
| **A — Feature subagent doesn't see Fix 5 principles** | The feature subagent at deploy step 4b adds new controllers, new workers, new subscription handlers. Any of those can violate Principles 2, 3, 4. The scaffold-subagent-brief has the MANDATORY principles block; the feature-subagent-brief does not. Fix 5's pre-ship assertions run only at scaffold time, so principle violations introduced during feature authoring land at close review, which is the same lag Fix 5 was supposed to eliminate. | [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) feature-subagent-brief region (~line 1672) |
| **B — Export has no trigger-time owner** | v8.97 Fix 3 Part C (server-side sentinel verifier) was dropped; Fix 6 (orchestrator self-gating prompt) is out of scope for this repo. `buildClosePostCompletion` in [internal/workflow/engine_recipe.go:129](../internal/workflow/engine_recipe.go#L129) says *"Export runs automatically"* but no component actually runs it — Fix 1 only *refuses* early exports. The close-complete response tells the user about publish but not about export. An agent reading the response has no instruction to run `zcp sync recipe export`. | [internal/workflow/engine_recipe.go:129-146](../internal/workflow/engine_recipe.go#L129-L146) |
| **C — Close sub-step order isn't enforced** | `initSubSteps(RecipeStepClose, plan)` returns `[code-review, close-browser-walk]` in that order (see [internal/workflow/recipe_substeps.go:143](../internal/workflow/recipe_substeps.go#L143)). The server's sub-step gate requires both attestations but doesn't require `code-review` to come first. An agent that runs `close-browser-walk` first, finds a visual issue, applies a fix, then runs `code-review` which finds a different issue and applies another fix, has a stale browser-walk attestation captured against pre-fix state. | `recipeCompleteSubStep` in [internal/workflow/engine_recipe.go:148](../internal/workflow/engine_recipe.go#L148) |

The three gaps are independent and low-blast-radius. Fix each at the source; no downstream coupling.

---

## 2. The fix stack

### Fix A — Mirror Fix 5 principles into the feature subagent brief

**What the simulation proved**: at T+00:20 in the v33 walkthrough, the feature subagent receives a MANDATORY block for file-op sequencing + tool policy + SSH-only executables (Fix 3 Parts A/B). It does NOT receive the six platform principles. At T+00:45 deploy completes with feature code shipped. At T+01:05 close-review finds a new controller that binds to `localhost:3000` (Principle 2 violation) or a new queue subscriber without competing-consumer semantics (Principle 4 violation). The close-review round-trip reintroduces exactly the lag Fix 5 was meant to eliminate — the principles applied to the scaffolded code but not to the code written on top of it.

**Change**: add a principle-sustain block to the feature-subagent-brief in `recipe.md`. Same MANDATORY syntax as Fix 5. Shorter body — the feature subagent doesn't scaffold, it extends; it inherits the principle facts the scaffold subagent already recorded and must not regress them.

Add immediately after the existing Fix 3 MANDATORY block in the feature-subagent-brief (after line ~1681, before the `Step 4b: Dispatch the feature sub-agent` paragraph):

```markdown
<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Feature pre-ship — sustain the platform principles

The scaffold subagent recorded facts naming how each applicable platform principle was satisfied in the scaffolded code (graceful shutdown, routable bind, proxy awareness, competing-consumer, structured creds, stripped build root). Your job is to **not regress** those facts as you add features.

For every new code path you author:

- **New HTTP route or controller**: verify the parent server still binds to all interfaces and still trusts the upstream proxy (Principles 2, 3). You do not re-bind; you verify nobody mis-configured during refactoring.
- **New long-lived resource (DB pool, broker client, cache client, file handle)**: verify it is closed by the existing graceful-shutdown hook (Principle 1). If not, extend the hook OR record a fact explaining the fire-and-forget decision.
- **New subscription / consumer / stream listener**: verify the subscription uses the broker's competing-consumer mechanism (Principle 4). Without it, every replica processes every message.
- **New external-service client that accepts credentials**: verify credentials are passed as structured options, not embedded in URL form (Principle 5).

If you add code that violates any principle, fix it inline and record a fact with `scope="both"` naming the principle number, the idiom you used, and the code location. The scaffold subagent already recorded the happy-path facts; your facts capture extensions and regressions prevented.

Do NOT re-enumerate the principle text here — the scaffold subagent brief is the authoritative source. If you are uncertain about a principle's intent, pull the scaffold brief via `zerops_workflow` step guidance and read it.

<<<END MANDATORY>>>
```

**RED tests** (extend existing `internal/workflow/recipe_mandatory_blocks_test.go`):
- `TestFeatureBrief_HasPrincipleSustainBlock` — grep the feature-subagent-brief region for `"Feature pre-ship — sustain the platform principles"`; assert present.
- `TestFeatureBrief_SustainBlockReferencesAllPrinciplesByNumber` — assert the block contains `"Principle 1"`, `"Principle 2"`, `"Principle 3"`, `"Principle 4"`, `"Principle 5"`. (Principle 6 does not apply — static deploys don't have feature code; if you add static-deploy-only showcases in future, revisit.)
- `TestFeatureBrief_SustainBlockIsMandatoryWrapped` — assert the block sits between `<<<MANDATORY` and `<<<END MANDATORY>>>` sentinels so the dispatch-construction rule transmits it verbatim.
- `TestFeatureBrief_SustainBlockPointsAtScaffoldBrief` — assert the block contains the string `"scaffold subagent brief is the authoritative source"` (or substring match on `"authoritative source"`). This enforces the single-source-of-truth intent: principle text lives in the scaffold brief; the feature brief references it.

**Expected v33+ impact**: principles apply at every code-authoring dispatch, not just at scaffold time. Feature-introduced principle violations are caught at feature pre-ship, not at close review. The happy path produces ≥ 1 additional `scope="both"` fact per codebase that acquires a new long-lived resource — raises the downstream-fact bar without needing a new check.

### Fix B — Export in the close-completion response

**What the simulation proved**: at T+01:22 the simulation's narration said *"Orchestrator's export prompt fires (or user requests)"*. Reading back the code: nothing in the close-complete response tells the agent to run export. `buildClosePostCompletion` populates `PostCompletionSummary` with the misleading phrase *"Export runs automatically"* and `PostCompletionNextSteps[0]` with the publish command. Fix 6 (orchestrator prompt) is out of scope for this repo. Fix 1 only refuses early exports — it does not initiate them. So export sits in limbo: server-side gated but nobody triggers it. In practice agents will eventually run it because the old orchestrator prompt is still in the run driver — but that's coincidence, not spec.

**Change**: make the close-complete response include the export command as `NextSteps[0]` (the action to take now, autonomously) and the publish command as `NextSteps[1]` (the action to take if the user asked). Update `PostCompletionSummary` to describe the real sequence.

Edit `buildClosePostCompletion` in [internal/workflow/engine_recipe.go:129](../internal/workflow/engine_recipe.go#L129):

```go
func buildClosePostCompletion(plan *RecipePlan, outputDir string) (string, []string) {
    slug := "<slug>"
    if plan != nil && plan.Slug != "" {
        slug = plan.Slug
    }
    dir := "<recipe-dir>"
    if outputDir != "" {
        dir = outputDir
    }
    summary := "Recipe verified (code-review + close-browser-walk complete). Next: run export autonomously against the output directory; relay the publish command to the user only if they explicitly asked to ship."
    nextSteps := []string{
        fmt.Sprintf("Export the archive now (autonomous, not user-gated): run `zcp sync recipe export %s`. The server-side close gate (Fix 1) is satisfied; export will succeed. Include `--include-timeline` if TIMELINE.md is not yet present.", dir),
        fmt.Sprintf("To publish to zeropsio/recipes: run `zcp sync recipe publish %s %s`. This opens a PR on the recipes repo; relay to the user only when they explicitly asked to ship.", slug, dir),
    }
    return summary, nextSteps
}
```

Two structural changes from the shipped version:

1. `NextSteps[0]` is now export, unambiguously labeled as autonomous.
2. `NextSteps[1]` is publish, labeled as user-gated.

The agent reads the response, runs export without asking the user, then either relays or drops the publish command based on whether the user asked for publication in the session.

**RED tests** (extend existing `internal/workflow/recipe_test.go`):
- `TestHandleComplete_CloseStepPostCompletionHasExportAtZero` — fixture close complete; assert `NextSteps[0]` contains `"zcp sync recipe export"` and the word `"autonomous"` (so the agent reading it knows to run it without asking).
- `TestHandleComplete_CloseStepPostCompletionHasPublishAtOne` — fixture close complete; assert `NextSteps[1]` contains `"zcp sync recipe publish"` and a phrase indicating user-gated intent (`"user explicitly asked"` / `"explicitly asked to ship"`).
- `TestHandleComplete_CloseStepSummaryDoesNotClaimAutomaticExport` — assert summary does NOT contain the phrase `"Export runs automatically"` (the shipped version's misleading wording). The summary must describe export as the agent's autonomous next action, not as something that happens without an agent.

Update the existing `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` (in [internal/workflow/recipe_test.go:335](../internal/workflow/recipe_test.go#L335)):

```go
// Two NextSteps entries now: export at [0] (autonomous), publish at [1] (user-gated).
if len(nextSteps) != 2 {
    t.Fatalf("expected exactly 2 NextSteps entries (export + publish), got %d: %+v", len(nextSteps), nextSteps)
}
if !strings.Contains(nextSteps[0], "zcp sync recipe export") {
    t.Errorf("NextSteps[0] must name export CLI command; got %q", nextSteps[0])
}
if !strings.Contains(nextSteps[1], "zcp sync recipe publish") {
    t.Errorf("NextSteps[1] must name publish CLI command; got %q", nextSteps[1])
}
```

**Also update the close section in `recipe.md`** (~line 2941): the phrase *"After close completes, export runs automatically"* is now ambiguous. Replace with:

```markdown
- After close completes, the workflow response includes `postCompletion.nextSteps[0]` — the export command. Run it autonomously (the server-side close gate passes; export succeeds). `postCompletion.nextSteps[1]` is the publish command; relay it to the user only if they explicitly asked to ship.
```

**Expected v33+ impact**: export has a named trigger — the agent, acting on the structured close-complete response. No orchestrator dependency. No silent-export-never-runs failure class. The §4.5 deliverable-completeness bar from v8.97 stays satisfied because the agent now has explicit instruction to run export every time.

### Fix C — Enforce close sub-step ordering server-side

**What the simulation proved**: canonical close flow is `code-review` → (apply fixes) → `close-browser-walk`. The static review is cheap and catches issues the agent then fixes; the browser walk is expensive and serves as final dynamic verification *after* fixes are in. The shipped `recipeCompleteSubStep` handler in [internal/workflow/engine_recipe.go:148](../internal/workflow/engine_recipe.go#L148) accepts the two attestations in any order. If an agent runs browser-walk first, captures an attestation against pre-fix state, then runs code-review which triggers fixes, the browser-walk attestation is stale by the time `complete step=close` fires. Server accepts anyway (both attestations present), but the verification chain is broken.

**Change**: reject `substep="close-browser-walk"` attestation unless `substep="code-review"` has already been attested in the same session. One conditional guard in `recipeCompleteSubStep` — no new sub-steps, no new checks.

Add to `recipeCompleteSubStep` in `engine_recipe.go`:

```go
// v8.98 Fix C: close sub-step ordering. close-browser-walk is expensive
// dynamic verification and must run AFTER code-review's static pass so
// browser-walk observes the post-fix state. Without this guard the agent
// can attest browser-walk first against pre-fix state, then run code-review
// which applies fixes, leaving a stale browser-walk attestation.
if step == RecipeStepClose && subStepName == SubStepCloseBrowserWalk {
    closeStep := state.Recipe.Steps[RecipeStepCloseIdx] // index of close in Steps slice
    codeReviewDone := false
    for _, ss := range closeStep.SubSteps {
        if ss.Name == SubStepCloseReview && ss.Status == StepComplete {
            codeReviewDone = true
            break
        }
    }
    if !codeReviewDone {
        return nil, fmt.Errorf(
            "%s: close sub-step %q must be attested BEFORE %q. Dispatch the code-review subagent first, apply any fixes, then run the browser walk so it observes the post-fix state. Attesting browser-walk first against pre-fix code produces a stale verification signal.",
            platform.ErrSubagentMisuse, SubStepCloseReview, SubStepCloseBrowserWalk,
        )
    }
}
```

Use whichever constant names the codebase already uses for the close step index (`RecipeStepCloseIdx` is illustrative — check [internal/workflow/recipe_substeps.go](../internal/workflow/recipe_substeps.go) for the actual accessor).

**RED tests** (new file `internal/workflow/recipe_close_ordering_test.go`):
- `TestCloseSubStepOrder_BrowserWalkBeforeReviewRejected` — fixture session at step=close; attempt `substep=close-browser-walk` without prior `code-review` attestation; assert error returned, error code is `SUBAGENT_MISUSE`, and message names both sub-steps by their literal names.
- `TestCloseSubStepOrder_ReviewBeforeBrowserWalkAccepted` — fixture session at step=close; attest `code-review` → attest `close-browser-walk`; both succeed; no error.
- `TestCloseSubStepOrder_ReviewTwicePermitted` — fixture session at step=close; attest `code-review` twice (agent re-ran review after a fix); assert both accepted, no error. (The server may deduplicate at the state level; what matters is the guard doesn't block legitimate re-review.)
- `TestCloseSubStepOrder_BrowserWalkErrorIsActionable` — fixture session at step=close; attempt browser-walk first; assert error message contains the substrings `"code-review"`, `"close-browser-walk"`, `"before"`, and `"post-fix state"`. The agent must be able to read the error and recover without guessing.

**Expected v33+ impact**: browser-walk attestations are always captured against post-fix code. A run that tries the wrong order gets a specific diagnostic at the first offending call, with remediation in the error text — recoverable in one extra sub-step attestation. Zero impact on runs that already follow canonical order (the overwhelming majority).

---

## 3. What stays UNTOUCHED

v8.97's rollback-calibration rule still holds. Do not:

- Resurrect Fix 3 Part C (server-side sentinel verifier). v8.97's `Shipped` section dropped it deliberately; v8.98 respects that decision.
- Add new checks or modify existing check semantics.
- Add new sub-steps. Close stays `[code-review, close-browser-walk]` — Fix C orders the existing two, does not add a third.
- Introduce new subagent roles. Fix A extends the existing feature-subagent-brief; it does not spawn a new subagent.
- Touch `PostCompletionGuidance` typing. The inlined `PostCompletionSummary` + `PostCompletionNextSteps` shape from v8.97 is sufficient; Fix B only changes the values, not the shape.
- Add a new error code for Fix C's ordering violation. `SUBAGENT_MISUSE` covers "agent called a sub-step in the wrong order" — same semantic class as v8.90's existing rejections.

If v33's run surfaces something v8.98 didn't anticipate, write it down in `recipe-version-log.md` §v33 entry. Ship the smaller fix first.

---

## 4. v33 delta to v8.97's calibration bar

Only the workflow axis (W) changes. Everything else in v8.97 §4 holds.

### 4.4 Workflow (W = A) — additional bars for v8.98

- Feature subagent dispatch prompt contains the verbatim `<<<MANDATORY — TRANSMIT VERBATIM...>>> ### Feature pre-ship — sustain the platform principles` block (Fix A bar).
- Feature subagent records ≥ 1 `scope=both` fact per codebase that introduces a new long-lived resource or subscription (Fix A downstream-observable bar — replaces the missing "feature pre-ship caught a principle regression" signal).
- Close-complete `PostCompletionNextSteps` has exactly two entries: export at `[0]`, publish at `[1]` (Fix B bar).
- Close-complete `PostCompletionSummary` does not contain `"Export runs automatically"` (Fix B regression guard — the v8.97 shipped wording).
- Close sub-step attestations are in canonical order: every run's event log shows `code-review` attested before `close-browser-walk` (Fix C bar). Out-of-order attempts hit `SUBAGENT_MISUSE` and recover without shipping a stale browser-walk attestation.

---

## 5. Implementation phases

### Phase 1 — RED (target ~25 min)

1. Write all tests listed above without implementation. Expect every new test to fail.
2. Run `go test ./... -count=1 -short` — assert new tests fail, nothing pre-existing fails.

### Phase 2 — GREEN (target ~75 min)

Implement in this order to minimize interaction risk:

1. **Fix A (feature principles)** — 20 min. `recipe.md` content only. Reuses Fix 3's MANDATORY syntax (already shipped). Run `go test ./internal/content/... ./internal/workflow/...` after; `detailedGuide` extractor must still work.
2. **Fix B (export in NextSteps)** — 25 min. Edit `buildClosePostCompletion` + one close-section paragraph in `recipe.md`. Update the existing `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` test signature from one NextSteps entry to two. Lowest risk — isolated function change.
3. **Fix C (close sub-step ordering)** — 30 min. New conditional in `recipeCompleteSubStep`. Touches the happy-path of close completion, so run the full `internal/workflow/` test suite after; regression risk is any existing test that attested browser-walk first as a test-fixture convenience.

### Phase 3 — REFACTOR (target ~15 min)

1. `go test ./... -count=1` clean under `-race`.
2. `make lint-local` clean.
3. Verify file sizes: `engine_recipe.go` must remain under 350 lines after Fix C's insertion (check current size; split if the new conditional pushes it over).
4. Diff against CLAUDE.md conventions — no new globals, no `interface{}`, no fallbacks, errors wrapped.

### Phase 4 — v33 run + calibration

1. Execute a single `nestjs-showcase` run end-to-end.
2. Walk v8.97 §4 + v8.98 §4.4 calibration bars. Any fail = file a post-mortem in `recipe-version-log.md` §v33 entry identifying which fix's mechanism failed.

If every bar passes → v33 is A-grade. Close the v8.97+v8.98 milestone together.

---

## 6. Out-of-scope temptations

**"Let me also add the feature-principles block to the code-review subagent brief"** — no. Code-review doesn't author code; it inspects. Principle verification at review time is already the default code-review contract (Fix 5's own "caught by the close-step code review" clause). The code-review brief should stay lean; adding the principles block to it duplicates enforcement and risks divergence.

**"Let me enforce ordering across deploy sub-steps too (readmes must come after verify-stage)"** — no. v8.97 shipped without deploy-sub-step ordering enforcement because the existing deploy-check chain catches out-of-order readmes at emit time. If v33 surfaces a deploy-sub-step ordering bug, file it; do not preemptively extend Fix C's pattern without evidence.

**"Let me revive Fix 3 Part C to verify the feature-principles block transmitted"** — no. v8.97's Shipped section dropped Part C deliberately. If v33 shows principle regressions caused by dropped MANDATORY blocks, THEN spec Part C properly. Not before.

---

## 7. One-sentence summary per fix

- **Fix A**: principle enforcement extends from scaffold-time to feature-time via a mirrored MANDATORY block in the feature-subagent-brief, so new controllers/workers/subscriptions can't introduce violations that scaffold-time pre-ship would have caught.
- **Fix B**: `PostCompletionNextSteps[0]` is now the export command (autonomous) and `[1]` is publish (user-gated), so the agent has an explicit trigger for export without relying on the out-of-scope orchestrator prompt.
- **Fix C**: `recipeCompleteSubStep` rejects `close-browser-walk` attestation when `code-review` hasn't been attested yet, so browser-walk always observes post-fix state rather than pre-fix state.

Ship all three. Run v33. Grade A on the v8.97 + v8.98 bar.
