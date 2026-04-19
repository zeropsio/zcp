# v8.97 — Masterclass (the A-grade plan)

**Intended reader**: a fresh Opus 4.7 instance. This doc is self-contained.

**Goal**: v33 lands a clean A-grade run. Not A−, not mixed A/B dimensions — A on all four axes, within 90 min, complete deliverable.

## Shipped (v8.97)

Fixes 1, 2, 4, 5 and Fix 3 Parts A+B landed. Fix 3 Part C (server-side
sentinel verifier + `extractRecentAgentDispatch`) was dropped: MCP STDIO
is stateless per CLAUDE.md convention, and a session-keyed bounded buffer
of recent Agent dispatches introduces global state with race/eviction/
lifetime questions that Parts A+B already avoid. If drops persist through
v33, Part C will be specified with a concrete buffer design then.

Fix 6 (orchestrator self-gating prompt) is out of scope for this
repository — Fix 1's server-side refusal is authoritative. The
orchestrator prompt MAY self-gate on `action=status` but is not
required for the failure class Fix 1 closes.

Test layout deltas from the plan:
- Fix 4's historical-cascade fixtures renamed to
  `_IllustrativeApidev` / `_IllustrativeEnv4` (not named "regression" —
  they're consequences of `ThreeChecksOneSurface`, not per-cluster guards).
- Fix 4 adds `TestAllReadSurfacesAreStable` (user feedback #4) —
  enforcement against run-varying tokens in surface strings.
- Fix 5 structural invariant: principle bodies are backtick-free
  (`TestScaffoldBrief_PrincipleBodiesAreBacktickFree`), replacing the
  hardcoded deny-list of idiom substrings in the original plan.
- `PostCompletionGuidance` inlined as `PostCompletionSummary string` +
  `PostCompletionNextSteps []string` fields on `RecipeResponse` — no
  typed struct for a single NextSteps entry.


**Prerequisite reads (in order)**:
1. [docs/recipe-version-log.md](recipe-version-log.md) §v32 entry — the last run. Focus on the "4 stacked failures" in the post-v32 analysis.
2. [docs/implementation-v8.96-author-side-convergence.md](implementation-v8.96-author-side-convergence.md) §§5-6 — Theme A + Theme B. v8.97 does NOT revisit their architecture; it closes the gaps the shipped implementation left open.
3. [internal/sync/export.go](../internal/sync/export.go), [internal/workflow/session.go](../internal/workflow/session.go), [internal/tools/workflow_checks_recipe.go](../internal/tools/workflow_checks_recipe.go), [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md).
4. [CLAUDE.md](../CLAUDE.md). Especially: "no fallbacks", "fix at the source, not downstream", "RED before GREEN", "max 350 lines per .go file".

**Target ship window**: single release. Five code-level fixes + one orchestrator-prompt update. Zero new checks. Zero existing-check semantic changes.

---

## 1. Why 13 consecutive runs have missed A

Since v20 (the only A− ever recorded), every release has added machinery that passed its own tests and then failed a different way in the next run:

| Cluster | Recurred in | Root cause |
|---|---|---|
| Close-step framing drift | v18, v19, v32 | `recipe.md` close heading bundles "verify" + "publish"; constraint text over-generalizes |
| Export ran before close attestation | v29, v30, v32 | `zcp sync recipe export` has no view of workflow state |
| Multi-round check cascades | v22-v25, v31, v32 | Check failures don't name their coupled siblings; agent fixes one, regresses the other |
| Rules lost in subagent dispatch | v21, v22, v32 | Main agent compresses subagent brief when constructing `Agent()` prompt; load-bearing rules get dropped |
| Missing mandatory runtime handlers | v30, v31, v32 | Scaffold-subagent-brief doesn't enumerate the recurring handler checklist (SIGTERM, `enableShutdownHooks`, etc.) |

Each recurrence is a *failure of the brief-delivery system*, not a failure of the briefs themselves. Briefs in `recipe.md` are correct. Checks in `workflow_checks_*.go` are correct. What keeps breaking is the path from specification to actual agent behavior.

v8.97 closes that path at five load-bearing points.

---

## 2. The fix stack

Five fixes. Each one addresses a specific cluster above. Read all five before implementing any — they interact (Fixes 3+5 share the MANDATORY-block syntax).

### Fix 1 — Server-side export gate (catastrophic-blocker → closed permanently)

**What v32 proved**: `zcp sync recipe export` ran at 16:53:13 while the workflow's step=close was still `in_progress`. The published tree missed per-codebase READMEs + CLAUDE.md because the writer subagent had landed them at `/var/www/{apidev,appdev,workerdev}/README.md` on the container but export's overlay logic only copied files the close step would have staged. No server-side gate, no error, silent incomplete deliverable.

**Change**: `internal/sync/export.go` `ExportRecipe` reads the workflow session state and refuses if `step=close` is not `complete`.

Concrete steps:

1. Add to `internal/platform/errors.go`:
   ```go
   const ErrExportBlocked = "EXPORT_BLOCKED"
   ```

2. Add to `ExportOpts` struct in `internal/sync/export.go`:
   ```go
   SessionStateDir string // path to workflow state dir (defaults to $ZCP_STATE_DIR or ~/.zcp/state)
   SessionID       string // session ID from env var $ZCP_SESSION_ID or --session flag
   SkipCloseGate   bool   // ONLY for explicit --force-export — prints warning
   ```

3. Before the existing tar-writer body of `ExportRecipe`, add:
   ```go
   if !opts.SkipCloseGate {
       sessionID, sourceLabel := resolveSessionID(opts.SessionID) // "--session" > "$ZCP_SESSION_ID" > ""
       if sessionID == "" {
           // Ad-hoc CLI export outside any orchestrated run: no session to gate against.
           fmt.Fprintln(os.Stderr, "note: no session context (--session unset, $ZCP_SESSION_ID unset); skipping close-step gate.")
       } else {
           state, err := loadRecipeSession(opts.SessionStateDir, sessionID)
           if err != nil {
               return nil, fmt.Errorf(
                   "%s: session %q declared (via %s) but state could not be loaded: %w. Verify the session ID is correct; if exporting outside an orchestrated run, unset both --session and $ZCP_SESSION_ID. Retry with --force-export to bypass (not recommended).",
                   ErrExportBlocked, sessionID, sourceLabel, err,
               )
           }
           if state.Steps[RecipeStepClose].Status != StepComplete {
               return nil, fmt.Errorf(
                   "%s: close step is %s. Dispatch the code-review subagent, run the close browser walk, then `zerops_workflow action=complete step=close` before exporting. Exporting without close produces an incomplete deliverable (per-codebase READMEs + CLAUDE.md not staged, no code-review signals).",
                   ErrExportBlocked, state.Steps[RecipeStepClose].Status,
               )
           }
       }
   }
   ```
   The three branches are individually diagnosable. The v32-era confusion — "is the gate failing because close is incomplete, or because the session file can't be found, or because there's no session at all?" — is eliminated by message-level distinction rather than error-code proliferation.

4. Helper `resolveSessionID(optExplicit string) (id, source string)` in `internal/sync/export.go` — under 20 lines. Precedence: explicit `--session` flag value → `$ZCP_SESSION_ID` env var → empty. Returns the source label (`"--session"` / `"$ZCP_SESSION_ID"`) so the error message above can tell the agent where the ID came from.

5. Load helper `loadRecipeSession(dir, id string)` in a new `internal/sync/session_load.go`. Keep it under 80 lines. Returns `*workflow.RecipeState` — that type is already exported from `internal/workflow/recipe.go`. If the workflow package exposes no loader, add a minimal one at `internal/workflow/session_load.go`.

6. Wire `--session-state-dir` and `--session` flags in `cmd/zcp/sync.go` export subcommand. `$ZCP_SESSION_ID` is set by the MCP server at workflow start — rely on that as the default.

**RED tests** (`internal/sync/export_test.go`):
- `TestExportRecipe_RefusesWhenCloseInProgress` — fixture session with `Steps[close].Status = in_progress`, `--session` set; assert returns `ErrExportBlocked` and message contains "close step is in_progress".
- `TestExportRecipe_AllowsWhenCloseComplete` — fixture with `Steps[close].Status = complete`, `--session` set; assert export proceeds and produces an archive.
- `TestExportRecipe_ForceExportBypassWarning` — `SkipCloseGate: true`, `--session` set with incomplete close; assert export proceeds AND stderr contains warning text.
- `TestExportRecipe_DeclaredSessionWithMissingStateReturnsBlocked` — `--session=nonexistent` set, no state file; assert `ErrExportBlocked` message names the session ID and the source (`via --session`), distinguishing the cause from "close not complete".
- `TestExportRecipe_NoSessionContextSkipsGate` — both `--session` unset AND `$ZCP_SESSION_ID` unset; assert export proceeds AND stderr contains `"no session context"` note. This is the ad-hoc CLI path; the gate must not trigger.
- `TestResolveSessionID_PrecedenceExplicitOverEnv` — both flag and env set; assert explicit wins and source label is `"--session"`.

**Expected v33 impact**: the failure class that skipped close in v32 cannot silently succeed. Three distinct diagnostics replace v32's ambiguous "cannot read session state" error: (a) no session → harmless note, export proceeds for ad-hoc CLI use; (b) declared session with missing state → actionable error that names the ID and its source; (c) state loaded + close incomplete → actionable error that names the current status. Export never ships an incomplete deliverable again.

### Fix 2 — Close is verify-only; publish leaves the workflow

**What v32 proved**: same 582-byte `detailedGuide` as v31, same model, opposite interpretations. v31's agent read "Do NOT publish without explicit user request" as gating publish only; v32's agent read it as gating the whole close step. The text admits both readings because the workflow mixes two concerns (autonomous verify + user-gated publish) into one step. The earlier v8.97 draft tried to resolve this with a "Group VERIFY vs Group PUBLISH" split inside close. That still asks the agent to distinguish the two at interpretation time. Cleaner fix: take publish out of the workflow.

**Change**: close contains only verify (`code-review` + `close-browser-walk`). Export still runs autonomously after close (Fix 1 gates it). Publish is no longer a workflow state — it's a post-workflow CLI operation, surfaced in the workflow's completion response as optional next-step guidance.

Concrete steps:

**Part A — `recipe.md` close section rewrite** (replace the existing close section, lines ~2820-2832):

```markdown
<section name="close">
## Close

Close has two sub-steps. Both are always autonomous — no user prompt gates either.

1. **code-review** — dispatch the code-review sub-agent, apply any fixes it recommends.
2. **close-browser-walk** — main agent walks the deployed dev + stage URLs in a browser, confirming the features render.

Run both every time. v32 asked the user "should I run the review?" and when no reply came, skipped close entirely — do not repeat. If you are tempted to ask the user for permission before either sub-step, you are misreading this section; re-read.

### Constraints

- Sub-step gate: `zerops_workflow action="complete" step="close"` requires both `substep="code-review"` AND `substep="close-browser-walk"` attestations. Missing either → server-side rejection.
- Browser walk is main agent only — never delegate to a sub-agent.
- Do NOT dispatch `zerops_browser` calls from the code-review sub-agent (proven to fork-exhaust the parent).
- After close completes, export runs automatically (Fix 1 gates export on close = complete).

Publishing (`zcp sync recipe publish <slug> <dir>`) is a separate CLI command the user runs manually when they are ready to open a PR on `zeropsio/recipes`. It is not part of this workflow, not a sub-step, and not something the agent should run unprompted. The workflow response after close completion includes publish instructions the agent can relay to the user.
</section>
```

The close section must contain the literal string `"always autonomous"` (calibration guard) and must NOT contain `"user-gated"` anywhere (the old framing).

Verify the new text doesn't break the existing `detailedGuide` extractor in `internal/workflow/recipe_guidance.go` — the extractor reads by section name, not by heading wording.

**Part B — `PostCompletionGuidance` on the close-completion response** (code):

1. Add to `Response` struct in `internal/workflow/recipe.go`:
   ```go
   type PostCompletionGuidance struct {
       Summary   string   `json:"summary"`   // one-sentence status for the agent to relay
       NextSteps []string `json:"nextSteps"` // optional follow-up actions; currently: publish CLI command
   }
   ```
   The `Response` returned by `handleComplete` for `step=close` populates this with:
   ```go
   Summary: "Recipe verified (code-review + close-browser-walk complete). Export archive written to <archivePath>.",
   NextSteps: []string{
       "To publish to zeropsio/recipes: run `zcp sync recipe publish <slug> <archive-dir>`. This opens a PR on the recipes repo; run only when you are ready to ship.",
   },
   ```
   Rendered by the agent as a final user-facing message. Populate `NextSteps` unconditionally — the agent decides whether to relay to the user; the workflow's job is to surface the option.

2. Delete every `publish` vocabulary reference from workflow state:
   - `internal/workflow/recipe.go` sub-step definitions for close must contain only `code-review` and `close-browser-walk`; no `publish` or `export` as sub-steps.
   - `internal/content/workflows/recipe.md`: grep for `publish` inside `<section name="close">`. All occurrences in the rewritten text point at the CLI command in prose only — no workflow semantics.
   - `internal/tools/workflow_checks_*.go`: no check may reference a publish state or sub-step.

3. Export remains autonomous (Fix 1 enforces the close=complete precondition). Nothing in Part B changes export behavior; it only removes publish from workflow state.

**RED tests**:

In `internal/content/workflows/recipe_close_framing_test.go` (new):
- `TestCloseSection_BothSubStepsAlwaysAutonomous` — assert close section contains literal `"always autonomous"`; assert it does NOT contain `"user-gated"`, `"Group VERIFY"`, or `"Group PUBLISH"` (old framing guard).
- `TestCloseSection_V32ForwardGuard` — assert `"v32 asked the user"` regression phrase is present.
- `TestCloseSection_NoPublishAsSubStep` — parse close section; assert the numbered sub-step list contains only `code-review` and `close-browser-walk`. Publish may appear in the trailing CLI-guidance paragraph only.
- `TestDetailedGuide_CloseStepUnambiguous` — invoke `detailedGuide` for close step; assert it contains `"always autonomous"` and does NOT contain `"user-gated"`.

In `internal/workflow/recipe_test.go` (extend):
- `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` — fixture close complete; assert `Response.PostCompletion.NextSteps[0]` contains the substring `"zcp sync recipe publish"`.
- `TestRecipeCloseSubSteps_ExactlyTwoAutonomousSubSteps` — enumerate close sub-steps from `recipe.go`; assert exactly `[code-review, close-browser-walk]`. No publish. No export. No additional sub-steps.

**Expected v33 impact**: the close step has a single semantic (verify). The agent cannot be confused about "should I publish?" because publish is not a workflow decision. Export runs autonomously when close completes (gated by Fix 1). Publish guidance surfaces in the completion response; the agent decides whether to relay it to the user but never acts on it without explicit instruction. The v32 close-skip failure class is closed at the root: there is no user-gated sub-step left to ambiguously interpret.

### Fix 3 — MANDATORY-block brief transmission (close the courier-loss class)

**What v32 proved**: `recipe.md` contains the Read-before-Edit rule at 5 subagent briefs (lines 803, 1586, 2080, 2250, 2858). Main agent dispatched 3 scaffold subagents with 6,323-char prompts — NONE of which contained the rule text. All 4 scaffold-layer "File has not been read yet" errors happened in subagents that never saw the rule. Same pattern: v21 lost "mount is not an execution surface" preamble; v22 lost NATS URL-embedded-creds forbid. Briefs keep growing; main keeps compressing; load-bearing rules keep disappearing.

**Change**: introduce `<<<MANDATORY>>>...<<<END MANDATORY>>>` sentinel syntax + server-side verification of transmission.

Three parts, implemented in order:

**Part A — Sentinel syntax in `recipe.md`** (content-only, no code):

Mark every load-bearing subagent-brief rule with the sentinel. Example at line 803 (scaffold-subagent-brief):

```
<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

**File-op sequencing** — every `Edit` must be preceded by a `Read` of the same file in this session. The Edit tool enforces this. Hitting "File has not been read yet" and reactively Read+retry is trace pollution. Plan up front: before the first Edit, batch-Read every file you intend to modify. For scaffolder-created files (nest new, npm create vite, etc.) Read each one once after the scaffolder returns and before any Edit.

**Tool-use policy** — permitted tools: Read, Edit, Write, Grep, Glob on the SSHFS mount; Bash ONLY as `ssh {hostname} "..."`. Forbidden: zerops_workflow, zerops_import, zerops_env, zerops_deploy, zerops_subdomain, zerops_mount, zerops_verify. Violating any of these corrupts workflow state.

**SSH-only executables** — NEVER `cd /var/www/{hostname} && <executable>` in Bash. ALWAYS `ssh {hostname} "cd /var/www && <executable>"`. Files via Write/Edit on the mount.

<<<END MANDATORY>>>
```

Apply the same wrapper to: feature-subagent-brief (line 1586 region), readme-writer-brief (line 2080+2250 regions), code-review-subagent-brief (line 2858 region). Each brief gets exactly ONE MANDATORY block containing its load-bearing rules — not multiple blocks, not inline sentinels within prose.

**Part B — Dispatch-construction rule** (add to `recipe.md` section where main agent is told how to build Agent() prompts, around the "dispatch" subsection of each subagent topic):

```
### Constructing the Agent() dispatch prompt

The brief you receive from the workflow may be 10-15 KB. You will compress / re-order / add context when constructing the Agent tool's `prompt` parameter for dispatch. That compression is normal.

**One exception**: blocks wrapped in `<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>> ... <<<END MANDATORY>>>` must be included BYTE-IDENTICALLY in the dispatch prompt. Do not paraphrase, do not summarize, do not extract bullets. Copy the sentinels and everything between them as-is.

The workflow verifies transmission: after any substep completion that follows a subagent dispatch (`substep=subagent` for feature, `substep=readmes` for writer, close's `substep=code-review`), the next response includes a `mandatoryTransmission` field. If a rule was dropped, this field names it — fix on the next dispatch or your subagent is flying blind on that rule.
```

**Part C — Server-side transmission verifier** (code):

1. New file `internal/workflow/mandatory_block_rules.go` — under 120 lines. Defines:
   ```go
   type MandatoryBlock struct {
       SubagentType string   // "scaffold", "feature", "readmes-writer", "code-review"
       Sentinels    []string // phrases from the MANDATORY block that must appear verbatim
   }
   
   var MandatoryBlocks = []MandatoryBlock{
       {
           SubagentType: "scaffold",
           Sentinels: []string{
               "every `Edit` must be preceded by a `Read`",
               "Bash ONLY as `ssh {hostname} \"...\"",
               "NEVER `cd /var/www/{hostname} && <executable>`",
           },
       },
       // ... one entry per subagent type
   }
   ```
   Sentinels are short unique phrases chosen from each MANDATORY block's prose — they must not appear elsewhere in the brief, so a single substring match proves transmission.

2. Hook in `internal/tools/workflow.go` `handleComplete` for substep values that follow a dispatch (`subagent`, `readmes`, `code-review`): read the session's most recent `Agent` tool_use from the MCP event stream (the `extractRecentAgentDispatch` helper below); for each relevant `MandatoryBlock`, check every sentinel appears. Any missing sentinel → add to `Response.MandatoryTransmission` field (new; see schema below) with the subagent type and the dropped sentinel name.

3. Add `MandatoryTransmission` to `Response` in `internal/workflow/recipe.go`:
   ```go
   type MandatoryTransmission struct {
       SubagentType string   `json:"subagentType"`
       Dropped      []string `json:"dropped"`
       Impact       string   `json:"impact"` // human-readable: "scaffold subagent did not receive file-op sequencing rule; expect Read-before-Edit errors"
   }
   ```
   Populate it non-blocking — warning level, not error. The goal is observability; blocking on dropped MANDATORY blocks risks halting runs that are otherwise fine.

4. Helper `extractRecentAgentDispatch(sessionID string) (string, error)` — reads the MCP event log, returns the `prompt` string from the most recent `Agent` tool_use in the session. Location: `internal/workflow/agent_dispatch_extract.go`. Depending on how MCP event logs are stored, this may require a small buffer in the server handler to remember the last N Agent dispatches.

**RED tests**:
- `TestMandatoryBlockSentinels_AllSubagentTypesCovered` — assert `MandatoryBlocks` has entries for every subagent type enumerated in `recipe.md`.
- `TestRecipeMd_AllMandatoryBlocksClosed` — regex walks `recipe.md`, asserts every `<<<MANDATORY` is followed by `<<<END MANDATORY>>>` before the next `<<<MANDATORY`.
- `TestRecipeMd_AllSentinelsPresent` — for every `MandatoryBlock`, assert its sentinels appear in the corresponding subagent brief's MANDATORY block.
- `TestHandleComplete_DetectsDroppedSentinel` — fixture dispatch prompt missing a sentinel; assert `MandatoryTransmission.Dropped` contains it.

**Expected v33 impact**: dropped load-bearing rules surface as named warnings in workflow responses. Calibration bar includes "0 dropped sentinels across the run".

### Fix 4 — Surface-derived coupling graph (first-principles, no hand-maintained cluster list)

**What v32 proved**: `worker_worker_queue_group_gotcha` round-1 fix introduced `worker_gotcha_distinct_from_guide` round-2 regression. Theme A shipped diagnostic fields on ~15 checks; only 1 (`comment_ratio`) populates `CoupledWith`. Deploy still converged in 2 fail rounds, same as v31.

**The first principle**: any two checks that read the same surface (`ReadSurface` field) are coupled — an edit to that surface to satisfy one may destabilize the other. This holds regardless of what the surface is or what the checks measure. Hand-maintaining a list of "known cascade clusters" covers today's four known cases and misses every future one. Derive coupling from existing data (`ReadSurface`) rather than curating it.

**Change**: introduce a single emit-time helper that stamps `CoupledWith` on every failed check based on shared `ReadSurface`. No per-cluster code. No maintained coupling tables. The coupling graph is a function of the checks' own surface declarations.

Concrete steps:

1. New helper `internal/workflow/coupling.go` (under 100 lines):
   ```go
   // CoupledNames returns, for every failed check, the names of other checks
   // in the same emit batch that declare an identical ReadSurface.
   // The match is an exact-string equality on ReadSurface; callers should
   // ensure ReadSurface values are stable strings (not per-run derived).
   func CoupledNames(checks []StepCheck) map[string][]string {
       bySurface := map[string][]string{}
       for _, c := range checks {
           if c.ReadSurface == "" {
               continue
           }
           bySurface[c.ReadSurface] = append(bySurface[c.ReadSurface], c.Name)
       }
       out := map[string][]string{}
       for _, c := range checks {
           if c.Status != statusFail || c.ReadSurface == "" {
               continue
           }
           for _, sibling := range bySurface[c.ReadSurface] {
               if sibling != c.Name {
                   out[c.Name] = append(out[c.Name], sibling)
               }
           }
       }
       return out
   }
   ```

2. Single emit-site wrapper `StampCoupling(checks []StepCheck) []StepCheck` in the same file — under 40 lines. For each failed check with at least one coupled sibling:
   - Populate `CoupledWith` with the sibling names.
   - Append a standardized tail to `HowToFix`:
     > `"\n\n**Coupled checks on same surface (`<surface>`)**: `<name1>`, `<name2>`. An edit to this surface that satisfies this check may destabilize the coupled checks. Read their `Required` fields before editing; re-run and verify all pass after your edit."`
   - The tail names every coupled sibling by full check name. Agent reads `HowToFix` line-by-line; paraphrased coupling gets ignored, so naming is non-negotiable.

3. Callers: every check-assembly function in `internal/tools/workflow_checks_recipe.go`, `internal/tools/workflow_checks_finalize.go`, and any other `workflow_checks_*.go` file ends with `return StampCoupling(checks)` rather than returning the raw slice. No per-cluster code. No new fields on `StepCheck` beyond `CoupledWith` (which already exists).

4. `ReadSurface` hygiene: the helper requires `ReadSurface` strings to be stable and comparable. Audit existing checks once: any `ReadSurface` that includes per-run state (timestamps, absolute paths that vary, dynamically-generated IDs) needs normalization. Expected audit outcome: zero changes required, because existing surfaces are hostname / env / file-relative.

**RED tests** (new file `internal/workflow/coupling_test.go`):
- `TestCoupledNames_SameSurfaceCouples` — fixture two failed checks with identical `ReadSurface`; assert each appears in the other's coupled list.
- `TestCoupledNames_DifferentSurfacesDoNotCouple` — fixture two failed checks with distinct surfaces; assert empty coupled list for both.
- `TestCoupledNames_ThreeChecksOneSurface` — fixture three checks on one surface; assert each lists the other two (not itself, no duplicates).
- `TestCoupledNames_PassedChecksNotStamped` — fixture one failed + one passed check on same surface; assert the passed check has no `CoupledWith` populated (only failures need the hint).
- `TestCoupledNames_EmptySurfaceIgnored` — fixture check with `ReadSurface = ""`; assert no coupling emitted (empty is not a match key).
- `TestStampCoupling_HowToFixNamesAllSiblings` — fixture a failed check with two coupled siblings; assert emitted `HowToFix` contains both sibling names verbatim.
- `TestStampCoupling_V32WorkerGotchaRegression` — reconstruct v32's exact failure: checks `{worker_knowledge_base_authenticity, worker_gotcha_distinct_from_guide, worker_worker_queue_group_gotcha}` all declaring `ReadSurface = "workerdev/README.md — #knowledge-base fragment"`; one fails; assert the emitted `HowToFix` names the other two. This is the regression case — if this test passes, the v32 cascade cannot silently recur.
- `TestStampCoupling_EnvCommentClusterRegression` — same shape for env4 comment triad (`env4_import_comment_ratio, _comment_depth, _cross_env_refs` all declaring the same env4 import.yaml surface).

**Expected v33 impact**: every cascade-cluster that shares a read surface gets a coupling hint at round 1, derived mechanically from the checks' own declarations. The four historical clusters are covered as a consequence of their shared surfaces. Any future cascade on a new surface is covered the moment the check declares its `ReadSurface`. Deploy and finalize converge in ≤1 fail round on the entire cascade class, not just on historically-observed instances. Zero hand-maintained cluster tables.

### Fix 5 — Scaffold pre-flight platform principles (first-principles, not framework idioms)

**What v30, v31, v32 proved**: every run missing either workerdev SIGTERM (v30 CRIT) or apidev `enableShutdownHooks` (v31 CRIT) or both. Each close review catches it. Each next run's feature subagent re-introduces it because the scaffold brief doesn't enumerate the constraints.

An earlier draft of this fix enumerated framework-specific idioms (NestJS `enableShutdownHooks`, Express `trust proxy`, Vite `allowedHosts`, NATS `queue:`, Kafka consumer-group IDs, etc.). That helps the three frameworks in today's showcase and misses every future one: Fastify's `addHook('onClose', ...)`, Hono, Bun's `serve`, Deno's `Deno.serve` + `addSignalListener`, axum's `with_graceful_shutdown`, Python ASGI `lifespan.shutdown` — each invents its own API for the same platform obligation. Hardcoded idiom lists go stale with every new runtime.

**First principle**: state the platform invariant (what Zerops requires, why, and what breaks if it's absent). Trust the subagent — which just scaffolded the framework and knows its idioms — to translate the principle into the framework's specific API. Every principle is absolute; only the idiom varies.

**Change**: add a MANDATORY block (combined with Fix 3's syntax) enumerating platform principles. Each principle has a fixed three-part shape: **Platform constraint** (what the platform demands), **Symptom of violation** (what breaks), **Obligation** (what the subagent must verify or implement). No API names inside the principle blocks.

Add immediately before the scaffold brief's pre-ship assertions section:

```markdown
<<<MANDATORY — TRANSMIT VERBATIM IN AGENT DISPATCH PROMPT>>>

