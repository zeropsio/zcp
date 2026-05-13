# Launch-Production Feedback Fixes Plan

**Date:** 2026-05-13
**Source:** Real-world test run on `laravel-showcase-agent` (session `a7f59349-…`) attempted launch-production via `"nasaď to na prod"` natural-language entry. Agent surfaced 10 findings.
**Continues:** `plans/production-lifecycle-2026-05-11.md` (Part 1) + `plans/production-lifecycle-part2-2026-05-12.md` (Part 2 Path B).
**Current shipped:** v9.88.3 (Part 2 + ergonomics — sourceContext + scope-prompt enrichment + atom rewrite).
**Codex review:** 2026-05-13 — block-as-written then greenlight after corrections; corrections incorporated into this revision.

---

## 1. Findings classification (from external session 2026-05-13)

| # | Finding | Severity | Type | Fix locus |
|---|---|---|---|---|
| 1 | `launchKey` chybí v MCP input schema (`unexpected additional properties`) | 🔴 CRITICAL | Real bug — `json:"-"` blocks schema export | `workflow.go:152` |
| 2 | `availableRuntimes` obsahuje `zcp` (control-plane self) | 🟠 HIGH | Filter gap — `zcp@1` projde `USER` category | `launch_source_context.go::gatherLaunchSourceContext` |
| 3 | ZCP/platform-injected envs do classify-prompt (`zeropsSubdomain*`, `ZCP_*`, `envIsolation`, `sshIsolation`) | 🟠 HIGH | Design gap — exact-known platform envs should auto-classify | `launchClassifyPromptResponse` + `needsClassifyPrompt` |
| 4 | dev/stage pair (`appdev` + `appstage`) zobrazený jako 2 entries | 🟡 MEDIUM | Ergonomic — ignoruje pair-keyed `ServiceMeta` | `launch_source_context.go` + `workflow.ManagedRuntimeIndex` |
| 5 | `sshIsolation="vpn service@zcp"` carry-forward → broken reference | 🟡 MEDIUM | Specific case of #3 | resolved by #3 |
| 6 | `action="status"` (generic) ztrácí launch-production state | 🟡 MEDIUM | Architecture gap — generic status handler nečte launch state file | `workflow_action.go` status handler |
| 7 | `status` envelope vždy ukazuje bootstrap guidance | 🟢 LOW | UX noise — generic envelope fallback | resolved by #6 |
| 8 | `targetService` singular — silent single-runtime promotion | ⚪ BACKLOG | Product-safety on multi-runtime sources (codex re-classification) | `plans/backlog/launch-multi-runtime-promotion.md` |
| 9 | Token via chat = leak v transcript | ⚪ BACKLOG | Inherent in user-paste; transcript out-of-control | `plans/backlog/launch-transcript-safe-token-handoff.md` |
| 10 | Positive: trigger phrases, sourceContext, classify rubric fungují | — | Validation | n/a |

**Reclass:** #8 moved from NOT-A-BUG → backlog. Silently promoting one of multiple runtimes is a product-safety surface, not an absence of behavior. Trigger to promote: first real-world multi-runtime promotion attempt.

---

## 2. Fix priority + sequencing (post-codex revision)

Atomic phases, each verifiable on its own (per `CLAUDE.md` "Phased refactors verify each phase before continuing"):

| Phase | Fixes | Severity | Estimate | Ship |
|---|---|---|---|---|
| F1 | #1 (`launchKey` schema unblock + stale-comment + atom-overpromise + schema-desc) | CRITICAL | ~1 hr | v9.89.0 |
| F2 | #2 (zcp self-filter) + #4 (pair-keyed collapse + stage-half normalization) | HIGH+MEDIUM | ~3 hr | v9.89.1 |
| F3 | #3 + #5 (auto-classify platform envs, exact-key list, `needsClassifyPrompt` patch) | HIGH | ~3 hr | v9.89.2 |
| F4 | #6 + #7 (status action active-launch recovery, deterministic resolver) | MEDIUM/LOW | ~3 hr | v9.89.3 |

