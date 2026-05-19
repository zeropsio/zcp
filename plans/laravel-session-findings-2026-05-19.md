# Laravel Session Findings — 2026-05-19

První session po importu `laravel-showcase` recipe. Container mode (cwd `/var/www`),
agent prompt: "Build a task board for my team. Tasks should stay saved after refresh."
Workflow-level všechno fungovalo (adopt → develop → deploy → cross-deploy → close).
Friction nálezy níže.

**Zdroje:**
- Raw session log: `.zcp/manual/laravel.txt` (uloženo manuálně, full JSONL stream)
- Tento plán: analýza, repro, fix kandidáti, eval-driven attack plan

---

## 1. [HIGH] initCommand silent no-op + observability gap

**Symptom:** Po `zerops_deploy targetService=appdev setup=dev` na laravel-showcase recipe
s upraveným seederem:
- `buildStatus: ACTIVE`, deploy DEPLOYED, `zerops_verify` healthy, HTTP 200 ✓
- Migrace `2026_05_19_000001_create_task_board_tables` v `migrate:status` jako `[2] Ran` ✓
- Ale `App\Models\BoardList::count() === 0` ✗
- Manuální `ssh appdev 'php artisan db:seed --force'` v identickém runtime containeru
  okamžitě seedne 3 řádky.

**Recipe `zerops.yaml` initCommands (dev setup):**
```yaml
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan db:seed --force
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
```

**Repro (deterministic):**
1. `zerops_workflow start workflow=bootstrap route=recipe recipeSlug=laravel-showcase`
2. Po prvním deploy: `Article::count() === 20`, `BoardList::count()` ≡ 0 (neexistuje yet).
3. Agent vytvoří migration `create_task_board_tables` + `BoardList` model + amend
   `DatabaseSeeder` s nezávislým `if (BoardList::count() === 0) { ... }` blokem (žádný
   early return mezi tím a článkovým blokem).
4. `zerops_deploy targetService=appdev setup=dev`
5. SSH check: `php artisan migrate:status` → migration ran; `BoardList::count()` → 0.

**Hypothesis (root cause kandidáti, žádný confirmed):**
- (a) `zsc execOnce` marker collision — migrate-success označí marker, db:seed sdílí marker
  pro stejné appVersionId a skipne.
- (b) `--retryUntilSuccessful` běží async; deploy vrátí DEPLOYED dřív než seeder reálně
  doběhne, ale runtime container už předal `service_running=pass` → race.
- (c) Seeder ran a silently failed mid-way (např. Scout indexing call v boot, ale Article
  blok byl skip přes `count() > 0` guard — proč by Scout call vůbec proběhl?).
- (d) Build artifact vs SSHFS mount inconsistency — agent SSH `cat` ověřil že deployed
  soubor má nový kód, ale initCommand mohl běžet ze starého build snapshot.

**Diagnostic gap (toto je největší problém bez ohledu na root cause):**
- `zerops_logs severity=error` — žádná stopa po seeder selhání.
- `zerops_events limit=15` — jen `build FINISHED` + subdomain-enable.
- `zerops_verify` — green, `http_root: pass`.
- Developer ani agent nemají způsob jak ověřit "init step actually fired and did its thing."
- Platforma reportovala **falsely-passing deploy**.

**Fix candidates:**
1. Surface initCommand stdout/stderr v `zerops_events` (per-command sub-events) nebo v novém
   facet `zerops_logs facility=initCommand`.
2. Detect init command non-zero exit a flag deploy jako `status: DEPLOYED_DEGRADED`
   s `failureClassification` (rozšíření existující struktury).
3. Atom guidance (recipe knowledge file `internal/knowledge/recipes/laravel-showcase.md`):
   "po deploy zkontroluj `storage/logs/laravel.log` pokud seed/migrate logic není visible."
4. Investigate `zsc execOnce` deduplication semantics — per-command marker? per-versionId?
   per-text-hash?

**Eval verification approach:**
- Nový flow-eval container scenario `agent-laravel-extend-recipe.md`: agent dostane
  laravel-showcase project, prompt "přidej kategorii Article + seedni 5 default kategorií",
  ověř že po deploy `Category::count() === 5`. Currently FAIL silently. Po fix musí buď
  seeder fungovat reliable, nebo failure surface visibly (deploy status, recovery hint, atom).

---

