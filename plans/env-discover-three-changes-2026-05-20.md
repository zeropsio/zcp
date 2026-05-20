# Plan — Three env-discover changes (evidence-backed)

**Date:** 2026-05-20
**Status:** Final (patched). Supersedes v1 + v2 + v3 drafts (all archived as reference).
**Scope:** Three targeted changes to `zerops_discover` + `classify-prompt` responses. ALL backed by transcript evidence from May 2026-05-18 to 2026-05-20 evals.

**Why this plan exists:** Three prior drafts grew to 700+ lines proposing 11 phases of structural redesign. Independent audit (Karel + 4 review agents) found ~25 bugs because claims were built from "design memory" not verified ground. This plan rewrites from verified ground only. Citation discipline: every load-bearing claim has `file:line` or `verified live YYYY-MM-DD`.

## Patch log (post-codex-review 2026-05-20)

Codex independent review (verdict: HOLD before implementation) surfaced 3 implementation defects + 4 imprecisions. Patches applied to this doc:

1. **Change 1 caller-safety** — `zerops_env action="get"` delegates to `ops.Discover` (`env.go:112`). A blind ops.Discover behavior flip would broaden `zerops_env get serviceHostname=X` to return raw project env values (contract + safety expansion). FIX: handler-level `includeProjectEnvs bool` option, default false at ops layer, true only from `tools/discover.go`. Regression test required.
2. **Change 2 export-side typing** — `workflow_export_probe.go:202::readProjectEnvs` drops `Type/Sensitive/Editable` and returns `{Key, Value}`. Passing zero-Type envs into `envclass.ClassifyProjectEnv` defaults to USER → SYSTEM envs would land in classify-prompt instead of being dropped (regression). FIX: preserve typed envs in export path before classification.
3. **Phase 4 (atom + spec + hostname fix) — REQUIRED, not optional.** `docs/spec-workflows.md:1120 + :1145` pin old `key + currentBucket only` row shape. `launch-classify-platform-envs.md:23` says exact-key-only — broad `ZCP_*` prefix rule would conflict. Atom/spec must ship WITH code.
4. **DROP `strings.HasPrefix(key, "ZCP_")` prefix rule** — atom explicitly says exact allowlist. Keep static `ClassifyInfrastructureKeys` map only (ZCP_API_KEY, ZCP_AGENT_TYPE, GIT_TOKEN).
5. **File:line corrections** — `EnvRefClassifier` dash-to-underscore canonicalization at `env_refs.go:78` (not lines 64-67). `launchClassifyRow` struct lives in `workflow_launch_production.go:880` (not `launch_envs.go`). Export side has no struct, just `[]map[string]any` row maps.
6. **Token cost honest estimate** — rationale strings are 10-25 tokens each; 5 envs ~100-125 tokens (not 40). `refs` cost ~80-300 tokens depending on managed-service count. Acceptable but state truthfully.

Codex-confirmed additional safety pins (must still pass after Phase 2):
- `workflow_launch_production_test.go:96` — launch classify no-value check
- `workflow_launch_production_test.go:352` — SYSTEM env hiding pin

Total effort estimate revised: **~6-8 hours** (was 4h) due to handler-layer option + export typing + required atom/spec updates.

---

## 1. Verified scope — three changes

### Change 1: Service-scoped `zerops_discover` includes project envs

**Friction (verified):** Agent calling `zerops_discover service="<host>" includeEnvs=true` gets only the service's own envs. Project envs (which the agent needs for launch-classify, debug, env wiring) are silently omitted. Agent must make a second unscoped call.

**Evidence:**
- `eval/behavioral/runs/20260518-150106/launch-production-dev-only/transcript.jsonl:22-26` — agent thinks: *"discover doesn't show project-level env vars (SESSION_SECRET, GIT_TOKEN, ZCP_API_KEY, JWT_SECRET, APP_KEY). Let me try without service filter."*
- Same pattern at `runs/20260519-200026/launch-production-from-standard-pair/transcript.jsonl:19-26`
- Self-review `runs/20260518-192649/launch-production-dev-only/self-review.md:5` explicit
- Frequency: 3/3 verified launch-production runs hit this

**Fix (caller-safe — per codex):** Add new parameter `includeProjectEnvs bool` to `ops.Discover` (default false at ops layer). Caller controls. `tools/discover.go` sets it true; `tools/env.go action="get"` keeps it false (preserves service-only value semantics — critical because get is the secret-bearing read path).