**Codex re-sequencing:** original plan split #2 + #4 across F2/F3. Codex flagged this would rework `gatherLaunchSourceContext` twice. New F2 merges them. F3 becomes the auto-classification phase. F4 unchanged.

Order rationale:
- F1 unblocks the entire workflow — without it, no agent can complete launch-production.
- F2 cleans the runtime list in one source-context pass.
- F3 removes platform-env friction on classify-prompt.
- F4 hardens compaction-recovery surface.

Total ~10 hours over 4 ship boundaries. Ship boundaries preserved (not batched) because each user-retest closes one feedback loop — Plan §5 methodology depends on it.

---

## 3. Per-fix detail

### F1 — `launchKey` accepted on input schema (CRITICAL)

**Root cause:** `WorkflowInput.LaunchKey` declared `json:"-"` (`workflow.go:152`) — `encoding/json` strips this field from BOTH serialization AND deserialization. mcp-go SDK uses the JSON tag for schema generation. Effect: agent sends `launchKey=...`, server rejects with `unexpected additional properties [\"launchKey\"]`. Workflow cannot complete.

**Question the artefact (per `CLAUDE.local.md`):** the `json:"-"` was defensive for P-LP-1. But P-LP-1 ("launchKey never in state/log/audit/response") is enforced at:
- Response struct shape (`launchProductionResponse` carries no field) ✓
- `launchState` shape (no field) ✓ — pinned by `TestLaunchState_NoLaunchKeyFieldExists`
- Audit log shape (`launchAuditEntry` carries no field) ✓ — pinned by `TestAuditLog_NeverContainsLaunchKey`
- Sentinel-scan of response text ✓ — pinned by `TestHandleLaunchProduction_LaunchKeyNeverInResponse`

`json:"-"` was over-broad. It also blocked the input path (where the field is REQUIRED to function). Other leak paths (`%+v` reflection, raw MCP request logging) are not solved by `json:"-"` anyway.

**Changes (all in one commit):**

1. `internal/tools/workflow.go:152`:
   ```go
   LaunchKey string `json:"-"`
   ```
   →
   ```go
   LaunchKey string `json:"launchKey,omitempty" jsonschema:"Launch-production publish only: one-shot account-wide Zerops API token. Required to advance ready-to-launch → launching. ZCP holds it in memory for the workflow invocation only — never persisted to state, audit log, or response. After launched status, user must revoke the token in Zerops dashboard."`
   ```

2. `internal/tools/workflow.go:35` — schema description for `Workflow` field still lists only `bootstrap, develop, or export`. Add `launch-production` to the list.