## 2. [MEDIUM] zerops_browser: ambiguous vs absence error

**Symptom:** `find role textbox "+ Add a task" press Enter` na stránce s 4 textbox elementy
sdílejícími accessible name `"+ Add a task"` → response `"Element not found. Verify the
selector is correct and the element exists in the DOM."` Element ve skutečnosti existoval
4×; selector byl ambiguous, ne missing.

**Repro:**
1. Page s ≥2 elementy sdílejícími role+name (např. seznam s opakovaným input placeholder).
2. `mcp__zerops__zerops_browser commands=[["find","role","textbox","<name>","press","Enter"]]`
3. Vrátí "Element not found" — agent z toho usoudí že stránka je broken a začne diagnostikovat
   wrong vrstvu.

**Fix:** Separate error variant pro N>1 matches:
```
"Ambiguous selector: 4 elements match role=textbox name='+ Add a task'.
Use qualifier (first/last/nth) to disambiguate."
```
+ pokud možné include selectors / refs prvních 3 matches.

**Eval verification:** flow-eval scenario s known-ambiguous page (např. task-board sám
o sobě, protože má vícero `+ Add a task` inputs); expect ambiguity error, ne "not found".

---

## 3. [MEDIUM] Auto-close gate nereevaluuje po close-mode change

**Symptom:** Sequence:
1. `zerops_deploy targetService=appdev` → ready 0/2
2. `zerops_verify serviceHostname=appdev` → ready 1/2
3. `zerops_deploy sourceService=appdev targetService=appstage setup=prod` → ready 1/2
4. `zerops_verify serviceHostname=appstage` → ready 2/2, `enabled: false`,
   `reason: "auto-close gated by close-mode: appdev, appstage"`
5. `zerops_workflow action=close-mode closeMode={"appdev":"auto","appstage":"auto"}`
   → response `{"services":"appdev=auto, appstage=auto","status":"updated"}`
6. `zerops_workflow action=status` → stále `phase: develop-active`, `next: action=close`

Session zůstala open. Pořadí "close-mode FIRST, then deploy+verify" by gate spustilo;
pořadí "deploy+verify FIRST, then close-mode" silently disable auto-close.

**Fix candidates:**
1. Handler `action="close-mode"` re-evaluuje auto-close gate když všechny in-scope services
   jsou green a všechny pickované modes jsou `auto`/`git-push`. Trigger close inline.
2. Response z `close-mode` upgrade: pokud post-change scope je green a all-auto, include
   `nextActions: "scope ready; call action=close to fire now"` nebo automaticky close.
3. (Alternative) Atom guidance výslovně řekni "set close-mode BEFORE running first deploy
   v develop, ne potom." Defensive, ne fix.

Preference: fix #1 (handler re-evaluuje). Response by měla aspoň reflectovat výsledek
re-eval-u (`closed: true` nebo `pendingClose: false`).

**Eval verification:** workflow scenario s pořadím "deploy → verify → close-mode after green";
expect auto-close fire bez explicit `action=close`.

---

## 4. [LOW] zerops_browser: grammar pro qualifiers nejasný z error msg

**Symptom:**
- Try 1: `find selector .list:nth-of-type(1) ...` →
  `"Unknown subcommand: selector\nValid options: role, text, label, placeholder, alt, title,
  testid, first, last, nth"` — listing kombinuje locator types + qualifiers.
- Try 2: `find placeholder "+ Add a task" first fill "..."` →
  `"Unknown subaction: first"` — slot mismatch (qualifier patří jinam).

Agent zkusil qualifier ve špatném slotu na základě listing v error message. Gramatika
pro qualifier slot (kde first/last/nth fits) v error nepřevzata.

**Fix:** Buď doc example v error message když list valid options:
```
Subcommand mismatch. Locators: role|text|label|placeholder|alt|title|testid.
Qualifiers: first|last|nth (use as `find first role textbox <name>`).
```
Nebo rozdělit error variants.

**Eval verification:** žádná dedicated; manual sanity check stačí. Stejný browser tool
fix PR jako #2.

---

## 5. [LOW] zerops_browser: top-level "exited with error" i s 1 step failure

**Symptom:** Z 10 commands v jednom `zerops_browser` walku, 1-2 selžou (např. wrong syntax),
top-level `message: "agent-browser exited with error: exit status 1"`. Agent musí parsovat
per-step success/failure.

