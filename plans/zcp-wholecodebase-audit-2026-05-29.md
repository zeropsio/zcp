# ZCP Whole-Codebase Audit — Head Programmer Report (2026-05-29)

Method: dynamic workflow, 24 subsystem deep-read auditors over all of `internal/` + `cmd/`
(~295k LOC non-test), then an adversarial verifier per critical/high finding (default-refute
unless the real code confirms), then synthesis. 46 agents, ~4.2M tokens.
Counts: **35 raw → 13 confirmed, 8 refuted, 14 med/low passthrough.**
Top-8 criticals spot-verified by hand against the real code before this report.

> Audit-workflow rule: **no edits without approval.** This doc is findings + proposed fixes;
> Karel picks what to action. Recipe-scope findings (Aleš) are flagged "refer", not actioned.

---

## Codex cross-check (2026-05-29)

The full report was re-verified by Codex (GPT-5-class) against the live source, independently of
the Claude audit. Result: **all 8 ranked criticals AGREE** (real bugs); **all 8 refuted dismissals
AGREE**; **no missed criticals** in the hot files. Net adjustments folded into this doc:

| Item | Codex | Action taken |
|---|---|---|
| #2 git-push stamp | PARTIAL | Fix broadened: only stamp on terminal `ProcessStatusFinished`, not just "no timeout" (a restart can complete as FAILED/CANCELED). |
| #5 phantom-tree walk | PARTIAL | Fix broadened: also split ENOENT-vs-real-error at the top-level `os.Stat(mountRoot)`, not only in the WalkDir callback. |
| B2 parseSemver | PARTIAL | Fix changed: report validity + "no update" on invalid version in the *comparison*; `NewChecker` can't return error. |
| **B4 launch `source.RepoURL`** | **DISAGREE** | **Removed — false positive.** `_ = source` is a redundant no-op, not a clear; `source` still holds its value and the gate at `:875` guarantees `RepoURL != ""`, so `:714` reads the real URL. (The misleading comment at `:530` tripped the audit agent.) Hand-confirmed. |
| **D2 `stateDir` param** | **DISAGREE** | **Removed — false positive.** `stateDir` IS used (`WriteServiceMeta(stateDir, meta)` at `:312`). Hand-confirmed. |
| D4 lookupDetail | PARTIAL | Re-categorized: latent smell, not dead-code; tests rely on miss-returns-empty. |

Codex's quality verdict: *"directionally useful, most ranked criticals are real; overclaims in the
lower/dead-code section (two false positives), a few fixes too narrow. A strong triage artifact, not
implementation-ready without source-level verification."* — which is exactly the cross-check it got.

---

## Executive verdict