3. `internal/tools/workflow.go` — `TargetService` and `EnvClassifications` descriptions are export-only. Update to include `launch-production` presence (they're shared fields between export + launch).

4. `internal/tools/workflow_launch_production.go:34` — stale comment:
   ```
   // P-LP-1: input.LaunchKey is json:"-" so it never appears in any …
   ```
   Update to describe the new reality: P-LP-1 is enforced at response/state/audit struct shape; the input tag allows the value IN, but no output surface carries it back out.

5. `internal/content/atoms/launch-intro.md:24` — current text:
   > The one-shot key flows in via the `launchKey` parameter only during `publish` action; it is never written to state, logs, or transcripts.

   "transcripts" is false — the launchKey value IS in the MCP tool-args transcript (the client emits the call with `launchKey=<value>` and that's part of the conversation). Tighten to: "never written to ZCP-controlled state, logs, or audit trail" (truthful — what we control). The transcript-leak path is then explicitly out-of-band, covered by the backlog file for #9.

**Test pins:**

- **Delete:** `TestWorkflowInputLaunchKey_JSONTagOmits` (`workflow_launch_production_test.go:267-280`). This test asserts `LaunchKey` is omitted from `WorkflowInput` JSON marshal output. After F1, `LaunchKey` is NO LONGER omitted on input-side marshal — but P-LP-1 was never about input-marshal direction. The test was checking the wrong invariant. Delete entirely; the response/state/audit tests carry the real load.

- **Add:** `TestWorkflowToolSchema_AcceptsLaunchKey` — model after `deploy_local_test.go:42`. Spins up MCP session via `mcp.NewServer` + registers tools, calls `session.ListTools(ctx, &mcp.ListToolsParams{})`, finds the `zerops_workflow` tool, asserts `launchKey` is a property in its input schema with non-empty description.

- **Add:** `TestWorkflowTool_CallToolWithLaunchKey_NoAdditionalPropertiesError` — regression. Issues a real `CallTool` with `{"workflow":"launch-production","launchKey":"X",...}`; assert no `additional properties` error wraps the result. This is the test that reproduces the original failure shape.

- **Add:** `TestWorkflowInput_UnmarshalsLaunchKeyArg` — `json.Unmarshal([]byte(\`{"launchKey":"X"}\`), &input)` populates `input.LaunchKey`. Compile-time-adjacent pin.

- **Verify (no change required):** `TestHandleLaunchProduction_LaunchKeyNeverInResponse`, `TestLaunchState_NoLaunchKeyFieldExists`, `TestAuditLog_NeverContainsLaunchKey`, `TestHandleLaunchProduction_Mutation_AuthFailureWrappedSafely` — all stay green; they test the OUTPUT side of P-LP-1, untouched by F1.

**Atom adjustment:** `launch-intro.md` overpromise fix (above). No new atoms.

**Ship:** v9.89.0.

---

### F2 — Source-context cleanup: zcp self-filter + pair-keyed collapse + stage-half normalization

Codex merge: original F2a (#2) + original F3 (#4) — both edit `gatherLaunchSourceContext` and the surrounding scope-prompt handler. Splitting them would rework the same function twice.

#### F2.1 — `zcp` self-filter (#2)

**Root cause:** `gatherLaunchSourceContext` filters by `ServiceStackTypeCategoryName == "USER"`. The `zcp@1` service-stack-type carries `CategoryName="USER"` because ZCP itself runs as a user-runtime variant. Without exclusion, the control-plane appears as a promotion candidate.

**Two-level filter (defense-in-depth):**
1. **By type name** — exclude `s.ServiceStackTypeInfo.ServiceStackTypeVersionName == "zcp@1"`.
2. **By self-hostname** — exclude `s.Name == rt.ServiceName` (when running in-container; `rt.ServiceName` is empty on local-mode so this branch is a no-op there).

**Test pins:**
- `TestGatherLaunchSourceContext_FiltersZCPByTypeName` — Mock with `zcp@1` runtime named `zcp`; assert excluded.
- `TestGatherLaunchSourceContext_FiltersByRuntimeHostname` — Mock with NORMAL USER runtime named `appdev`, `rt.ServiceName="appdev"`; assert exclusion (NOT using `zcp@1` here per codex — must isolate the hostname branch alone).

#### F2.2 — Pair-keyed runtime collapse (#4)

**Root cause:** `gatherLaunchSourceContext` reads `client.ListServices` only — gets raw service-stack list, lists every USER-category hostname individually. Standard-mode pairs (`appdev` + `appstage`) appear as 2 entries. The actual logical identity is pair-keyed `ServiceMeta`.

**Resolution path:**

1. Thread `stateDir` into `gatherLaunchSourceContext`. The handler already has it (`handleLaunchProduction` parameter).
2. Read managed runtime metas: `metas, err := workflow.LoadAllRuntimeMeta(stateDir)` (or the local equivalent).
3. Build `metaByHost := workflow.ManagedRuntimeIndex(metas)` — pair-keyed index (one entry per dev OR stage hostname, value points at the dev-half meta).
4. For each USER-category service from `client.ListServices`:
   - If hostname maps to a meta whose `StageHostname == hostname`, this is the stage-half — SKIP (the dev-half will surface independently).
   - Otherwise emit a `runtimeChoice` with the meta's mode + stage-half hostname.

**Struct shape change:** `availableRuntimes []string` → `availableRuntimes []runtimeChoice`:

```go
type runtimeChoice struct {
    Hostname      string `json:"hostname"`
    Type          string `json:"type,omitempty"`           // e.g. "php-nginx@8.4"
    Mode          string `json:"mode,omitempty"`           // dev|simple|standard|local-stage|local-only
    StageHostname string `json:"stageHostname,omitempty"`  // set when Mode=standard or local-stage
}
```

`suggestedRuntime` remains `string` (the dev-half hostname when there's exactly one).

**Wire-shape break:** Pre-production codebase, no external consumers depend on `[]string` shape (verified: eval scenarios do not pin it). Per `CLAUDE.local.md` engineering priority — no backward-compat shim.

#### F2.3 — Stage-half input normalization

**Root cause (codex):** Even with `availableRuntimes` collapsed, a user can still say "promote appstage" and the agent passes `targetService=appstage`. Without normalization, the handler tries to find a runtime named `appstage`, gets a stage-half, and either errors confusingly or — worse — publishes from the stage-half's source tree.

**Resolution:**

In the scope-validation path (before any source-immutability hashing), if `input.TargetService` is supplied:
1. `meta, err := workflow.FindServiceMeta(stateDir, input.TargetService)`.
2. If err == nil and `meta.StageHostname == input.TargetService` (user specified the stage-half), set a `normalized` flag and return a scope-prompt with a blocker:
   - Category `scope`, ID `scope-stage-half-not-promotable`.
   - Message: "`{input}` is the stage-half of a dev/stage pair (`{dev}` ↔ `{stage}`). Promotion always uses the dev-half. Re-call with `targetService={dev}`."
   - This is a clear actionable scope-prompt, not a silent rewrite — matches existing scope-prompt blocker pattern.

**Test pins:**
- `TestGatherLaunchSourceContext_CollapsesStandardPair` — Mock with `appdev`+`appstage` pair (state has standard-mode meta); assert `availableRuntimes` has 1 entry, `Hostname=appdev`, `StageHostname=appstage`, `Mode=standard`.
- `TestGatherLaunchSourceContext_SimpleAndDevModesUnaffected` — `simple` + standalone `dev` modes pass through unchanged.
- `TestHandleLaunchProduction_StageHalfTarget_ScopePromptBlocker` — `targetService=appstage` returns scope-prompt with `scope-stage-half-not-promotable` blocker.
- `TestHandleLaunchProduction_DevHalfTarget_Accepted` — `targetService=appdev` proceeds normally.

**Atom adjustment:** `launch-scope-prompt.md` — update bullet on `targetService` to describe the new `runtimeChoice` shape and the stage-half normalization message. Specifically:
- Old: "If `sourceContext.suggestedRuntime` is populated (source has exactly ONE user runtime), use it without asking."
- New: "Use `sourceContext.suggestedRuntime` (dev-half hostname). `availableRuntimes[].hostname` is always the dev-half; `stageHostname` is present on standard-mode pairs for disclosure ("promoting `appdev` ships the dev-stage pair's published source"). If user names the stage-half (e.g. `appstage`), the scope-prompt blocker `scope-stage-half-not-promotable` will fire — re-call with the dev-half."

**Ship:** v9.89.1.

---

### F3 — Auto-classify platform-injected envs (#3 + #5)

**Root cause:** classify-prompt response surfaces every project env unbucketed, expecting agent to classify all of them. Several known platform-injected envs need NO agent judgment because the platform itself or ZCP itself produces them deterministically.

#### F3.1 — Exact-key list (NOT blanket prefix per codex)

Codex flagged blanket `ZCP_*` drop as too broad — a user app could legitimately use the prefix. Replace with a static exact-key list maintained alongside the handler:

| Key | Origin | Auto-class |
|---|---|---|
| `zeropsSubdomainHost` | Platform (per-project) | `infrastructure` (auto re-emitted in target) |
| `zeropsSubdomainString` | Platform (per-project) | `infrastructure` (auto re-emitted in target) |
| `envIsolation` | Project-level Zerops setting | `drop` (project setting, not env-var; new project gets its own) |
| `sshIsolation` | Project-level Zerops setting (carries `service@zcp` reference broken in prod) | `drop` (same) |
| `ZCP_API_KEY` | ZCP container injects (dev-side only) | `drop` |
| `ZCP_AGENT_TYPE` | ZCP container injects | `drop` |
| `ZCP_BASE_HOST` | ZCP container injects | `drop` |
| `ZCP_BUILTINS_DIR` | ZCP container injects | `drop` |
| `ZCP_PROJECT_DIR` | ZCP container injects | `drop` |
| (storage/CDN URL keys — TBD with explicit list) | Platform | `plain-config` |

The storage/CDN URL slice needs verification. Codex flagged: project-specific URLs carried forward unmutated would point prod at the source project's storage. Until exact keys are confirmed (read Zerops docs + check live project env shape on the eval-zcp playground), DEFER auto-classification of those — let them fall through to user classification, where the agent will bucket each correctly.

**Helper:** `internal/tools/launch_platform_envs.go` — new file:
```go
type platformEnvBucket struct {
    Key    string
    Bucket topology.SecretClassification // empty when Action=drop
    Action string // "classify" | "drop"
}

var platformEnvAutoClass = map[string]platformEnvBucket{ /* table above */ }

func classifyPlatformEnv(key string) (bucket platformEnvBucket, ok bool) {
    b, ok := platformEnvAutoClass[key]
    return b, ok
}
```

#### F3.2 — Layer: effective vs. visible classification

Codex correction: handler must NOT mutate the env-list at the source (that breaks P-LP-3 source-immutability hashing). Instead:
- Source snapshot stays raw (hash over the unmodified list).
- `effective classifications = auto + user`.
- `visible prompt rows = user-needed only` (auto-handled envs hidden from `classify-prompt` rows).
- Bundle composition (`internal/ops/launch_bundle.go`) reads the effective map. Auto-classified envs route to their bucket; `drop` envs are excluded from the import yaml entirely.

#### F3.3 — `needsClassifyPrompt` patch (codex miss)

Original plan filtered only the response rows. Codex flagged: the prompt status will still fire because `needsClassifyPrompt` checks raw unclassified envs. Fix in the same commit — `needsClassifyPrompt(envs, userClassifications)` consults `classifyPlatformEnv` first; auto-handled envs are considered satisfied even without user input.

**Test pins:**
- `TestClassifyPlatformEnv_TableDriven` — pins the exact-key list. Asserts each known key returns the documented bucket/action; unknown keys return `ok=false`.
- `TestNeedsClassifyPrompt_AutoHandledEnvsSatisfied` — feed `{zeropsSubdomainHost, envIsolation, APP_KEY}`; with only `APP_KEY` user-classified, asserts `needsClassifyPrompt` returns false (the two platform envs satisfy themselves).
- `TestClassifyPromptResponse_HidesPlatformEnvs` — feed envs including `APP_KEY` + `zeropsSubdomainHost` + `ZCP_API_KEY`; assert response carries only `APP_KEY` in rows.
- `TestLaunchBundle_AutoClassifiedEnvsRouteByBucket` — bundle composition assertion: `zeropsSubdomainHost` lands as `infrastructure`, `ZCP_API_KEY` is excluded entirely.
- `TestLaunchBundle_SourceHashUsesRawEnvSnapshot` — P-LP-3 preservation. Hash a raw env list, then a list with one auto-handled env removed — they must produce DIFFERENT hashes (proves hashing reads raw input, not filtered).
- `TestClassifyPlatformEnv_UserPrefixedKeysNotDropped` — e.g. `ZCP_CUSTOM_USER_THING` returns `ok=false` (not in the exact list, even though prefix matches).

**Atom adjustment:** create new atom `internal/content/atoms/launch-classify-platform-envs.md` with `phases: [launch-production-active]`, frontmatter `priority: 3`. Body explains: "Some envs are platform-injected and auto-classified (`zeropsSubdomain*`, ZCP-control envs). The classify-prompt rows you see contain only envs that need YOUR judgment — the rest are bucketed by ZCP." Brief example list with 3 entries. NO mention of `needsClassifyPrompt` or any handler-behavior verbs (axis-K hard-forbid per atom contract).

**Ship:** v9.89.2.

---

### F4 — `action="status"` launch-aware recovery (#6 + #7)

**Root cause:** generic `action="status"` returns the workflow engine's bootstrap envelope. It does not peek at `.zcp/state/launch-production/{launchID}.json`. Compaction mid-launch leaves the user stranded.

**Resolution (codex deterministic-resolver design):**

New helper `internal/tools/launch_state.go::findActiveLaunchState(stateDir, sourceProjectID string) (*launchState, []*launchState, error)`:
- Walks `.zcp/state/launch-production/`.
- For each `*.json`, reads via `readLaunchState`.
- Filters: `state.SourceProjectID == sourceProjectID` AND `state.Status` is non-terminal (i.e., not in `{LaunchStatusLaunched, LaunchStatusFailed}`).
- Sorts hits by `LastUpdate` descending.
- Returns `(activeState, allActive, nil)` where `activeState` is the most-recent OR nil if none, and `allActive` carries the full list (so the caller can decide on multi-active disambiguation).

In `workflow_action.go` status handler:
- Before returning the generic envelope, call `findActiveLaunchState(stateDir, sourceProjectID)`.
- If `activeState != nil` AND `len(allActive) == 1` — return a `kind: "launch-active"` envelope with `productionProjectName`, `status`, and a next-call hint (`workflow=launch-production` + same `productionProjectName`).
- If `len(allActive) > 1` — return a `kind: "launch-active"` envelope with all entries and a hint to pick one via `productionProjectName`. Agent surfaces the choice to the user.
- If `activeState == nil` — fall through to existing generic envelope.

**P-LP-2 guarantee:** the status path is read-only over the state directory. It MUST NOT call `newProjectAdminClient(...)` or any other ZCP→target-platform mutation surface. Pinned by a depguard-style test that scans the new code for `projectAdminClient` reference.

**Test pins:**
- `TestFindActiveLaunchState_Empty_ReturnsNil` — empty dir.
- `TestFindActiveLaunchState_SingleActive_ReturnsIt` — one non-terminal state.
- `TestFindActiveLaunchState_TerminalIgnored` — `LaunchStatusLaunched` + `LaunchStatusFailed` filtered out.
- `TestFindActiveLaunchState_DifferentSourceProjectIgnored` — state for `src-A` invisible when called with `src-B`.
- `TestFindActiveLaunchState_MultipleActive_ReturnsAllSortedByLastUpdate` — two non-terminal entries, assert sort order.
- `TestStatusAction_NoActiveLaunch_FallsThrough` — empty state dir; status returns existing generic envelope (no regression on bootstrap path).
- `TestStatusAction_ActiveLaunch_SurfacesLaunchEnvelope` — one active state; assert envelope `kind="launch-active"`, content fields populated, no `launchKey` field anywhere (P-LP-1 spot-check).
- `TestStatusAction_MultipleActive_SurfacesAmbiguous` — multiple actives; envelope shape signals choice required.
- `TestFindActiveLaunchState_NoAdminClientConstructed` (P-LP-2 pin) — instrumented factory that fails on call; helper completes without invoking it.

**Atom adjustment:** new atom `internal/content/atoms/launch-status-recovery.md` with `phases: [launch-production-active]`. Describes the recovery surface: "After context compaction, `action=status` returns `kind:launch-active` if a launch is mid-flight. Re-enter with `productionProjectName` from the envelope. No `launchKey` is required for status; the key is only needed on the publish call that advances ready-to-launch → launching." No directory-walking or implementation details.

**Ship:** v9.89.3.

---

## 4. Verification path

**After F1 (v9.89.0):** user re-runs `"nasaď to na prod"` on `laravel-showcase-agent` with FRESH launch token. Tool call with `launchKey` succeeds (no schema rejection). Workflow advances ready-to-launch → launching → configuring-pipeline → launched.

Remaining findings (#2-#7) still surface; F2-F4 clear them.

**After all 4 phases:** ergonomics complete. Future testing feedback lands per Plan §5 methodology.

**Live test substrate:** eval-zcp project (per `CLAUDE.local.md`). Token regeneration in dashboard required after every test cycle (token from session 2026-05-13 already revoked per user).

---

## 5. Testing methodology forward (how future feedbacks land here)

Per `CLAUDE.md` "Maintenance" + `CLAUDE.local.md` "Triage rule":

1. **Test surfaces a finding** — eval / real-world / hand-test.
2. **Triage** — root cause vs symptom (Step 1 in `CLAUDE.local.md`). Look one layer up.
3. **Severity classify** — critical / high / medium / low / not-a-bug / design-tradeoff.
4. **If small + scoped** — fix in current phase, no separate plan-doc.
5. **If structural OR needs design pass** — extend this plan-doc (`launch-production-feedback-fixes-<dateOfTest>.md`) OR file under `plans/backlog/launch-*` with trigger condition to promote.
6. **Per `CLAUDE.md` "New invariant → pin with test"** — every fix lands with a test that reproduces the original failure shape.
7. **Per codex pattern** — before implementation, brief codex on the plan-doc + relevant source. Codex catches over-broad defenses, false test-pin claims, missed `needsXxx` patches, atom-path drift.

Active backlog for launch-production:
- `plans/backlog/launch-pipeline-close-loop-oauth.md` — Path A revisit when Zerops exposes non-browser OAuth.
- `plans/backlog/launch-transcript-safe-token-handoff.md` (new, this round) — alternative token-flow shapes.
- `plans/backlog/launch-multi-runtime-promotion.md` (new, this round) — multi-runtime safety.

---

## 6. Token / transcript security note (out of fix scope)

Finding #9 fully transferred to `plans/backlog/launch-transcript-safe-token-handoff.md`. Two sketches preserved there:

- **Alt A:** `zcli launch publish ...` CLI path takes token via env/stdin/keyring; ZCP MCP only emits import yaml + atom guidance. Agent never sees token. Requires zcli command + handshake protocol with MCP state file.
- **Alt B:** Browser-mediated handoff via Zerops dashboard "Approve workflow for $LAUNCHID" page. Server-side links to existing launch state. More complex; needs platform-side cooperation.

Out of v9.89.x scope. Trigger to promote: user-reported transcript-leak pain OR external review flag.

---

## 7. Files affected per phase

| Phase | New files | Modified files |
|---|---|---|
| F1 | `internal/tools/workflow_schema_test.go` (new) | `internal/tools/workflow.go` (line 35 + 152 + descriptions), `internal/tools/workflow_launch_production.go` (line 34 stale comment), `internal/content/atoms/launch-intro.md` (line 24), `internal/tools/workflow_launch_production_test.go` (delete `TestWorkflowInputLaunchKey_JSONTagOmits`) |
| F2 | — | `internal/tools/launch_source_context.go` (struct shape + filter + pair-collapse), `internal/tools/workflow_launch_production.go` (stage-half normalization scope-prompt blocker), `internal/content/atoms/launch-scope-prompt.md` (sourceContext bullet update), `internal/tools/launch_source_context_test.go` (test extensions) |
| F3 | `internal/tools/launch_platform_envs.go` (helper + table), `internal/tools/launch_platform_envs_test.go`, `internal/content/atoms/launch-classify-platform-envs.md` | `internal/tools/workflow_launch_production.go` (`classifyPromptResponse` + `needsClassifyPrompt`), `internal/ops/launch_bundle.go` (effective classification consumption + source-hash preservation) |
| F4 | `internal/content/atoms/launch-status-recovery.md`, new helper file (or extension of `launch_state.go`) | `internal/tools/workflow_action.go` (status handler), `internal/tools/launch_state.go` (resolver helper if not new file), test additions |

Estimated total LOC delta across phases: ~600-800 (larger than original estimate after codex incorporation).

---

## 8. Backlog filings (this round)

| File | Reason filed | Trigger to promote |
|---|---|---|
| `plans/backlog/launch-transcript-safe-token-handoff.md` | #9 — token-in-transcript inherent to user-paste; ZCP-only state/log/audit defenses cover P-LP-1 but not the MCP transcript itself | User-reported leak pain OR external review flag |
| `plans/backlog/launch-multi-runtime-promotion.md` | #8 — silent single-runtime promotion is product-safety; codex re-classification from NOT-A-BUG to backlog | First real-world multi-runtime promotion request |

---

**End of plan. Implementation order:** F1 → F2 → F3 → F4. Each phase atomic + verifiable + shippable.