### Scaffold pre-flight — platform principles

The Zerops platform imposes invariants that every service must honor. Framework scaffolders (`nest new`, `npm create vite`, `cargo new`, etc.) do not automatically satisfy them. Before pre-ship assertions run, walk this list. For each principle that applies to your codebase type:

1. Identify the framework's specific idiom that satisfies the principle (you just scaffolded the framework — you know its APIs).
2. Verify the idiom is present in the scaffolded code. If absent, implement it.
3. Record a fact with `scope="both"` naming both the principle number AND the idiom used (e.g. `"Principle 1 satisfied via app.enableShutdownHooks() in main.ts"`). The porting user learns which idiom their runtime needs.
4. If the framework offers no idiom for a principle that applies, implement the behavior yourself AND record a fact explaining the implementation.

Principles are absolute. Idioms are framework-specific and not listed here; the subagent translates.

**Principle 1 — Graceful shutdown within 30 seconds**

Applies to: any long-running service (API server, worker, scheduled job, subscription consumer).

- **Platform constraint**: rolling deploys send SIGTERM, wait up to 30 seconds, then send SIGKILL. The service has that window to stop accepting new work, drain in-flight work, release external resources, and exit cleanly.
- **Symptom of violation**: mid-request 5xx responses during deploys; worker jobs processed but not acknowledged; database transactions left open; subscription messages redelivered.
- **Obligation**: on SIGTERM, stop accepting new work, await completion of in-flight work, close long-lived connections (database, message broker, object store, search index, cache), and exit within 30 seconds. Translate into the framework's shutdown-hook / signal-handler idiom.

