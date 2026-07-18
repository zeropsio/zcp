# Audit Report — MCP Tools, Architecture, Tests (2026-06-09)

> Scope: all 25 registered MCP tools (`internal/tools/` + the `zerops_recipe` registration
> surface), the MCP SDK boundary (`convert`/`errwire`/`guard`/`server`), zerops-go SDK usage
> in `internal/platform/`, `internal/ops/`, architecture/coding-standards compliance, and
> test-suite quality (unit / tool / integration / e2e).
> Method: 9 parallel deep-read audit agents (every finding traced to ground — callee + test
> read before reporting), build/vet/lint/test runs, an empirical SDK-upgrade trial build,
> and a live SDK version check. The audit changed NO production code.
> Raw per-agent reports: `/tmp/audit-agent-results/*.md` (transient).
>
> Purpose: input for follow-up planning — unify tool quality, fix verified defects.

## 0. Executive summary

The codebase is in **much better shape than the "100% LLM-authored" prior suggests**: the
4-layer architecture holds (zero layering violations found, depguard + AST tests verified),
`go build`/`go vet`/full `golangci-lint` are clean, mock-layer tests are genuinely
trustworthy (19 files sampled, zero tautologies; meta-lints ship self-test fixtures), and
the deploy preflight / failure-classification / subdomain paths have converged on single
owners as the conventions demand.

The defects that DO exist cluster into four systemic patterns, not random bugs:

1. **A fix lands at one site and its parallel siblings are missed.** Lexicographic
   timestamp sort fixed in `logfetcher` Since-filter but not its sort, nor `events.go`;
   `timedOut` surfaced by manage/subdomain but discarded by scale/env/delete;
   `filterUserVisible` on the discover path but not the 5 other hostname resolvers;
   `shellQuote` on token/remoteURL but not setup/workingDir/branch/host;
   `CanonicalRepoURL` at 4 of 5 compare sites; recovery hints on local git-push, not
   container.
2. **Tell/check drift on agent-facing surfaces.** Tool Descriptions/jsonschema promise
   behavior the dispatch/validator rejects (workflow=recipe, set-default-setup,
   record_fact-on-develop, dev_server command example + status/noHttpProbe, FlexBool on
   zerops_workflow), and the meta-tests that should pin these have structural blind spots
   (annotations table covers 17/24 tools, description-drift lint skips non-literal
   descriptions — including both deploy tools).
3. **The real-platform seam has zero automated verification.** The nightly e2e CI job
   passes the wrong env var, so the whole suite silently skips (green = nothing ran);
   `internal/platform`'s real SDK wrappers, mounter and deployer are at 0% coverage.
4. **Dead code with confident false documentation** — ~8 orphaned surfaces whose doc
   comments assert consumers/behavior that don't exist (launch readiness rubric 170 LOC,
   bash_guard, workflow_immediate, three "orphan twin" wrappers, etc.).

**6 P0 bugs** (below), **~23 deduplicated P1s**, and an SDK upgrade that is verified
safe-zero-touch. Nothing found suggests architectural rework; everything is fixable in
bounded, verifiable phases (§9).

## 1. Dependency currency

| Dependency | Used | Latest | Verdict |
|---|---|---|---|
| `github.com/modelcontextprotocol/go-sdk` | v1.5.0 | **v1.6.1** (2026-05-22) | Upgrade available; **verified SAFE, zero-touch** (§7) |
| `github.com/zeropsio/zerops-go` | v1.0.20 | v1.0.20 (2026-05-19) | Current — but see P0-1 (SDK writes to stdout) |

go-sdk v1.5.0 → v1.6.1 changes: v1.6.0 — OAuth client-credentials handler, HTTP header
standardization, changed cross-origin defaults, `SetError` preserves existing content,
security fixes; v1.6.1 — `MCPGODEBUG` Content-Type escape hatch. All HTTP/SSE/OAuth-surface;
ZCP is stdio-only. Empirical trial build + full test run on v1.6.1: byte-identical results.