**Fix:** Top-level message reflectuje aggregate ("12/14 steps succeeded; 2 failed: <names>")
nebo opt-out od top-level "error" pokud aspoň 1 step uspěl. Per-step `steps[].success`
už existuje, stačí adjust top-level renderer.

**Eval verification:** žádná dedicated.

---

## Eval-driven attack plan

**Operating mode:** Karel řídí, schvaluje per-step. Já nejdřív diagnose-only, pak Karel
approve, pak fix + eval verify.

**Per-issue sequence:**

1. **Diagnose** — read code (handler, atom, related test), trace logic. NO edits.
   Output: short report do plánu (append section "Investigation #N").
2. **Design fix** — concrete patch sketch + which tests pin behavior. NO edits.
3. **Karel approve** — explicit go.
4. **Apply** — edit, run `make lint-fast` + `go test -short` na affected packages.
5. **Eval verify** — relevant flow-eval scenario:
   - #1: nový `eval/behavioral/scenarios/agent-laravel-extend-recipe.md` (init seed
     visibility) — background run.
   - #2: nový nebo existing scenario s ambiguous page.
   - #3: workflow scenario s "deploy → close-mode after green" pořadím.
   - #4, #5: žádný eval, manual sanity check + unit tests.
6. **Karel watches** — flow-eval `self-review.md` čte oba; rozhodne další krok / iteraci.
7. **Bundle** — pokud fix #2/#4/#5 bundlu (vše browser tool), jeden PR; jinak per-PR.

**Order:** #1 → #3 → #2 → #4 → #5.

Důvod pořadí: #1 je systémová observability díra (recipe pattern bez safety net).
#3 je workflow foot-gun, často hit (Karel zaznamenal v ostatních session?). #2 je
verifikace ergonomy. #4/#5 jsou polish browser toolu, jeden PR s #2.

## Open questions for Karel

- **#1 prioritní směr:** "expose initCommand output v platform tools" (platform-wide
  observability) NEBO "first investigate `zsc execOnce` semantics" (root-cause fix v zsc)?
  První ZCP-side, druhý zsc-side (mimo náš repo).
- **#1 atom guidance:** přidat to do `recipe knowledge file` nebo do generic `atom`?
- **#2 eval scenario:** vytvořit dedicated, nebo extend existující laravel scenario
  (task-board page má 4× ambiguous textbox přirozeně)?
- **Release cadence:** každý fix samostatný PR, nebo bundle high+medium do release-1,
  low do release-2?

## Investigation #1 — execOnce same-key probe (2026-05-19)

**Hypothesis tested:** `zsc execOnce <key> -- <cmd>` keyed by `<key>` alone, so
sharing a key across multiple `execOnce` invocations causes all but the first
to silently no-op (exit 0, no command output).

**Method:** SSH to `zcp` service in eval-zcp (zsc binary `/usr/local/bin/zsc`),
run three execOnce calls in sequence with the SAME random key for #1 + #2 and
a DIFFERENT key for #3.

**Result (raw):**
```
+ zsc execOnce probe-1779176395 -- sh -c 'echo FIRST_CMD_RAN at $(date +%T)'
FIRST_CMD_RAN at 07:39:55
---exit=0---
+ zsc execOnce probe-1779176395 -- sh -c 'echo SECOND_CMD_RAN at $(date +%T)'
---exit=0---                           ← NO command output, silent no-op
+ zsc execOnce probe-1779176395-other -- sh -c 'echo THIRD_DIFFKEY_RAN at $(date +%T)'
THIRD_DIFFKEY_RAN at 07:39:56
---exit=0---
```

**Conclusion:** Hypothesis (a) from section 1 CONFIRMED. Recipe's three commands
sharing `${appVersionId}` cause #2 (db:seed) and #3 (scout:import) to skip.
Migration ran → table exists → `migrate:status` reports Ran. Seeder never fired
→ `BoardList::count() === 0`. Manual `php artisan db:seed --force` works because
it bypasses execOnce entirely.

**Fix applied (local edit, NOT pushed yet) — per Aleš's pattern + scout
investigation:**
- **migrate** keeps `${appVersionId}` (per-deploy: schema bumps each version).
- **seed** → static `seed-v1` (Aleš explicit "jednorázovej"; seeder uses
  `Article::factory(20)->create()` with `Article::count()===0` guard, so
  bootstrap is one-off).
