# Audit Fixes — Implementation Plan (2026-06-10)

> Source: `plans/audit-mcp-tools-2026-06-09.md` (9-agent audit) → 39-finding adversarial
> code re-verification (skeptical verifier per finding, each tracing the chain + assessing
> the fix) → 4 findings live-verified on eval-zcp.
> Verification outcome: **33 confirmed, 5 partial, 1 refuted** of 39.
> This document is the working artifact for the Codex review + implementation.

## 0. Calibration — what verification changed

The original audit labelled 6 findings P0. **Verification downgraded all six** — none is a
guaranteed data-loss or deadlock:

| Original | Verified | Why downgraded |
|---|---|---|
| P0-1 stdout | **P1** | Common manifestation = recoverable garbage line *between* messages (MCP TS client fires onerror + continues); guaranteed-fatal only when a println races a >pipe-buffer response mid-write. Still routinely *reachable* (ctx-cancel on Esc hits the same path). |
| P0-2 GrantSelfRole | **P1** | Caller treats failure as non-fatal by design (warning, proceeds); real harm = lost warning + silently-missing dashboard ADMIN role. The grant's consumer (env-key reads) has zero callers today. |
| P0-3 launch replace | **P1** | **Fail-closed, not destructive**: colliding import is rejected, prod service untouched. A guaranteed dead-end of a shipped flow with no in-ZCP recovery — bad UX, not data loss. |
| P0-4 env-get combo | **P2** | Real but narrow input combo; no value corruption, just a silent null. |
| P0-5 batch no-record | **P1** | Primary guided consumer (recipe flow) is unaffected (no work session); bites only develop-session + batch. |
| P0-6 e2e env | **P1** | Real (suite skip-green) but renaming the env alone flips skip→fail, not to a meaningful run — 12/25 e2e files need zcli + VPN. |

**Refuted (drop): B10** — deploy-batch progress race. `DeployBatchSSH` blocks on `wg.Wait()`
until every goroutine returns, and each emitter structurally cannot return until ≥1 poll
interval after its own emit, so the response can never coalesce with a progress notification.
Both proposed fixes were worse than status quo (one would silence 14 min of long-build
progress). **Removed from scope.**

Net worth-fixing: **7 P1, ~24 P2, ~7 P3** + the SDK upgrade. The unifying value is not the
individual one-line fixes — it is pairing each cluster with a **class-prevention pin** (the
repo's own discipline), so the four systemic patterns (parallel-path divergence, tell/check
drift, unverified platform seam, dead-code-with-false-docs) cannot silently recur.

## 1. Unifying pattern & phase map

Every phase = concrete fixes routed through a **single owner** + a pin that fails CI if the
class reappears. Ordered by user-facing risk; each phase independently verifiable
(build + `go test ./... -short` + `make lint-local` green, plus the named live/extra gate).

> **Hard ordering constraint (Codex-confirmed):** **P2 must precede P3.** P3's import-override
> failed-history gate (`failed_context.go::LatestFailedAppVersionContext`) calls the very
> `SearchAppVersions`/`SearchProcesses` that P2 (B1 layer A) rescopes — so B1ᴮ's correctness
> *depends on* B1ᴬ shipping first. **P0-5 may move from P4 into P2's tail** (isolated to
> `deploy_batch.go`, small + high-value). **P2 ships as two commits**: (a) SDK-response /
> ES-scoping / cancellation; (b) logs-sort / mounter / mutex. No P4/P5 fix depends on the
> `PlatformError.Unwrap` change (it only affects platform error tests + `errors.Is`).