## 2. Tool-by-tool scorecard

| tool | implementation | docs | tests |
|---|---|---|---|
| zerops_discover | OK (meta-error swallow) | OK | rich |
| zerops_env | **issues** — get-scope P0, restart gaps, timeout discard | minor drift | combo untested |
| zerops_events | **issues** — sort, org-wide limit starvation | OK | weak ordering pins |
| zerops_logs | OK | OK | OK |
| zerops_scale | issues — timeout discarded | `%%` literals | OK |
| zerops_process | OK | OK | OK |
| zerops_subdomain | **OK — reference implementation of the cluster** | OK (drift-linted) | exemplary |
| zerops_mount | OK | OK | OK |
| zerops_manage | OK | OK | TimedOut unpinned |
| zerops_delete | issues — timeout discarded | OK | OK |
| zerops_browser | OK | OK | missing tool-layer tests |
| zerops_dev_server | **issues** — injection gap, SSH-error swallow | self-contradicting schema | missing tool-layer tests |
| zerops_deploy (local) | good | good | good |
| zerops_deploy (ssh/container) | good | good | strong (30+ behavior tests) |
| zerops_deploy_batch | **poor** — P0 no attempt recording, progress race | parity promise undelivered | weak (3 tests, no mixed batch) |
| deploy git-push (strategy) | fair — credential-contract bypass, quoting | good | good |
| deploy preflight/strategy-gate/subdomain | good — converged single owners | very good | very good |
| zerops_verify | good | good (ReadOnlyHint caveat) | good |
| zerops_import + override gate | good — gate recomputes live; window bug | good | strong |
| zerops_export | good | drift (3 items) | good; leak-test gap |
| launch-production (existing) | **P0 — replace never replaces** | promises unimplemented behavior | replace outcome untested |
| launch-production (pipeline/reset/gate) | good state machine; ack-scope hole, apiHost, promotables dead-end | stale headers | broad; cross-shape replay untested |
| zerops_knowledge | solid (5-mode exclusivity, nil-safe) | MODE 5 omitted; stale comments | strong |
| zerops_guidance | correct | hint points at blocked action | good |
| zerops_workflow (dispatcher) | dispatch complete; dead fall-throughs | recipe advertised+rejected; action missing from schema | broad; schema list unpinned |
| zerops_preprocess | correct | accurate | good |
| zerops_workspace_manifest | correct | minor error-message drift | good |
| zerops_record_fact | correct mechanics | promises develop; gate forbids it | good |
| zerops_recipe (registration surface only) | dispatch 15/15 | Description lists 12/15 actions | pinned in recipe pkg |

## 3. P0 — verified bugs

### P0-1: zerops-go SDK prints transport errors to stdout — corrupts the MCP stdio protocol
`~/go/pkg/mod/github.com/zeropsio/zerops-go@v1.0.20/sdkBase/utils.go::Method` contains
`fmt.Println("Method Request error:", …)` / `fmt.Println("Method Do error:", …)` on every
transport failure — the funnel for EVERY SDK call ZCP makes. ZCP's MCP server shares the
same stdout fd (`mcp.StdioTransport`, `internal/server/server.go:250`); no redirection
guard exists. Any connection-refused/DNS/timeout/VPN-drop mid-session (routine in local
mode, which requires VPN) injects a raw non-JSON line into the JSON-RPC stream —
transport-fatal per ZCP's own invariant. `TestNoStdoutOutsideJSONPath` scans only
`internal/`, so the dependency hole is structurally invisible.
**Fix:** at MCP-serve startup dup the real stdout fd for the transport and repoint
`os.Stdout` to stderr before constructing the SDK client; upstream a zerops-go fix; add a
pin so an SDK re-introducing stdout writes fails CI.