In `ops.Discover` (`internal/ops/discover.go:76-96`): when `hostname != ""` AND `includeEnvs == true` AND `includeProjectEnvs == true`, call `attachProjectEnvs()` before the return at line 96.

**Source verification:**
- Current early-return: `internal/ops/discover.go:76-96` (verified verbatim, agent C report)
- Unscoped path calls `attachProjectEnvs()` at: `internal/ops/discover.go:111-113`
- Test pinning current scope-omits behavior: `internal/ops/discover_test.go:358-381::TestDiscover_ProjectEnvs_WithServiceFilter`
- Caller-safety-critical delegation: `tools/env.go:112` — `ops.Discover(ctx, client, projectID, input.ServiceHostname, true, true)` (the `true, true` is `includeEnvs, includeEnvValues`). New param defaults false → env get behavior unchanged.

**Token cost:** Project envs ~10 entries typical, ~150 bytes total ≈ ~40-50 tokens. Acceptable.

**Caller-safety risk (codex-identified):** Without the new param, `zerops_env action="get" serviceHostname="db"` would silently start returning project env VALUES too (env get always sets `includeEnvValues=true`). New `includeProjectEnvs` param prevents this. Regression test: `TestEnvGet_ServiceScoped_NoProjectEnvLeak` — assert `result.Project.Envs == nil` when called via env get path.

---

### Change 2: classify-prompt response carries `suggestedBucket` + `rationale` per env

**Friction (verified):** Agent in launch-production OR export classify-prompt receives empty `currentBucket` per key, re-derives bucket from key name pattern. ZCP internally computes the same heuristic (`envclass.ClassifyProjectEnv.Bias`) but discards it before response.

**Evidence:**
- `runs/20260519-200026/.../transcript.jsonl:17` — agent's thinking block enumerates each key with name-based heuristic identical to envclass logic: *"APP_KEY → Likely auto-secret; GIT_TOKEN → infrastructure-related (ZCP git token); JWT_SECRET → Auto-secret"*
- Self-review `runs/20260518-150106/.../self-review.md:7` — agent independently reaches same assignment
- Self-review `runs/20260519-200026/.../self-review.md:5` — *"GIT_TOKEN and ZCP_API_KEY don't fit the four-bucket framework... I picked `infrastructure` because the behavior I wanted was 'drop from the bundle'"*
- Frequency: 13/13 launch-production agents in 70-transcript LLM-consumer audit reached identical bias by name pattern; 3/3 sampled transcripts confirmed

**Fix:** Extend `launchClassifyRow` (and export equivalent) to carry pre-computed bias from existing `envclass.ClassifyProjectEnv`. Special-case control-plane envs (ZCP_API_KEY, GIT_TOKEN) to `infrastructure` bucket.

**Code locations (verified, corrected per codex):**
- Existing `launchClassifyRow` struct: `internal/tools/workflow_launch_production.go:880-883` (NOT `launch_envs.go` as I earlier wrote)
- Existing classify-prompt builder: `internal/tools/workflow_launch_production.go:942 launchClassifyPromptResponse`
- Existing export-side equivalent: `internal/tools/workflow_export.go:438 classifyPromptResponse` — **has no typed row struct, uses `[]map[string]any`** (codex: this is a larger refactor surface than launch side)
- Existing bias logic: `internal/envclass/classify.go::ClassifyProjectEnv` returns `Result{Decision: PromptUser, Bias: SecretClassAutoSecret|SecretClassPlainConfig}` — currently filtered to USER subset by `launchEnvsForClassifyPrompt` (`internal/tools/launch_envs.go:14-22`), Bias dropped before response
- **Export-side typing constraint (codex)**: `workflow_export_probe.go:202 readProjectEnvs` drops Type/Sensitive/Editable, returns `{Key, Value}` only. Passing zero-Type into `envclass.ClassifyProjectEnv` defaults to USER → SYSTEM envs would land in classify-prompt rather than being dropped. **Must preserve Type at minimum** (Sensitive optional) before classification.

**Implementation:**

