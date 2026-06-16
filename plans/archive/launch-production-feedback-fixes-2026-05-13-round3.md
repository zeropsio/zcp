# Launch-Production Feedback Fixes — Round 3

**Date:** 2026-05-13
**Continues:** rounds 1 + 2 (F1-F14 shipped as v9.88.4..v9.90.4).
**Trigger:** Real-world test on `laravel-showcase-agent` after v9.90.4 (Karel session 2026-05-13). Workflow finally reached the mutation phase end-to-end, surfacing platform-side rejections that round 1-2 fixes had hidden behind the discoverability + error-surface gaps. Manual fallback (`export` → hand-edit → `zcli project project-import`) shipped the prod project successfully.

Karel directive: "tak zatim nic neres. dej mi jenom feedback z toho tohoto bodu, predam to jako celek vyvojarum zcp" — capture findings; **do not implement in this pass**.

---

## Summary

End-to-end flow finally hits the bundle composition + platform rejection layer. F1-F14 made the workflow reachable; the round-3 findings are about **bundle correctness** when reaching CreateAndImportProject.

The smoking-gun pattern: ZCP's `internal/ops/launch_bundle.go` composer doesn't share schema-correctness logic with `internal/ops/export_bundle.go`. The export workflow correctly omits `mode:` for object-storage; launch always emits it. Likewise, neither bundle currently emits the required `objectStorageSize:` for object-storage. Two separate bundle composers, drifting independently.

---

## 1. Findings

### F19 — Launch classify-prompt leaks platform-reserved CDN env keys 🟠 HIGH

**Symptom:** mutation failed with `projectEnvUseOfSystemKey` for `storageCdnUrl`, `staticCdnUrl`, `apiCdnUrl`.

**Trigger:** `launchClassifyPromptResponse` listed these keys for user classification; user (via agent) bucketed them `plain-config`; bundle composer emitted them verbatim under `project.envVariables`; platform rejected.

**Root cause:** F3 (v9.89.1) shipped an exact-key auto-classification table that caught `zeropsSubdomainHost/zeropsSubdomainString/envIsolation/sshIsolation/ZCP_API_KEY/ZCP_AGENT_TYPE/ZCP_BASE_HOST/ZCP_BUILTINS_DIR/ZCP_PROJECT_DIR`. The table missed three platform-reserved CDN URL keys (`storageCdnUrl`, `staticCdnUrl`, `apiCdnUrl`). Karel's round-1 plan-doc explicitly deferred storage/CDN URL handling: "Storage/CDN URL slice needs verification ... DEFER auto-classification of those — let them fall through to user classification" (plan §3 F3.1). Round 1 deferral now bites.

**Fix scope (~30 min):**
- Add the 3 keys to `platformEnvAutoClass` in `internal/tools/launch_platform_envs.go` with `Action=drop` (platform re-injects on new project) OR `Bucket=infrastructure` (drops from `project.envVariables`, platform supplies its own).
- Verify against eval-zcp live project: confirm exact spelling and that platform re-injects all 3.
- Test pin: `TestClassifyPlatformEnv_TableDriven` adds the 3 rows.

**ZCP file:** `internal/tools/launch_platform_envs.go:46-72`

---

### F20 — Launch bundle composer emits `mode:` for object-storage 🔴 CRITICAL

**Symptom:** platform rejection `projectImportInvalidParameter: { "hostname": ["storage"], "storage.mode": ["mode not supported"] }` regardless of `keepNonHA` content. Workflow stuck at `failed`; user can't unblock from any input.

**Root cause:** `BuildLaunchBundle` in `internal/ops/launch_bundle.go:207-225` always emits `mode:` for every managed service entry. The mode value flips on `KeepNonHA` membership (HA vs NON_HA) but the field itself is non-optional. Object-storage rejects ANY `mode:` value at the import endpoint.

**Comparison with export:** `internal/ops/export_bundle.go` has the same composition path but the export workflow's emitted bundle for the same source did NOT include `mode:` for storage (verified via the export classify-prompt preview Karel captured). Export's composer either branches on type=object-storage or its `ManagedServiceEntry` upstream loop excludes the field — that omission logic is NOT shared with launch.