### P0-2: `GrantSelfRole` ignores the API-level result of `PutClientUserRoles`
`internal/platform/project_admin.go:259` discards the response value; in zerops-go the
second return is transport-only — HTTP 4xx/5xx surface only via `resp.Output()`. This is
the ONLY one of 30+ SDK call sites that drops the response (grep-verified). A 400/403 on
the role write returns nil; launch-production proceeds believing ADMIN was granted and
fails later, far from the cause. **Fix:** two-step check like the sibling call five lines
above + unit test injecting an API-error response.

### P0-3: launch-existing `replace` strategy collects a destructive ack but never replaces
`internal/tools/launch_existing_conflict.go:139` comments that the platform overwrites on
import — but the platform only overwrites when the service entry carries `override: true`
(ZCP's own `ops.Import` injects it for exactly this reason; the bundle composer never
does). The user is walked through the conflict prompt, supplies `confirmDestructive`, the
gate clears — then `ImportServices` rejects the colliding hostname. Worse:
`mutateProjectEnvs` runs BEFORE the import (`launch_existing.go:317`) → partial mutation
after a burned ack. For managed services replace can never work, yet the prompt offers it.
**Fix:** inject `override: true` for replace-acked runtime entries at compose; don't offer
replace for managed; pin the emitted YAML.

### P0-4: `zerops_env action=get project=true serviceHostname=X` silently returns null envs
`internal/tools/env.go:213` — with both params set, the handler runs a scoped Discover
with `includeProjectEnvs=false`, then projects the never-populated `result.Project.Envs` →
`"envs": null` with project identity attached and no warning. The agent concludes the
project has zero env vars and may re-set existing values. set/delete resolve the same
combo as project-scope — get diverges from its siblings. **Fix:** match EnvSet precedence
(clear hostname when project=true) or reject the combo; pin.

### P0-5: `zerops_deploy_batch` never records DeployAttempt — auto-close gate blind to batch deploys
`internal/tools/deploy_batch.go:139` — every sibling deploy path calls
`workflow.RecordDeployAttempt` on success AND failure; batch records nothing (`_ = engine
// reserved`). The tool Description recommends batch for "every 3-deploy cluster". After a
batch deploy: `needsDeploy` stays true, the auto-close gate can never fire,
`stampFirstDeployedAt` never runs (never-deployed atoms keep firing, staleVerify computed
against a missing timestamp), and the response's own `workSessionState.progress`
contradicts the per-entry DEPLOYED results beside it. **Fix:** record one attempt per
entry mirroring deploy_ssh; pin `ws.Deploys` + `FirstDeployedAt` after a mixed batch.

### P0-6: Nightly e2e CI passes the wrong env var — the entire e2e suite silently skips
`.github/workflows/e2e.yml:24` exports `ZEROPS_TOKEN`; every e2e test gates on
`ZCP_API_KEY` (`t.Skip` when missing). Nothing reads `ZEROPS_TOKEN`. Corroboration: the
job timeout is 5m while a single deploy test documents 900s — with a real key the suite
would time out, not pass. Combined with `internal/platform`'s 0% coverage of all real SDK
wrappers + mounter + deployer: **no automated process anywhere exercises the real Zerops
API**. **Fix:** rename the env, raise the timeout, add a canary failing the job when every
test skipped.

## 4. P1 — correctness risks / contract drift (deduplicated)

Root-caused groups first:

- **B1 — ES search scoping: server-side filter is clientId-only; projectID filtered
  client-side AFTER the limit.** One root cause (`platform/zerops_search.go`,
  `zerops.go::ListServices` sends no limit at all), three live symptoms:
  (a) the import-override gate's failed-history window (`failedContextLimit=10`) is
  account-scoped — 10 unrelated newer appVersions evict the target's failed history and
  the destructive gate silently passes (`ops/failed_context.go`);
  (b) `zerops_events` can be starved to an empty timeline by a busy sibling project
  during diagnosis;
  (c) `projectAdminClient.ListServices` runs on the org-wide launch key by design — in a
  large org, post-import verification sees a server-default-page truncated list.
  Fix once at the owner: add `projectId eq` ES term + explicit limits, keep client-side
  filters as defensive asserts.