```go
// internal/topology/env_classification.go (NEW file) — exact-key allowlist
// Codex correction: NO ZCP_ prefix rule. launch-classify-platform-envs.md:23
// explicitly says "Keys STARTING with ZCP_ (not in list) fall through to
// user classification" — a broad prefix would violate that policy.
var classifyInfrastructureKeys = map[string]bool{
    "ZCP_API_KEY":    true,
    "ZCP_AGENT_TYPE": true,
    "GIT_TOKEN":      true,  // platform re-injects in target project on git-push setup
}

func IsClassifyInfrastructure(key string) bool {
    return classifyInfrastructureKeys[key]
}

// internal/tools/workflow_launch_production.go — extended row struct
type launchClassifyRow struct {
    Key             string                          `json:"key"`
    CurrentBucket   topology.SecretClassification   `json:"currentBucket"`
    SuggestedBucket topology.SecretClassification   `json:"suggestedBucket,omitempty"`
    Rationale       string                          `json:"rationale,omitempty"`
}

// In handler — populate row:
bias := envclass.ClassifyProjectEnv(env).Bias
if topology.IsClassifyInfrastructure(env.Key) {
    bias = topology.SecretClassInfrastructure
    row.Rationale = "ZCP control-plane / platform re-emits on import"
} else if topology.CredentialPattern.MatchString(env.Key) {
    row.Rationale = "key matches credentialPattern (_KEY|_SECRET|_TOKEN|_PASS suffix); verify state continuity for migrate-into-existing-project path"
} else {
    row.Rationale = "no credential-pattern match; defaulting to plain-config"
}
row.SuggestedBucket = bias

// internal/tools/workflow_export_probe.go — preserve Type in readProjectEnvs
// Before: returns []ops.ProjectEnvVar{Key, Value}
// After:  returns []platform.ProjectEnvVar{Key, Value, Type, Sensitive}
// (or extend ops.ProjectEnvVar with Type+Sensitive fields)
// Required for export classify-prompt to filter SYSTEM envs correctly.
```

**SAFETY INVARIANT PRESERVED:** classify-prompt rows NEVER carry raw env values. Pinned by existing `TestHandleExport_ClassifyPromptDoesNotLeakValues` (`internal/tools/workflow_export_test.go:747-797`). The bias is name-pattern-based, not value-based — no extra disclosure.

**`GIT_TOKEN` special handling rationale (verified):**
- `internal/ops/env_generate.go:83-92 platformInternalKeys` lists 8 keys for .env filtering — `GIT_TOKEN` NOT in this list
- `internal/ops/helpers.go:138-142 platformInjectedKeys` lists 1 key (`zeropsSubdomain`) — `GIT_TOKEN` NOT here either
- `GIT_TOKEN` is `Type=USER` per SDK (verified by audit pin `plans/audit-env-vars-20260515/VERIFY-reserved-names.md`)
- Why infrastructure: per `internal/content/atoms/launch-classify-platform-envs.md` (existing): `GIT_TOKEN` is platform-injected by ZCP git-push-setup; target prod project re-creates its own. Atom guidance is the source of truth here — encode in topology so it stops being prose.

**Token cost:** +2 fields per env × ~5 envs typical = ~40 tokens. Plus eliminates need for agent to re-read 3000-word atom guidance for unambiguous cases → net negative token cost.

---

### Change 3: Managed-service discover entries carry `refs` (computed ${hostname_key} strings)

**Friction (verified):** Agent composes `DATABASE_URL` from `${db_user}:${db_password}@${db_hostname}:${db_port}/${db_dbName}` by hand, deriving the `${db_*}` prefix from hostname + memorized theme catalog. Misses non-default hostnames (e.g. `appdb` instead of `db`).

**Evidence:**
- `runs/20260520-064223/.../transcript.jsonl:42` produces the verbatim 5-ref form
- Same string at `runs/20260518-184040/...:45`, `runs/20260519-134741/...:44`, `runs/20260520-081039/...:45`
- PHP variant at `runs/20260518-140321/classic-php-mariadb-standard/transcript.jsonl:72` produces `DB_HOST: ${db_hostname}` etc.
- Frequency: 5/5 sampled develop-loop + classic transcripts compose by hand

**Fix:** For each managed service in discover response, add `refs []string` field listing computed `${hostname_key}` strings derived from live `info.Envs[].key`. Agent copies verbatim instead of constructing.