Healthy codebase. Architecture discipline (layer boundaries, check-before-mutate,
progress-emit ordering, atomic writes, pair-keyed meta) holds up under scrutiny — most
subsystems came back clean. The criticals are **not architectural rot**; they're a single
recurring local discipline gap (`_ =` swallowing of load-bearing errors/status) plus one
HTTP-readiness threshold-drift pair. Every confirmed critical has a low-risk, localized
root-cause fix; none reshapes a public surface. Two (#1 stage-lock, #5 phantom-tree) want a
new pinning test before merge — the missing test is *how the bug slipped*.

**Dominant systemic pattern: silent `_ =` discard at I/O / validation boundaries — the
direct cause of 5 of the 8 high-impact bugs.** The tell: a `_ =` on a meaningful error/status
while a *sibling* call in the same function checks it.

---

## Critical bugs (ranked — act top-down)

### 1. Stage hostname lock silently bypassed → concurrent-bootstrap state corruption
`internal/workflow/engine.go:726` (`checkHostnameLocks`)
The loop iterates **both** dev + stage hostnames (720-724) but resolves each via
`ReadServiceMeta` (726), which only reads `services/{hostname}.json`. Stage halves have **no
direct file** (pair-keyed: stage is a field on the dev meta), so it returns `(nil,nil)` →
`continue` → lock skipped. Two sessions can bootstrap the same stage hostname concurrently,
both writing incomplete metas. Only lock-skip path in the codebase.
**Fix:** swap `ReadServiceMeta` → `FindServiceMeta` (`service_meta.go:541`, already pair-aware:
direct file then scan for `StageHostname` match). Brings one straggler to parity.
**Risk:** low (one identifier) but touches the pair-keying invariant — **add a stage-half
lock test** (none exists; that's the gap that hid this).
Verified: `FindServiceMeta` confirmed pair-aware.

### 2. `git-push-setup` stamps `configured` even when the restart poll times out
`internal/tools/workflow_git_push_setup.go:427`
`_, _ = pollManageProcess(ctx, client, restartProc, nil)` discards both status and fail flag,
then line 430 unconditionally sets `meta.GitPushState = GitPushConfigured`. If restart never
completes, `GIT_TOKEN` never lands in shell, but meta says "configured" → next
`deploy strategy=git-push` SSHes in, token absent, agent gets a cryptic zcli auth failure with
no path back. The two error paths *directly above* (LookupService, RestartService) **do** handle
errors with `WithRecoveryStatus()` — the asymmetry is the smoking gun.
**Fix (broadened per Codex cross-check):** capture BOTH returns — `finalProc, pollFailed := pollManageProcess(...)` — and only stamp `GitPushConfigured` when `!pollFailed && finalProc.Status == platform.ProcessStatusFinished`. `pollManageProcess` treats *any* terminal status as "done", so checking only the timeout/error flag would still stamp on a restart that completed as FAILED/CANCELED. Refuse + `WithRecoveryStatus()` otherwise.
**Risk:** low — additive, mirrors the adjacent handling.

### 3. `sync push`: `zerops.yaml` commit failures swallowed → incomplete PR ships as `Created`
`internal/sync/push_recipes.go:222, 225`
Both `zerops.yaml` `UpdateFile` calls are `_ = gh.UpdateFile(...)`. README one line up (211)
checks the error and returns `Status:Error`. If the `zerops.yaml` API call fails
(auth/rate-limit/net), the fn proceeds to `CreatePR` and returns `Status:Created` with a valid
PR URL — README updated, `zerops.yaml` stale, no caller signal. Silent infra drift in the
published recipe app repo.
**Fix:** `if err := gh.UpdateFile(...); err != nil { return PushResult{Slug:slug, Status:Error, Err: fmt.Errorf("update zerops.yaml: %w", err)} }` in both branches; keep the size-guard conditional intact.
**Risk:** low — symmetric to existing README handling.

### 4. Cross-service env-ref resolution inverts precedence (service var beats yaml-baked)
`internal/ops/env_generate.go:239` (consumed by `findEnvValue`, 492)
Cache built as `envs = append(envs, yb...)` (service vars, then yaml-baked). `findEnvValue`
returns **first** match → on a key collision the **service-level** var wins. But the codebase's
own authoritative comment (`env_effective.go:53-54`) states container precedence is
`project < service userData/secret < yaml-baked run.envVariables` — yaml-baked is **highest**.
So generated `.env`/validation resolves to a value the container does **not** use.
**Fix:** `envs = append(yb, envs...)` — prepend yaml-baked so first-match honors real precedence.
Lookup-only cache, zero side effects.
**Risk:** low — one-line reorder. Add a colliding-key fixture (none exercises the conflict).
Verified: precedence direction confirmed against the codebase's own precedence comment.

### 5. Recipe phantom-tree check false-negatives on inaccessible paths → stale content publishes
`internal/tools/workflow_checks_canonical_output_tree.go:53-56`
Walk error discarded twice: top-level `_ = filepath.WalkDir(...)` and callback returns `nil`
on **any** `walkErr`. The nolint claims "missing files = no phantom," but permission-denied /
stale SSHFS / broken symlink are inaccessibility, not absence. A phantom dir behind an
unreadable path makes the gate pass → finalization succeeds despite a violation → stale publish.
**Fix (broadened per Codex cross-check):** in callback `if walkErr != nil { if !os.IsNotExist(walkErr) { return walkErr }; return nil }`;
capture the `WalkDir` return and emit `statusFail` when non-nil. **Also** apply the same
ENOENT-vs-real-error split at the top-level `os.Stat(mountRoot)` site (a non-ENOENT stat failure
must fail the check too). Continue only on confirmed absence.
**Risk:** medium — changes walk contract + adds error branch on a publish-time gate; error case
untested. Strictly toward correctness (false-pass → fail). Add a permission-denied fixture.

### 6. `statusDevServer` accepts 4xx as `Running=true`
`internal/ops/dev_server_lifecycle.go:244`
`result.Running = httpCode >= 200 && httpCode < 500` marks 400/404/429 as healthy. The real
probe (`dev_server_start.go:316`) and `checkHTTPRoot` (`verify_checks.go:150`) both use `< 400`.
A 4xx = up-but-rejecting = not ready. Agents polling status treat a broken endpoint as healthy.
**Fix:** `< 400`. Better: extract a shared `httpReadyCode()` predicate (see pattern #2).
**Risk:** low — no test covers the 4xx case; add one while fixing.

### 7. Editorial-review delta check rejects the authorized `ambiguous` outcome
`internal/workflow/editorial_review_checks.go:115`
Accepts a row only when `Final == ReviewerSaid` (or writer/reviewer pre-agreed). The flow
authorizes `Final == "ambiguous"` for genuinely debatable classifications, but the check treats
it as unresolved disagreement and fails validation — blocking `close.editorial-review`
attestation exactly when the reviewer correctly used the authorized escape hatch.
**Fix:** `if row.Final == row.ReviewerSaid || row.Final == "ambiguous" { continue }`. Purely additive.
**Risk:** low — confirm the token matches the return payload; pin the ambiguous case (none today).

### 8. Binary auto-update can leave a non-executable binary (cross-FS fallback)
`internal/update/apply.go:23` (`copyFile`) + `:119` (fallback call site)
Atomic-rename path chmods the temp file (105). The cross-FS fallback uses `copyFile`, which
opens `dst` with `os.O_WRONLY|os.O_TRUNC, 0o755` — on an **existing** file the mode arg is
ignored (only applies on `O_CREATE` creation), so `dst` keeps its prior bits, never chmod'd.
If not already `+x`, the next `zcp` invocation fails permission-denied — broken update shipped
to users who update across a FS boundary. (Note: the finding's "add O_CREATE" alternative does
**not** fix this — dst exists; explicit chmod is correct.)
**Fix:** `os.Chmod(dst, 0o755)` before `copyFile`'s final return, mirroring line 105.
**Risk:** low — single guarded chmod on a rare path; common path unchanged.

### Below the line (confirmed, lower blast radius)
- **eval-analyze workflow unmarshal** `analyze/session.go:356` (high): the one switch case doing
  `_ = json.Unmarshal` while Bash/Edit/Write/Agent all guard. Malformed input → zero-valued
  Action/Step/Substep silently enter the scan → wrong eval metrics. Pure consistency fix.
- **parseSemver** `update/check.go:209/212/215` (high): all three `Atoi` errors discarded →
  `foo.bar.baz` parses as `0.0.0` → any real version is "newer" → spurious forced auto-update.
  Fix (per Codex cross-check): make semver parsing report validity and have the *comparison*
  return "no update" on an invalid current/latest version (keeping the explicit `current=="dev"`
  skip). Validating in `NewChecker` is awkward — it currently can't return an error.
- **mock CancelProcess fidelity** `mock_methods.go:426` (high): returns wire `CANCELLED`; real
  `mapProcess` normalizes to `CANCELED`. Fix: `p.Status = ProcessStatusCanceled` (**bare**
  constant — mock is in `package platform`; finding's `platform.` qualifier is wrong).

---

## Systemic patterns

1. **Silent `_ =` discard at I/O / validation boundaries** — root-cause class behind the
   highest-impact bugs. Sites: #2 git-push poll, #3 sync zerops.yaml, #5 phantom-tree walk,
   eval-analyze unmarshal, parseSemver Atoi. **Action:** one sweep — grep `_ = `, `, _ :=`,
   `_, _ :=` across `internal/`; triage each against "does the equivalent sibling check this?";
   handle load-bearing ones, document genuinely best-effort ones with a *why*-nolint. The
   eval-analyze + sync cases are pure "make the odd one match its siblings" — ship with criticals.

2. **Divergent HTTP-readiness thresholds** — `< 400` vs `< 500`, no shared predicate, drift
   independently. `statusDevServer` `<500` (#6); `runHealthProbe` `<400`; `checkHTTPRoot` `<400`
   (`verify_checks.go:150`) but a second verify site `<500` (`:162`). **Action:** extract one
   `httpReadyCode(code) bool` (>=200 && <400); route all readiness through it. Confirm whether
   `verify_checks.go:162`'s `<500` ("responded at all") is intentional before collapsing it.

3. **Mutex held across blocking I/O** — violates pinned "copy under lock, release, then I/O."
   `getClientID` locks then calls `GetUserInfo` (network) under lock (`zerops.go:69-81`);
   `work_session` Record*/Touch/MaybeFireAutoClose hold `workSessionMu` across file read+write
   (`work_session.go:185-334`). Low immediate impact; fix opportunistically (double-check pattern,
   confirm stale-read tolerance for work_session).

4. **Mock fidelity drift** — mock returns a different shape than the real mapper for the same op,
   so green-against-mock masks a real mismatch. Confirmed once (CancelProcess); the
   mapper-normalization seam exists for every status the mock sets directly. **Action:** fix the
   instance; consider a fidelity test asserting mock + `mapProcess` agree on every status constant.

---

## Dead code

| Item | Location | Action |
|---|---|---|
| Dead browser-walk loop in `ComputeSessionMetrics` (discards each `WorkflowCall`, populates nothing) | `analyze/session.go:686-696` | Remove + docstring-note deferral, or complete it. |
| Unused `fmDone` flag (decl 299, set 311, only `_ =` at 320) | `sync/transform.go:299/311/320` | Delete all three lines. |

Re-categorized after Codex cross-check:
- `lookupDetail` returns zero-value `StepDetail` on miss with no error (`workflow/bootstrap.go:370-378`)
  is **not dead code** — it's a latent silent-failure smell, and tests currently rely on the
  miss-returns-empty behavior, so a `(StepDetail, bool)` signature change needs careful test updates.
  Low priority.

Style-only (note, not action): mixed `0755`/`0o755` across `init/*.go` — cosmetic, Go-identical.

---

## Aleš-scope (refer to owner — do NOT action)

- **Eager topics suggested as missing in retry deltas** — `workflow/recipe_guidance.go:481-500`.
  `missingCriticalTopics()` doesn't exclude Eager topics that `InjectEagerTopics()` already
  inlines → retries emit contradictory "here it is inlined" + "you may have missed it." Medium.
  One-line fix (`if t.Eager { continue }`) is Aleš's call.

---

## Refuted (verifier killed these — recorded so we don't re-discover)

- platform-core `projectAdminClient.Close()` "race" — single-threaded by design (per-workflow,
  never shared across goroutines); `-race` is clean. Close() zeros fields for GC, not sync.
- platform-core `waitForReady()` "ignores ctx" — the `select` has `<-ctx.Done()`; enforces
  min(ctx, wall-clock). Correct.
- ops-verify `WaitHTTPReady` "context leak" — `Do()` is synchronous; `cancel()` runs on every
  path. Correct.
- ops-verify "Skip-instead-of-Recovery on unresolved subdomain" — pinned by
  `TestVerify_RuntimeSubdomainAccessUnresolved_SkipsHTTPRoot`; Skip-vs-Recovery distinction is
  intentional (enabled-but-not-ready = transient = Skip).
- workflow-engine "ModeStage cross-deploy stale BuildSetup" — gated at `workflow_close_mode.go:194`
  (git-push impossible for ModeStage); pinned golden confirms BuildSetup set-but-not-emitted is intended.
- workflow-bootstrap "setup-name missing for existing recipe targets" — `IsExisting=true` is only
  set on the adopt route, never recipe.
- tools-infra "nil-deref in buildProgressCallback (req.Session)" — MCP guarantees non-nil Session
  when progressToken present; callers guard `onProgress != nil`.
- support-cmd "global mutable state in service.runFunc" — `internal/service` is a CLI utility
  (separate process per invocation), not an MCP handler; the stateless-tools invariant doesn't apply.

---

## Recommended order

1. **State-integrity first:** #1 stage-lock + #2 git-push stamp (can corrupt session state / strand a deploy).
2. **Ship consistency fixes alongside:** #3 sync, #4 env-precedence, eval-analyze unmarshal, mock fidelity.
3. **Hardening:** #5 phantom-tree gate, #8 update-path, #6 dev-server threshold (+ shared predicate), #7 ambiguous.
4. **Systemic sweep:** `_ =` triage + the `httpReadyCode()` predicate.
5. **Pinning tests** where flagged "no current test covers this" (#1 stage-lock, #6 4xx) — closing the gap that hid them.
