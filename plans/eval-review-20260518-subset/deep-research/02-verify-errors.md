# Deep-research 02 — Findings C (verify http_root) and F (SERVICE_NOT_FOUND wording)

Investigation against repo state at HEAD (`9186712a`); evidence cited as `path:line`.
Both findings are STILL-BROKEN per
`plans/eval-review-20260518-subset/synthesis.md:13` and `:57`.

---

## Finding C — `verify http_root` reports `pass` for HTTP 404

### Root cause (verified)

The pass criterion in `internal/ops/verify_checks.go:134` is
**`resp.StatusCode < 500`** — every 1xx/2xx/3xx/4xx response is reported as
`status: pass`. The doc-comment on lines `100-111` deliberately defends
this: it argues that http_root only asks "is the HTTP server alive?",
and that 4xx is legitimate for API-only services whose root path isn't
served. The pin at `internal/ops/verify_test.go:606-662`
(`TestCheckHTTPRoot_NonFailingStatuses`) explicitly locks **404, 401,
405 as pass**, with the test docstring stating: "The rule change was
made after every showcase run ever flagged apidev as 'degraded' because
/api/health works but / returns 404."

So this is not a bug-in-implementation; it's an intentional collapse of
two semantically distinct outcomes ("server responds 200 with content"
vs. "server responds 404 Cannot GET /") into a single signal (`pass`).
The wire shape carries `httpStatus: 404` + `bodyText: "Cannot GET /"`
faithfully, but the headline `status: pass` is what the agent reads
first, and the agent does treat it as proof of life — see the eval
result fixture at
`eval/results/scenario-20260502_162106/.../tool-calls.json:140`:

> `"checks":[{"name":"service_running","status":"pass"},...,{"name":"http_root","status":"pass","httpStatus":404,"bodyText":"Cannot GET /"}]`

…and the agent's self-review at
`plans/eval-review-20260518-subset/per-session/groupA-regression.md:47`
("Agent did NOT notice — self-review never mentions 404-as-pass").
This is a **design choice that proved wrong in practice**: the
"reachability oracle" framing in the comment does not survive contact
with agents who treat `status: pass` as success.

The artefact question (per CLAUDE.local.md problem-solving discipline):

- **Why does it exist?** Reaction to "every showcase run flagged apidev
  as degraded because / returns 404". The fix solved the false-fail by
  removing the signal entirely, instead of carrying both pieces of
  information forward distinctly.
- **Does it belong systemically?** Yes — generic verify SHOULD answer
  the aliveness question. But aliveness and responsiveness are
  different oracles, and merging them under one name is the bug.
- **Behavioral asymmetry surfaced by previous evals**: same check now
  fails (`fail / context deadline exceeded`) on dev-mode Next.js JIT-
  compile (`plans/eval-review-20260517-083949/synthesis.md:26`) — so
  the check **simultaneously over-passes (404 → pass) and over-fails
  (slow legit response → fail)**. Both stem from collapsing distinct
  signals into one boolean: the check is doing too many jobs.

### Parallel paths checked