| Phase | Theme | Findings | Class-prevention pin |
|---|---|---|---|
| **P0** | go-sdk v1.6.1 upgrade | (SDK) | trial-build parity already proven |
| **P1** | MCP protocol integrity | P0-1 | stdout-purity scanner extended to the SDK module dir |
| **P2** | Platform-layer correctness | P0-2, B1ᴬ, B2, C8, C9, C10 | AST lint: no response-discard `_, err =` in `internal/platform` |
| **P3** | Import/launch gate integrity | P0-3, B1ᴮ, B12, B13, B14, B15, B16 | emitted-YAML `override:true` pin; ack scope-bind test |
| **P4** | Parallel-path parity (deploy/env) | P0-5, B5, B6, B7, B8, B17, B23, C1, C3 | shared liveness predicate; `RecordDeployAttempt` batch pin |
| **P5** | Agent-facing contract + schema | P0-4, B3, B4, B9, B11, C2, B18, B19, C6 | shellQuote AST lint; schema-enum-vs-dispatch pin; drift-lint covers BinaryExpr/Ident |
| **P6** | Dead code + test infrastructure | B20, B21, B22, C4, C5, C7, P0-6 | annotations completeness loop; TestMain corpus guard |

---

## Phase 0 — go-sdk v1.5.0 → v1.6.1

**Why:** the "latest MCP SDK" goal; isolates a dependency bump from behavior fixes.
**Verified safe (zero-touch):** trial build via alternate modfile produced byte-identical test
results. ZCP imports only `go-sdk/mcp`, stdio-only; the `SetError` semantic change is
unreachable (handlers never return Go errors). jsonschema-go bumps v0.4.2→v0.4.3 (benign).
**Steps:** `go get github.com/modelcontextprotocol/go-sdk@v1.6.1 && go mod tidy`.
**Coordination:** FYI to Aleš — shared `internal/recipe` registration path, behavior-transparent.
**Gate:** build + full test + `make lint-local` green.

> Note: Phase 0 is independent of every other phase. It can ship first or last. Recommended
> first (clears the "latest SDK" goal and de-risks later schema work against the newer
> jsonschema-go).

---

## Phase 1 — MCP protocol integrity (P0-1)

**Problem:** `zerops-go`'s `sdkBase.Method` (the funnel for *every* SDK call) does
`fmt.Println("Method Do error:", …)` on every transport failure (utils.go:52,59). ZCP's MCP
server shares that stdout fd (`server.go:250 &mcp.StdioTransport{}`). **Live-reproduced:** a
standalone client mirroring `NewZeropsClient` against an unreachable host wrote 159 bytes of
non-JSON to stdout on a DNS error. Trigger set is wide (conn-refused / DNS / TLS / timeout /
**ctx-cancel on Esc**). `TestNoStdoutOutsideJSONPath` scans only `internal/`, so the
dependency hole is invisible.

**Fix (verifier + Codex-corrected — the naive "repoint before client" is wrong;
`StdioTransport.Connect` reads the live `os.Stdout` at Connect time inside `Run`, so it would
redirect the transport too):**
1. `cmd/zcp/main.go` **serve path only, BEFORE `run()`** (not CLI subcommands, which
   legitimately print): `realStdout := os.Stdout; os.Stdout = os.Stderr`. **Codex: must be
   before `run()`, not inside `Server.Run`** — `run()` does auth + `NewZeropsClient` +
   `auth.Resolve` (a live `GetUserInfo`) *before* `server.New`/`srv.Run` (main.go:195+), so a
   startup transport failure (bad token/host) would leak to stdout before the transport is even
   built. Catches `fmt.Println` from *any* dep (the SDK dereferences `os.Stdout` at call time).
2. `internal/server/server.go`: `Server.New`/`Run` accept the real writer; swap
   `&mcp.StdioTransport{}` → `&mcp.IOTransport{Reader: os.Stdin, Writer: <realStdout in a no-op
   closer>}`. **Codex: do NOT pass `*os.File` directly** — go-sdk closes the writer via
   `rwc.Close()`→`ioConn.Close()` (transport.go:328,656), which would close fd 1; wrap in a
   local nop-closer mirroring the SDK's own `nopCloserWriter` (transport.go:101). `IOTransport`
   takes `Reader io.ReadCloser` + `Writer io.WriteCloser` (transport.go:108, v1.5.0+).
3. **Upstream complement:** the two `fmt.Println` in zerops-go are debug leftovers (the error
   is already returned in `r.Err`). Zerops owns the SDK — delete them + release + bump. The
   local repoint ships regardless as defense-in-depth (guards *any* dependency).