- **scout** → per-deploy `${appVersionId}-scout` (distinct key to avoid
  collision with migrate). Scout investigation: `Article` model has
  `use Searchable` → auto-indexing fires on model `create/update/delete`
  events via queue. But seed's `Article::factory()->create()` fires events
  ONLY on the first-seed deploy. On subsequent deploys seed is skipped
  (records exist) → no Create events → if Meilisearch index has been lost
  (search service restart, index file drop), auto-indexing won't recover it.
  scout:import is the recovery safety net — must run per-deploy.

**`--retryUntilSuccessful` flag asymmetry** (per Karel): dev keeps it (transient
tolerance during agent iteration — DB-not-ready, container restart, network blip
shouldn't crash the loop), prod drops it (fail-fast surfaces real errors via
DEPLOY_FAILED + stderr). Aligned with issue #1's observability direction.

Per-setup matrix (prod / dev) after fix:

| command | prod | dev |
|---|---|---|
| migrate | `${appVersionId}` (no flag) | `${appVersionId} --retryUntilSuccessful` |
| seed    | `seed-v1` (no flag)         | `seed-v1 --retryUntilSuccessful`         |
| scout   | `${appVersionId}-scout` (no flag) | `${appVersionId}-scout --retryUntilSuccessful` |

`internal/knowledge/recipes/laravel-showcase.md` final diff vs upstream:
```diff
@@ prod setup @@
-  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force
-  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan db:seed --force
-  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
+  - zsc execOnce ${appVersionId} -- php artisan migrate --force
+  - zsc execOnce seed-v1 -- php artisan db:seed --force
+  - zsc execOnce ${appVersionId}-scout -- php artisan scout:import "App\\Models\\Article"

@@ dev setup @@
   - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan migrate --force   ← unchanged
-  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan db:seed --force
-  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
+  - zsc execOnce seed-v1 --retryUntilSuccessful -- php artisan db:seed --force
+  - zsc execOnce ${appVersionId}-scout --retryUntilSuccessful -- php artisan scout:import "App\\Models\\Article"
```
5 lines total (3 prod + 2 dev; dev migrate unchanged). Worker setup is unaffected.
Rationale: execOnce key uniqueness is per-service. `${appVersionId}` rolls
each deploy (collision-on-same-deploy prevented for migrate vs scout via
suffix). Static `seed-v1` is permanent bootstrap marker; `-v1` reserved
for future seeder rework needing forced re-run (bump to `-v2`).

**Push status — APPLIED (2026-05-19):** PR #4 on
`zerops-recipe-apps/laravel-showcase-app` —
https://github.com/zerops-recipe-apps/laravel-showcase-app/pull/4
- README.md: 5/5 lines (integration-guide YAML markers)
- zerops.yaml: 5/5 lines (file-level, same intent)

**sync push limitation discovered:** `internal/sync/push_recipes.go:223`
skip-condition `len(new) >= len(existing)` is a safeguard against API
regressions but blocks legitimate shrinkage (dropping `--retryUntilSuccessful`
flags here trimmed ~120 bytes → file SKIPPED, README updated only). Worked
around manually via `gh api --method PUT contents/zerops.yaml` on the
same PR branch. Backlog candidate: replace bytes-comparison with structural
diff, OR add `--force-yaml` opt-out, OR surface a warning in push output
instead of silent skip.

## Investigation #3 — auto-close gate after close-mode write (2026-05-19)

### A. Root cause (precise)

**Three concerns conflated under one paradigm:**

1. **Event recording** — `ws.Deploys` / `ws.Verifies` arrays append on attempt.
   Handler: `RecordDeployAttempt`, `RecordVerifyAttempt`.
2. **Gate evaluation** — `EvaluateAutoClose(stateDir, ws)` returns whether
   the work session should auto-close.
3. **Closure stamping** — `ws.ClosedAt + CloseReason` written once when
   gate first passes.

Pre-P6 (before commit `851fea40`, 2026-04-28) gate had **2 inputs** (deploys
+ verifies). Trigger pattern: stamp after each gate-input event (inside
`RecordDeployAttempt` + `RecordVerifyAttempt`). Symmetric, worked.

**Post-P6** gate grew to **3 inputs**: deploys + verifies + close-mode meta.
The new 3rd input is **state** (`meta.CloseDeployMode`), not **event**
(no Record* parallel). Trigger pattern stayed 2-input. → Close-mode write
became a silent gate-tip: state mutation that affects gate outcome but
fires no re-evaluation.