| Path | Status |
|---|---|
| `internal/ops/http_ready.go:39+88` (`WaitHTTPReady`) | **Same `<500` convention** — used by deploy_subdomain.go to wait for L7 propagation. There the binary signal IS correct (we don't care WHAT responds, only THAT something does). Different consumer, same predicate. Should stay. |
| `internal/ops/verify.go:200-204` (subdomain-disabled branch) | Emits `http_root: fail` with subdomain Recovery when subdomain access is off but service is running. Carries structured Recovery shape — this is the model the 404 branch should follow. |
| `internal/ops/verify.go:192-197` (subdomain unresolved branch) | Emits `http_root: skip` with `detail: "cannot resolve subdomain URL"`. Reserved status for "URL not yet propagated"; pinned by `TestVerify_RuntimeSubdomainAccessUnresolved_SkipsHTTPRoot`. Confirms `skip` is reserved for transient, non-actionable causes. |
| `internal/content/atoms/develop-first-deploy-verify.md:16` | Atom tells agent: "Dev-mode dynamic runtimes deploy with `start: zsc noop --silent` — nothing is listening yet. `zerops_verify` will return `http_root: HTTP 502` and that is NOT a deploy failure." So the atom already KNOWS http_root is dual-purpose; the implementation just doesn't differentiate. |
| `internal/tools/verify.go:33` (tool Description) | "Returns structured results: service status, error logs, **startup detection, HTTP connectivity**." Promises "HTTP connectivity" — that's the aliveness framing. Description and impl agree; both are wrong about what agents need. |

### Blast radius

Renaming would touch these surfaces; bullet for each:

- `internal/ops/verify_checks.go:14-16, 100-144` — constants, checkHTTPRoot itself.
- `internal/ops/verify.go:140-150, 194, 200, 260, 265, 269` — six places naming `"http_root"` (the skip-and-replace machinery).
- `internal/ops/verify_test.go:188-195, 290-339, 387-410, 412-440, 600-720` — `TestCheckHTTPRoot_*`, `TestVerify_*HTTPRoot*` etc; tests pinning the current 404-as-pass and would-need-update.
- `internal/ops/verify_render_test.go:213-280` — render augmentation tests reference `checkHTTPRoot`.
- `internal/ops/verify_recovery_test.go:135-145` — subdomain Recovery path.
- `internal/tools/verify.go:107-126` (`classifyVerifyFailure`) — switches on `c.Name == "service_running"`. Other names fall through to `FailureClassVerify`. If the check is split, the same fall-through applies, no change needed.
- `internal/workflow/build_plan.go:297` (doc-comment example "http_root: 502").
- `internal/workflow/compute_envelope_test.go:671,700,706` — fixture text `"http_root: 502 Bad Gateway"` in attempt Summary/Reason. No semantic dependence on the name, just literal.
- `internal/workflow/render_test.go:296,308` — same shape, fixture text.
- `internal/tools/deploy_poll.go:55` doc-comment.
- `internal/content/atoms/develop-first-deploy-verify.md:16` — atom prose; would need rewording with the new check names.
- `internal/workflow/testdata/atom-goldens/develop/*.md` — three golden files reference the atom text.

### Fix shape

Option (a) — **gate `pass` on non-4xx/5xx and emit `fail` for 4xx with a clear semantic detail** — is the structurally correct shape, lowest churn, and aligns with the rest of the verify vocabulary. Key design moves:

1. In `checkHTTPRoot` at `verify_checks.go:134`: split the response branch.
   - 1xx-3xx → `status: pass, httpStatus: <code>`.
   - 4xx → `status: fail, httpStatus: <code>, detail: "HTTP <code> at /: <truncated body>. Server reachable but root path not served by this scaffold."`. **No Recovery** — this is descriptive, not actionable as a structured tool call (the agent should curl the actual app path).
   - 5xx → unchanged (existing fail branch).
2. Optionally add a Recovery hint pointing the agent at framework-aware paths
   (e.g. `Recovery: {Tool: "zerops_logs", ...}` or guidance to curl
   the recipe-known health path) — but this is opportunistic; the
   detail string is enough to break the false-pass.
3. Re-pin tests: `TestCheckHTTPRoot_NonFailingStatuses` becomes
   `TestCheckHTTPRoot_4xxFails_5xxFails_3xxPasses`. Update doc-comment
   on lines 100-111 — the new contract is "server alive AND root path
   served" rather than "server alive". Atom `develop-first-deploy-verify.md`
   updates the dev-mode line: instead of "http_root: HTTP 502" expect
   `http_root: fail, detail: HTTP 502`.

Option (b) — splitting into `http_reachable` + `http_responsive` — is
structurally cleaner long-term (preserves the original aliveness oracle
verbatim AND adds responsiveness). It is more invasive but matches the
parallel JIT-compile concern: `http_reachable` answers "TCP/HTTP came
back at all" (the JIT-compile case would still pass once the response
arrives), `http_responsive` answers "non-error response on /" (the
404 case fails). Recommend tabling this as a follow-up if the (a) fix
proves insufficient — start with (a) because the call-sites and atom
prose don't fan out.