**Principle 2 — Routable network binding**

Applies to: any HTTP, WebSocket, or gRPC server.

- **Platform constraint**: the L7 balancer routes to the container's pod IP. A server bound only to loopback is unreachable from the balancer.
- **Symptom of violation**: 502 Bad Gateway on every request; the service's own healthcheck passes from inside the container but the platform healthcheck fails immediately.
- **Obligation**: bind to all interfaces (`0.0.0.0` / IPv6 equivalent) or to the container's advertised IP — never to `localhost` / `127.0.0.1`. Translate into the framework's listen-address option.

**Principle 3 — Client-origin awareness behind a proxy**

Applies to: any HTTP server that cares about the client IP, scheme, or host.

- **Platform constraint**: the L7 balancer is a trusted single-hop reverse proxy. Without explicit proxy trust, the server sees the balancer as the client.
- **Symptom of violation**: rate-limits and audit logs attribute every request to the balancer IP; geo-IP / abuse detection fails; HTTPS-aware redirects mis-fire; CSRF origin checks reject legitimate requests.
- **Obligation**: configure the framework's proxy-trust / forwarded-header handling for exactly one upstream hop. Translate into the framework's trust-proxy / forwarded-header option.

**Principle 4 — Competing-consumer semantics at replica count ≥ 2**