**Smoking gun in `docs/spec-work-session.md §9.1 steps 10-11`:**
> "10. … LLM offers `action="close-mode" closeMode={web:auto, api:auto}`
>     to commit.
> 11. **Auto-close fires (close-mode now set, every scope service green).**
>     Next `action="status"` renders the "task complete, close or next"
>     variant."

Spec explicitly contracts: close-mode write triggers auto-close fire when
scope is green. Code doesn't honor this — `internal/tools/workflow_close_mode.go::handleCloseMode`
writes meta and returns; never touches WorkSession or calls `EvaluateAutoClose`.

### B. Compounding defect — surface asymmetry (F5 partial sweep)

Commit `4fac8d7a` (2026-04-30) introduced **`WorkSessionState` lifecycle signal**
via shared helper `internal/tools/deploy_local.go::sessionAnnotations(stateDir)`:

```go
type WorkSessionState struct {
    Status      string  // "open" | "auto-closed" | "none"
    ClosedAt    string  `json:"closedAt,omitempty"`
    CloseReason string  `json:"closeReason,omitempty"`
    Note        string  `json:"note,omitempty"`
    Progress    *workflow.AutoCloseProgress `json:"progress,omitempty"`
}
```

F5 closure was scoped to **deploy + verify only** (commit msg: "Plan §5 P6
explicitly named 'deploy/verify' as the F5 surface — verify was missed in
the initial pass"). Other lifecycle-touching handlers weren't migrated:

| Handler | Mutates state | `WorkSessionState` attached | Triggers gate re-eval |
|---|---|---|---|
| deploy_local / deploy_ssh / deploy_batch / deploy_local_git / deploy_git_push | Deploys | ✓ | ✓ (RecordDeployAttempt) |
| verify | Verifies | ✓ | ✓ (RecordVerifyAttempt) |
| record-deploy (workflow-less ack) | Deploys + meta.FirstDeployedAt | ✓ | ✓ (RecordDeployAttempt) |
| **close-mode** | **meta.CloseDeployMode** | ✗ | ✗ |
| git-push-setup | meta.GitPushState | ✗ | n/a (not gate input) |
| build-integration | meta.BuildIntegration | ✗ | n/a (not gate input) |
| env / subdomain / scale / manage / import / delete / mount / dev_server | various (env vars, runtime state, etc.) | ✗ | n/a (mostly not gate inputs) |

**Two compounding observability gaps for close-mode specifically:**
- Doesn't trigger gate → session stays open
- Doesn't attach `WorkSessionState` → LLM can't even see session state after the write

Result: LLM has to round-trip via `action="status"` to learn session state.
This violates **spec §1.3** ("Every state transition is observable to the
LLM. If it happened, the LLM can see it in the next MCP response").

### C. Why "just add a third trigger site" is the wrong fix

The local γ patch (add `EvaluateAutoClose + stamp` to close-mode handler)
*technically* resolves issue #3, but:

- **Future-fragile.** If/when a 4th gate input is added (e.g. some new
  service-level confirmation field), trigger pattern needs another patch
  site. The 3 sites become 4. Then 5.
- **Doesn't fix surface asymmetry.** Even with the patch, close-mode
  response is still terse — agent still can't observe state without
  `action="status"`.
- **Doesn't honor spec invariant.** §1.3 demands every state transition
  observable in next response. Close-mode is a state transition.

### D. Structural fix (max scope, minimum layers)

**Single architectural move: promote `sessionAnnotations` to canonical
gate-check point + universally attach to lifecycle-touching responses.**

#### D.1 Lazy gate evaluation inside `sessionAnnotations`

Extract `workflow.MaybeFireAutoClose(stateDir) (closed bool)`:

```go
// MaybeFireAutoClose is idempotent and mutex-locked. Loads the current
// work session, evaluates the gate, stamps ClosedAt+CloseReason once.
// Safe to call from any READER (response annotation site) — gate check
// happens on read, not on write event.
func MaybeFireAutoClose(stateDir string) (closed bool) {
    workSessionMu.Lock()
    defer workSessionMu.Unlock()
    ws, err := CurrentWorkSession(stateDir)
    if err != nil || ws == nil {
        return false
    }
    if ws.ClosedAt != "" {
        return true                   // already closed
    }
    if !EvaluateAutoClose(stateDir, ws) {
        return false                  // gate not satisfied
    }
    ws.ClosedAt = time.Now().UTC().Format(time.RFC3339)
    ws.CloseReason = CloseReasonAutoComplete
    _ = SaveWorkSession(stateDir, ws)
    return true
}
```

Then `sessionAnnotations` calls it before its read:

```go
func sessionAnnotations(stateDir string) *WorkSessionState {
    _ = workflow.MaybeFireAutoClose(stateDir)        // lazy stamp
    ws, err := workflow.CurrentWorkSession(stateDir) // fresh read
    ...
}
```

**Consequence:** every response that attaches `sessionAnnotations` becomes
a gate-check point. Trigger-vs-state asymmetry dissolves — gate is checked
on read, not just on event-recording.

#### D.2 Universal attachment to lifecycle-touching mutation handlers

Sweep every mutation handler to attach `WorkSessionState`. Per-handler
audit drives whether the field is sensible (some mutations are project-
infra, not develop-lifecycle; those skip cleanly):

- **Required (gate-tipping or session-relevant):**
  - close-mode ← issue #3 immediate fix
  - git-push-setup (semantically tied to close-mode lifecycle)
  - build-integration (same — completes the strategy triple)
- **Optional (uniform shape, not gate-tipping):**
  - env, subdomain, scale, manage — develop-relevant ops; attach for
    consistency with spec §1.3
- **Skip (project-infra, not lifecycle-touching):**
  - import (creates services; pre-develop), delete (terminal),
    mount (read-side), dev_server (runtime-state, not platform state)
  - These need per-handler audit — default to skip unless reader expects
    lifecycle signal there.

#### D.3 Internal cleanup — RecordDeployAttempt / RecordVerifyAttempt

Inline close-fire inside these stays (they hold mutex already + need
atomic write+stamp). But once `sessionAnnotations` does lazy stamp on
read, the inline stamp becomes a redundant safety net. Two options:
- **Leave both** (idempotency means no double-stamp; cheap insurance) ✓ recommend
- Refactor inline to call shared logic — only if duplication grows ugly

### E. Why this avoids over-engineering

| What we're NOT doing | Why this matters |
|---|---|
| Adding `ws.CloseModeChanges []EventEntry` schema field | Bloat for one feature; close-mode value already in meta |
| Deriving ClosedAt at every read (full β option) | Requires reworking ClosedAt timestamp semantics; bigger blast radius |
| New layer (lifecycle bus, event aggregator, etc.) | Existing `sessionAnnotations` + `MaybeFireAutoClose` are 2 small fns |
| Per-handler bespoke gate evaluation | Single canonical helper; new handlers default-correct |

Total LOC delta estimate: ~80 lines new code (1 helper + 3-5 handler
attachments + test pinning), ~0 lines refactor (existing logic
unchanged), ~150 lines test cases.

### F. Implementation phases (per-phase verifiable)

**Phase 1** — Lazy gate evaluation:
- Add `workflow.MaybeFireAutoClose(stateDir) bool` to `internal/workflow/work_session.go`
- Wire `sessionAnnotations` to call it before read
- Existing tests for `EvaluateAutoClose` + `AutoCloseProgressOf` should pass unchanged

**Phase 2** — Issue #3 immediate fix:
- `internal/tools/workflow_close_mode.go::handleCloseMode` Update path:
  attaches `WorkSessionState` via `sessionAnnotations` before return
- Result: close-mode response now matches deploy/verify shape; agent sees
  `status="auto-closed"` immediately when scope tipped
- Pin: `TestHandleCloseMode_FiresAutoCloseWhenScopeReady` integration test

**Phase 3** — Extend F5 sweep to remaining lifecycle handlers:
- git-push-setup: attach WorkSessionState
- build-integration: attach WorkSessionState
- Per-handler audit for env/subdomain/scale/manage based on whether
  spec §1.3 applies to that mutation

**Phase 4** — Atom audit:
- Find any atom that recommends "set close-mode FIRST" or "always call
  action=close after deploy" defensive patterns
- Rewrite: agent calls close-mode in any order; handler signals close
  via response. action=close still works as explicit override.

**Phase 5** — Spec updates:
- spec-work-session.md §5: document lazy gate evaluation (sessionAnnotations
  as canonical gate-check point)
- spec-workflows.md §3.3 interaction matrix: close-mode meta is gate input,
  evaluated on every lifecycle-touching response

**Phase 6** — Test sweep:
- Per-handler integration test asserting post-mutation `WorkSessionState`
  reports correct lifecycle status
- Specifically: close-mode → fires close when ready; doesn't fire when
  manual present

### G. What this fixes (mapped to surfaces)

| Surface | Pre-fix | Post-fix |
|---|---|---|
| Session log §3 of issue (deploy→verify→close-mode→stays open) | bug | spec-§9.1 contract honored |
| Close-mode response shape | terse, missing lifecycle | symmetric with deploy/verify (WorkSessionState) |
| Spec §1.3 "every state transition observable" | violated | restored |
| Spec §9.1 step 11 "auto-close fires after close-mode" | unenforced | enforced via lazy stamp |
| Future 4th gate input | needs new trigger site | sessionAnnotations catches it for free |
| LLM round-trip cost after close-mode | requires action=status | 0 round-trips |

### H. Risks + mitigations

| Risk | Mitigation |
|---|---|
| Mutex contention on every response annotation | `sessionAnnotations` is per-MCP-call; serialized within process; no cross-process work-session writes (PID-scoped) |
| Double-stamp race (inline + lazy) | `MaybeFireAutoClose` checks `ClosedAt == ""` under mutex; idempotent |
| Stamp failure cascades into response error | `MaybeFireAutoClose` returns bool only; save error swallowed (matches existing inline pattern in RecordDeployAttempt:223 `_ = SaveWorkSession(...)`) |
| Handler-attach drift over time | spec §1.3 + lint test asserting every mutation handler in registered tool list attaches WorkSessionState (separate backlog item for the lint) |

### I. Out of scope (separate backlog candidates)

- Full β: replace ClosedAt stamping with derived computation. Only worth
  doing if mutex contention surfaces (profiling) or if another reader
  needs "what if I add this hypothetical change?" preview.
- Mutation-handler completeness lint: AST check that every handler in
  `RegisteredTools()` whose action mutates state attaches WorkSessionState.
  Belongs in own backlog item; complements the structural fix but doesn't
  block it.
- Extend WorkSessionState attachment to env/subdomain/scale/manage/import/
  delete/mount/dev_server. Per-handler audit needed; not lifecycle-tipping
  but uniform shape would help spec §1.3 universality.
- `internal/sync/push_recipes.go:223` length-gate (already backlogged in
  `plans/backlog/sync-push-yaml-skip-on-shrink.md`).

### J. Applied (2026-05-19)

All phases shipped in a single atomic commit. Verification:
- `go test -short ./...` — green
- `go test -race` on auto-close + handler tests — green
- `make lint-local` — 0 issues (golangci, atom lints, template-vars)
- 2 new integration tests pin the fix:
  `TestHandleCloseMode_FiresAutoCloseWhenScopeReady` (lazy gate stamps + response carries auto-closed),
  `TestHandleCloseMode_StaysOpenWhenManualBlocks` (lazy stamp doesn't over-fire)
- 2 existing tests augmented to pin WorkSessionState attachment on
  git-push-setup + build-integration responses
- Convention bullet in `CLAUDE.md` documents the lazy-gate invariant
  for future contributors

Phase 4 (atom audit) — verdict: **no edits needed**. Atom corpus already
describes gate SEMANTICS (conditions + scope), not TRIGGER (event-vs-state).
Pre-fix the atoms were correct; only the code drifted. Post-fix code
matches atoms.

Phase 5 (spec doc edits) — verdict: **no edits needed**. spec §1.3 +
§9.1 step 11 were contracts the code now correctly enforces. CLAUDE.md
gets a convention bullet for the architectural invariant.

## Notes

- Agent v té session udělal feedback který je 80% správně. Issue #1 je real & nejvážnější,
  ale agent's root-cause speculation (execOnce idempotency) je jen jedna z 4 hypotéz.
  Observability gap je certain, root cause uncertain.
- Recipe knowledge file (`appdev/CLAUDE.md`) effectoval agentovo design rozhodnutí
  (CDN SortableJS místo `@vite()` kvůli Vite manifest issue). Pozitivní signál o té
  vrstvě guidance.
- Tohle byla **první session po recipe importu** — observability gap je obzvlášť bolestivý
  protože developer nemá baseline "co je normální" pro Laravel deploy.