**Invariant restored:** `http_root: pass` means "root path served a
non-error response", not "TCP came back". `bodyText` and `httpStatus`
remain on the wire shape for cases where the agent wants to see what
specifically responded.

### Risks / non-obvious

- **API-only services regression risk** — services that legitimately
  serve only `/api/*` and return 404 on `/` will now show `http_root:
  fail` again, recreating the 2026-04 "every showcase flagged
  degraded" surface that motivated the original change. Mitigation:
  Detail string + agent guidance that 4xx-with-API-scaffold-body is
  not a deploy failure; recipe-level health-path knowledge stays the
  authoritative oracle (matches the verify.go:175-186 doc-comment
  intent that workflow-specific paths belong to workflows, not
  generic verify).
- **The aggregate-status downgrade** at `internal/ops/verify.go:188-206`
  treats any `fail` (other than service_running) as `degraded`. 4xx
  with API-only-on-/ will downgrade overall status to `degraded`.
  Agents reading aggregate status alone will see "degraded" without
  reading the detail. Two options: keep degraded (forces atom-level
  guidance about reading `checks[].detail`), or introduce a
  CheckInfo-level outcome for 4xx (advisory, doesn't downgrade
  aggregate) — but the latter brings back the original confusion
  shape with a new name.
- **Dev-mode 502 contract** — atom-pinned at
  `develop-first-deploy-verify.md:16` says `http_root: HTTP 502` is
  expected and not a failure. Rewording is mandatory; missing the
  atom update means agents will still treat 502-as-fail (which it
  will be, correctly) but with no contextual "this is expected"
  carve-out.

### Verification test

Add `TestCheckHTTPRoot_4xx_FailsWithReachabilityDetail` to
`internal/ops/verify_checks_test.go`: probe a server returning
404 with body `Cannot GET /`, assert `status: fail`, `httpStatus:
404`, detail contains both `HTTP 404` AND the body excerpt. Update
`TestCheckHTTPRoot_NonFailingStatuses` to cover 200/301 only, and
remove 401/404/405 cases (or rename to `_3xxAnd2xxPass`). Drive
`TestVerify_DynamicRuntime_AllChecks` at
`internal/ops/verify_test.go:153` to expect `http_root: fail` when
the test handler returns 404 (the test currently calls
`http.DefaultClient` against a service that has no real server up,
so it already fails — but the test name and the
`TestCheckHTTPRoot_NonFailingStatuses` pin codify intent that needs
updating).

---

## Finding F — `SERVICE_NOT_FOUND: not bootstrapped` misleads at 3 handler sites

### Root cause (verified)

The bug is a **missed migration**. Commit `5478623c`
(2026-05-06, `git show --stat 5478623c`) split `ErrPrerequisiteMissing`
into `ErrAdoptRequired` to encode the semantic "service IS found in
Zerops, it just lacks ZCP bootstrap metadata" — naming the right
Recovery shape (`zerops_workflow action=start workflow=bootstrap
route=adopt`) in the code itself. The commit message lists exactly
three migration sites:

> - workflow_develop.go:113 — already had specific Recovery from H2 fix
> - workflow_develop.go:195 — errStandardPairStageMissing path
> - workflow_close_mode.go:90 — was using ErrServiceNotFound (wrong
>   semantic — service IS found, just unmanaged). Migrated.

Two parallel handlers in the same deploy-strategy decomposition Phase 5
were missed:

- `internal/tools/workflow_git_push_setup.go:79-83` (input `service`
  not in meta) — still emits `ErrServiceNotFound` with `"Service %q is
  not bootstrapped"` + generic `WithRecoveryStatus()` (status, not
  bootstrap+adopt).
- `internal/tools/workflow_build_integration.go:75-80` (same shape;
  same generic Recovery).

Wire-form rendered through `internal/tools/errwire.go:113-118`
(`WithRecoveryStatus`) is:

```json
{"code":"SERVICE_NOT_FOUND","error":"Service \"appstage\" is not bootstrapped",
 "suggestion":"Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\"",
 "recovery":{"tool":"zerops_workflow","action":"status"}}
```