**Pin (class-prevention):** extend `internal/topology/architecture_stdout_purity_test.go` to
also scan the resolved zerops-go module dir (`go list -m -f '{{.Dir}}'`) so an SDK upgrade
re-introducing a stdout write fails CI; + a test pinning the serve-path IOTransport wiring.
**Blast radius:** `cmd/zcp/main.go`, `internal/server/server.go` (Run/New signature),
`internal/topology/architecture_stdout_purity_test.go`. No other `Server.Run` callers.
**Gate:** re-run the live repro → 0 bytes captured on stdout; build + test + lint. **Codex-
recommended extra test:** a subprocess test that starts serve mode with an unreachable API host
+ fake token, asserts stdout is byte-empty while stderr may carry SDK diagnostics, and asserts
MCP JSON still flows through the saved writer.

---

## Phase 2 — Platform-layer correctness (single-owner)

All in `internal/platform`. Bundles the SDK-usage correctness cluster behind one new lint.

- **P0-2 GrantSelfRole** (`project_admin.go:259`): capture the `PutClientUserRoles` response,
  check `resp.Output()` via `mapSDKError` exactly like the `GetClientUserRoles` step five
  lines above. The only response-discard of 30+ SDK call sites. + unit test (httptest: 200 on
  GET roles, JSON 4xx on PUT, assert mapped error). **Class-prevention pin:** AST lint
  forbidding `_, err = …handler.X(…)` response-discards in `internal/platform`.
- **B1 layer A ES scoping** (`zerops_search.go`): add `projectId eq` term to `SearchProcesses`
  + `SearchAppVersions`; explicit `Limit` on `ListServices`/`ListProjects`. **Live-verified
  2026-06-10** that process/app-version/service-stack search all accept the `projectId` term
  without error or zeroing (matches what official zcli sends). Keep client-side filter as a
  defensive assert. (Headline cross-project starvation is only reachable via the zcli
  ScopeProjectId fallback — primary auth hard-fails multi-project — but the import-gate window
  symptom B1ᴮ in Phase 3 is the real one and shares this owner.)
