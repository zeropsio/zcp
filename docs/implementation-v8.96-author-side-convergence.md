# v8.96 — Author-Side Convergence (standalone implementation guide)

**Intended reader**: A fresh Opus 4.7 instance (or equivalent) tasked with implementing this change from scratch. This doc is self-contained — you don't need prior conversation context.

**Prerequisite reading (in order)**:

1. [docs/recipe-version-log.md](recipe-version-log.md) §v31 entry (lines 371-?) — the run that motivates this plan. Focus on the "Top cost drivers" table and the two 3-round convergence loops (deploy-complete README checks, generate-finalize env checks).
2. [docs/implementation-v8.95-content-surface-parity.md](implementation-v8.95-content-surface-parity.md) §5 — the previous release. v8.96 does NOT revisit env-README Go templates; v8.95 already handled those.
3. [docs/implementation-v8.86-plan.md](implementation-v8.86-plan.md) §§1-3 — the "inversion of verification direction" thesis. v8.96 extends the inversion from a single writer subagent to every author surface.
4. [internal/workflow/bootstrap_checks.go](../internal/workflow/bootstrap_checks.go) — the `StepCheck` type; read it to understand what we extend.
5. [internal/tools/record_fact.go](../internal/tools/record_fact.go) — the facts log schema.
6. [CLAUDE.md](../CLAUDE.md) — project conventions. Especially: "Max 350 lines per .go file", "fix at the source, not downstream", "no fallbacks", "RED before GREEN."

**Target ship window**: single release (v8.96). Two structural changes, staged migration of existing checks. No hardcoding of specific check names or failure strings — both changes are uniform extensions of existing machinery.

---

## 0. Dry-run simulation status

**This guide has been walked through actual code surfaces once**. Six implementation traps and two runtime-prediction gaps surfaced. All corrections are reflected in §§5-9 below. The original (pre-simulation) guide had these defects:

1. **Theme A assumed host-prefixed check names that don't exist** — [workflow_checks_recipe.go:598-608](../internal/tools/workflow_checks_recipe.go#L598) emits bare `"comment_ratio"`. Fixed by dropping the renaming aspiration from §5.3: `ReadSurface` field disambiguates without a Name rewrite.
2. **Theme B misplaced the injection point** — zcp doesn't see Agent tool dispatches. Correct hook is [recipe_guidance.go:472 `buildSubStepGuide`](../internal/workflow/recipe_guidance.go#L472) via a per-topic `IncludePriorDiscoveries` flag. Corrected in §6.
3. **`RecipeState` has no `SessionID` field** ([recipe.go:23](../internal/workflow/recipe.go#L23)) — must thread `sessionID` through `BuildResponse → buildGuide → buildSubStepGuide`. §6.2 now names the 3 signature changes explicitly.
4. **"One sentence HowToFix" was too restrictive** — real cases (coupled yaml-in-README + on-disk sync) need 2 statements. Relaxed to "1-3 sentences, imperative mood, no hedging words" in §5.1.
5. **v8.95 manifest-completeness check would fail on Scope=downstream facts** — [workflow_checks_content_manifest.go:194](../internal/tools/workflow_checks_content_manifest.go#L194). Fixed in new §6.3: completeness filters Scope.
6. **Brief-size bloat risk** — scaffold over-recording could push feature brief past 3KB. Added 8-entry cap with elision in §6.2.
7. **Runtime adoption gap** — agents have no history of `Scope`. Added v32 calibration bar in §9: "≥2 downstream-scoped facts recorded; if 0, mechanism untested."

### Fresh simulation (second pass, run after revisions above were applied)

A second walk-through surfaced four more traps. All are reflected below:

8. **`GuidanceTopic`, not `RecipeTopic`** — the struct in [recipe_topic_registry.go:13](../internal/workflow/recipe_topic_registry.go#L13) is named `GuidanceTopic`. §6.3 and §8 step 15 corrected.
9. **`BootstrapState.buildGuide` exists with identical name** — [bootstrap.go:246](../internal/workflow/bootstrap.go#L246) calls `b.buildGuide` which is the bootstrap variant, not recipe. §6.2 clarified that v8.96 touches ONLY the Recipe path.
10. **No Scope validation in `AppendFact`** — without it, a typo like `"downsteam"` silently defaults to content-scope (writer reads it, downstream subagents don't). Added a `knownScopes` enum check in §5.3 of this doc (below).
11. **`prefix` parameter shape in finalize helpers** — `prefix = folder + "_import"`, so back-deriving `envFolder` for ReadSurface requires `strings.TrimSuffix(prefix, "_import")`. Acceptable in v8.96 (one TrimSuffix call per helper); v8.97 may refactor to explicit parameter.

### What a second simulation should look for

Before starting the RED phase, the implementer SHOULD:

1. Simulate §5.1 by hand on `factual_claims` (multi-entry per file) — verify each entry gets an independent ReadSurface + HowToFix.
2. Simulate §6 against a real facts-log capture — verify the 8-entry cap fires with realistic v31-scale fact volumes.
3. Re-read the phase-5 wire-up in §8 against `buildSubStepGuide`'s current signature; confirm the signature ripple is additive (param added; no callers broken).

---

## 1. Context — what v31 surfaces

v31 ran at **86 min wall / 100% gotcha-origin genuine / A− overall** — the best grade since v20. The v8.95 structural fixes (scaffold-artifact-leak check, env-README Go-template edits, ZCP_CONTENT_MANIFEST contract) held without exception.

But the timeline analysis identified two **structural convergence limiters** that together account for ~6 min of the 86 min wall:

### 1.1 The deploy-complete 3-round loop (~2 min)

At `complete step=deploy` (12:25:19), 4 README checks failed. Main agent fixed inline. Second attempt (12:28:56): `comment_ratio` STILL failed even though main had boosted comment density in `apidev/zerops.yaml`. Reason: `comment_ratio` reads **the YAML fenced block INSIDE apidev/README.md's integration-guide**, not the on-disk `zerops.yaml`. Main learned this by second failure, re-synced embedded YAML, third attempt passed.

The check's semantics are correct (published README is the shippable artifact; embedded YAML must stay accurate). What's wrong is that the author had no way to know what the check reads until the check failed and the author inferred it from the failure text.

### 1.2 The finalize 3-round loop (~4 min)

At `complete step=finalize`, the first attempt failed on 11 distinct check violations across 6 env `import.yaml` files: `comment_ratio` below 30%, `comment_depth` below 35%, `cross_env_refs` (comments explicitly named sibling tiers), `factual_claims` (comment said `minContainers: 1` but YAML declared 2). Main rewrote all 6 env yamls inline. Second attempt still failed. Third attempt passed.

Same pattern: the author (main agent) has prose guidance about env-comment rules in `topic=env-comment-set`, but no way to run the actual check before attesting.

### 1.3 Cross-subagent duplicate archaeology (~80s cumulative)

Three separate subagents investigated the same framework quirks during the run:

- Scaffold subagent (SUB-a62) spent ~20s grepping Meilisearch v0.57 type defs to discover the `waitTask` / `EnqueuedTaskPromise` API shape.
- Feature subagent (SUB-aa9) spent another ~15s re-investigating the same API 20 minutes later.
- Code-review subagent (SUB-a34) spent ~15s investigating the same svelte-check vs typescript version mismatch the feature subagent already discovered.

None of these facts shipped as content (writer correctly discarded them as library-meta / tooling-meta). But each subagent re-discovered them because the facts log is read ONLY by the writer, not by downstream delegates.

### 1.4 Secondary inefficiencies noted but NOT in scope

- `zerops_knowledge` schema-churn (5 errors / 15s) — tool-ergonomics concern, out of scope.
- Feature subagent port-kill dance after `dev_server stop` — prompt-level concern, out of scope.
- Scaffold "File has not been read yet" errors (~18s) — prompt-level concern, out of scope.
- Git-lock transient on SSHFS parallel ops (~90s) — platform-layer concern, out of scope.

These are individually small (<20s each). Fixing them requires touching disparate surfaces (MCP schema, prompt templates, platform deployer). v8.96 is structurally focused; v8.97+ can pick up ergonomic polish if v32 still surfaces them.

---

## 2. Root cause — two structural asymmetries

### 2.1 Authors don't have access to the rules the gate runs

Every content check has a Go implementation that defines its semantics authoritatively. The author (main agent during finalize, writer subagent during deploy.readmes, etc.) has only prose guidance describing what the rule is supposed to be. The prose drifts from the code; the prose also can't express **what file the check reads** or **which other files are coupled**.

Current flow:
```
author writes → attests complete → gate runs checks → gate emits pass/fail
                                                    → if fail, author reads prose and infers what to change
                                                    → author revises → attests again
```

This is the v23-era pattern v8.86 flipped for **one specific actor** (the readmes writer subagent). The writer's brief embeds the rules as runnable pre-checks (awk / grep patterns), and the writer iterates internally until its self-check passes. Convergence dropped from 5 rounds to 1.

But v8.86's flip applies **only to the writer subagent**. Every other author — main agent during finalize, main agent during deploy-comment-ratio recovery, scaffold subagents — still operates in the pre-v8.86 "write → attest → fail → infer → retry" pattern.

### 2.2 Facts log is a single-reader queue, not a message bus

Today `zerops_record_fact` appends to `/tmp/zcp-facts-{sessionID}.jsonl`. The readmes writer subagent reads the log and classifies each fact. Other subagents (feature, code-review, follow-up scaffolds) do NOT read the log. The writer's classification taxonomy (`discard`, `ship as gotcha`, `ship as IG`, `ship as CLAUDE.md`) is content-facing only — it has no "forward this to the next subagent so they don't re-investigate" channel.

Result: every downstream subagent that could benefit from an upstream discovery re-does the archaeology.

---

## 3. Principle — what symmetric author-side convergence looks like

**Principle A**: The author of any content surface must be able to run every check that gates that surface, with structured output, BEFORE attesting.

**Principle B**: Facts discovered by one subagent must be visible to downstream subagents whose work would be aided by them, independent of whether those facts ship as published content.

Corollaries:

1. The check is the source of truth; prose guidance is derived from (and explicitly points at) the check, never the reverse.
2. A check failure response carries enough structured detail for a single revision round to converge.
3. The facts log is a shared message bus; routing is determined by the fact's classification, not by who records it.

These two principles — one per asymmetry — are the entire scope of v8.96.

---

## 4. Non-goals

- **NO new dedicated pre-check tool surface.** `zerops_workflow action=complete …` already returns check results on failure. v8.96 extends that response; it does not add a second code path.
- **NO dry-run mode.** Current behavior already allows iteration (a failing `complete` does NOT advance state). Adding a `dry_run: true` flag would be redundant with "call complete, observe fail, fix, call again". The improvement is in the SHAPE of the fail response, not a new action.
- **NO removal of prose guidance.** Prose in `recipe.md` topic registry stays; it gains a short "the gate evaluates this via check `<name>`" cross-reference so readers can see the coupling.
- **NO per-check file splits.** Extend the existing `StepCheck` struct; the fields are optional so legacy checks keep working during migration.
- **NO new fact Type values.** The existing `RecordFactInput.Type` taxonomy is sufficient; v8.96 adds a `Scope` field orthogonal to `Type` (content vs downstream-delegation vs both).

---

## 5. Change A — structured, self-describing check failures

### 5.1 Extend `StepCheck` with optional diagnostic fields

`internal/workflow/bootstrap_checks.go` currently defines:

```go
type StepCheck struct {
    Name   string `json:"name"`
    Status string `json:"status"` // pass, fail, skip
    Detail string `json:"detail,omitempty"`
}
```

Extend to:

```go
type StepCheck struct {
    Name   string `json:"name"`
    Status string `json:"status"` // pass, fail, skip
    Detail string `json:"detail,omitempty"`

    // v8.96 — structured failure diagnostics. All optional; legacy checks
    // emit only Name/Status/Detail. Checks that surface in convergence loops
    // SHOULD populate these per the migration table in §5.3.

    // ReadSurface describes what the check actually read. Names a file path
    // and, when relevant, the byte range or fragment marker that bounded the
    // read. A one-line human-readable description.
    // Example: "embedded YAML in apidev/README.md lines 20-240 (fragment #integration-guide)"
    ReadSurface string `json:"readSurface,omitempty"`

    // Required is the threshold or shape required to pass. Typed loosely
    // (string) so each check can emit what makes sense: a ratio threshold,
    // a count floor, an expected pattern name, an enum value.
    // Example: "≥30% of lines comment-only" / "≥3 gotcha bullets" / "NATS_PASSWORD matching yaml declaration"
    Required string `json:"required,omitempty"`

    // Actual is the observed value the check computed. Same loose typing.
    // Example: "14%" / "2 gotchas found" / "NATS_PASS (mismatch)"
    Actual string `json:"actual,omitempty"`

    // CoupledWith lists file paths whose state is implicitly bound to the
    // ReadSurface. An author edit to CoupledWith[i] may invalidate the
    // check's pass state unless ReadSurface is resynced.
    // Example for comment_ratio on apidev/README.md: ["apidev/zerops.yaml"]
    CoupledWith []string `json:"coupledWith,omitempty"`

    // HowToFix is a concrete 1-3-sentence remedy, imperative mood. NO
    // prose-hedging (NO "consider"/"you might"/"review"/"try"). When the
    // fix requires editing CoupledWith files in sequence, state the
    // coupling explicitly. First sentence starts with a verb.
    // Example (single-surface): "Add `#` comment lines to the YAML block
    //   inside apidev/README.md's #ZEROPS_EXTRACT_START:integration-guide
    //   fragment until density reaches 30%."
    // Example (coupled-surface): "Boost comment density in the YAML block
    //   inside apidev/README.md's integration-guide fragment. If the YAML
    //   mirrors apidev/zerops.yaml (IG step 1), edit both files so they
    //   stay byte-identical."
    HowToFix string `json:"howToFix,omitempty"`

    // Probe is a shell command the author can run (as-is, no substitution)
    // to re-evaluate this check on their current workspace state, returning
    // exit 0 iff pass. Optional — not every check has a trivially-probeable
    // form — but checks that operate on files SHOULD provide one.
    // Example: "zcp check comment-ratio --file /var/www/apidev/README.md --floor 0.30"
    Probe string `json:"probe,omitempty"`
}
```

Six new fields, all optional. Legacy checks compile and run unchanged.

### 5.2 Aggregate `StepCheckResult` gains a summary guidance field

```go
type StepCheckResult struct {
    Passed  bool        `json:"passed"`
    Checks  []StepCheck `json:"checks"`
    Summary string      `json:"summary"`

    // v8.96 — when Passed=false and all failing checks populate HowToFix,
    // NextRoundPrediction is a one-line estimate of convergence likelihood:
    // "single-round-fix-expected" / "multi-round-likely" / "coupled-surfaces-require-sequencing".
    // Derived from the structured fields of failing checks, not hand-written.
    NextRoundPrediction string `json:"nextRoundPrediction,omitempty"`
}
```

The prediction is computed, not hardcoded. Heuristic (in one helper function, <30 lines):

- All fails have HowToFix populated and no CoupledWith entries → `single-round-fix-expected`
- Any fail has CoupledWith entries → `coupled-surfaces-require-sequencing` plus a one-line describing the sequencing rule
- Any fail has HowToFix empty → `multi-round-likely` (instrument gap — the check didn't populate the fields and the author will have to infer)

This is telemetry-grade: post-hoc analysis of logs can correlate `NextRoundPrediction` with actual round counts to validate the heuristic.

### 5.3 Staged migration of existing checks

The top convergence offenders (from v31) migrate first. Others migrate opportunistically when edited.

**IMPORTANT — bare check names stay.** Simulation found that [workflow_checks_recipe.go:598-608](../internal/tools/workflow_checks_recipe.go#L598) emits `"comment_ratio"` with no host prefix — `checkReadmeFragments` is called per-host but without a hostname parameter. Renaming to `{host}_comment_ratio` would require a helper-signature change with ripple through tests. That ripple is **not required**: when `ReadSurface` is populated (e.g., `"embedded YAML in apidev/README.md #ZEROPS_EXTRACT_START:integration-guide"`), the author disambiguates by ReadSurface, not by Name. Check names in the table below use bare or currently-emitted forms; the column describes what the check is conceptually, not a renaming target.

| Check (current Name emission) | Package | Priority | ReadSurface template | CoupledWith | Notes |
|---|---|---|---|---|---|
| `comment_ratio` (bare; fires per-host) | `workflow_checks_recipe.go` | **P0** | `"embedded YAML in {host}/README.md #ZEROPS_EXTRACT_START:integration-guide fragment"` | `[{host}/zerops.yaml]` | the v31 two-round offender; host inferred from mount path in `checkCodebaseReadme` |
| `{env}_import_comment_ratio` | `workflow_checks_finalize.go` | **P0** | `"environments/{envFolder}/import.yaml"` | `[]` | env-finalize first-round offender; prefix already host/env-scoped |
| `{env}_import_comment_depth` | `workflow_checks_comment_depth.go` | **P0** | same yaml, WHY-marker lines | `[]` | env-finalize first-round offender |
| `{env}_import_cross_env_refs` | `workflow_checks_finalize.go` | **P0** | same yaml, line numbers of matches | `[]` | ReadSurface names which lines triggered the match |
| `{env}_import_factual_claims` | `workflow_checks_factual_claims.go` | **P0** | same yaml, specific line | `[]` | **emits one StepCheck per mismatch**; each entry gets its own ReadSurface + HowToFix |
| `knowledge_base_authenticity` (bare; fires per-host) | `workflow_checks_recipe.go` | **P1** | `"{host}/README.md #knowledge-base fragment"` | `[]` | count-vs-floor |
| `gotcha_distinct_from_guide` (bare; fires per-host) | `workflow_checks_recipe.go` | **P1** | both `#knowledge-base` + `#integration-guide` fragments | `[]` | HowToFix names which gotcha restates which IG item |
| `fragment_intro_blank_after_marker` (bare; fires per-host) | `workflow_checks_recipe.go` | **P1** | `"{host}/README.md after intro marker"` | `[]` | trivial structural fix |
| `claude_readme_consistency` (fires per-host) | `workflow_checks_claude_md.go` | P2 | README ↔ CLAUDE.md shared-invariant set | `[{host}/CLAUDE.md]` | |
| `scaffold_artifact_leak` (fires per-host) | `workflow_checks_scaffold_artifact.go` | P2 | file tree under `{host}/` | `[]` | ReadSurface names the offending files |
| `cross_readme_gotcha_uniqueness` | `workflow_checks_dedup.go` | P2 | all 3 README fragments | `[{other}/README.md]` | CoupledWith drives sequencing |

P0 checks migrate in v8.96's first phase. P1 follow in phase 2 of the same release. P2 migrate opportunistically when the file is next edited.

**Migration pattern (per check)**:

```go
// BEFORE:
return &StepCheck{
    Name:   "apidev_comment_ratio",
    Status: "fail",
    Detail: fmt.Sprintf("%.0f%% below 30%% floor", ratio*100),
}

// AFTER (minimal diff):
return &StepCheck{
    Name:        "apidev_comment_ratio",
    Status:      "fail",
    Detail:      fmt.Sprintf("%.0f%% below 30%% floor", ratio*100),
    ReadSurface: "embedded YAML in apidev/README.md lines " + fragmentLineRange + " (#ZEROPS_EXTRACT_START:integration-guide fragment)",
    Required:    "≥30% of YAML lines comment-only",
    Actual:      fmt.Sprintf("%.0f%%", ratio*100),
    CoupledWith: []string{"apidev/zerops.yaml"},
    HowToFix:    "after editing apidev/zerops.yaml, re-copy its content into apidev/README.md's integration-guide YAML block",
    Probe:       "", // omitted for now; v8.97 can ship a zcp-side probe CLI
}
```

The migration is mechanical per-check. No check-specific logic changes.

### 5.4 Test strategy

- **RED first**: add a new test file `workflow_checks_diagnostics_test.go` that asserts every P0 check populates `ReadSurface`, `Required`, `Actual`, and `HowToFix` when failing. Initially all assertions fail.
- Migrate one P0 check. Verify that check's test passes; others still fail.
- Iterate through P0 + P1.
- Add a separate test `TestNextRoundPrediction` with three-fixture cases (one all-HowToFix, one with CoupledWith, one with missing HowToFix).

**Quality assertions on HowToFix** (enforce in test helper, applied across every P0/P1 migrated check):

- `len(HowToFix) >= 50` — rejects stubs like "fix the file".
- `len(HowToFix) <= 600` — rejects multi-paragraph walls of text (max ~3 sentences).
- First rune of the trimmed string is an uppercase letter (rough imperative-mood proxy).
- `HowToFix` does NOT contain any of `["consider", "you might", "you may want to", "review the", "could", "should probably"]` (case-insensitive substring). These hedging patterns signal the author didn't commit to a concrete remedy.
- When `CoupledWith` is non-empty, `HowToFix` must mention at least one path from `CoupledWith` by basename. Enforces that couplings aren't silently dropped.

Violating checks fail the test loudly, naming the check name and the specific assertion.

### 5.5 Tool-side surfacing

`zerops_workflow action=complete` handler already returns the `StepCheckResult`. No handler-side change required — the new fields flow through JSON serialization automatically.

Recipe workflow guidance (`recipe.md`) gets a short paragraph in the deploy and finalize step preambles:

```
When a complete call returns Passed=false, every failing check now includes ReadSurface
(what file the check actually read), CoupledWith (files you must keep in sync), and
HowToFix (a one-sentence concrete remedy). Read these fields, not the Detail prose, to
diagnose what to change. If CoupledWith is non-empty on any failing check, sequence your
edits so the coupled files stay in sync within a single round.
```

That's it for Change A.

---

## 6. Change B — facts log as cross-subagent message bus

### 6.1 Add `Scope` field to `RecordFactInput`

Current (`internal/tools/record_fact.go`):

```go
type RecordFactInput struct {
    Type        string `json:"type" ...`
    Title       string `json:"title" ...`
    Substep     string `json:"substep,omitempty" ...`
    Codebase    string `json:"codebase,omitempty" ...`
    Mechanism   string `json:"mechanism,omitempty" ...`
    FailureMode string `json:"failureMode,omitempty" ...`
    FixApplied  string `json:"fixApplied,omitempty" ...`
    Evidence    string `json:"evidence,omitempty" ...`
}
```

Add one field:

```go
    // Scope controls who reads this fact. Omitting defaults to "content"
    // (pre-v8.96 behavior — only the readmes writer subagent consumes it).
    //
    //   content      — only the readmes writer subagent reads this fact.
    //                  Default for platform-invariants, gotcha-candidates, IG-candidates.
    //
    //   downstream   — downstream subagents (feature, code-review, follow-up
    //                  scaffolds) receive this fact in their dispatch brief
    //                  under a "Prior discoveries" section. The writer does
    //                  NOT read it.
    //                  Use for framework-API-quirks, tooling-version-mismatches,
    //                  scratch knowledge that would otherwise be re-discovered.
    //
    //   both         — visible to both the writer and downstream subagents.
    //                  Use sparingly; most facts belong in exactly one lane.
    Scope string `json:"scope,omitempty" jsonschema:"One of: content, downstream, both. Defaults to 'content' (writer-only, the pre-v8.96 behavior). Set 'downstream' for framework/tooling discoveries that don't belong in published content but would waste downstream subagents' time if re-investigated."`
```

`ops.FactRecord` gains the same field. `ops.AppendFact` serializes it AND validates against a `knownScopes` enum (fresh-sim trap 10):

```go
var knownScopes = map[string]bool{
    "":           true, // unset → legacy content-only behavior
    "content":    true,
    "downstream": true,
    "both":       true,
}
// In AppendFact, before the Timestamp defaulting:
if !knownScopes[rec.Scope] {
    return fmt.Errorf("fact record: unknown scope %q (valid: content, downstream, both)", rec.Scope)
}
```

Prevents a typo (e.g. `"downsteam"`) from silently defaulting to content-scope — the writer would read it while downstream subagents skip it, a silent-miss bug class.

### 6.2 Injection at topic-resolution time (not dispatch)

**Correction from simulation**: zcp does NOT see Agent tool dispatches. Briefs flow to the main agent as topic-block strings inside `zerops_workflow` response payloads; main agent decides what to include in the Agent tool prompt. Injection must happen when zcp assembles the topic-block response, not at a hypothetical "dispatch handler."

The correct hook is [recipe_guidance.go:472 `buildSubStepGuide`](../internal/workflow/recipe_guidance.go#L472):

```go
func (r *RecipeState) buildSubStepGuide(step, subStep string) string {
    topicID := subStepToTopic(step, subStep, r.Plan)
    if topicID == "" {
        return ""
    }
    resolved, err := ResolveTopic(topicID, r.Plan)
    if err != nil || resolved == "" {
        return ""
    }
    return resolved
}
```

Extended:

```go
func (r *RecipeState) buildSubStepGuide(step, subStep, sessionID string) string {
    topicID := subStepToTopic(step, subStep, r.Plan)
    if topicID == "" {
        return ""
    }
    resolved, err := ResolveTopic(topicID, r.Plan)
    if err != nil || resolved == "" {
        return ""
    }
    if TopicIncludesPriorDiscoveries(topicID) {
        if block := BuildPriorDiscoveriesBlock(sessionID, subStep); block != "" {
            resolved = block + "\n\n---\n\n" + resolved
        }
    }
    return resolved
}
```

**Signature ripple**: `sessionID string` param added to three methods. The change is purely additive; no existing caller breaks.

1. [recipe_guidance.go:14 `buildGuide(step string, iteration int, kp knowledge.Provider) string`](../internal/workflow/recipe_guidance.go#L14) → add `sessionID string` param.
2. `buildSubStepGuide(step, subStep string)` → add `sessionID string` param.
3. [recipe.go:377 `BuildResponse(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider)`](../internal/workflow/recipe.go#L377) → pass its existing `sessionID` through to `buildGuide`.

Do NOT add `SessionID` to `RecipeState`. That would create two sources of truth (WorkflowState.SessionID vs RecipeState.SessionID) and conflict with the state-restore invariant.

### 6.3 Topic-registry opt-in + writer filter + brief-size cap

Extend `GuidanceTopic` (in `recipe_topic_registry.go`; the struct is named `GuidanceTopic`, NOT `RecipeTopic` — fresh-sim correction) with one field:

```go
type GuidanceTopic struct {
    // ... existing fields ...

    // IncludePriorDiscoveries — when true, buildSubStepGuide prepends a
    // "Prior discoveries" block (downstream-scoped facts from upstream
    // subagents) to the resolved content. Use for delegation briefs whose
    // subagent would otherwise re-investigate framework/tooling surfaces
    // an upstream subagent already characterized.
    IncludePriorDiscoveries bool
}
```

Expose a public accessor `TopicIncludesPriorDiscoveries(topicID string) bool` so `buildSubStepGuide` can query without leaking registry internals.

**Opt-in topic set**:

| Topic ID | IncludePriorDiscoveries | Rationale |
|---|:-:|---|
| `scaffold-subagent-brief` | false | scaffolds run first; no upstream facts exist |
| `subagent-brief` (feature) | **true** | benefits from scaffold-recorded framework quirks |
| `readme-fragments` / `content-authoring-brief` (writer) | false | writer reads facts log directly via v8.95 manifest contract |
| `code-review-agent` | **true** | benefits from feature/scaffold tooling observations |

**Writer-side filter** (fixes Trap 5): [workflow_checks_content_manifest.go:194 `checkManifestCompleteness`](../internal/tools/workflow_checks_content_manifest.go#L194) currently asserts every fact in `ops.ReadFacts(factsLogPath)` has a manifest entry. Add a filter before the assertion:

```go
facts = filterContentScoped(facts) // drop Scope=="downstream"; keep content, both, unset
```

Two-line change + one new 5-line helper. Writer brief (in `recipe.md` `content-authoring-brief` topic) gains a sentence:

> The facts log may contain entries with `scope: "downstream"` — those are scratch
> knowledge for other subagents, not content. Skip them when classifying; your
> `ZCP_CONTENT_MANIFEST.json` must not list them.

**8-entry cap on the prior-discoveries block** (fixes Trap 6): `BuildPriorDiscoveriesBlock` sorts eligible facts by recency (newest first), takes the first 8, and if any were elided appends:

```
_... and N more earlier discoveries (see /tmp/zcp-facts-{sessionID}.jsonl)_
```

Prevents pathological bloat if a subagent over-records.

### 6.4 `BuildPriorDiscoveriesBlock` — full signature

New file `internal/workflow/recipe_brief_facts.go`:

```go
package workflow

// BuildPriorDiscoveriesBlock reads the session's facts log and returns a
// markdown block of downstream-scoped facts recorded upstream of the given
// substep. Returns empty string if the log is missing, malformed, or has
// no eligible entries (all three cases silently produce the pre-v8.96
// behavior — subagents run without the block).
//
// Capped at 8 entries, sorted newest first. Excess elided with a footer
// line pointing at the log path.
func BuildPriorDiscoveriesBlock(sessionID, currentSubstep string) string {
    // 1. ops.FactLogPath(sessionID) → resolve path
    // 2. ops.ReadFacts(path) — tolerate ENOENT / parse errors silently
    // 3. filter: rec.Scope in {"downstream", "both"}
    //    AND substepIsUpstream(rec.Substep, currentSubstep)
    // 4. sort by timestamp desc (assumes ops.FactRecord gains Timestamp — see §6.5)
    // 5. truncate to 8 + capture elision count
    // 6. render:
    //    - heading "## Prior discoveries (recorded earlier this session)"
    //    - preamble: "These facts were surfaced by upstream subagents.
    //      They do NOT belong in published content but save investigation time."
    //    - one bullet per fact: "- **{Title}** (_{Mechanism}_) — {FailureMode}; {FixApplied}"
    //    - if elided: footer with count + log path
}

// substepIsUpstream returns true iff candidate appears strictly earlier than
// current in the deploy-phase substep sequence. Facts recorded at or after
// current are excluded (they're not "prior" to the dispatch).
func substepIsUpstream(candidate, current string) bool { /* ... */ }
```

### 6.5 FactRecord timestamp

For recency sorting, `ops.FactRecord` needs a `Timestamp` field. If it already has one, skip this. If not, add `Timestamp string` (RFC3339 UTC), populate in `AppendFact`. Small additive change.

### 6.6 Brief-construction call sites (revised)

| Dispatch | Current substep | Topic ID returned by subStepToTopic | v8.96 change |
|---|---|---|---|
| scaffold (3× parallel) | generate.scaffold | `scaffold-subagent-brief` | none — `IncludePriorDiscoveries=false` |
| feature (single) | deploy.subagent | `subagent-brief` | `IncludePriorDiscoveries=true` → block prepended at topic resolution |
| readmes writer | deploy.readmes | `content-authoring-brief` | none — `IncludePriorDiscoveries=false`; writer reads log directly |
| code-review | close.code-review | `code-review-agent` | `IncludePriorDiscoveries=true` → block prepended at topic resolution |

The change is at topic-resolution time in `buildSubStepGuide`, not at a handler edge. When the main agent calls `zerops_workflow action=complete substep=init-commands`, the response's `detailedGuide` for the next substep (`subagent`) is assembled via `buildSubStepGuide(step, subStep, sessionID)`, which sees `topicID="subagent-brief"`, checks the flag, and prepends the block.

### 6.3 Recording-side guidance

`recipe.md`'s deploy-step `where-commands-run` topic already mandates `zerops_record_fact` during deploy. v8.96 extends the example list in that topic to cover the new Scope field:

```
Scope="content" (the default) — platform invariants, gotcha candidates, IG-item candidates.
Scope="downstream"            — framework-API quirks the NEXT subagent would otherwise re-investigate.
                                Examples: "Meilisearch v0.57 renamed class from MeiliSearch to Meilisearch",
                                "cache-manager v6 returns absolute-epoch TTLs, not relative durations",
                                "svelte-check@4 is not compatible with typescript@6 — $state shows 'untyped' errors
                                 but runtime build is unaffected".

When in doubt, default to "content". A fact that turns out to be useless downstream costs
nothing; a fact that SHOULD have been recorded as downstream but wasn't costs another subagent
~20s of re-archaeology.
```

### 6.4 Test strategy

- **RED first**: new test `recipe_brief_facts_test.go` with three fixture cases:
  1. Empty facts log → empty string.
  2. Facts log with 2 content-scoped and 1 downstream-scoped entry → block contains only the 1 downstream entry.
  3. Facts log with downstream entries whose Substep is DOWNSTREAM of current → block excludes them (forward-in-time leaks).
- GREEN: implement `BuildPriorDiscoveriesBlock`.
- Integration test: run a fake feature-dispatch with a seeded facts log, assert the final brief contains the expected block.

---

## 7. Call-site changes summary

| File | Change | Size |
|---|---|---|
| `internal/workflow/bootstrap_checks.go` | Extend `StepCheck` + `StepCheckResult`, add `NextRoundPrediction` helper | +60 lines |
| `internal/tools/workflow_checks_deploy.go` | Migrate `{host}_comment_ratio` | ~10 lines per check × 3 hosts |
| `internal/tools/workflow_checks_finalize.go` | Migrate `{env}_import_*` checks | ~10 lines per check × 4 rules × 6 envs — extract a helper |
| `internal/tools/workflow_checks_comment_depth.go` | Migrate depth check | ~15 lines |
| `internal/tools/workflow_checks_factual_claims.go` | Migrate factual_claims | ~20 lines |
| `internal/tools/workflow_checks_recipe.go` | Migrate P1 README checks | ~10 lines per check × 3-4 checks |
| `internal/tools/record_fact.go` | Add `Scope` field + plumb to FactRecord | +5 lines |
| `internal/ops/facts.go` | Add `Scope` to FactRecord struct + serialization | +3 lines |
| `internal/workflow/recipe_brief_facts.go` | NEW — `BuildPriorDiscoveriesBlock` | ~80 lines |
| `internal/tools/workflow.go` (or wherever subagent dispatches compose) | Prepend prior-discoveries block for feature + code-review dispatches | ~10 lines |
| `internal/content/workflows/recipe.md` | Guidance paragraph in deploy + finalize step preambles + Scope example block | ~30 lines |

Total: ~250-300 lines of production code + matching tests. Within one release window.

---

## 8. Implementation sequence (RED → GREEN phases)

### Phase 1 — structural scaffolding (RED)

1. Extend `StepCheck` + `StepCheckResult` in `bootstrap_checks.go`.
2. Write `TestNextRoundPrediction` with three fixture cases (all fail).
3. Add `Scope` to `RecordFactInput` + `FactRecord`.
4. Write `recipe_brief_facts_test.go` with three fixture cases (all fail).
5. Write `workflow_checks_diagnostics_test.go` asserting P0 checks populate structured fields (all fail).

Commit as single RED commit: "test: v8.96 diagnostic fields + facts routing (RED)".

### Phase 2 — GREEN for the new surface

6. Implement `NextRoundPrediction` helper. First test passes.
7. Implement `BuildPriorDiscoveriesBlock`. Second test passes.
8. Plumb `Scope` through `record_fact.go` → `ops/facts.go`. Manual integration check.

Commit: "feat: v8.96 structured check diagnostics + facts log Scope field".

### Phase 3 — migrate P0 checks

9. Migrate `{host}_comment_ratio` (3 instances). Diagnostics test for that check passes.
10. Migrate `{env}_import_comment_ratio` + `comment_depth` (extract shared helper — 6 envs × 2 rules = 12 call sites).
11. Migrate `cross_env_refs`.
12. Migrate `factual_claims`.

Commit per migrated check (or one commit per conceptually-grouped set): "feat: v8.96 P0 migration — comment_ratio structured diagnostics" etc.

### Phase 4 — migrate P1 checks

13. Migrate README knowledge-base + gotcha-distinct-from-guide + fragment-intro-blank. Per-check commits.

### Phase 5 — wire injection at topic-resolution

14. Thread `sessionID string` through `BuildResponse → buildGuide → buildSubStepGuide`. Three signatures; purely additive. Run `go build ./...` + `go test ./internal/workflow/...` to confirm no caller broke.
15. Add `IncludePriorDiscoveries bool` to `GuidanceTopic`. Set `true` on `subagent-brief` and `code-review-agent`. Implement `TopicIncludesPriorDiscoveries(topicID)` accessor.
16. Extend `buildSubStepGuide` to call the accessor and prepend `BuildPriorDiscoveriesBlock` output when set. Covered by the topic-level test in phase 1.
17. Add writer-side filter in `checkManifestCompleteness` (5 lines). Run the existing content-manifest tests — they should keep passing since no test fixture uses `Scope=downstream` yet.
18. Integration test: seed a facts log with mixed Scope entries, call `buildSubStepGuide` with a `subagent-brief` substep, assert the returned string contains the prior-discoveries heading and the downstream-scoped facts but not the content-scoped ones.

### Phase 6 — documentation

17. Update `recipe.md` deploy + finalize step preambles with the "read the structured fields" paragraph.
18. Update `recipe.md` `where-commands-run` topic with the Scope examples.
19. Update `spec-recipe-quality-process.md` noting the new structured-diagnostics contract.

Final commit: "docs: v8.96 — structured check diagnostics + facts log routing".

`make lint-local` + `go test ./... -count=1` green at every phase boundary.

---

## 9. v32 calibration bar — what must be true to call v8.96 a success

After v31 → v32 under v8.96 changes, the following must hold:

1. **Deploy-complete README checks converge in ≤1 fail round.** v31 had 3 rounds (2 real fail rounds + 1 pass). v32 target: 1 fail round (discovery) + 1 pass. If ≥2 fail rounds, the structured-diagnostics migration is incomplete or the HowToFix guidance is too abstract — investigate the specific check that bounced twice.
2. **Finalize-complete converges in ≤1 fail round.** v31 had 3 rounds. Same reasoning.
3. **Zero duplicate framework-archaeology across subagents.** v32 should show feature subagent spending ≤5s on framework discovery that scaffold already recorded (vs v31's ~15s). Measure: grep for the feature subagent investigating API shapes that match an earlier scaffold fact's Mechanism field.
4. **≥50% of P0 check failures emit non-empty `ReadSurface` + `Required` + `Actual` + `HowToFix`.** If not all P0 checks are migrated before v32 runs, the lower bound matters.
5. **`NextRoundPrediction` correlates with actual round count.** On failures labeled `single-round-fix-expected`, v32 should converge in 1 fail round ≥80% of the time. On `coupled-surfaces-require-sequencing`, ≤2 fail rounds.
6. **`Scope="downstream"` facts flow end-to-end.** At least one v32 subagent brief should contain a non-empty "Prior discoveries" block, and telemetry should confirm the receiving subagent did NOT re-investigate the recorded fact.
7. **At least 2 facts recorded with `Scope="downstream"`** during scaffold and/or feature subagents. If 0, the mechanism is UNTESTED regardless of other wins — the writer-brief example block and the `where-commands-run` guidance didn't take, and v8.96 Theme B's real adoption is unverified. Record this separately in the v32 post-mortem.

All seven bars must hold. If any fails, the v32 post-mortem writes up the failure mode and v8.97 addresses it.

---

## 10. Rollback criteria

If v32 surfaces evidence that Change A or Change B regressed something, roll back the specific change — NOT the other.

**Roll back Change A (structured diagnostics)** if:

- v32 wall clock increases by >10% vs v31 on the same feature scope (unlikely — the fields are diagnostic-only, don't affect execution).
- Any P0 check's structured fields are **wrong** in a way that leads the author to the wrong fix (e.g., CoupledWith points at the wrong file). Diagnose per-check; if one check is wrong, correct that check rather than roll back the mechanism.
- The `NextRoundPrediction` heuristic consistently emits `single-round-fix-expected` for failures that actually take 3+ rounds. This indicates the HowToFix guidance is insufficient for some class — tighten the heuristic's signal set, not roll back.

**Roll back Change B (facts scope)** if:

- Recorded-as-downstream facts pollute subagent briefs with irrelevant noise that confuses or misleads the subagent.
- Facts-log file size grows unbounded across many sessions (should be bounded by session — one file per session — so this is a non-issue unless the file-naming regresses).
- Downstream subagents START trusting "Prior discoveries" as authoritative when it's actually scratch — i.e., the subagent SHIPS a scratch fact in published content. Tighten the brief wording; don't roll back.

**Partial rollback**: If only the `NextRoundPrediction` heuristic is wrong, remove JUST that field (keep the structured diagnostic fields). If only one P0 check's migration is wrong, revert JUST that migration commit.

Full rollback of either change is unlikely. Both are additive — the old code path (prose-only Detail, single-lane facts log) still works for any check or caller that doesn't adopt the new fields.

---

## 11. What v8.97+ is watching for

Explicit non-scope now, candidates for next release:

- **Probe-tool surface**: a zcp-side CLI `zcp check <name> --target <file>` that author subagents could invoke inline. Only justified if v32 evidence shows that even with structured-diagnostics responses, authors STILL burn rounds because they can't easily re-evaluate between write and attest. If so, `Probe` field (already defined, initially empty) gets populated and a thin CLI wraps the check invocations.
- **P2 check migrations**: claude_readme_consistency, scaffold_artifact_leak, cross_readme_gotcha_uniqueness. Not in v8.96 scope unless v32 surfaces one of them in a convergence loop.
- **Fact-expiry / session-pruning**: not a problem today (one log per session), may become one if the log grows across many subagents. Measure at v32.
- **Downstream-fact selective delivery**: today BuildPriorDiscoveriesBlock delivers ALL downstream-scoped facts from upstream. If briefs bloat, add a `target` dimension to Scope (e.g., `Scope="downstream:feature"` vs `Scope="downstream:all"`). Defer.
- **Tooling-ergonomic polish**: `zerops_knowledge` error-message hardening, `dev_server stop` post-condition documentation, scaffold "Read-before-Write" rule. Each individually <30s of v31 wall cost; bundle into a v8.98 ergonomic release if they persist through v32-v34.

---

## 12. One-paragraph mental model for the implementer

v31 proved v8.95's structural fixes work. The remaining friction is **author-vs-gate asymmetry**: the gate knows what it reads, what it couples, and how to make it pass; the author only knows what to write. v8.96 closes that gap in two ways — (A) every check's failure response carries the gate's own knowledge (ReadSurface, CoupledWith, HowToFix), so the author converges in one revision round instead of three; (B) the facts log becomes a cross-subagent message bus so downstream subagents inherit upstream discoveries instead of re-investigating. Both changes are additive extensions of existing machinery — one field on `StepCheck`, one field on `RecordFactInput`, one helper to compose briefs. No new tool surfaces, no new workflow actions, no hardcoded check-specific logic. The migration is staged: P0 checks first, P1 next, P2 as-edited. v32 is the calibration; v8.97 picks up what v32 surfaces.