The agent reads three problems:
1. `SERVICE_NOT_FOUND` code reads as "service doesn't exist".
2. `Suggestion` mentions `route=adopt` only IMPLICITLY (says "bootstrap"
   without `route=adopt`).
3. `Recovery` points at generic `action=status`, not at the actionable
   `action=start workflow=bootstrap route=adopt`.

Compare the correct shape at
`internal/tools/workflow_close_mode.go:93-101`:

```go
return convertError(platform.NewPlatformError(
    platform.ErrAdoptRequired,
    fmt.Sprintf("Service %q is not bootstrapped", hostname),
    "Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""),
    WithRecovery(&RecoveryHint{
        Tool:   "zerops_workflow",
        Action: "start",
        Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
    })), nil, nil
```

The launch-production-pipeline-not-configured eval transcript
(`plans/eval-review-20260518-subset/per-session/groupB-static-launch.md:57`)
shows the live impact:

> Self-review: "I assumed `build-integration` was the right verification
> call, and tried it on `appstage`. It failed with `SERVICE_NOT_FOUND:
> Service \"appstage\" is not bootstrapped`. That error message is
> misleading in context — the service exists (discover shows it
> ACTIVE), it's just not bootstrapped in ZCP."

There is a SECOND issue surfaced in the same session — the **cross-
project scope** problem:

> "More importantly, even if it had been bootstrapped, `build-integration`
> operates on services in this project. The launch-production workflow
> promoted things to a separate production project, and this ZCP is bound
> to the dev/stage project. There is no path from here into the production
> project's state."

But this is NOT what the SERVICE_NOT_FOUND error message is reporting.
The `FindServiceMeta` path
(`internal/workflow/service_meta.go` consumed via
`workflow_build_integration.go:68`) only knows whether ZCP-local meta
exists. A service in the prod project simply has no meta in the dev/
stage state dir — there's no platform call to disambiguate "doesn't
exist anywhere" vs. "exists in another project". The misleading
message blocks the agent from realizing this is a cross-project
problem; once the agent realizes, the actual recovery is "switch ZCP
session to prod token + project", which is **NOT** bootstrap+adopt.
So **two distinct root causes share one wire shape**:

1. (a) Local missed-migration: same shape as workflow_close_mode.go, fix is symmetric.
2. (b) Cross-project scope: `FindServiceMeta` returns nil because the
   service is in another project; bootstrap+adopt is the WRONG recovery.

### Parallel paths checked

| Site | Status |
|---|---|
| `internal/tools/workflow_close_mode.go:86-102` | **Correct**: `ErrAdoptRequired` + structured Recovery `{tool:zerops_workflow, action:start, args:{workflow:bootstrap, route:adopt}}`. Pinned by `TestErrAdoptRequiredCarriesAdoptRecovery` at `recovery_contract_test.go:67-83`. Reference implementation. |
| `internal/tools/workflow_develop.go:114-123` | Correct, same shape. Pinned by `recovery_contract_test.go:44-65`. |
| `internal/tools/workflow_develop.go:228-240` | Correct (`errStandardPairStageMissing` path), with a more-specific Suggestion. |
| `internal/tools/workflow_git_push_setup.go:79-83` | **Broken**: `ErrServiceNotFound` + `WithRecoveryStatus()`. |
| `internal/tools/workflow_build_integration.go:75-80` | **Broken**: identical shape to git_push_setup. |
| `internal/tools/workflow_git_push_setup.go:71-77` (meta-read failure) | Different concern — `meta, err := FindServiceMeta`. When `err != nil` (filesystem-level error), `ErrServiceNotFound` is overloaded; the actual `meta == nil` case at line 78 is the missing-meta path. The read-error path at lines 71-77 could arguably stay as a separate transient code, but unifying both at `ErrAdoptRequired` would be wrong (read error is not adopt-required). Keep distinct. |
| `internal/tools/workflow_build_integration.go:68-74` | Same shape — meta-read failure branch separate; only line 75-80 is the migration target. |
| `internal/tools/workflow_close_mode.go:79-85` | Same shape — meta-read failure branch; intentionally kept as ErrServiceNotFound (file I/O error, not missing-meta). |
| `grep -n "not bootstrapped" internal/tools/` | Exactly 3 hits, all in handlers — close-mode (correct), git-push-setup (broken), build-integration (broken). No other handlers emit this phrase. |
| `internal/tools/subdomain.go:122` | Doc-comment uses phrase descriptively — "ServiceMeta is missing (service not bootstrapped under the current session)". Not an error message; no migration needed. |
| `internal/tools/launch_source_read.go:82` | Different `ErrServiceNotFound` site (source-read on platform-side missing service). Distinct semantic; correct as-is. |