**Fix scope (~1-2 hr):**
- Identify the type-specific exclusion in `internal/ops/export_bundle.go` (where storage's mode is omitted) and apply equivalent logic in `BuildLaunchBundle`. Likely a `serviceTypeAcceptsMode(type)` predicate consulted before writing the field, or the composer reads source service's actual `mode` (object-storage source returns no mode in `ListProjectServices` output).
- Verify against eval-zcp: import yaml with explicit `mode: NON_HA` on `type: object-storage` should fail; import yaml omitting the field should succeed.
- Test pin: new `TestBuildLaunchBundle_ObjectStorage_OmitsMode` — assert the rendered yaml's storage entry has no `mode:` key.

**ZCP files:** `internal/ops/launch_bundle.go`, `internal/ops/export_bundle.go` (cross-reference)

**Severity:** CRITICAL — any project with object-storage (Laravel-style, common stack) is locked out of launch-production without manual fallback.

---

### F21 — Both launch + export bundles missing required `objectStorageSize` 🟠 HIGH

**Symptom:** `projectImportMissingParameter: { "hostname": ["storage"], "parameter": ["storage.objectStorageSize"] }` after F20 is fixed. Surfaced via Karel's manual fallback only because F20 blocked first.

**Root cause:** Source `object-storage` service exposes `quotaGBytes` (e.g. `1`). The import schema requires `objectStorageSize` on object-storage entries (units: GB, range 1-100 per zerops_knowledge). Neither launch nor export composer maps source `quotaGBytes` → bundle `objectStorageSize`.

**Fix scope (~30 min):**
- Where `ManagedServiceEntry` is built (probably `internal/ops/discover.go` or similar populating service-list for both bundles): preserve `quotaGBytes` from the source. Pass into bundle composer.
- In both `BuildLaunchBundle` and `BuildBundle` (export): for `type: object-storage`, emit `objectStorageSize: <quotaGBytes>` (default to 1 when source returned 0/empty).
- Test pin: `TestBuildLaunchBundle_ObjectStorage_EmitsSize`, `TestBuildExportBundle_ObjectStorage_EmitsSize`.

**ZCP files:** `internal/ops/launch_bundle.go`, `internal/ops/export_bundle.go`, source-side discovery

**Severity:** HIGH — also blocks export-based fallback workflow (Karel had to hand-edit before zcli import succeeded).

---

### F22 — Export workflow variant/targetService error message inverted 🟡 MEDIUM

**Symptom:** Reproducer:
```
zerops_workflow workflow="export" targetService="appstage" variant="stage"
```
where `appstage` IS the stage half of a `mode=standard` pair.

Returned error:
```
INVALID_PARAMETER: Variant=stage but targetService="appstage" is the dev half of the pair
```

`appstage` is the stage half, not the dev half. Message is inverted.

**Root cause:** error template in `internal/tools/workflow_export.go` (or similar) likely uses a fixed phrase regardless of which half is named. Probably:
```go
fmt.Errorf("Variant=%s but targetService=%q is the %s half of the pair", variant, hostname, oppositeHalf)
```
where `oppositeHalf` is mis-derived.

**Fix scope (~15 min):**
- Find emit site (likely `internal/tools/workflow_export.go` variant-validation branch); correct the half-naming logic OR simplify the message ("targetService and variant mismatch — pair=(<dev>, <stage>); pass targetService=<dev> with variant=dev or targetService=<stage> with variant=stage").
- Test pin: feed the four combinations (dev+dev, dev+stage, stage+dev, stage+stage); assert correct vs. rejection message text.

**ZCP file:** likely `internal/tools/workflow_export.go` (variant validation)

**Severity:** MEDIUM — confusing but recoverable; user can re-call with corrected args once the message direction is sorted.

---

### F23 — zcli token-persist trap during launch-production fallback 🟢 LOW (atom only)

**Symptom:** Launch-production trust model says "one-shot token, never persisted." If launch fails and user falls back to `zcli login <token>` for manual `zcli project project-import`, zcli writes the token to `~/.config/zerops/.zcli.yml`. User assumes the token is gone after the workflow finished; in reality zcli kept it.

**Root cause:** zcli's auth model is unrelated to ZCP's launch flow — `zcli login` persists by design (`~/.config/zerops/.zcli.yml`). The trust violation is in **user expectation**, set by the launch-production atom's "never persisted" wording, then broken by the fallback path's recommendation to use zcli.

**Fix scope (~15 min, atom only):**
- `internal/content/atoms/launch-post-checklist.md` (or `launch-delete-key.md`): add a bullet — "If you used `zcli login <token>` at any point (manual fallback, debugging), run `zcli logout` before revoking the dashboard token. Otherwise the token survives in `~/.config/zerops/.zcli.yml` on the machine that ran zcli."
- Optionally `launch-mutation-key-required.md`: pre-warn "do not paste the token into `zcli login` unless you also plan to `zcli logout` after."

**ZCP file:** `internal/content/atoms/launch-*.md`

**Severity:** LOW — security UX hardening; not a bug, but ZCP's "never persisted" promise is loaded with the implication "by either us or any tool the user might pivot to".

---

### F24 — Workflow recovery loses accumulated inputs after `failed` 🟡 MEDIUM

**Symptom (Karel observation):** After mutation failure (F20), workflow returns to `phase: idle`. The user-supplied `productionProjectName`, `targetService`, `envClassifications`, `keepNonHa` are discarded. Re-running requires walking the entire prompt chain again (re-AskUserQuestion, re-paste token).

**Root cause:** `launchState` is written before mutation (`writeLaunchState(state)` at `workflow_launch_production.go:324`), but `failed` status causes the next `action="start"` to be treated as a fresh workflow start. The state file is still on disk; the handler just doesn't re-read it.

**Fix scope (~1 hr):**
- F4 (v9.89.2) shipped the `findActiveLaunchState` resolver that surfaces non-terminal launches via `action="status"`. Extend it: treat `failed` as non-terminal-but-retryable when the failure category is platform-rejection (not auth/permission). Re-prime input echo from saved state on next `action="start"` call.
- Alternative simpler fix: on `failed`, response includes `inputs` echo from `state` so the agent can keep them in the running accumulator (per F9's stateless-accumulator pattern). Agent then re-passes them on retry. No state-file resurrection needed.

**Preferred:** alternative — preserves F9's stateless model.

**ZCP file:** `internal/tools/workflow_launch_production.go` (failure response path)

**Severity:** MEDIUM — high-friction recovery; agents repeat work; users re-paste tokens (which then leak again per F23).

---

### F25 — `keepNonHa` UX surfaces object-storage as if it has HA/NON_HA mode 🟢 LOW

**Symptom (Karel observation):** AskUserQuestion for `keepNonHa` listed all 4 managed deps as choices, including storage. Karel chose all 4. Object-storage has no HA/NON_HA mode at all — the question implies it's selectable when it isn't.

**Root cause:** `gatherLaunchSourceContext` / response builder doesn't filter `keepNonHa`-eligible services by type. Object-storage entries are surfaced in `availableManagedDeps` alongside db/redis/search, but they should be excluded (or marked).

**Fix scope (~30 min, atom + minor code):**
- Filter or annotate: in source-context response, mark each managed dep with `supportsHA: true|false`. Atom guidance: "when asking about keepNonHA, EXCLUDE entries with supportsHA=false from the question."
- OR: just hardcode-exclude object-storage from `keepNonHa` consideration (it has its own quota mechanism, no scaling-mode lever).
- Test pin: source-context output for a project with object-storage + db; assert object-storage either absent from keep-nonHa candidates OR marked `supportsHA: false`.

**ZCP file:** `internal/tools/launch_source_context.go`, atom guidance

**Severity:** LOW — user-visible weirdness, but recoverable.

---

### F26 — `objectStoragePolicy` not preserved in bundle (observation) ⚪ DEFERRED

**Symptom (Karel observation):** Default `objectStoragePolicy: private` is silently OK, but if source had `public-read` (or custom IAM), the bundle would lose it.

**Fix scope (~1 hr, depends on platform discovery API):**
- Source's `objectStoragePolicy` may not be readable via `ListProjectServices`. Verify whether the platform exposes it on the service object.
- If yes: preserve in `ManagedServiceEntry` and emit in bundle composer (both launch + export).
- If no: docs note that custom policies don't survive launch/export and must be re-applied post-import in the dashboard.

**ZCP file:** `internal/platform/types.go::ManagedServiceEntry` + bundle composers

**Severity:** LOW unless user has custom policies; defer until first real-world complaint.

---

### F27 — Eval scenarios for varied source-state shapes (Karel's separate request) 📋 BACKLOG

**Karel directive:** "udelej ruzne vychozi scenare ktere pokryji nejake zakladni stavy kdy je tam jenom dev service, ale neni stage, pak kdy stage + dev, kdy je tam cely velky projet (treba laravel showcase)"

Existing scenarios in `eval/behavioral/scenarios/launch-*`:
- `launch-production-from-standard-pair.md` (dev/stage pair)
- `launch-production-pipeline-configured.md` (pipeline already configured)
- `launch-production-pipeline-not-configured.md` (pipeline missing)
- `launch-production-pipeline-skip.md` (skip-pipeline-setup)

**Missing scenarios per Karel:**
- `launch-production-dev-only.md` — single runtime in `mode=dev` (no stage half), simple managed dep
- `launch-production-laravel-showcase.md` — full Laravel stack (php-nginx pair + worker + db + redis + search + object-storage); reproduces F19+F20+F21+F25 surface

**Fix scope (~2-3 hr):**
- Write 2 new scenario files following the existing template (`launch-production-from-standard-pair.md` is the model).
- Each needs a fixture in `eval/behavioral/scenarios/fixtures/`.
- Run `flow-eval all` to confirm both pass without regressing existing.

**ZCP files:** `eval/behavioral/scenarios/launch-production-{dev-only,laravel-showcase}.md`, `eval/behavioral/scenarios/fixtures/*.yaml`

**Severity:** PROCESS — eval coverage gap, not a workflow bug. Karel's pain ("celý zabugovaný a netestovaný") was the symptom; F19-F25 are the actual bugs. Wider eval coverage prevents recurrence.

---

## 2. Suggested sequencing for ZCP dev team

1. **F20** (CRITICAL, ~2 hr) — launch + export composers share an `serviceTypeAcceptsMode` predicate; field omitted for object-storage. Unblocks Laravel-style stacks entirely.
2. **F21** (HIGH, ~30 min) — emit `objectStorageSize` from source `quotaGBytes` in both composers. Bundles validate against import schema after F20.
3. **F19** (HIGH, ~30 min) — add 3 CDN URL keys to `platformEnvAutoClass`. Confirm exact spellings against live project first.
4. **F24** (MEDIUM, ~1 hr) — preserve inputs across `failed` so retry is one call, not 5.
5. **F22** (MEDIUM, ~15 min) — export error message text correction.
6. **F25** (LOW, ~30 min) — keepNonHA filter/annotation for object-storage.
7. **F23** (LOW, ~15 min, atom only) — `launch-post-checklist` mentions `zcli logout`.
8. **F27** (PROCESS, ~2-3 hr) — write 2 missing eval scenarios.
9. **F26** (DEFERRED) — wait for first real-world request with non-default policy.

Total: ~6-7 hours of implementation; ~80% of value lands with the first 3 items (F20+F21+F19).

---

## 3. What worked (manual fallback verification, for the dev team's reference)

Karel's session shipped the prod project end-to-end via the export-fallback path. Captured for context:

1. `zerops_workflow workflow="export" targetService="appdev" variant="dev" envClassifications=<map>` → bundle preview at `classify-prompt`.
2. Hand-edit the preview's importYaml: rename `hostname: appdev` → `app`, switch `zeropsSetup: dev` → `prod`, change project name to `<source>-prod`, add `objectStorageSize: 1` to storage. (Same edits a fixed F20+F21 launch composer would do automatically.)
3. `zcli login <one-shot-token>`
4. `zcli project project-import <edited-yaml>` → 5 services queued, app build started.

So the platform-side semantics work; the bug is purely in ZCP's bundle composition for launch (and partially for export's object-storage entries).

---

## 4. Methodology note

Round 3 surfaced because rounds 1-2 made the workflow reachable. The pattern continues from the round-2 retrospective: **each shipped unblock reveals the next layer's friction**. F20 was always there since Part 1; nobody hit it until the discoverability + error-surface gaps were closed.

Recommendation for round-4 planning (after F19-F25 ship): re-run `flow-eval launch-production-from-standard-pair` + the 2 new F27 scenarios. The next layer will surface — likely pipeline-config or post-launch checklist quality.

---

**End of plan. No implementation in this pass per Karel directive.**