Applies to: any subscription-driven worker (message queue, event stream, pub/sub) that runs with `minContainers ≥ 2`.

- **Platform constraint**: with N replicas, each replica subscribes independently. Without competing-consumer semantics on the subscription, every message is processed N times.
- **Symptom of violation**: duplicate database rows; duplicate emails; double-charge; non-idempotent side effects fire N times per message.
- **Obligation**: enable the broker's competing-consumer mechanism on every subscription (queue group / consumer group / shared subscription / durable subscription / SQS visibility — the name varies by broker). If the broker does not support it, the worker must run at `minContainers=1` and record a fact explaining the scaling constraint.

**Principle 5 — Structured credential passing**

Applies to: any client connecting to a service with generated credentials.

- **Platform constraint**: generated passwords may contain URL-reserved characters (`@`, `#`, `/`, `?`, `%`, `:`). Embedding in `protocol://user:pass@host` URLs silently drops credentials in some clients and fails parse in others.
- **Symptom of violation**: "authentication failed" despite credentials being correct in env vars; intermittent connect failures; library-specific quirks that look like network errors.
- **Obligation**: pass user + pass as structured client options (object / config struct / separate parameters) rather than as URL components. If only a URL form is available, URL-encode the password before embedding.

**Principle 6 — Stripped build-output root for static deploys**