The 3-handler missed-migration is the precise gap described by the
`TestErrAdoptRequiredCarriesAdoptRecovery` docstring at
`recovery_contract_test.go:37`:

> The test drives each handler with input that triggers the rejection
> path. New ErrAdoptRequired sites added without a corresponding row
> here will fail when their Recovery shape diverges.

The test enumerates two handlers. The two broken sites are NOT in the
test today — because they aren't using `ErrAdoptRequired` to begin
with. The test pins post-migration sites; it cannot pin pre-migration
ones.

### Blast radius

| Layer | File | Change |
|---|---|---|
| Source | `internal/tools/workflow_git_push_setup.go:78-83` | Switch `ErrServiceNotFound` → `ErrAdoptRequired`; Suggestion adds `route="adopt"`; `WithRecoveryStatus` → `WithRecovery(&RecoveryHint{Tool: "zerops_workflow", Action: "start", Args: {workflow: "bootstrap", route: "adopt"}})`. |
| Source | `internal/tools/workflow_build_integration.go:75-80` | Same shape. |
| Test | `internal/tools/recovery_contract_test.go` | Add two table rows: `git_push_setup_unbootstrapped_service` and `build_integration_unbootstrapped_service` driving the new path. Pattern is identical to existing `close_mode_unbootstrapped_service` row at lines 67-83. |
| Atom | `internal/content/atoms/bootstrap-adopt-discover.md` (and any sibling atoms describing this Recovery shape) | Verify atom prose still matches; the migration shouldn't change atom content but should be sanity-checked. |

Cross-project scope (the second variant) is a **larger, separate**
fix — out of scope for the Finding F symmetry move. It needs:
- Either a platform read in `FindServiceMeta` to disambiguate "no
  meta locally" from "service is in another project entirely", which
  requires a `client.ListServices` call from layer 3 handlers (acceptable
  per CLAUDE.md "tools/eval reach platform via ops"), OR
- A guidance atom that fires when the meta is missing AND the handler
  is in a launch-production context, pointing at "switch ZCP context
  to prod project" rather than bootstrap+adopt.

Recommend tackling cross-project as a follow-up backlog entry; the
symmetric ErrAdoptRequired migration is the load-bearing fix.

### Fix shape

For each of the two broken sites, the change is mechanical and identical
to the existing `workflow_close_mode.go:93-101` correct shape:

```go
// Before (workflow_git_push_setup.go:79-83):
if meta == nil || !meta.IsComplete() {
    return convertError(platform.NewPlatformError(
        platform.ErrServiceNotFound,
        fmt.Sprintf("Service %q is not bootstrapped", input.Service),
        "Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\""),
        WithRecoveryStatus()), nil, nil
}

// After:
if meta == nil || !meta.IsComplete() {
    return convertError(platform.NewPlatformError(
        platform.ErrAdoptRequired,
        fmt.Sprintf("Service %q is not bootstrapped", input.Service),
        "Run bootstrap first: zerops_workflow action=\"start\" workflow=\"bootstrap\" route=\"adopt\""),
        WithRecovery(&RecoveryHint{
            Tool:   "zerops_workflow",
            Action: "start",
            Args:   map[string]string{"workflow": "bootstrap", "route": "adopt"},
        })), nil, nil
}
```

Extend `TestErrAdoptRequiredCarriesAdoptRecovery` table at
`recovery_contract_test.go:43-95` with two new drive functions for
git_push_setup and build_integration handlers; pattern is the
`close_mode_unbootstrapped_service` row at lines 67-83.