- **B2 timestamp sort** (`logfetcher.go:205`): pre-parse timestamps once into a decorated
  slice, `sort.SliceStable` on `time.Time` with a tie-break; keep the malformed-drop rule
  consistent with the Since filter. Fixes the lexicographic sub-second misorder the package's
  own api-tagged contract test already proves. **+ correct the CLAUDE.md bullet** ("filterEntries
  uses parse-compare only" overstates what shipped). (`events.go:306` parallel site fixed in P4.)
- **C8 mutex-during-IO** (`zerops.go:69`): `getClientID` holds the client mutex across a 30s
  `GetUserInfo`. Check-under-lock → release → fetch → re-lock-store (idempotent read, rare
  duplicate harmless). Fix the false "retrying" comment. (Optional: have `auth.Resolve`'s
  existing startup `GetUserInfo` populate `cachedID`.) Reject singleflight (adds a dep for a
  once-per-process path).
- **C9 mounter output** (`mounter.go:226`): `execWithTimeout` → `CombinedOutput()` + a local
  `execError{Output, Err}` with `Unwrap`, mirroring `SSHExecError` (parallel-path parity). Do
  NOT reuse `SSHExecError` (its Hostname/SSH semantics don't apply).
- **C10 cancellation chain** (`errors.go`): add `Cause error` field + `Unwrap()` on
  `PlatformError`; set `Cause` only at the `mapSDKError` seam (do NOT change `NewPlatformError`'s
  signature — hundreds of call sites). + test `errors.Is(mapped, context.Canceled)`.
**Gate:** build + test + lint; new response-discard lint green; B2 mixed-precision same-second
fixture.

---

## Phase 3 — Import / launch gate integrity

The destructive-gate + launch-correctness cluster (`internal/tools/launch_*`, `internal/ops/bundle`,
`internal/ops/failed_context.go`).

- **P0-3 replace actually replaces:** (a) **Codex-corrected ownership — bundle-owned, not
  tools-side YAML mutation.** Add an `Override bool` (or replace-hostname set) to the bundle
  input and emit it in `runtimeEntryFromInput` (`ops/bundle/launch.go:183` — the single owner
  for runtime-service YAML), so the composer owns the YAML shape and `BuildLaunch` tests inspect
  the final document. (The `ops/import.go`-style post-compose mutation is weaker — it mutates
  after bundle validation.) (b) extend the recompose trigger `launch_existing.go:281` from
  `hasSkips` to `hasSkips || hasReplaces` (replace is only known after the first compose);
  (c) add a **desired-kind** field to `existingProjectConflict` (`launch_existing_conflict.go:35`)
  derived from the desired service SOURCE (not the existing service type — Codex), since
  `detectExistingProjectConflicts` (:51) merges promoted + managed hostnames; **don't offer
  `replace` for managed services** (platform restricts override to runtimes). (d) update the
  conflict-prompt + atom text so "replace" no longer implies overwrite-without-`override` (Codex).
  **Pin:** emitted YAML contains `override:true` for replace-acked runtime hostnames; managed
  collision refuses replace; final YAML re-validates after composition. **Live gate:** exercise a
  replace conflict on eval-zcp end-to-end.
- **B1 layer B** (`failed_context.go`): thread the `projectId` scoping (from Phase 2) into the
  import-override failed-history window so 10 unrelated newer appVersions can't evict the
  target's failed history and silently pass the destructive gate.
- **B12 classification boundary** (`classify_envs.go` + both project-env composers): validate
  bucket strings against `topology.SecretClassificationValues()` at the input boundary with a
  **structured rejection naming each invalid key+value + the valid set** (silent "treat as
  unclassified" lets the agent resubmit the same typo blind). Treat `""` as missing (re-prompt).
  Fix BOTH `composeProjectEnvVariables` + `applyClassificationToProjectEnvs` (parallel-path) so
  a typo can't route a credential verbatim into a publish-ready bundle / prod project.
- **B13 ack scope-bind** (`launch_reset.go`): append `state.TargetProjectID` to `Targets` on
  the launchKey path so a state-file-only-minted ack fails `targetSetsEqual` and forces a fresh
  refusal whose `wouldDestroy` lists `projects[]` before a real project is deleted.
- **B14 promotables-only seed** (`workflow_launch_production.go`): single seed site in
  `handleLaunchProduction` — `if input.TargetService=="" && len(Promotables)>0 { derive from
  Promotables[0] }` — so the flow doesn't refuse `TargetService==""` *after* the launchKey is
  minted. (Hoist `resolveLaunchRuntimes` if seeding inside `executeLaunchMutation`.)
- **B15 apiHost threading** (`server.go:190` → `RegisterWorkflow` signature → 4 factory call
  sites): pass `s.authInfo.APIHost` (the seam already exists for deploy registrations) so
  launchKey / existingProdToken / reset clients hit the user's instance, not the canonical
  default.
- **B16 per-body ref scan** (`ops/bundle/launch.go`): replace `mergeZeropsYAMLBodiesForRefs`
  (concatenation → duplicate-key YAML → silent empty ref set) with a per-runtime loop calling
  `extractZeropsYAMLRunEnvRefs` on each distinct body, union the sets; delete the function +
  the false "line-based" comment. Fixes blind M2 detector + false "nothing references ${x_*}"
  warnings on multi-runtime launches.
**Gate:** build + test + lint; live replace-conflict run on eval-zcp.

---

## Phase 4 — Parallel-path parity (deploy / env / manage)

The "fix landed at one site, propagate to siblings" cluster — each routes to a shared owner.

- **P0-5 batch records attempts** (`deploy_batch.go:139`): after `DeployBatchSSH`, loop
  `result.Entries` and record one `workflow.DeployAttempt` per entry mirroring
  `deploy_ssh.go:238-250` (AttemptedAt, Setup from preflight-resolved `entry.Target.Setup`,
  Strategy=zcli; DEPLOYED→SucceededAt; `entry.Error` kickoff→Error+FailureClass; non-DEPLOYED→
  `classifyDeployStatus`). Detail: `entry.Error` is a string — wrap or thread the typed error
  for `classifyTransportError`. **Pin:** `ws.Deploys` + `FirstDeployedAt` after a mixed batch.
  **Live gate:** batch deploy on eval-zcp → auto-close gate sees the deploys.
- **B5 timedOut propagation** (`scale.go`, `env.go`, `delete.go`): surface `timedOut` like
  manage/subdomain. Add a `TimedOut`/`Warnings` field to the tools-side response shapes
  (`ops.ScaleResult` has none — wrap at the boundary like `manageResponse`); make NextActions
  timeout-aware; delete must not silently skip ServiceMeta cleanup.
- **B6 user-visible filter** (`ops/helpers.go`): (1) **suggestion leak** — `FindService` →
  `ListHostnames(filterUserVisible(services))`, one line, fixes the `core`/`buildapi*` leak at
  *every* call site (live-confirmed via `zerops_manage restart` on eval-zcp; surface is wider
  than cited — also verify.go:161, export.go:125, deploy_ssh.go:121,126); (2) **lookup filter
  — Codex-mandated caller matrix, NOT a global `FindService` change.** Each caller resolves OR
  resolves-then-classifies; categorize all of them before touching the lookup:
  | caller | behavior | filter the lookup? |
  |---|---|---|
  | delete / manage / scale | mutate a user service | **yes** — refuse system targets |
  | `MountService` (mount.go:72) | mounts a user service | yes |
  | `UnmountService` (mount.go:214) | uses lookup to detect a mount pointing at a *deleted* service | **no** — must still resolve gone/system services |
  | subdomain / import / eval / dev-server | resolve-then-classify via `IsSystem` | **no** — they classify downstream |
  Suggestion filter (1) is universal + safe; lookup filter (2) only in the mutating rows.
- **B7 liveness predicate** (`env.go:459,470`): one shared `RUNNING||ACTIVE` predicate for
  `resolveRestartTargets` + `isAutoRestartEligible` (restart-of-RUNNING is proven safe by the
  git-push-setup path). Fix the now-stale prose (the "not ACTIVE" warning, "No ACTIVE services"
  NextActions) on both the service- and (silently-skipping) project-level paths.
- **B8 env upsert disclosure** (`ops/env.go:155,196,257`): create-first-delete-after is NOT
  viable (platform rejects duplicate key — that's *why* delete-then-create exists). Instead,
  gate on the existing `replaced` flag: annotate the create-failure error "previous value was
  removed before this write — re-run to restore", and override the misleading
  `userDataDuplicateKey` translation when `replaced==true`.
- **B17 adopt error code** (`guard.go:124`): `requireAdoption` → `ErrAdoptRequired` + structured
  Recovery (template: `workflow_git_push_setup.go:135-142`). Reconcile with the uniform-Recovery
  contract test (`recovery_contract_test.go:143`) — the local-mode branch needs the same shape
  or a documented variant. Second tell to update: `atoms/idle-adopt-entry.md`.
- **B23 in-flight attempt** (`deploy_ssh.go:246`, `deploy_local.go:172`): branch on
  `result.TimedOut` — record an in-flight attempt (no FailureClass), point at
  `zerops_events` + `record-deploy`. (Residual: needsDeploy still true — acceptable, mirrors
  git-push; the win is no false "Last attempt failed: BUILD_TRIGGERED".)
- **C3 subdomain port filter** (`ops/subdomain.go:231`): filter `attachSubdomainUrlsToResult`
  by `HTTPSupport` — but DON'T collapse to one port (`PreferredHTTPPort` returns one, dropping
  legitimate multi-http URLs) and DON'T empty the list for unset-Scheme ports (keep single-port
  services' URL). Filter on the HTTP-support flag, preserve all HTTP ports.
- **C1 DM-2 over SSH** (`ops/deploy_ssh.go:162`): the validation block (incl. the DM-2 hard
  error) is gated on `os.Stat(mountPath)` — skipped when the mount isn't stat-able. Fix =
  read the yaml over SSH like the git-push path (better than hard-fail, which would block
  legitimate recipe/pre-bootstrap self-deploys). **Careful:** the ops unit suite rides the skip
  branch (no `/var/www/<src>` on the dev machine) — update those tests.
**Gate:** build + test + lint; live batch deploy on eval-zcp.

---

## Phase 5 — Agent-facing contract + schema

The tell/check-drift cluster + the schema-strictness fixes. Pairs with meta-test hardening so
the drift class stays closed.

- **B3 shellQuote sweep** (`ops/deploy_ssh.go:264`, `ops/deploy_git_push.go:43-62`,
  `git_auth_probe.go`, `tools/deploy_git_push.go:49,215`): `shellQuote(branch/workingDir/setup)`;
  hostname regex on `parseGitHost` (host sits in a double-quoted echo — `$`/backtick live);
  `healthPath` regex in `validateDevServerParams`. **Pin (Codex-narrowed — a broad "no
  `fmt.Sprintf` shell" rule is too noisy):** AST lint flagging only `ExecSSH`/`ExecSSHBackground`
  calls whose command arg is a `fmt.Sprintf(...)` directly OR a variable assigned from
  `fmt.Sprintf`, with an allowlist of builders that quote/validate their dynamic inputs. Current
  risky sites: deploy_ssh.go:253, deploy_git_push.go:31, dev_server_start.go:313. **Codex test:**
  command-builder unit tests feeding spaces / quotes / `;` / newlines / suspicious git-host strings.
- **B4 FlexBool on workflow** (`workflow.go:304`): the only FlexBool tool without an explicit
  `InputSchema` → inferred `type:boolean` rejects `force="true"` (live-confirmed: published
  schema is `boolean` vs env/discover's `oneOf`). **Do NOT hand-author `objectSchema` for the
  30-field `WorkflowInput`** (drift surface). **Codex-confirmed approach:** derive the base via
  `jsonschema.For[WorkflowInput](nil)` (exists in jsonschema-go@v0.4.2 `infer.go:82`; handles
  the nested structs + emits `additionalProperties:false`), then patch only the `force` /
  `skipPipelineSetup` properties to `flexBoolSchema`. + wrap `zerops_browser`'s `ForceReset`
  (registered as the ops-package `BrowserBatchInput`, browser.go:44) in a tools-package FlexBool
  struct. **Pin + test:** a `ListTools`/schema test asserting `force`, `skipPipelineSetup`, and
  browser `forceReset` each accept BOTH boolean and string, while unknown keys are rejected.
- **C2 additionalProperties** (`flexbool.go:117`): set `objectSchema`'s
  `AdditionalProperties = &jsonschema.Schema{Not:&jsonschema.Schema{}}` (one line, single owner
  for all 6 explicit-schema tools incl. both `zerops_deploy` variants) so typo'd keys
  (`working_dir`) reject instead of being silently dropped. Live-verified the wording matches
  the inferred-schema rejection.
- **B9 dev_server schema** (`dev_server.go:49,53`): fix the `command` example to
  `env PORT=3000 npm run dev` AND rewrite "Env assignments and pipes are supported" (bare
  `KEY=VAL cmd` is rejected); for `port`/status either implement a no-probe status (pidfile
  `kill -0`) or correct the schema text to "status always requires a port".
- **P0-4 env-get combo** (`env.go:213`): clear `input.ServiceHostname` when `project=true`
  (option A — verified safe: the unscoped Discover always attaches project envs). Fixes the
  silent `envs:null` (live-confirmed).
- **B11 git-token contract** (`deploy_git_push.go:356`): route the GIT_TOKEN-missing branch
  through `convertError` (NOT append-to-Instructions — that creates a second credential-emission
  site outside the single owner) so `appendCredentialContract` fires.
- **B18 + B19 + C6 drift batch:** workflow Description drops `recipe` (keep one-clause redirect),
  `bootstrap|develop|recipe`→`bootstrap|develop`, add launch-production to the unknown-workflow
  error (`workflow.go:306,326,580`); `record_fact` Description+refusal text aligned with the
  real gate (`record_fact.go`); `zerops_guidance` routes 3 plain-text errors through
  `convertError` + drop the v9.0.1-blocked `guidanceTopicIds` hint (`guidance.go:32,48,84`).
**Pin (class-prevention):** extend `description_drift_test.go` to fold `BinaryExpr`/`Ident`
descriptions (currently skips both deploy tools); add a schema-action-enum-vs-dispatcher pin.
**Gate:** build + test + lint; extended drift + schema pins green.

---

## Phase 6 — Dead code + test infrastructure

Removes the dead-code-with-false-docs surface and closes the "can't trust green / can't verify
the real platform" gaps.

- **C7 dead-code sweep** (~14 confirmed-dead items): `synthesizeImmediateGuidance` +
  `immediatePhaseFor` + transitive `SynthesizeImmediatePhase` (its only caller) + collapse the
  no-action/handleStart immediate blocks; `stale_meta_setup.go` + wire type; `EnsureEnvLocal`;
  test-only twins (`ops.Verify`/`VerifyAll`, `HasSuccessfulDeploy`, `AutoCloseProgressFor` —
  point tests at the production successors); `AtomExists`, `FormatOfferings`, `RenderDiff`,
  `effectiveProdSetupName`, `httpStatusFromPlatformCode`, `uriToPath`; `server.CurrentPID`;
  `platform.CreateOpts`; `LogAccess.AccessToken/Expiration`. **`bash_guard.go::CheckBashCommand`
  — flag to Aleš first** (his post-mortem origin), don't auto-delete.
- **B22 readiness rubric** (`launch_readiness.go`): **delete** (wiring-as-is would be buggy —
  the rubric reads raw `MinContainers` while the composer applies floor-with-warning; all 5
  invariants already enforced at compose-time). Correct the CLAUDE.md pin reference.
- **B20 hermetic corpus** : add a `TestMain` to each of `internal/knowledge`, `internal/recipe`,
  `internal/tools` checking a corpus floor (≥5 non-mailpit recipes via the embedded store) →
  one loud "run `zcp sync pull`" failure instead of 35 cryptic content assertions. (A single
  `TestCorpusPresent` doesn't suppress the other 34 — must be `TestMain`.) CI stays strict.
- **B21 annotations completeness** (`annotations_test.go`): add the reverse loop over the
  registered tool map (fail on any untabled tool) + 6 table entries (guidance, preprocess,
  record_fact, workspace_manifest, deploy_batch, recipe — decide hint values) + documented
  `zerops_browser` exemption (it has a dedicated test + isn't in `listAllTools` under bare
  `runtime.Info{}`).
- **C4 mock fidelity** (`mock_methods.go`): materialize-on-toggle from
  `ImportResult.ServiceStacks` (keep the mock dumb — no YAML parsing) + flip
  `TestIntegration_ImportThenDiscover` to expect 3 services; `NewPlatformError` on miss-paths;
  `Search*` sort-by-Created-desc + apply limit.
- **C5 e2e cleanup safety** (`helpers_test.go`): **track created hostnames during the run and
  delete only those** — the prefix+8hex regex is WRONG (bootstrap families build `bs`+4hex+`dev`,
  `b6`+4hex, etc., not uniform prefix+8hex; a naive regex silently stops cleaning them).
- **P0-6 e2e CI** (`.github/workflows/e2e.yml`): rename `ZEROPS_TOKEN`→`ZCP_API_KEY`, raise
  `-timeout` + `timeout-minutes` to suite scale, add a canary step failing the job when every
  test skipped. **NEEDS A DECISION** (see §3): renaming alone flips skip-green→fail-red because
  12/25 e2e files need `zcli` + VPN on the runner — either install zcli + bring VPN up, or run
  a VPN-free `-run` allowlist + canary.
**Gate:** build + test + lint; **fresh-checkout** `go test -short` shows the one corpus-guard
message (not 35); CI workflow validated.

---

## 2. What was dropped / deferred

| Item | Disposition | Reason |
|---|---|---|
| B10 batch progress race | **Dropped** | Refuted — `wg.Wait` + per-emitter post-emit sleep make coalescing structurally impossible; both fixes regress. |
| zerops-go upstream `fmt.Println` removal | **Complement to P1, separate release** | Needs a zerops-go tag + bump; local stdout guard ships independently. |
| Splitting `workflow_launch_production.go` (1385 LOC) | **Not in scope** | Cohesion-justified; maintidx passes. Optional later. |

## 3. Open decisions for Karel

1. **P0-6 e2e depth** — minimal (rename + canary, accept that VPN/zcli tests still skip on the
   runner but the job is now honestly red when the VPN-free subset breaks) vs full (install
   zcli + VPN on the runner for a real nightly run). Full is a mini-project; minimal is a
   correct stopgap that stops the false green. **Recommend minimal now + a tracked follow-up.**
2. **Phase ordering / scope** — all 7 phases, or a subset? Recommend **P0 → P1 → P2 → P3** as
   the high-value core (latest SDK + protocol integrity + platform correctness + destructive-
   gate integrity), then P4/P5/P6 as quality passes.
3. **bash_guard deletion** — needs Aleš's nod (his post-mortem origin) before C7 touches it.

## 4. Effort sketch

| Phase | Findings | ~LOC | ~Days |
|---|---|---|---|
| P0 SDK | 1 | ~5 | 0.25 |
| P1 protocol | 1 | ~60 + pin | 0.5 |
| P2 platform | 6 | ~200 + lint | 1.5 |
| P3 import/launch | 7 | ~350 | 2.5 |
| P4 parallel-path | 9 | ~400 | 2.5 |
| P5 contract/schema | 9 | ~250 + pins | 2 |
| P6 deadcode/test | 7 | ~300 (mostly deletion) | 1.5 |
| **Total** | **40** | **~1500** | **~11 days** |

Each phase ships as its own commit(s), build+test+lint+gate green before the next.

## 5. Codex review — verdict + integrated must-fixes

Independent senior review (Codex, 309k tokens, read the cited code). **Verdict: plan sound, but
do not hand to an LLM implementer unchanged.** 5 must-fix edits — all integrated above:

1. **P1 stdout** — redirect must be in `cmd/zcp/main.go` **before `run()`** (not inside
   `Server.Run`; `run()` does auth/`GetUserInfo` first), and the `IOTransport.Writer` must be a
   **no-op closer** around saved stdout (the SDK closes the writer via `ioConn.Close`). ✅
2. **P3 override** — **bundle-owned** `Override` flag in `runtimeEntryFromInput` (composer is the
   single YAML owner), not tools-side post-compose mutation; desired-kind from the service source. ✅
3. **B6** — full **caller matrix** (mount-resolve vs unmount-detect-deleted differ; subdomain/
   import/eval classify downstream) — not a global `FindService` change. ✅
4. **B4** — `jsonschema.For[WorkflowInput](nil)` + patch only the two fields; explicit list-tools
   schema test for workflow + browser FlexBool. ✅
5. **Shell AST lint** — narrowed to `ExecSSH(…, fmt.Sprintf(…))` + Sprintf-derived vars, with an
   allowlist of validating builders (broad "no Sprintf shell" would be noisy). ✅

Codex also **confirmed:** B10 correctly dropped (verified `wg.Wait` + terminal-before-emit);
P0-2 / P0-3 / B15 / B4 real; P0-6 correctly a decision. Per-high-risk-fix test guardrails (P1
subprocess-stdout test, P3 replace/managed/YAML-revalidate tests, P5 boolean+string schema test,
B3 metacharacter command-builder tests, P0-5 mixed-batch + FirstDeployedAt test) folded into the
phase gates. **Nice-to-have (integrated):** P2-before-P3 hard ordering, P2 split into two commits,
P0-5 may move to P2's tail.