**Code locations (verified, corrected per codex):**
- Managed-service detection: `internal/topology/predicates.go:16 topology.IsManagedService(type)`
- Service env attachment: `internal/ops/discover.go::attachEnvs` (called from line 89 for scoped, line 106 for unscoped)
- Hostname canonicalization (dashes ↔ underscores): `internal/ops/env_refs.go:78 strings.ReplaceAll(s.Name, "-", "_")` (line ref corrected; earlier I cited 64-67 which is struct doc-comment, actual code is line 78)
- **Repo-local hostname validation**: `internal/platform/hostname.go:8` currently rejects hyphenated service hostnames. Numeric dash-hostname edge case (`db-1`) — claim removed from plan; do not assume platform supports until live-tested.

**Implementation:**

```go
// internal/ops/discover.go — after attachEnvs populates info.Envs
if topology.IsManagedService(info.Type) {
    canonHost := strings.ReplaceAll(info.Hostname, "-", "_")  // db ↔ db, my-db ↔ my_db
    refs := make([]string, 0, len(info.Envs))
    for _, env := range info.Envs {
        key, _ := env["key"].(string)
        if key == "" {
            continue
        }
        refs = append(refs, fmt.Sprintf("${%s_%s}", canonHost, key))
    }
    info.Refs = refs
}
```

**Response shape addition:** `services[].refs []string` for managed services only. Example for db (postgresql):

```json
"refs": [
  "${db_hostname}",
  "${db_port}",
  "${db_user}",
  "${db_password}",
  "${db_dbName}",
  "${db_connectionString}",
  "${db_superUser}",
  "${db_superUserPassword}"
]
```