**Invariant restored:** Every "service exists but no ZCP meta" emit
site carries `ErrAdoptRequired` + the canonical bootstrap+adopt
Recovery. The existing pin (`TestErrAdoptRequiredCarriesAdoptRecovery`)
now covers all four real sites instead of two; any new "not
bootstrapped" emit added without the migration will be caught by the
test docstring's first-row template.

### Risks / non-obvious

- **Suggestion vs. code-name redundancy** — both the close-mode and
  the new sites carry the Recovery args (`workflow=bootstrap,
  route=adopt`) AND the Suggestion text mentions the same. This is
  intentional (one is structured, one is free text), but agents who
  parse only one will get the same message; no risk of drift.
- **Generic `ErrServiceNotFound` would be more accurate for cross-
  project case** — if Finding F variant (b) ever gets a real fix, the
  cross-project path will need a NEW code (e.g. `ErrServiceCrossProject`
  or `ErrServiceWrongScope`) so the agent can distinguish "doesn't
  exist" from "exists, wrong project" from "exists, no ZCP meta".
  The current symmetric migration doesn't paint anyone into a corner —
  it just brings two handlers into parity with their sibling.
- **One callsite already uses `ErrServiceNotFound` for the meta-read
  filesystem failure** above the missing-meta branch (lines 71-77 in
  git_push_setup.go). That's a transient "could not read the file"
  case — keep as ErrServiceNotFound; do NOT collapse with the
  missing-meta case at line 78. Distinct semantics.

### Verification test

The migration's invariant is already pinned: extend
`TestErrAdoptRequiredCarriesAdoptRecovery` at
`internal/tools/recovery_contract_test.go:37` with two new table rows:

1. `git_push_setup_unbootstrapped_service` — drive `handleGitPushSetup`
   with no ServiceMeta written for service `appdev`, assert wire
   response carries `code:ADOPT_REQUIRED` + structured Recovery
   `{tool:zerops_workflow, action:start, args:{workflow:bootstrap,
   route:adopt}}`.
2. `build_integration_unbootstrapped_service` — drive
   `handleBuildIntegration` with same no-meta fixture, same assertions.

Pattern is the existing `close_mode_unbootstrapped_service` row at
lines 67-83; the defensive assertion at lines 116-118 ("must NOT fall
back to generic status Recovery") will catch any future regression
that drops back to `WithRecoveryStatus()`.

---

## Summary

- **Priority: F before C.** F is a 30-line mechanical migration with an
  already-pinned invariant (`TestErrAdoptRequiredCarriesAdoptRecovery`)
  the existing tests already model; the fix is symmetry with a known
  correct implementation and won't surprise downstream. C is structurally
  correct but touches a deliberately-defended design choice in
  `verify_checks.go:100-111` and inverts a pinned test at
  `verify_test.go:606-662` — the design-pass cost is real, and the
  4xx-as-degraded fallout needs atom prose updates to avoid recreating
  the 2026-04 "every showcase degraded" surface that motivated the
  current shape.
- **The motivating frequency is reversed**: C hits agents on every
  scaffold deploy where root path is unserved (multi-session, every
  greenfield run), F hits the launch-production cross-project path
  (one observed session out of 17). C is hotter, but its fix is
  riskier — frame F as the warm-up and tackle C in the same window
  with deliberate test re-pinning.
- **Cross-cutting pattern: Recovery shape is the load-bearing detail.**
  Both findings reduce to "the structured Recovery hint is the agent's
  primary decision input"; C's option-(a) fix would benefit from
  attaching a structured Recovery to the 4xx-fail branch
  (e.g. pointer at zerops_logs or recipe-known-path guidance) for
  symmetry with the subdomain-disabled `http_root: fail` already at
  `verify.go:200-204`; F's fix IS the Recovery shape. The verify-
  check Recovery contract documented in CLAUDE.md's "Verify checks
  carry structured Recovery for actionable preconditions" invariant
  is exactly the shape both fixes should land on.