- **B2 — Lexicographic timestamp sorting (two sites).** `ops/events.go:306` (merges 3 API
  sources — exactly the cross-response case) and `platform/logfetcher.go:205` (the sort —
  only the Since-filter was converted; the package's own api-tagged contract test proves
  lex compare fails at sub-second boundaries). Both also trim by limit AFTER the bad sort,
  so the genuinely-newest entry can be dropped. NOTE: the CLAUDE.md bullet "filterEntries
  uses time.Parse + time.Before only" overstates what shipped — update it with the fix.
- **B3 — Shell interpolation gaps in SSH command builders.** `shellQuote` is applied to
  token/email/remoteURL but missed for: `setup` + `workingDir` (`ops/deploy_ssh.go:264`),
  netrc `host` inside a double-quoted string + `branch` + `workingDir`
  (`ops/deploy_git_push.go:43-62`, `git_auth_probe.go`), tools-side `workingDir`
  (`tools/deploy_git_push.go:49,215`), and `healthPath` — the only dev_server input
  neither regex-validated nor quoted (`ops/dev_server_start.go:317`). `branch` is a raw
  MCP input; recipe-session deploys (meta=nil) skip the preflight that would resolve
  `setup`. Direct violation of a stated convention with no lint pin (§8).
- **B4 — FlexBool tolerance is dead on `zerops_workflow`** — the only FlexBool-carrying
  tool registered WITHOUT an explicit InputSchema; the inferred schema publishes
  `"type":"boolean"` and the SDK validates BEFORE UnmarshalJSON. Reproduced end-to-end:
  `force="true"` → schema rejection, the exact v7-postmortem class FlexBool was built to
  eliminate. The guard test pins field TYPES, never schema wiring. (`workflow.go:304`;
  also `zerops_browser` registers `ops.BrowserBatchInput` with a raw `bool`, escaping the
  guard's claimed "every *Input" coverage.)
- **B5 — Poll-timeout silently discarded by scale / env / delete** — `pollManageProcess`
  returns `timedOut`; manage + subdomain surface it (subdomain's comment documents the bug
  class), three siblings discard it: stale process state returned as success with
  success-shaped NextActions; delete also silently skips ServiceMeta cleanup
  (`scale.go:69`, `env.go:224,236,316`, `delete.go:58`).
- **B6 — System services resolvable + hostnames leaked by every non-discover resolver** —
  `filterUserVisible` exists on the discover path (2026-05-27 leak fix) but
  `ops/helpers.go::FindService` consumers (delete, manage, scale, subdomain, mount)
  resolve against unfiltered ListServices: not-found suggestions list `core`/L7 hostnames,
  and `zerops_delete serviceHostname=core` reaches the platform API.
- **B7 — env auto-restart treats RUNNING as not-live** — `env.go:459,470` accept only
  `ACTIVE`; the codebase's liveness predicate elsewhere is RUNNING||ACTIVE. A RUNNING
  service keeps its boot env indefinitely while the warning says "will apply on next
  start" (no action needed) — wrong on both counts.
- **B8 — EnvSet upsert is delete-then-create** — create failure after delete destroys the
  existing value (e.g. a credential) unrecoverably; multi-pair calls leave partial state
  (`ops/env.go:155-210`).
- **B9 — dev_server schema contradicts its validator twice** — (a) the `command` field's
  own example `'PORT=3000 npm run dev'` is rejected by `devShellEnvPrefixRe` (the tool
  Description carries the opposite, correct guidance); (b) `port` claims status is exempt
  under `noHttpProbe=true` but `statusDevServer` always requires a port — workers (the
  flag's exact audience) have no status path at all.
- **B10 — deploy_batch parallel polls can violate the progress-before-response invariant**
  — the teardown guard is per poll loop; batch shares one `onProgress` across N goroutines,
  so a sibling can emit microseconds before the last terminal return → the documented
  Claude Code TS coalescing teardown, stochastic and unpinned (`deploy_batch.go:126`).
- **B11 — GIT_TOKEN-missing response bypasses the credential-contract single owner** —
  returned as a non-error payload that never passes `convertError`, so
  `appendCredentialContract` never fires; the instructions even contain the
  fabrication-inducing phrasing without the "NEVER generate a token" sentence
  (`deploy_git_push.go:356`).
- **B12 — env classification accepts any string as a bucket** — `needsClassifyPrompt`
  checks key PRESENCE only; a typo (`"secret"`, `""`) routes a credential verbatim into a
  publish-ready bundle / prod project env (non-sensitive). `topology.
  SecretClassificationValues()` has zero production consumers — a validation set with no
  validator. The service-env path got the safe default (REPLACE_ME); project-env paths
  diverged (`classify_envs.go:61`, `ops/bundle/helpers.go:94`).
- **B13 — launch-reset ack scope hole** — `ValidateDestructiveAck` compares
  Operation+Targets only; an ack minted from a state-file-only refusal validates a later
  call that adds `launchKey` and deletes a real project the agent never saw in any
  `wouldDestroy.projects[]` (`launch_reset.go:112`, `destructive_ack.go:104`).
- **B14 — Promotables-only launch dead-ends at publish** — scope/classify/ready-to-launch
  accept it; both mutation paths hard-refuse `TargetService==""` AFTER the launchKey is
  minted (`workflow_launch_production.go:1055` vs `:868`).
- **B15 — launch-side credential clients hardcode `apiHost=""`** — launchKey /
  existingProdToken / reset clients always resolve to the canonical host; on a
  non-canonical instance the token goes to the wrong API and reset can never reach the
  orphan (`workflow_launch_production.go:463,514`, `launch_reset.go:137`,
  `launch_existing.go:137`).
- **B16 — `mergeZeropsYAMLBodiesForRefs` concatenation breaks YAML parsing** — the
  comment claims the ref scanner is line-based; it is `yaml.Unmarshal`, and two bodies
  with top-level `zerops:` produce a duplicate-key error → silent empty ref set: the M2
  indirect-infra detector goes blind and false "nothing references ${x_*}" warnings fire
  for any 2+-runtime launch with distinct bodies (`ops/bundle/launch.go:270`; verified
  empirically).
- **B17 — Adoption gate emits SERVICE_NOT_FOUND where ADOPT_REQUIRED exists** —
  `guard.go:124` predates the `ErrAdoptRequired` split and was never converted; discover's
  warning promises ADOPT_REQUIRED; the container branch also lacks structured Recovery.
- **B18 — zerops_workflow Description/schema drift (3 items)** — advertises
  `workflow="recipe"` that handleStart hard-rejects (3 surfaces contradict);
  `set-default-setup` dispatched but absent from the Action schema enum (nothing pins the
  jsonschema list); unknown-workflow error omits launch-production (the typo-recovery
  surface denies a real workflow).
- **B19 — record_fact promises develop-workflow support; the gate makes develop
  impossible** — requires a v2 engine SessionID; production develop is stateless. Three
  surfaces (Description, error text, gate) disagree pairwise (`record_fact.go:71,132`).
- **B20 — Test suite is non-hermetic** — 35 tests across knowledge/recipe/tools fail on
  any checkout without `zcp sync pull`, with content-shaped (not precondition-shaped)
  failures. Verified root cause: gitignored synced corpus + `go:embed`. (This worktree was
  red until the corpus was copied from the main checkout during this audit.)
- **B21 — `TestAnnotations_AllToolsHaveTitleAndAnnotations` covers 17 of 24 tools** — no
  completeness loop; missing browser, guidance, preprocess, record_fact,
  workspace_manifest, yml_exists, deploy_batch. Wrong destructive/read-only hints ship
  green; annotations drive client auto-approve UX.
- **B22 — Launch readiness rubric (170 LOC) shipped but never wired** — zero production
  callers; docs claim it feeds the launch response `checks[]` (no such field); CLAUDE.md
  cites `readinessCheckSubdomainDisabled` as a pin — the reference dangles
  (`launch_readiness.go`).
- **B23 — Poll timeout records a FAILED DeployAttempt for a still-running build** — a
  legitimate >15-min build lands in history as "Last attempt failed: deploy status
  BUILD_TRIGGERED" and `needsDeploy` directs a redeploy on top of the in-flight build
  (`deploy_ssh.go:246`, `deploy_local.go:172`).

## 5. P2 — selected quality findings (by theme)

**Dead code with false documentation** (violates the repo's own clean-code standard):
`workflow_immediate.go` + both dispatcher fall-throughs (immediate set is empty — export
forks earlier); `bash_guard.go::CheckBashCommand` (80 LOC + 19 tests, "not interceptable"
per its own doc; Aleš-adjacent — flag before deleting); `stale_meta_setup.go` + wire type;
`ops/env_local_overlay.go::EnsureEnvLocal`; orphan twins kept alive only by tests
(`ops.Verify`/`VerifyAll`, `workflow.HasSuccessfulDeploy`, `AutoCloseProgressFor`) — tests
pin the wrapper, not the production path; dead funcs with fictional consumers
(`AtomExists`, `FormatOfferings`, `RenderDiff`, `effectiveProdSetupName`,
`httpStatusFromPlatformCode` with a false `//nolint:unused` justification,
`knowledge.uriToPath`); `server.CurrentPID`; `platform.CreateOpts` accepted + documented +
ignored; `LogAccess.AccessToken/Expiration` dead fields.

**Doc/comment drift** (tell ≠ check): deploy-failure `Strategy` doc says
"push-dev/push-git/manual", live labels are `zcli`/`git-push`/`record-deploy` — a signal
authored from the doc is dead on arrival (2 sites); stale "Phase 5 will swap this"
(already swapped + pinned); knowledge.go Mode-N comments contradict the file's own
taxonomy; zerops_knowledge Description lists 4 of 5 modes; zerops_recipe Description lists
12 of 15 actions; export drift warning tells the agent the opposite of what the bundle
does; `EnvClassifications` schema documents the platform-rejected `<@pickRandom(...)>`
emission already replaced by literal REPLACE_ME; launch_reset header claims "no
ProjectAdminClient construction" (false since orphan-delete); guidance unknown-topic hint
points at an action blocked since v9.0.1; `jsonschema:"required,…"` misunderstands the tag
grammar — "required," ships as description prose on 5 fields (requiredness actually
derives from omitempty absence — correct by accident).

**MCP boundary consistency**: hand-written schemas (`objectSchema`) leave
`additionalProperties` unset while inferred schemas emit `false` — the 6 explicit-schema
tools (incl. both deploy variants) silently drop typo'd keys that 18 inferred tools
reject; guidance returns plain-text "Error: …" with IsError=false on 3 paths;
description-drift lint skips `Description: <Ident>`/BinaryExpr (both deploy tools
unscanned); duplicate `statusActive` aliases; `jsonResult` marshal-failure emits plain
text vs convertError's typed fallback.

**Platform/ops robustness**: DM-2 validation silently skipped when the SSHFS mount can't
be stat'd — the one self-destruction guard bypassable by a transient mount state
(`ops/deploy_ssh.go:162`; low frequency, high consequence); subdomain URL composer emits
non-HTTP ports (diverged from verify's PreferredHTTPPort fix); dev_server status/logs
swallow SSH transport errors and report "not listening" when the probe never ran; start
(<400) vs status (<500) disagree on what "running" means; 404 mapping has no "project"
branch (stale project → "Check service hostname"); `getClientID` holds the client mutex
across a 30s network call (Do-NOT rule) with a comment claiming retries it doesn't do;
mounter discards all command output (MOUNT_FAILED reaches the agent with zero diagnosis —
the B6 lesson unapplied); context cancellation flattened to generic API_ERROR +
PlatformError severs the Unwrap chain; `ssh_ready` test-override unguarded while
`http_ready` is mutex-guarded (-race gate risk); local git-push origin compare is raw
bytes, not CanonicalRepoURL (`.git` suffix false-refuses); container git-push omits
`WithRecoveryStatus` on 6 error paths; env auto-restart calls `client.RestartService`
directly from tools (the lone ops-bypass mutation; the AST pin covers only 3 read
methods); classify-prompt warning can embed a full sentinel-matched secret value (e.g.
`sk_test_…`); service-env SECRET keys never appear in any classify prompt despite the
comment claiming they do.

**Test gaps**: mock `ImportServices` never materializes services
(`TestIntegration_ImportThenDiscover` passes byte-identically without the import call);
mock not-found errors are plain `fmt.Errorf`, not `*PlatformError` (error-code branching
untestable on default paths); mock Search* ignore projectID/limit/ordering (the
destructive-gate's "latest" semantics tested only against fixture order);
`CheckSymbolContractEnvVarConsistency` — an entire production check at 0% coverage; e2e
orphan cleanup deletes by 2-letter hostname prefixes (`in`, `ba` — `inventory`, `backend`
in whatever project the key points at); 5 specialized e2e tests permanently dormant (env
gates nothing ever sets — incl. the riskiest launch-into-existing path); browser +
dev_server have zero handler-level tests (11 hand-copied fields unpinned); `manage`
TimedOut serialization unpinned; one flake observed under full-suite load
(`TestDeployIntoDerivedClosedSession_SucceedsAndRecords` — failed once, passed 6×
after; uses real-time deps worth a look).

## 6. Test suite verdict

Coverage (-short): tools 77.6% · ops 84.6% · ops/bundle 85.2% · ops/checks 77.4% ·
workflow 82.8% · **platform 33.7%** (all real SDK wrappers, mounter, deployer at 0% — live
seam covered only by build-tagged + e2e tests that never run automatically) ·
ops/inventory 0%.

e2e/integration map: workflow/import/discover/deploy/delete/subdomain/export/env/process/
events/verify have e2e tests (all theoretical until P0-6 is fixed); knowledge/mount/scale/
manage/logs integration-only; browser/dev_server/deploy_batch/guidance/preprocess/
record_fact/workspace_manifest/yml_exists/recipe tool-layer-mock only (mitigated: tool
tests run through a real in-memory MCP server).

**Verdict: trust the green for handler/orchestration/wire-shape correctness — 19 sampled
files, zero tautologies; meta-lints ship self-test fixtures and cannot silently go blind;
naming holds at ~93% of 2,496 test functions; parallelism discipline is documented per
test. Do NOT read green as evidence the binary works against the live platform: that
evidence today comes only from manual e2e runs and flow-evals.**

## 7. MCP SDK upgrade v1.5.0 → v1.6.1

**SAFE — zero-touch.** Verified empirically (trial build + full test run via alternate
modfile: byte-identical results). ZCP imports only `go-sdk/mcp`, uses StdioTransport +
InMemory; no HTTP/SSE/OAuth/resources surface. `SetError`: zero direct calls; handlers
never return Go errors (leaf-payload invariant), so the behavior change is unreachable.
jsonschema-go bumps v0.4.2→v0.4.3 (benign; no test pins validation strings).
Migration: `go get github.com/modelcontextprotocol/go-sdk@v1.6.1 && go mod tidy` + test +
lint. FYI courtesy to Aleš (shared registration path; behavior-transparent per tests).

## 8. Architecture & standards compliance

Build PASS · vet PASS · full golangci-lint 0 issues · atom gates green (122 atoms).
Layering: all 6 CLAUDE.md rules verified clean by grep + depguard + architecture test.

**Enforcement gaps** (convention stated, nothing pins it):
1. depguard's `ops-not-workflow` glob covers only direct children — `ops/bundle/` +
   `ops/inventory/` rely solely on the architecture test (the claimed redundancy doesn't
   exist for sub-packages). Fix: `**/internal/ops/**/*.go` + checks carve-out.
2. "No panic()" — unpinned; 3 production sites (documented programmer-error guards; one
   could move to a _test.go file).
3. "Never bare `return err`" — wrapcheck off; 89 sites (worst: `internal/init/`).
4. "shellQuote for shell" — unpinned; live violations (B3). AST-lint candidate.
5. "No global mutable state" — pinned only for tools/; ops//service//recipe/ carry
   mutable test-seam vars. Scope the rule text or pin with an allowlist.
6. `go mod tidy` drift: BurntSushi/toml is direct but declared indirect.
7. CLAUDE.md drift: the logfetcher parse-compare bullet overstates the fix (B2); the
   `readinessCheckSubdomainDisabled` pin reference points at dead code (B22).
8. FlexBool guard pins field types, not published schemas (B4); annotations test has no
   completeness loop (B21); description-drift lint has structural blind spots.

Files >800 lines: only `workflow_launch_production.go` (1385) is a real split candidate
(along the existing `launch_*.go` family); the rest are Aleš's scope, the frozen v2
cluster, or cohesion-justified (maintidx passes repo-wide).

## 9. Recommended next steps (prioritized, phased)

Each phase independently verifiable; ordering by user-facing risk:

1. **Protocol + destructive-gate integrity (P0s):** stdout guard for the SDK (P0-1),
   GrantSelfRole response check (P0-2), launch-replace `override:true` (P0-3), env-get
   combo (P0-4), batch DeployAttempt recording (P0-5), e2e workflow env var (P0-6).
   Small, isolated fixes; each with a pinning test.
2. **Single-owner scoping fixes:** ES search projectId term (B1 — clears the import-gate
   bypass, events starvation, and admin truncation at once); parse-compare sort at both
   sites (B2); shellQuote sweep + AST lint (B3).
3. **Agent-contract repairs:** FlexBool schema wiring + guard extension (B4); timedOut
   propagation in scale/env/delete (B5); FindService filtering (B6); RUNNING||ACTIVE
   liveness owner (B7); EnvSet ordering (B8); adoption-gate error code (B17); credential
   contract on the git-push branch (B11); dev_server schema/status reconciliation (B9).
4. **Docs/schema drift batch:** B18, B19, knowledge/recipe Description fixes, stale
   comments, `%%` literals, `required,` prefixes — mechanical, one PR, plus extend the
   drift lint + annotations completeness loop so the class stays fixed (meta-test
   hardening is the part that lasts).
5. **Launch hardening:** ack scope-binding (B13), promotables dead-end (B14), apiHost
   threading (B15), YAML ref-scan per body (B16), classification bucket validation (B12),
   readiness rubric wire-or-delete (B22).
6. **Dead-code sweep** (§5 list; git preserves history; bash_guard → flag to Aleš first).
7. **Test infrastructure:** hermetic-corpus guard (B20), e2e cleanup safety, mock
   fidelity (import materialization, PlatformError shapes, search ordering), dormant-e2e
   decision, platform-seam coverage strategy (the honest fix for "0% real-API
   verification" is making P0-6's nightly job actually run).
8. **SDK upgrade** to go-sdk v1.6.1 (verified safe; any time, ideally its own commit).

Items NOT recommended: architectural rework (layering is healthy), wholesale test rewrite
(mock-layer suites are good), splitting files beyond `workflow_launch_production.go`.