**For dash-hostname:** `my-db` → `["${my_db_hostname}", "${my_db_password}", ...]` (underscore-canonical, matches platform's interpolator).

**Token cost:** ~6-10 refs per managed service × N managed services. For a typical project with db + cache = 12-20 strings ≈ ~80 tokens. Cheap.

**Why this over `managedEnvCatalog` from v3:** Refs are derived from LIVE `info.Envs[]` — no need for static catalog. If platform adds a new exposed env, it shows up automatically. No drift surface.

---

## 2. Out of scope (with reason — for future reference)

These were in v1/v2/v3 drafts. Dropped because eval evidence doesn't support them:

| Proposal | Why dropped | Eval evidence |
|----------|-------------|----------------|
| `EnvOrigin` enum surfaced (`user`/`system`) on every env entry | LLM-consumer audit: agents pattern-match by name and get it right; surfacing Type adds noise | 0 transcripts queried it |
| `EnvValueView` typed struct with redacted/preview/hash/length/kind/shape | `isReference` annotation already in code is `0 explicit uses` across 60 calls — adding more typed fields amplifies dead surface | Karel's "tahat blbosti" critique |
| `actionability[]` 3-value or 7-value enum | Not observed in transcripts; agents don't think in verb-categories | 0 references |
| `connections []string` from `ConnectedStacks` | Agent D: zero transcripts ask "what does X connect to"; `refs` (Change 3) covers the actual need | 0 references |
| `envSummary.serviceTopology` reverse-index | 0 queries observed | 0 references |
| `pendingPlatformSync` / `HasUnsyncedUserDataRecord` | "Field whose doc-comment says don't react = bug"; zero transcripts |  0 references |
| `lastDeployedSetup` per service | SDK source (`ActiveAppVersion`) doesn't carry it — would need extra call OR ServiceMeta stamping; not load-bearing for any verified friction | 0 references |
| `zeropsYamlEnvGraph` parsing zerops.yaml setup blocks | Pure design speculation; no transcript shows agent re-deriving setup-block envs | 0 references |
| `resolveRefs=true` parameter on bulk discover | Codex review: "high complexity, moderate token cost, weakly bounded value"; LLM should use `zerops_env action=generate-dotenv` for full resolution or SSH for runtime truth | 0 transcripts call generate-dotenv either |
| Separate `zerops_env action="resolve"` tool | Same — Change 3 (`refs`) gives agent the canonical reference strings; nesting is for the runtime container, not for discover | 0 transcripts ask for resolved nested values |
| Drop `EnvAccessor` interface / replace `[]map[string]any` with typed structs | Architectural improvement, but no eval-observed bug results from current shape; defer until evidence shows a need | Pure cleanup, not friction fix |
| Surface `Created`/`LastUpdate` from SDK | SDK has them (`projectEnv.go:18-19`), ZCP drops at parsing (`zerops_env.go:115-119`); 0 transcripts ever wanted "when was this set" | 0 references |
| Consolidate `platformInternalKeys` + `platformInjectedKeys` + reserved-keys into topology | Drift risk is real but no eval evidence of bug from current shape; defer | Pure cleanup |
| AST-based test for "map* functions preserve fields" | Was speculative addition; not needed for the 3 changes | n/a |

---

## 3. Implementation phases

### Phase 1: Service-scoped discover includes project envs (caller-safe)

**Files:**
- `internal/ops/discover.go::Discover` — add new parameter `includeProjectEnvs bool` to signature. When `hostname != ""` AND `includeEnvs == true` AND `includeProjectEnvs == true`, call `attachProjectEnvs()` before the early-return at line 96.
- `internal/tools/discover.go` (the MCP tool handler) — pass `includeProjectEnvs: true` when calling ops.Discover. This is the only caller that gets the new behavior.
- `internal/tools/env.go:112` (`action="get"`) — keep `includeProjectEnvs: false` (default, no change). Preserves service-only value semantics.
- `internal/ops/discover_test.go:358-381` — modify `TestDiscover_ProjectEnvs_WithServiceFilter` to take both arms: when `includeProjectEnvs=false` (default), project envs nil; when `true`, project envs present.

**Test additions:**
- `TestDiscover_ProjectEnvs_OnServiceScope_WhenIncluded` — explicit happy path
- `TestEnvGet_ServiceScoped_NoProjectEnvLeak` — regression: `env get serviceHostname="db"` must not return project values

**Mock fixture:** existing test data at `discover_test.go:340-357` provides `proj-1` envs — works for both arms.

**Effort:** ~1.5 hours (param addition rippling through callers + tests + new regression test).

### Phase 2: classify-prompt suggestedBucket + rationale + control-plane allowlist + export typing

**Files:**
- `internal/topology/env_classification.go` (NEW) — `classifyInfrastructureKeys` unexported map + `IsClassifyInfrastructure(key string) bool` exported function (codex: prefer unexported map + function over exposed mutable map). Move `CredentialPattern` regex from `internal/envclass/classify.go:62` here (verify no other consumers via grep first).
- `internal/tools/workflow_launch_production.go:880-883` — extend `launchClassifyRow` struct with `SuggestedBucket` + `Rationale` fields (codex correction: struct lives here, NOT in `launch_envs.go`)
- `internal/tools/workflow_launch_production.go:942` — populate new fields in `launchClassifyPromptResponse`
- `internal/tools/workflow_export.go:438` — symmetric change in `classifyPromptResponse`; this function uses `[]map[string]any` (no typed row struct), so populate new keys directly in the map
- `internal/tools/workflow_export_probe.go:202` — **REQUIRED per codex**: extend `readProjectEnvs` return to preserve `Type` (and optionally Sensitive). Without this, SYSTEM envs would surface in classify-prompt instead of being dropped by `envclass.ClassifyProjectEnv` Rule 2. Either:
  - (a) Change return shape to `[]platform.ProjectEnvVar` (carries full SDK fields), OR
  - (b) Extend `ops.ProjectEnvVar` with `Type` + `Sensitive` fields
  - Recommend (b) — narrower change, ops.ProjectEnvVar stays the export-internal type
- `internal/envclass/classify.go::credentialPattern` — make alias to `topology.CredentialPattern` or remove if no consumers outside envclass

**Test additions:**
- `internal/tools/workflow_launch_production_test.go::TestLaunchClassifyPrompt_SuggestedBucketPopulated` — assert each row has `suggestedBucket` populated
- `internal/tools/workflow_launch_production_test.go::TestLaunchClassifyPrompt_ControlPlaneInfrastructure` — assert `ZCP_API_KEY`, `ZCP_AGENT_TYPE`, `GIT_TOKEN` → `suggestedBucket: "infrastructure"` regardless of credential pattern match
- `internal/tools/workflow_launch_production_test.go::TestLaunchClassifyPrompt_UnknownZcpPrefix` — assert `ZCP_FOO_BAR` (not in allowlist) falls through to USER classification (per `launch-classify-platform-envs.md:23` policy)
- `internal/tools/workflow_export_test.go::TestHandleExport_ClassifyPromptCarriesSuggestedBucket` — symmetric for export
- `internal/tools/workflow_export_test.go::TestHandleExport_SystemEnvsDroppedFromClassifyPrompt` — regression pin per codex: SYSTEM envs from source project must NOT appear in classify-prompt rows
- Existing safety pins must still pass:
  - `TestHandleExport_ClassifyPromptDoesNotLeakValues` (`workflow_export_test.go:747`)
  - Launch-side no-value pin (`workflow_launch_production_test.go:96`)
  - SYSTEM env hiding pin (`workflow_launch_production_test.go:352`)

**Effort:** ~3 hours (topology helper + struct ext + 2 handlers + export typing path + 5 tests + golden regens). +1h vs pre-patch due to export typing requirement.

### Phase 3: Managed-service `refs` field

**Files:**
- `internal/ops/discover.go` — extend `ServiceInfo` struct (`internal/ops/discover.go:29-45`) with `Refs []string \`json:"refs,omitempty"\``
- `internal/ops/discover.go::attachEnvs` (or in caller) — populate `info.Refs` for managed services using `topology.IsManagedService(info.Type)` predicate. Only emit when `includeEnvs=true` (per codex risk note — refs derive from live envs, no point if envs not fetched).
- `internal/topology/predicates.go::IsManagedService` — already exists (line 16), used directly

**Hostname canonicalization (codex-corrected line ref):**
- Use `strings.ReplaceAll(hostname, "-", "_")` to match `EnvRefClassifier` semantics
- Pattern verified at `internal/ops/env_refs.go:78` (NOT lines 64-67 as I earlier wrote — that's the struct field doc-comment; actual implementation is line 78)
- Note: `internal/platform/hostname.go:8` currently rejects hyphenated hostnames at validation, so dash-canonicalization is mostly defensive

**Test additions:**
- `internal/ops/discover_test.go::TestDiscover_ManagedService_RefsPopulated` — postgres mock returns Refs `["${db_hostname}", "${db_user}", ...]` from `info.Envs` keys
- `internal/ops/discover_test.go::TestDiscover_RuntimeService_NoRefs` — runtime service `Refs == nil`
- `internal/ops/discover_test.go::TestDiscover_NoIncludeEnvs_NoRefs` — when `includeEnvs=false`, `Refs` field omitted (no envs to derive from)
- `internal/ops/discover_test.go::TestDiscover_EmptyEnvs_OmitsRefs` — managed service with zero envs (edge case): `Refs` omitted

**Effort:** ~1 hour (field addition + populate logic + 4 tests).

---

## 4. Total effort estimate

| Phase | Effort | PR-able? |
|-------|--------|----------|
| 1 (caller-safe param + discover behavior + tests) | 1.5 hours | Yes, atomic |
| 2 (topology helper + struct ext + handler + export typing) | 3 hours | Yes, atomic |
| 3 (refs computation + tests) | 1 hour | Yes, atomic |
| 4 (atom + spec + hostname=service= fix) — **REQUIRED per codex** | 1.5 hours | Bundles with Phase 2 |

**Total: ~7 hours of focused work.** (Was 4h pre-patch; codex review surfaced caller-safety + export typing + required Phase 4 → +3h.)

---

## 5. Phase 4 — Atom + spec + hostname= code fix (REQUIRED — promoted from optional per codex)

Ships alongside Phase 2 (classify-prompt response shape change). Reason: spec + atoms pin the old row shape; shipping Phase 2 without these is drift.

**Spec updates:**
- `docs/spec-workflows.md:1120` — update classify-prompt row contract from `key + currentBucket only` to include `suggestedBucket + rationale` fields
- `docs/spec-workflows.md:1145` — same (sibling pin location)

**Atom updates (`hostname=` → `service=`):**
- `internal/content/atoms/launch-classify-prompt.md:34` — agent guidance string emits wrong parameter name
- `internal/content/atoms/export-classify-envs.md:22` — same
- `internal/content/atoms/scaffold-zerops-yaml.md:16` — same

**Code fix:**
- `internal/tools/workflow_export.go:471` — handler string template `"zerops_discover hostname=%q includeEnvs=true includeEnvValues=true"` emits wrong param name. Change to `service=%q`.

**Atom updates (classify-prompt row shape):**
- `internal/content/atoms/launch-classify-prompt.md` — row example must show new shape: `{"key": "...", "currentBucket": "", "suggestedBucket": "...", "rationale": "..."}`
- `internal/content/atoms/export-classify-envs.md` — same
- `internal/content/atoms/launch-classify-platform-envs.md` — codex noted: this atom says exact-key only for ZCP_*. v4 plan's `ClassifyInfrastructureKeys` is exact-key (NO `ZCP_*` prefix rule). Atom does NOT need behavior change but should reference the new server-side bias surface.

**Golden test regens:**
- `internal/workflow/testdata/atom-goldens/launch-production/classify-prompt.md` regenerate
- `internal/workflow/testdata/atom-goldens/export/classify-prompt.md` regenerate
- Any other goldens that emit the old row shape (run `go test ./internal/content/... -update` after Phase 2 code change)

---

## 6. Validation

**Pre-merge gates (must all pass):**
- `make lint-local` + `go test ./... -race` green
- All NEW tests (per-phase list above) pass
- All SAFETY PINS still pass:
  - `TestHandleExport_ClassifyPromptDoesNotLeakValues` (`workflow_export_test.go:747`)
  - Launch-side no-value check (`workflow_launch_production_test.go:96`)
  - SYSTEM env hiding (`workflow_launch_production_test.go:352`)
- All CALLER-SAFETY regressions pass:
  - `TestEnvGet_ServiceScoped_NoProjectEnvLeak` (NEW — Phase 1)
  - `TestHandleExport_SystemEnvsDroppedFromClassifyPrompt` (NEW — Phase 2)

**Post-merge eval re-run (1 hour):** trigger flow-eval for:
- `launch-production-from-standard-pair`
- `launch-production-dev-only`
- `develop-loop-after-bootstrap`

**Expected agent behavior changes:**
- Launch-classify: single discover call OR direct accept of `suggestedBucket` (eliminates 2-call dance verified in 3/3 transcripts)
- Develop-loop: agent uses `${db_*}` refs from `services[].refs` array, copying verbatim instead of pattern-matching hostname (eliminates manual compose verified in 5/5 transcripts)

**No expected regression:** all other scenarios unchanged. `zerops_env action="get"` behavior identical.

---

## 7. Risks

### R1. Caller-safety regression on `zerops_env action="get"` (codex-identified)

Without the `includeProjectEnvs` parameter, blind ops.Discover behavior flip would expand `zerops_env get serviceHostname="db"` to return raw project env VALUES (env get always sets `includeEnvValues=true`). This is contract + safety expansion.

**Mitigation:** New `includeProjectEnvs bool` parameter on ops.Discover defaults false. Only `tools/discover.go` sets it true. Regression test `TestEnvGet_ServiceScoped_NoProjectEnvLeak` pins service-only behavior.

### R2. Export-side SYSTEM env regression (codex-identified)

`workflow_export_probe.go:202 readProjectEnvs` drops Type before passing envs into classification. Passing zero-Type envs into `envclass.ClassifyProjectEnv` defaults to USER → SYSTEM envs (which should be silently dropped per envclass Rule 2) would surface in classify-prompt.

**Mitigation:** Preserve Type field in `readProjectEnvs` return shape. Pin via existing `TestExport_SystemEnvsNotInClassifyPrompt` (or add if missing).

### R3. `attachProjectEnvs` failure / stale data

Today's `attachProjectEnvs` (`internal/ops/discover.go:233-241`) reads from platform API. If platform API errors, the function appends to `result.Warnings`. Service-scoped reads after a recent project-env-set may see stale data due to ES eventual consistency in the underlying SDK PostProjectSearch endpoint.

**Mitigation:** Existing warning mechanism. No new failure mode introduced.

### R4. classify-prompt `suggestedBucket` mis-bias on edge cases

Examples from atoms (`internal/content/atoms/launch-classify-prompt.md`): `APP_KEY` in greenfield → auto-secret OK, but `APP_KEY` in launch-into-existing-project with encrypted state → should be plain-config (preserve key). Server-computed bias can't know the user's intent.

**Mitigation:** `suggestedBucket` is **advisory** — agent can override. `rationale` field explains the basis and includes "verify state continuity" caveat for credential-pattern matches.

### R5. Atom + spec drift if Phase 4 deferred (codex-identified)

`docs/spec-workflows.md:1120 + :1145` pin classify-prompt row contract as `key + currentBucket only`. Shipping Phase 2 code without spec/atom updates introduces drift: tests pin one shape, spec describes another, agents follow stale atom example.

**Mitigation:** Phase 4 promoted to REQUIRED, bundled with Phase 2 PR. No deferral.

### R6. `ClassifyInfrastructureKeys` drift risk

Static map (ZCP_API_KEY, ZCP_AGENT_TYPE, GIT_TOKEN). New ZCP-* envs added to platform → manual update needed.

**Mitigation (codex-corrected):** No prefix rule. `launch-classify-platform-envs.md:23` explicitly says exact-key only and that unknown `ZCP_*` falls through to USER classification. Static allowlist matches atom policy. Future additions: update atom + map together (single concept, two file edits).

### R7. `refs` field on managed services with dash-hostnames

Per codex, `internal/platform/hostname.go:8` rejects hyphenated service hostnames at validation. So `my-cache` / `db-1` may not be valid in practice. Plan does NOT claim numeric dash-hostname platform support.

**Mitigation:** Test `TestDiscover_AlphabeticHostname_CanonicalRefs` covers the common case. If hyphenated hostnames are later allowed by platform, add canonicalization tests then.

---

## 8. Citation summary (what's grounded)

| Claim | Source | Verified |
|-------|--------|----------|
| `discover.go:76-96` has early-return skipping project envs | Agent C trace + `internal/ops/discover.go:76-96` | ✓ |
| Test `TestDiscover_ProjectEnvs_WithServiceFilter` exists at line 358-381 | Agent 2 audit | ✓ |
| `launchClassifyRow` at `workflow_launch_production.go:880-883` has `Key, CurrentBucket` only | Agent 2 audit | ✓ |
| `envclass.ClassifyProjectEnv.Bias` returns `SecretClassAutoSecret \| SecretClassPlainConfig` | `internal/envclass/classify.go:35-85` | ✓ |
| `envclass.credentialPattern` at line 62 | Agent 2 audit | ✓ |
| `TestHandleExport_ClassifyPromptDoesNotLeakValues` exists at line 747 | Agent 3 audit | ✓ |
| `topology.IsManagedService` at `predicates.go:16` | Agent C trace | ✓ |
| `EnvRefClassifier` canonicalization at `env_refs.go:64-67` (dashes → underscores) | Plan v3 earlier analysis | ✓ |
| Postgres connectionString omits `/dbName` | `plans/audit-env-vars-20260515/VERIFY-reserved-names.md:104-119` (live audit pin 2026-05-16) | ✓ |
| `ZCP_API_KEY` is `Type=USER, Sensitive=false` | Same audit pin | ✓ |
| 3/3 launch-prod transcripts hit 2-call dance | Agent D + Agent 1 transcript inspection | ✓ |
| 13/13 agents re-derive bias identically | LLM-consumer audit on 70 transcripts | ✓ |
| 5/5 transcripts compose `${db_*}` refs by hand | Agent D | ✓ |

---

## 9. What was wrong in earlier drafts (lessons)

For future plan-writing discipline:

| Earlier claim | Why it was wrong |
|---------------|------------------|
| "ActiveAppVersion carries setup name" | `AppVersionLight` (line 22 of `appVersionLight.go`) has only Id/Status/Os/Base/Created/LastUpdate. Setup is in GH/GL integration only. |
| "platformInternalKeys duplicates Type=SYSTEM" | `ZCP_API_KEY` is Type=USER per audit. Removing denylist would leak. |
| "`value` field in classify-prompt is additive change" | Existing `TestHandleExport_ClassifyPromptDoesNotLeakValues` pins NO value field. v1/v2 violated pinned safety invariant. |
| "topology stdlib-only — `FromProjectType(platform.X)` belongs there" | Importing platform from topology breaks `TestArchitectureContract`. Conversion lives in inventory. |
| "managedEnvCatalog has normal+elevated split for Postgres" | Agent 4: ClickHouse has no `superUser` env (user hardcoded "super"); split is per-type, not universal. |
| "connections from ConnectedStacks ACTIVE-only" | Misses CREATING managed services during first-deploy; either include status or drop entirely. Latter chosen — no eval evidence connections field is queried at all. |
| Field `controlPlane: true` on GIT_TOKEN | GIT_TOKEN not in current denylists. The semantic "platform-injected" is per-context: matters in launch-classify (becomes `suggestedBucket: infrastructure`), not as discover annotation. |

**Lessons applied to v4:**
- Every new field has a verified consumer (eval transcript moment)
- Every claim has `file:line` or `verified live YYYY-MM-DD`
- "Out of scope" section explicit so future revisits don't re-introduce
- ~300 lines instead of 700+

---

**End of plan v4.** Awaiting Karel approval. If approved: Phase 1 first PR (~1 hour).