Applies to: any static / SPA deploy whose build output lives in a subdirectory (`dist/`, `build/`, `public/`, etc.).

- **Platform constraint**: the deployed tree's root becomes the served root. If build output sits at `./dist/index.html` and the tree is deployed as-is, `/index.html` returns 404 and the asset is at `/dist/index.html`.
- **Symptom of violation**: every request returns 404 or the fallback index; asset URLs in the served HTML point at `/dist/<asset>` paths.
- **Obligation**: in `zerops.yaml` static deploy, strip the build-output directory prefix at deploy time (Zerops syntax: `deployFiles: <build-dir>/~` — the tilde suffix makes the directory's contents become the served root).

For every principle that applies, both the scaffold pre-ship script and the feature subagent verify the implementation. A missing implementation blocks the pre-ship assertion. An implementation that is syntactically present but semantically wrong (e.g. a shutdown hook registered but never awaited) is caught by the close-step code review.

<<<END MANDATORY>>>
```

**RED tests** (extend existing `internal/content/workflows/recipe_test.go`):
- `TestScaffoldBrief_PrinciplesSectionPresent` — grep for literal `"Scaffold pre-flight — platform principles"`; assert present.
- `TestScaffoldBrief_AllSixPrinciplesPresent` — grep for `"Principle 1"` through `"Principle 6"`; assert all six present.
- `TestScaffoldBrief_EachPrincipleHasShape` — for each of the six principles, assert the three labels `"Platform constraint"`, `"Symptom of violation"`, `"Obligation"` appear in that principle's block.
- `TestScaffoldBrief_NoFrameworkIdiomsInPrincipleProse` — within the MANDATORY block only, assert none of the following substrings appear: `enableShutdownHooks`, `trust proxy`, `allowedHosts`, `queue: 'workers'`, `consumer-group ID`, `durable`, `SIGTERM`, `0.0.0.0`, `127.0.0.1`, `vite.config`, `main.ts`, `process.on`, `app.close`, `Kafka`, `NATS`, `Redis`, `Meilisearch`. (The obligations describe what the code must do, not how a specific framework does it. Any of those substrings inside the block is idiom leakage; push it out.)
- `TestScaffoldBrief_PrincipleRecordFactInstruction` — assert the block instructs the subagent to record a fact naming both the principle number and the chosen idiom.

**Expected v33 impact**: the scaffold subagent maps each principle onto the framework it just scaffolded. For NestJS: Principle 1 → `app.enableShutdownHooks()`, Principle 2 → `app.listen(3000, '0.0.0.0')`, Principle 3 → `app.set('trust proxy', 1)`. For Fastify: Principle 1 → `fastify.addHook('onClose', ...)`, Principle 2 → `fastify.listen({ host: '0.0.0.0' })`, Principle 3 → `fastify.register(fastifyProxy, { trustProxy: true })`. Same principles, different idioms, correctly applied per framework. Missing-handler CRITs at close review drop to 0. When a new runtime enters the showcase set, the MANDATORY block needs no edit.

### Fix 6 — Orchestrator prompt self-gates (not zcp code)

**What v32 proved**: the "Export the recipe session artifacts" templated prompt fires on orchestration schedule, not on workflow state. In v32 it landed during the agent's 2m20s deferral window and pre-empted close.

**Change**: the orchestrator prompt (wherever it lives — your run driver, scheduled trigger, or hardcoded session config) must self-gate. Replace:

```
Export the recipe session artifacts:
1. Recipe archive — run `zcp sync recipe export` ...
```

with:

```
Before exporting, check workflow state: call `zerops_workflow action=status`.

If `progress.steps[5].status != "complete"` (close step not complete): do NOT export. Instead, complete the close step first — dispatch the code-review subagent, run the close browser walk, then `zerops_workflow action=complete step=close`. Only AFTER close is complete, proceed to the export steps below.

If close is already complete, export the recipe session artifacts:
1. Recipe archive — run `zcp sync recipe export` ...
```

Fix 1 enforces this server-side (export refuses when close not complete), so Fix 6 is belt-and-suspenders. Ship both — Fix 1 closes the failure class; Fix 6 prevents the agent from wasting a round-trip on a refused export.

No tests. Ship via editing whatever config drives the orchestrator.

---

## 3. What stays UNTOUCHED

Rollback-calibration rule is still in effect. Do not:

- Add new checks (content, generate, deploy, finalize — any step).
- Modify existing check semantics (thresholds, regex patterns, pass/fail criteria).
- Add new brief content beyond the MANDATORY-block wrappers in Fixes 3+5.
- Touch `recipe_templates.go` Go templates — v8.95 Fix B has held for 2 runs.
- Touch `record_fact` schema — v8.96 `scope` field has proven load-bearing.
- Touch the writer-subagent fresh-context architecture — v8.94 holds across 3 runs.
- Refactor `StepCheck` — the existing struct is sufficient; Fix 4 only populates `CoupledWith` (already exists).
- Add new subagent roles — 5 subagents (3 scaffold + feature + writer) + code-review is the right shape.
- Reintroduce publish as a workflow state, sub-step, or gate — Fix 2 removes it by design; it is a post-workflow CLI operation only.
- Modify substep-delivery mechanism — v8.90 held across v25, v28, v29, v30, v31, v32.

If you're tempted to add a check to catch something v33 surfaces, stop. Write it down in the post-v33 entry of `recipe-version-log.md`. Ship the smaller fix first.

---

## 4. v33 calibration bar

v33 is **A-grade or the masterclass plan failed**. No partial credit. Each category below must pass every item.

### 4.1 Structural (S = A)
- All 6 workflow steps attested `status=complete`
- Both close sub-steps attested (`code-review` + `close-browser-walk`)
- All 5 features exercised 200-OK on dev AND stage (10 curl passes)
- Both browser walks fired (`deploy.browser` + `close.browser`)
- 0 close-step CRITs shipped post-review (CRIT caught + fixed + re-verified = OK; CRIT shipped = NOT OK)
- Published local tree contains every required file (see §4.5)

### 4.2 Content (C = A)
- Root README intro names real managed services (v17 `dbDriver` holds for 11th run)
- Each per-codebase README ≥ 200 lines with non-empty intro / integration-guide / knowledge-base fragments
- Gotcha-origin ratio ≥ 80% genuine (sustain v31's 100%)
- 0 folk-doctrine fabrications in any surface
- 0 writer DISCARD overrides (per ZCP_CONTENT_MANIFEST.json vs facts log)
- Each per-codebase CLAUDE.md ≥ 1200 bytes with ≥ 3 custom sections beyond template
- Env 4 import.yaml comments at v7-gold quality (two-axis `minContainers` teaching, per-service WHY, no templated per-service phrasing, no self-contradictions)
- 0 env README factual-drift claims (grep env 0/3/4/5 READMEs against adjacent env YAMLs)
- 0 cross-tier data-persistence fabrications in env 0 README
- Preprocessor directive `#zeropsPreprocessor=on` on all 6 env yamls

### 4.3 Operational (O = A)
- Wall clock ≤ 90 min
- Main bash total ≤ 5 min
- 0 very-long bash on main (>60s)
- 0 MCP schema-validation errors on `zerops_knowledge`
- 0 EADDRINUSE / port-still-bound events
- 0 scaffold-phase artifact leaks (preship.sh, .assert.sh, `.DS_Store` in published tree)
- 0 `.git/index.lock` / `.git/config.lock` contention errors
- 0 `cd /var/www/{host} && <exec>` zcp-side patterns

### 4.4 Workflow (W = A)
- Substep attestations in canonical order, real-time (0 out-of-order retries — v32 had 1 on `verify-stage`)
- 0 `SUBAGENT_MISUSE` rejections (v8.90 sustain)
- Deploy-complete README checks converge in ≤ 1 fail round (v31/v32 had 2)
- Finalize env checks converge in ≤ 1 fail round
- 0 dropped MANDATORY sentinels across all subagent dispatches (Fix 3 bar)
- Export refused 0 times (means close was completed every time — not that Fix 1 is inert, but that agents completed close before trying export)
- Close sub-steps are exactly `[code-review, close-browser-walk]` — no `publish` or `export` as sub-steps (Fix 2 bar)
- `Response.PostCompletion.NextSteps` populated on close completion with publish CLI guidance (Fix 2 bar)
- ≥ 2 facts recorded with `scope=downstream` (v8.96 bar sustained)
- ≥ 1 Prior Discoveries block delivered to a downstream subagent (code-review receives ≥2 downstream facts from feature subagent)
- Every failed check with a populated `ReadSurface` has `CoupledWith` derived from sibling checks on the same surface (Fix 4 bar — if a failure emits with empty `CoupledWith` despite sharing surface with another check, the stamping helper regressed)
- Scaffold subagent records ≥ 1 fact per applicable principle from Fix 5 (e.g. apidev records facts for Principles 1, 2, 3; workerdev for 1, 4, 5; appdev for 2, 6) — proves principles-to-idioms translation happened

### 4.5 Deliverable completeness (new — added after v32's missing-files catastrophe)

Run from the published recipe root:

```bash
for f in README.md TIMELINE.md ZCP_CONTENT_MANIFEST.json \
         apidev/README.md apidev/CLAUDE.md \
         appdev/README.md appdev/CLAUDE.md \
         workerdev/README.md workerdev/CLAUDE.md \
         "environments/0 — AI Agent/README.md" \
         "environments/0 — AI Agent/import.yaml" \
         "environments/1 — Remote (CDE)/README.md" \
         "environments/1 — Remote (CDE)/import.yaml" \
         "environments/2 — Local/README.md" \
         "environments/2 — Local/import.yaml" \
         "environments/3 — Stage/README.md" \
         "environments/3 — Stage/import.yaml" \
         "environments/4 — Small Production/README.md" \
         "environments/4 — Small Production/import.yaml" \
         "environments/5 — Highly-available Production/README.md" \
         "environments/5 — Highly-available Production/import.yaml"; do
  [ -f "$f" ] || echo "MISSING: $f"
done
```

0 MISSING lines → pass. 21 required artifacts; every A-grade run must ship all 21.

---

## 5. Implementation phases

### Phase 1 — RED (target ~45 min)

1. Write all unit tests from §2 without implementation. Expect every new test to fail.
2. Run `go test ./... -count=1 -short` — assert the new tests fail, nothing pre-existing fails.

### Phase 2 — GREEN (target ~4.5 hrs)

Implement in this order to minimize interaction risk:

1. **Fix 2 (close verify-only + PostCompletionGuidance)** — 45 min. Two parts: (a) `recipe.md` close section rewrite (content only); (b) `PostCompletionGuidance` on the close-completion `Response` and purge of publish vocabulary from `recipe.go` sub-step definitions. Run `go test ./internal/content/... ./internal/workflow/...` after — `detailedGuide` extractor must still work.
2. **Fix 5 (scaffold pre-flight principles)** — 30 min. recipe.md content only. Reuses Fix 3's syntax but doesn't require Fix 3's code (sentinel-verification is non-blocking). Tests verify shape (three labels per principle) and absence of framework idioms inside principle prose.
3. **Fix 4 (surface-derived coupling)** — 30 min. Single new helper in `internal/workflow/coupling.go` (≤100 lines) + `StampCoupling` wrapper. Every check-assembly site in `workflow_checks_*.go` ends with `return StampCoupling(checks)`. No per-cluster code. Tests verify the helper's behavior on shared-surface fixtures.
4. **Fix 3 (MANDATORY-block system)** — 90 min. Split: Part A (recipe.md wrappers, 20 min) + Part B (dispatch rule text, 10 min) + Part C (server-side verifier, 60 min incl. `extractRecentAgentDispatch` helper).
5. **Fix 1 (export gate with session disambiguation)** — 75 min. Plumb session state through `ExportOpts`, add `resolveSessionID` + three-branch gate logic, update CLI flags. Highest-risk because it touches the CLI + reads workflow state across package boundaries + distinguishes three export-call modes.

### Phase 3 — REFACTOR (target ~30 min)

1. `go test ./... -count=1` clean under `-race`.
2. `make lint-local` clean.
3. Verify file sizes: no `.go` file introduced by v8.97 exceeds 350 lines.
4. Diff against CLAUDE.md conventions — no new globals, no `interface{}`, no fallbacks, errors wrapped.

### Phase 4 — Orchestrator update (Fix 6)

1. Locate the orchestrator prompt template (outside zcp's repo — wherever the run driver lives).
2. Replace the export prompt with the self-gating variant from Fix 6.
3. Smoke-test: trigger a test run, verify the prompt fires a `zerops_workflow action=status` call before any export.

### Phase 5 — v33 run + calibration

1. Execute a single `nestjs-showcase` run end-to-end.
2. Walk through §4 calibration bar item-by-item. Any fail = masterclass failed; file a post-mortem in `recipe-version-log.md` §v33 entry identifying which fix's mechanism failed, and which class (framing / delivery / coupling / pre-flight / export-gate / orchestrator).

If every item passes → v33 is A-grade. Write the version-log entry. Close the masterclass milestone.

---

## 6. Two things that will be tempting but are out of scope

**"Let me also fix the apidev ClientsModule OnModuleDestroy gap"** — no. Fix 5's MANDATORY block covers it ("Long-lived connection providers ... if no graceful-close hook, record a fact"). The feature subagent will either implement the handler or record a fact about the fire-and-forget decision. Don't add a new check for this; the pre-flight list is the right lever.

**"Let me add a check for orchestrator self-gating"** — no. Fix 1's export-gate is the structural mechanism; Fix 6 is observability. If v33 shows the orchestrator prompt still pre-empted close, the evidence is export refused → agent completes close → retries export. That's the correct loop. No third check needed.

---

## 7. One-sentence summary per fix

- **Fix 1**: export refuses when close isn't complete; three distinct error paths (no session context, declared session with missing state, close incomplete) give the agent specific remediations, so silent close-skip is impossible.
- **Fix 2**: close contains only `code-review` + `close-browser-walk` — both always autonomous; publish is removed from workflow state entirely and surfaces as post-completion CLI guidance, so the user-gated ambiguity that made v32 skip close cannot exist.
- **Fix 3**: subagent-brief rules wrapped in MANDATORY sentinels must be transmitted verbatim; server observes dropped rules and names them.
- **Fix 4**: `CoupledWith` is derived at emit-time from checks sharing a `ReadSurface` — a single 100-line helper replaces hand-maintained cluster tables and covers every future cascade class for free.
- **Fix 5**: scaffold pre-flight MANDATORY block states platform principles (graceful shutdown, routable bind, proxy trust, competing-consumer, structured creds, stripped build root) and lets the subagent translate each into its framework's idiom — no framework names in the principle prose.
- **Fix 6**: orchestrator export prompt self-gates on close attestation so the export-in-flight/close-pending race doesn't waste a round-trip.

Ship all six. Run v33. Grade A.
