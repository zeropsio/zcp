# MASTER PLAN — Flow-Eval Friction Battery (2026-06-05)

Source: `plans/flow-eval-friction-report-2026-06-05.md` (verified 12-round flow-eval friction analysis). Six clusters, single-owner-drift corrections: **3 REAL_BUGs** (F11 recipe deployed-state, F35 planless-discover deadlock, F36 git-push diagnostic) + atom/guidance/recipe drifts. Karel's HARD requirement is enforced per plan: **TDD-proven functional + regression-proven safe + LIVE-re-verified on eval-zcp via flow-eval**. Release/install remains a SEPARATE explicit Karel decision — NOT part of any DoD here.

---

## 1. Overview + Phase Order

**Sequencing principle:** lowest blast-radius first (pure prose/golden), then the three isolated handler/engine fixes (each Go-pinned, no cross-dependency), then the recipe edit that ships out-of-tree via `sync push`. Every plan is **one atomic commit** — compiles, passes full suite, self-consistent on its own. No plan depends on another's code; they are ordered by risk and review ergonomics, not by data flow.

| # | Plan | Type | Touches | Commit shape | Why this slot |
|---|------|------|---------|--------------|---------------|
| **P1** | atom-tell-parity-sweep | 7 atom/guidance drifts + 1 prod-hint + 1 test-assertion | atoms, 1 guide, `ops/import.go`, 6 goldens | 1 commit | Lowest risk; pure tell-vs-check convergence. Validates the golden-regen pipeline early. |
| **P2** | bootstrap-recipe-deployed-state-parity (F11 REAL_BUG) | handler S-fix | `bootstrap_outputs.go` + 1 new test | 1 commit | Isolated meta-write fix; no golden, no atom. Deterministic, unit-pinned. |
| **P3** | git-push-probe-diagnostic-surfacing (F36 REAL_BUG) | handler S-fix | `workflow_git_push_setup.go` + 1 new test | 1 commit | Isolated error-surfacing; reuses battle-tested deploy helpers. No golden. |
| **P4** | bootstrap-discover-planless-deadlock (F35 REAL_BUG) | engine/handler M-fix | `engine.go` + `workflow_bootstrap.go` + ~14 test conversions | 1 commit | Largest test blast-radius (test conversions); deadlock-breaking. Slotted after the smaller bug fixes so the suite is already proven stable. |
| **P5** | export-compose-ready + launch-pipeline knowledge | 7 coordinated edits | atom-axis, 4 atoms, scenario, 1 envelope field, 3 goldens | 1 commit | Multi-layer (workflow + content + tools); needs all 7 edits coherent before suite passes (Edit 4 atom can't parse without Edit 1; Edit 5 scenario required or CoverageGate fails). |
| **P6** | recipe nextjs-ssr deployFiles clarification (F16) | recipe CONTENT via sync push | `internal/knowledge/recipes/nextjs-ssr-hello-world.md` (gitignored) | NO zcp commit — upstream PR | Ships out-of-tree (Strapi/app-repo PR → cache-clear → pull). Last because its "ship" is a separate sync workflow, not a git commit. |

**Critical ordering note for re-verification:** flow-eval rebuilds the working tree and runs sequentially on the **shared** eval-zcp container (~15 min each). A plan's eval re-run MUST run AFTER its commit is in the tree (P1–P5) or AFTER the `sync pull` lands the upstream recipe (P6). Do not parallelize eval runs against eval-zcp.

---

## 2. Per-Plan Sections

---

## P1 — atom-tell-parity-sweep

Lowest-risk, highest-impact-per-effort cluster: 7 guidance/atom fixes where the TELL the agent reads drifts from the CHECK the platform/handler enforces. Every fix is a small surgical prose edit plus a deterministic golden/lint regen — except F27, which also lives in **production Go** (`internal/ops/import.go:101`, pinned by a unit test) and in **two extra atoms the brief did not enumerate** (`develop-local-env-channels.md`, 3 occurrences). Per single-owner consistency those are folded in (flagged below as scope additions for Karel's awareness).

Baseline confirmed green before any edit: `go test ./internal/content/ -run TestAtomAuthoringLint` and `go test ./internal/workflow/ -run TestScenarios_GoldenComparison` both pass.

### F1+F55 — bootstrap-adopt-discover worked example self-contradicts ("plan derived for you" vs a same-type pair that forces the two-step pairing choice)

**Root cause.** `internal/content/atoms/bootstrap-adopt-discover.md` line 33 shows the worked example `scope=["appdev","appstage"]`. Line 36 promises "the plan is derived for you." But `appdev`+`appstage` are the canonical same-type dev/stage pair: `internal/workflow/adopt.go::BootstrapCompleteAdoptPlan` lines 158-161 detect exactly-two adoptable runtimes sharing `CanonicalBareForm` and return `ErrAdoptPairingChoice` (rendered by `adoptPairingChoice` lines 280-301). So the example the agent copies deterministically hits the one path where the plan is NOT auto-derived. Handler is by-design-correct (pinned by `TestBootstrapCompleteAdoptPlan_TwoSameType_RefusesWithTemplates`); the ATOM is wrong. Dominated the friction (~18×).

**EXACT EDIT** — `internal/content/atoms/bootstrap-adopt-discover.md`:
- Line 33: narrow the worked-example scope to a single runtime so the auto-derive promise holds:
  - current: `zerops_workflow action="complete" step="discover" scope=["appdev","appstage"]`
  - replace: `zerops_workflow action="complete" step="discover" scope=["appdev"]`
- Line 36: after the existing sentence ending "…never rename an adopted service." add:
  - `Two adoptable runtimes of the SAME runtime type are the one exception — see below; the plan is NOT auto-derived there.`
- Line 38 (no change): the two-template paragraph stays the canonical landing for the pair case.

Chain traced: `references-fields` frontmatter (line 8) unchanged → `TestAtomReferenceFieldIntegrity` unaffected. Only consumer golden: `bootstrap/adopt/discover-existing-pair.md`.

**TDD.** Pure-atom edit → golden gate + lint. RED: `go test ./internal/workflow/ -run TestScenarios_GoldenComparison/bootstrap_adopt_discover-existing-pair` FAILS. GREEN: regenerate via `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/ -run TestScenarios_GoldenComparison`. Inspect diff: only the scope-example line + new forward-pointer sentence move.

**REGRESSION GUARD.** Stay green: `TestAtomAuthoringLint` (forward-pointer sentence has no `do not|don't|never`+verb → axis-K clean), `TestAtomReferenceFieldIntegrity`, `TestAtomReferencesAtomsIntegrity`. Regenerates: `discover-existing-pair.md` + `_coverage-map.md` (no-op re-emit, IDs unchanged). Handler tests `TestBootstrapCompleteAdoptPlan_*` UNTOUCHED.

**EVAL-ZCP.** `adopt-existing-standard-pair` (seeds exactly `appdev`/`appstage` nodejs@22 + `db`). Success: agent's first `complete step="discover"` either submits a single runtime then handles the pair in ONE follow-up, OR goes straight to the two-template `plan=[...]` resubmit — NO confused retry loop re-hitting `ErrAdoptPairingChoice` twice. ABSENT signature: ≥2 consecutive `ErrAdoptPairingChoice` rejections or treating pairing-choice as failure. LLM-shaped → **2-3 runs**; bar = double-rejection/confusion absent in ≥2 of 3 (vs ~18× pre-fix).

**RISK.** One atom, one golden. Near-zero — narrowing the example strictly removes the contradiction; line 38 still owns the pair case. Rollback: `git checkout` atom + golden.

### F27+F53 — obsolete `zerops_env action=set scope=project key=X value=Y` form

**Root cause.** `internal/tools/env.go::envInputSchema` (43-68) accepts ONLY `action, serviceHostname, setup, preview, force, project (bool), variables ([]string), skipRestart`. No `scope`/`key`/`value` — the obsolete form fails at the SDK boundary on three params. Correct form already lives in `export-publish-needs-setup.md:23` and the env guide line 149. Single-owner drift: TELL hand-authored separately from the schema.

**EXACT EDITS — six owners (brief named 3; 3 more found):**

1. `internal/content/atoms/bootstrap-provision-rules.md:77` → `zerops_env action="set" project=true variables=["<KEY>=<value-or-preprocessor-directive>"]`
2. `internal/content/atoms/bootstrap-recipe-import.md:21` → `zerops_env action="set" project=true variables=["APP_KEY=<@generateRandomString(<32>)>"]`
3. `internal/workflow/bootstrap_guide_assembly.go:300` (`formatRecipeImportYAMLForGuide`) — replace inner literal `zerops_env action=\"set\" scope=\"project\" ...` → `zerops_env action=\"set\" project=true variables=[\"<KEY>=<value>\"]`. No golden/snapshot pins this Go string; invisible to suite, corrects the live recipe-provision guide.
4. **`internal/ops/import.go:101`** (production `IMPORT_HAS_PROJECT` rejection hint — highest value) — Suggestion literal → `…set them FIRST via \`zerops_env action="set" project=true variables=["<KEY>=<value>"]\` (preprocessor directives…`
5. **`internal/content/atoms/develop-local-env-channels.md`** (scope-addition, NOT in brief — bare unquoted form): line 16 `…scope=project` → `…project=true`; line 31 `…scope=project key=X value=Y` → `…project=true variables=["X=Y"]`; line 36 `…scope=project` → `…project=true`.

**TDD.**
- Owner #4 (Go): real unit RED. `internal/ops/import_test.go:107` asserts `pe.Suggestion` contains `` `scope="project"` ``. Step 1 (RED): change assertion to `` `project=true` `` → FAILS against unedited `import.go:101`. Step 2 (GREEN): apply `import.go:101` edit → passes. **MUST land in the same commit.**
- Atom owners (#1,#2,#5): golden/lint. RED: `…/bootstrap_classic_provision-local` and `…/bootstrap_recipe_provision` FAIL. GREEN: `ZCP_UPDATE_ATOM_GOLDENS=1` regen. `develop-local-env-channels.md` is `coverageExempt` + `environments:[local]` → no golden; verified by lint-green + eval only.
- Owner #3: no test pins it; build-green + recipe eval transcript.

**REGRESSION GUARD.** Stay green: `import_test.go` IMPORT_HAS_PROJECT test (after the one-line assertion update), `TestAtomAuthoringLint`, `TestAtomLintAcceptedActionsMatchDispatcher` (`action="set"` unchanged). Regenerates: `bootstrap/classic/provision-local.md` + `bootstrap/recipe/provision.md` + `_coverage-map.md` (no-op). The `envClassifications` half of the original finding is an agent-misread → NO change.

**EVAL-ZCP.** `classic-php-mariadb-standard` (classic → bootstrap-provision-rules) + `recipe-nestjs-minimal-standard` (recipe → bootstrap-recipe-import + guide-assembly). Success: project-level env set via `project=true` + `variables=["KEY=value"]`, SUCCEEDS first try; ABSENT: any `INVALID_PARAMETER`/"unexpected property scope/key/value" + retry. DETERMINISTIC (schema + unit test) → **1 run** per scenario.

**RISK.** 2 atoms + 1 guide string + 1 prod hint + 1 test assertion + 2 goldens (+ 3 develop-local lines). Low. Non-trivial item: `import_test.go` assertion must land with `import.go:101`. Rollback: `git checkout` 5 source files + 2 goldens.

### F22 — bootstrap-verify's blanket "Do NOT run `zerops_verify`" is FALSE for buildFromGit recipes

**Root cause.** `bootstrap-verify.md` lines 29-32 say `zerops_verify` only makes sense after develop's first deploy. True for classic/adopt; FALSE for recipe: `buildFromGit:` services are BUILT + DEPLOYED at import. The blanket prohibition renders into `bootstrap/recipe/close.md` — exactly where it's wrong.

**EXACT EDIT** — `bootstrap-verify.md` paragraph at lines 29-32. Replace with route-carved version:
> `For the **classic** and **adopt** routes there is no app layer yet — \`zerops_verify\` would report every runtime as failing and is noise here; skip it. The **recipe** route is the exception: \`buildFromGit\` services are built and deployed at import, so a recipe-provisioned HTTP runtime can legitimately be probed with \`zerops_verify\` once it reaches a running state — run it only on the recipe runtimes that serve HTTP.`

Axis-K: rewrite drops the bold-`**not**` trick, writes "skip it" → avoids the `(do not|don't|never)+verb` regex. No marker needed.

**TDD.** RED: `…/bootstrap_recipe_close` FAILS. GREEN: `ZCP_UPDATE_ATOM_GOLDENS=1` regen rewrites `bootstrap/recipe/close.md`.

**REGRESSION GUARD.** Stay green: `TestAtomAuthoringLint` (axis-K clean; axis-O — "report every runtime as failing" not a state-assertion pattern; axis-M — "that tool" removed). Regenerates: `bootstrap/recipe/close.md` + `_coverage-map.md` (no-op).

**EVAL-ZCP.** `recipe-nestjs-minimal-standard`. Success: at recipe close, agent does NOT decline a legitimate verify of the live buildFromGit runtime citing the atom; still skips for non-HTTP/managed. LLM-shaped (agents often verified anyway) → **2-3 runs**; bar = no run cites the atom to refuse a valid recipe verify. Lower-priority than F1/F27.

**RISK.** One atom, one golden. Low. Carve must not over-encourage verify on classic/adopt (kept explicit "skip it"). Rollback: `git checkout`.

### F19 — `run.start` runs via exec, not a shell: inline `KEY=VAL cmd` fails; use `env KEY=VAL cmd`

**Root cause.** `start:` is exec-form (healthCheck has a separate `exec:` for shell). Shell-isms (inline `KEY=VAL`, pipes, `&&`, `$VAR`) are NOT interpreted; `NODE_ENV=production node server.js` treats `NODE_ENV=production` as the binary name. `develop-reserved-env-names.md` is the right home.

**EXACT EDIT** — `develop-reserved-env-names.md`, append after line 17. Phrasing MUST avoid literal `run.start` (axis-runtime `\brun\.start\b` would fire); use `the start command` / `start:`:
> `**The \`start\` command runs via exec, not a shell.** Inline env-var prefixes (\`start: KEY=val node server.js\`) are NOT interpreted — \`KEY=val\` is taken as the binary name and the process fails to launch. To set an env var inline, wrap with \`env\`: \`start: env KEY=val node server.js\`. Pipes, \`&&\`, and \`$VAR\` expansion are likewise not available in \`start\`; put env vars in \`run.envVariables\` (the canonical location) or use \`env\`.`

(`run.envVariables` is safe — only `run.start`/`run.ports` are flagged.)

**TDD.** Renders into THREE goldens: `develop/first-deploy-dev-dynamic-container.md`, `develop/failure-tier-3.md`, `develop/first-deploy-recipe-implicit-standard.md`. RED: all three fail. GREEN: `ZCP_UPDATE_ATOM_GOLDENS=1` rewrites all three.

**REGRESSION GUARD.** Stay green: `TestAtomAuthoringLint` — CRITICAL confirm NO `run.start`/`run.ports`/`healthCheck`/`zsc noop`/`zerops_dev_server` token (axis-runtime), NO axis-K negation+verb, NO axis-O state assertion. `TestAtomReferenceFieldIntegrity`. Regenerates: 3 develop goldens + `_coverage-map.md` (no-op).

**EVAL-ZCP.** No sharp scenario. Closest: `greenfield-fullstack-multi-runtime`. Success: env-in-start uses `run.envVariables` or `env KEY=val`; ABSENT: `BUILD_FAILED`/launch failure from inline `KEY=val` prefix. Preventive → **lint+golden-gated for acceptance**; 1 run is a sanity check, not proof. **FLAG to Karel: F19 has no sharp eval scenario.**

**RISK.** One atom, three goldens. Low. Hazard: accidentally writing `run.start` trips axis-runtime — lint catches immediately. Rollback: `git checkout`.

### F9 — php `build.base` must be bare `php@X`, not composite `php-apache@X` / `php-nginx@X`

**Root cause (verified against live schema).** `internal/schema/testdata/zerops_yml_schema.json`: `build.base` enum is `php@X` (+ `alpine/php@X`, `ubuntu/php@X`) only — NO composite webserver forms. `run.base` carries `php-apache@X`/`php-nginx@X`. Composite in `build.base` FAILS `client.ValidateZeropsYaml`. Docs corroborate (`zerops-yaml-advanced.mdx:180`, `php-tuning.mdx:108`). Trap source: `php/how-to/build-pipeline.mdx` uses `php-apache@latest` for both. `develop-implicit-webserver.md` is the right home (`runtimes:[implicit-webserver]`).

**EXACT EDIT** — `develop-implicit-webserver.md`, add bullet after line 17:
> `- **\`build.base\` is bare \`php@X\`, never the composite \`php-apache@X\` / \`php-nginx@X\`.** Those webserver-bundled bases are valid ONLY as \`run.base\`; using one in \`build.base\` fails validation. The runtime image (\`run.base: php-apache@X\` or \`php-nginx@X\`) bundles the web server; the build image is the plain Alpine \`php@X\` toolchain.`

**TDD.** Renders into `develop/first-deploy-recipe-implicit-standard.md` (same golden as F19). RED: fails. GREEN: `ZCP_UPDATE_ATOM_GOLDENS=1` regen.

**REGRESSION GUARD.** Stay green: `TestAtomAuthoringLint` (`never the composite` — `never`+`the`, not a verb → axis-K no-match; `run.base` not in axis-runtime regex). `TestEmbeddedSchemasSelfConsistent` + `TestCatalog*` (schema-side pins that make the fact TRUE — untouched). Regenerates: `develop/first-deploy-recipe-implicit-standard.md` + `_coverage-map.md` (no-op).

**EVAL-ZCP.** `classic-php-mariadb-standard`. Success: agent writes `build.base: php@X` (bare) + `run.base: php-apache@X`/`php-nginx@X`; ABSENT: `zerops_deploy` build.base validation error naming the composite, or copying `build.base: php-apache@latest`. DETERMINISTIC (schema enum) → **1 run**; if PHP yaml authoring isn't reached, schema-self-consistency test proves the fact.

**RISK.** One atom, one golden. Near-zero (schema-pinned). Rollback: `git checkout`.

### F57 — client-side bundled env URLs (`NEXT_PUBLIC_*` / `VITE_*`) must be public subdomain, not internal `apidev:3000`

**Root cause.** `internal/knowledge/guides/environment-variables.md` shows the public-subdomain pattern (145-147) but NEVER states the internal-hostname trap. `NEXT_PUBLIC_*`/`VITE_*` are BAKED INTO THE BROWSER BUNDLE at build; the browser can't resolve internal Zerops hostnames → deployed SPA's API calls fail. Guide (not atom) → no atom-lint, no golden.

**EXACT EDIT** — `environment-variables.md`, after line 149:
> `**Client-bundled URLs must be the public subdomain, never the internal hostname.** \`NEXT_PUBLIC_*\` (Next.js) and \`VITE_*\` (Vite) vars are compiled into the JS bundle the browser downloads — they run in the user's browser, not in a Zerops container. An internal hostname like \`http://apidev:3000\` resolves only inside the project network, so a browser fetch to it fails. Point client-side URLs at the public subdomain (\`https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app\` or a custom domain). Internal hostnames (\`apidev:3000\`) are correct only for server-to-server calls (SSR fetch, API→DB), never for values shipped to the browser.`

**TDD.** Guide edit — NOT atom-golden-harnessed, NOT atom-lint-scanned. "Test": (1) knowledge corpus still loads (`go test ./internal/knowledge/...`), (2) eval re-run. **To publish to users: needs `zcp sync push guides` → PR → merge → cache-clear → pull. FLAG to Karel: F57 requires a separate guide sync push.**

**REGRESSION GUARD.** Stay green: any guide-corpus load/parse test. No golden, no atom-lint. Before `sync push`: preview full upstream-vs-local diff — should be exactly this one added paragraph (`+N / -0`); if larger, STOP per sync-amplification rule.

**EVAL-ZCP.** `greenfield-fullstack-multi-runtime`. Success: frontend `NEXT_PUBLIC_*`/`VITE_*` API URL = public subdomain; ABSENT: a browser-consumed URL pointed at `apidev:3000`/internal hostname, or post-deploy "API calls fail from browser". LLM-shaped + retrieval-gated → **2-3 runs**; bar = no run wires a browser-bundled URL to an internal hostname. (Also tests whether the agent pulls the env-vars guide at all.)

**RISK.** One guide file (+ a sync PR when published). Low — additive. Rollback: `git checkout` guide; don't push the PR or revert it.

### P1 cross-cutting verify (all 7 fixes)

```
ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/ -run TestScenarios_GoldenComparison   # regen 6 goldens
go test ./... -race -count=1
make lint-local                                                                              # atom-lint + atom-tree gates + goldens-compare
```
Goldens that regenerate (all EXPECTED, atom IDs unchanged, only body prose moves): `bootstrap/adopt/discover-existing-pair.md` (F1), `bootstrap/classic/provision-local.md` + `bootstrap/recipe/provision.md` (F27), `bootstrap/recipe/close.md` (F22), `develop/first-deploy-dev-dynamic-container.md` + `develop/failure-tier-3.md` + `develop/first-deploy-recipe-implicit-standard.md` (F19; last also F9), plus `_coverage-map.md` (no-op re-emit). One unit-test assertion edit: `import_test.go:107` `scope="project"` → `project=true` (F27, same commit as `import.go:101`).

### P1 scope additions beyond the brief (flagged for Karel)
1. **F27 owner #4 — `internal/ops/import.go:101`** (production IMPORT_HAS_PROJECT hint) + its pin `import_test.go:107`. Highest-value F27 site (live runtime hint). Mandatory for single-owner consistency.
2. **F27 owner #5 — `develop-local-env-channels.md` lines 16/31/36** (3 bare-form occurrences). Same drift, different atom.
Both in-scope-correct; surfaced rather than silently expanding the diff.

---

## P2 — bootstrap-recipe-deployed-state-parity (REAL_BUG F11, handler S-fix)

### The bug (root cause, verified)
A recipe whose import YAML declares `buildFromGit` is cloned, BUILT, and DEPLOYED at import — runtimes reach `ACTIVE` and serve curated code immediately (`bootstrap_guide_assembly.go:341-352` doc-comment + live close TELL). But `writeBootstrapOutputs` (`bootstrap_outputs.go:75-95`) builds the runtime `ServiceMeta` with `BootstrappedAt` set and **`FirstDeployedAt` empty**. Deploy-state derivation is meta-driven:
- `compute_envelope.go:316-319` `DeriveDeployed` → `if meta != nil && meta.IsDeployed()`
- `service_meta.go:112-114` `IsDeployed()` → `return m.FirstDeployedAt != ""`

With `FirstDeployedAt==""`, `DeriveDeployed` returns **false** (signal 2 has no session deploy; signal 3 `IsAdopted()` false because a fresh recipe target carries a non-empty `BootstrapSession`). Envelope reports `deployed:false`; the develop-active first-deploy/scaffold branch re-fires over an already-ACTIVE app — the agent scaffolds a `zerops.yaml` and redeploys over a working recipe app.

Contradicts the intended TELL at two owners: `bootstrap-close.md:18` ("recipes that deployed during bootstrap show `deployed: true`") and `bootstrap_guide_assembly.go:352`. Single-owner drift: the same `buildFromGit` signal that drives the discover guide (`engine.go:584`) + close message (`bootstrap_guide_assembly.go:350-351`) is NOT consulted at the meta-write owning `FirstDeployedAt`.

### FIX 1 — stamp FirstDeployedAt for fresh recipe buildFromGit runtimes in `writeBootstrapOutputs`

**File:** `internal/workflow/bootstrap_outputs.go`. Inside the existing Gate-R block (gated `Route==recipe && !IsExisting`), append after the setup-name writes:
```go
		// F11 deployed-state parity: a recipe whose import YAML declares
		// buildFromGit is cloned, built, and DEPLOYED at import — the
		// Zerops runtime reaches ACTIVE and serves immediately. Stamp
		// FirstDeployedAt so DeriveDeployed reports deployed:true (matching
		// the bootstrap-close TELL) and develop does NOT re-fire the
		// first-deploy/scaffold branch over an already-running app. Derived
		// from the SAME buildFromGit signal the discover guide (engine.go)
		// and close message (bootstrap_guide_assembly.go) use — one owner.
		if recipeBuildFromGit(state) {
			meta.FirstDeployedAt = now
		}
```
Add the derive helper (single owner for the signal; mirrors `bootstrap_guide_assembly.go:350-351`):
```go
// recipeBuildFromGit reports whether the active recipe's import YAML declares
// buildFromGit — i.e. the recipe's runtimes are cloned, built, and deployed at
// import (ACTIVE + serving), not awaiting a first deploy.
func recipeBuildFromGit(state *WorkflowState) bool {
	return state.Bootstrap != nil &&
		state.Bootstrap.RecipeMatch != nil &&
		strings.Contains(state.Bootstrap.RecipeMatch.ImportYAML, "buildFromGit")
}
```
Add `"strings"` to the import block.

**Chain trace — `now` is `2006-01-02` (date-only), not RFC3339.** `bootstrap_outputs.go:29` sets `now := time.Now().UTC().Format("2006-01-02")`. Every `FirstDeployedAt` consumer is a non-empty test only (`IsDeployed()` `!= ""`, `DeriveDeployed`, `SnapshotsFromMetas:25` — never parsed). `omitempty` round-trips fine. Reusing `now` keeps one timestamp source consistent with `BootstrappedAt`. (Flagged, not blocking: a future RFC3339-parsing consumer must tolerate date-only — none does today.)

### FIX 2 — mirror in `writeProvisionMetas`

`bootstrap_outputs.go:171-188` partial-write counterpart. Same condition, same stamp (parity with the existing `// Gate R — partial-write counterpart` comment), so a crash between provision and close leaves the recipe runtime already marked deployed:
```go
		if state.Bootstrap.Route == BootstrapRouteRecipe && !target.Runtime.IsExisting {
			meta.PrimarySetupName = primarySetup
			meta.StageSetupName = stageSetup

			// F11 parity at the partial write too.
			if recipeBuildFromGit(state) {
				meta.FirstDeployedAt = time.Now().UTC().Format("2006-01-02")
			}
		}
```
(`writeProvisionMetas` has no `now` in scope; `time` already imported.)

**Why `mergeExistingMeta` is untouched:** the new stamp is gated `!IsExisting`. Expansion-merge (`101-104`, `190-194`) runs only for `IsExisting && existed && IsComplete()`, and `mergeExistingMeta:215` already preserves a prior `FirstDeployedAt`. Paths don't overlap.

**Local dev-half exemption — already satisfied for free.** The local-mode recipe projection `continue`s the `PlanModeDev` half (no meta — dev runtime is the user's CWD) and collapses a standard pair to a single `local-stage` meta representing the **Zerops stage**, which IS deployed at import. Stamping it mirrors `adopt_local.go:125-126`. `docs/spec-local-dev.md:105-106` documents this parity. No extra branch needed.

### TDD

**RED test (new), home = `internal/workflow/recipe_meta_wiring_test.go`** — `TestBootstrapRecipe_BuildFromGitStampsFirstDeployed`. Table: `container_buildFromGit_stamps` (want deployed true), `container_no_buildFromGit_unstamped` (false — negative guard the stamp is gated on the signal not route alone), `local_stage_buildFromGit_stamps` (true). Drives the FULL flow (`BootstrapStartWithRoute(...recipe...)` → `BootstrapCompleteRecipePlan(nil,false,nil,nil)` → `BootstrapComplete provision` → `BootstrapComplete close`) so `writeBootstrapOutputs` (FIX 1) AND `writeProvisionMetas` (FIX 2) run; asserts `ReadServiceMeta(dir, host).IsDeployed()`.

- **Before FIX:** the two buildFromGit cases FAIL (`IsDeployed()==false`). The no-buildFromGit case passes before+after.
- **After FIX:** all three GREEN.

**No golden regenerates** — Go-state change. `bootstrap-close.md:18` already states the intended `deployed:true` — no atom edit.

### REGRESSION GUARD

Confirmed these do NOT assert `deployed:false` for recipes:
- `compute_envelope_test.go:170-177`, `:196` — classic/adopt metas, no recipe route; untouched.
- whole `bootstrap_outputs_test.go` — all classic targets (`RecipeMatch==nil` → `recipeBuildFromGit()` false → no stamp). Stays green.
- `:1145`/`:1236`/`:1308` expansion/merge tests — `IsExisting=true`, stamp gated `!IsExisting`. Untouched.
- `recipe_meta_wiring_test.go:14` `TestBootstrapRecipe_MetaWiring` — asserts `ServesHTTP`/setup names, never `FirstDeployedAt`. Untouched (new test sits beside it).
- `bootstrap_outputs_test.go:655` `TestBuildTransitionMessage_RecipeBuildFromGit_DoesNotClaimNothingDeployed` — message-only; orthogonal.

**Auto-close non-regression (verified):** `EvaluateAutoClose` (`work_session.go:424`) requires a work session + per-service passed verify + auto/git-push `CloseDeployMode`. Fresh recipe bootstrap writes `CloseDeployMode=topology.CloseModeUnset` and has NO work session → `deployed:true` alone cannot trigger spurious auto-close. `derive_close_state_test.go` / `closed_at_lint_test.go` stay green.

**Verify:**
```
go test ./internal/workflow/... -race -run 'Bootstrap|Envelope|MergeExisting|CloseState|Recipe'
go test ./... -race
make lint-local
```

### EVAL-ZCP

**Scenario:** `recipe-nestjs-minimal-standard` (F11-originating). Secondary: `recipe-laravel-minimal-standard`.

**Observable success:** after bootstrap close, develop-start envelope shows the recipe runtime `deployed: true`, and the agent does NOT enter the first-deploy/scaffold branch (NO `zerops.yaml` scaffold, NO redeploy of the ACTIVE recipe app). Grep transcript for absence of "scaffold"/"write zerops.yaml"/a `zerops_deploy` with freshly-authored yaml against the recipe service; presence of `deployed: true` for `appdev`/the stage host.

**Runs:** DETERMINISTIC handler bug, unit-pinned (meta now carries `FirstDeployedAt`, `DeriveDeployed` pure) → **1 run** confirms the end-to-end observable. If the agent still scaffolds despite `deployed:true`, that's a SEPARATE develop-branch finding. `recipe-laravel-minimal-standard` optional 2nd run for parity.

### RISK + ROLLBACK
- **Blast radius:** two sibling functions + one 6-line helper + one new test. Gated `Route==recipe && !IsExisting && buildFromGit-in-YAML`; classic/adopt/managed-only/non-buildFromGit untouched.
- **Could regress:** (a) a buildFromGit recipe runtime that genuinely needs a develop first-deploy — pre-existing close-TELL inconsistency, not introduced here. (b) Auto-close — ruled out. (c) RFC3339-parsing future consumer choking on date-only — none today; flagged.
- **Rollback:** delete the two stamp blocks + helper + `strings` import + new test. Fully reversible (prior stamped metas read as `deployed:true`, which is correct).

---

## P3 — git-push-probe-diagnostic-surfacing (REAL_BUG F36, handler S-fix)

**Root cause (verified empirically).** The container-mode probe at `internal/tools/workflow_git_push_setup.go:384-393` renders the probe error with `%v`. `sshDeployer.ExecSSH` on failure ALWAYS returns `*platform.SSHExecError` (`deployer.go:90,123,130`), whose `Error()` is `fmt.Sprintf("ssh %s: %s", Hostname, Err)` — git stderr lives in `.Output` and is **dropped** by `%v`. Empirically: `SSHExecError{Output:"...fatal: Authentication failed...", Err:"exit status 128"}` renders as exactly `"ssh appdev: exit status 128"`. The agent sees only the exit code and can't distinguish bad-token (401) / repo-missing (404) / network / SSO.

**Parity reference.** The DEPLOY git-push path (`deploy_git_push.go:427`) already calls `gitPushErrorDetail(err, output)` (`:17-34`) → `errors.As(err, &sshErr)` → `truncateStderr(sshErr.Output)`. `ops.ClassifyDeployFailure` also reads `.Output` (`deploy_failure.go:274-280`). Both confirm `.Output` is canonical; the probe is the one site that ignores it.

**Why the existing test misses it.** `TestGitPushSetupContainer_ProbeFailure_NoStateMutation:132` seeds a PLAIN `errors.New(...)` — with a plain error `%v` == the full string, so message-surfacing accidentally works in-test while failing in production. It asserts only the error CODE + no-mutation. The new RED test must inject a real `*platform.SSHExecError` with distinct `.Output`.

### FIX (single edit + reuse existing helpers)

**`internal/tools/workflow_git_push_setup.go:382-393`** — replace the `%v` form:
```go
	if probeOut, probeErr := sshDeployer.ExecSSH(ctx, pushHost, probeCmd); probeErr != nil {
		// Surface git's actual stderr — ExecSSH wraps it in SSHExecError.Output,
		// but the error's %v renders only "ssh <host>: exit status 128".
		// gitPushErrorDetail reads .Output first (parity with deploy_git_push.go);
		// classifyTransportError labels credential/network from that stderr.
		detail := gitPushErrorDetail(probeErr, probeOut)
		classification := classifyTransportError(probeErr, deployStrategyGitPush)
		return convertError(platform.NewPlatformError(
			platform.ErrGitTokenInvalid,
			fmt.Sprintf("git-push-setup probe against %s failed: %s", input.RemoteURL, detail),
			"Verify: (1) PAT is correct and unexpired, (2) PAT has Contents: Read+Write on this repo (add Secrets/Workflows if integration=actions), (3) Remote URL exists and is reachable. Then re-call with corrected inputs. NO project state was modified.",
		), WithRecoveryStatus(), WithFailureClassification(classification)), nil, nil
	}
```
Chain traced — every symbol already exists in `tools`, no new imports: `gitPushErrorDetail` (`deploy_git_push.go:23`), `classifyTransportError` (`deploy_failure_classify.go:22`), `WithFailureClassification` (nil-safe no-op, `errwire.go:133`), `deployStrategyGitPush` (existing const). `probeErr` is passed raw (not `%v`-stringified) so `errors.As` succeeds inside each helper.

**Classifier half (recommended, +1 line):** gives a structured `failureClassification` (category `credential` for `Authentication failed`/`terminal prompts disabled`, `network` baseline) the agent reads FIRST per the deploy-failure-classification invariant. If Karel wants minimal, drop `classifyTransportError`+`WithFailureClassification`, keep only `detail` — **OPEN QUESTION below**.

**No local-mode change.** `confirmGitPushSetupLocal` → `RunGitAuthProbeLocal` already folds combined output into the error (`git_auth_probe.go:96`). Local is the existing-correct sibling; only the container path drifted. They reach parity after this edit.

### TDD

**RED — new `TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr`** in `workflow_git_push_setup_container_test.go`, modeled on `ProbeFailure_NoStateMutation` but injecting a real `*platform.SSHExecError{Hostname:"appdev", Output: "...fatal: Authentication failed...", Err: errors.New("exit status 128")}` on the `git ls-remote` call. Asserts the response body `strings.Contains(body, "Authentication failed")`.
- **Before FIX:** body is `ssh appdev: exit status 128`, substring ABSENT → FAILS.
- **After FIX:** `gitPushErrorDetail` pulls `truncateStderr(sshErr.Output)` in → GREEN.

**Optional second assertion (if shipping classifier):** `strings.Contains(body, "failureClassification")` + credential category (`Authentication failed` matches `transport:git-auth-failed` at `deploy_failure_signals.go:268`).

Pure Go unit-test fix — no golden, no atom, no `make schema-sync`.

### REGRESSION GUARD

- `TestGitPushSetupContainer_ProbeFailure_NoStateMutation:125` — stays green (edit changes message TEXT, not code/side-effects/SSH-call-count; its plain-error body still contains `authentication failed` + `GIT_TOKEN_INVALID`).
- `_RequiresGitToken:64`, `_HTTPSOnly_RejectsSCPForm:97` — gated before the probe; untouched.
- `_RestartPollFails_NoStamp:182`, `_TokenNeverEchoed:218` — probe-SUCCESS path; failure-branch edit doesn't reach them. **`TokenNeverEchoed` watch:** confirm the new test's injected `.Output` never contains the token literal (it's fixed GitHub stderr — it doesn't).
- `workflow_git_push_setup_local_test.go` — local path unchanged.
- Deploy-path parity owners sharing the helpers (must not regress; the edit is additive — no helper signature changes): `deploy_ssh_test.go` (`SSHExecError{Output:...}` at 1312, 1354), `ops/deploy_classify_test.go`, `ops/deploy_failure_test.go` (`transport:git-auth-failed` at 445-456).

Goldens: NONE regenerate (runtime error message only — no golden/atom/annotation surface).

**Verify:**
```
go test ./internal/tools/ -run GitPushSetup -race
go test ./... -race
make lint-local
```

### EVAL-ZCP

**Scenario:** `git-push-setup-with-cicd-method-prompt` (area `git-push`, fixture `nodejs-standard-deployed.yaml`) — the only listed scenario that hits the container probe with a token.

**Observable:** when the probe FAILS, the error payload message contains the actual git stderr line (`Authentication failed`/`Repository not found`/`Permission denied`/`terminal prompts disabled`), NOT a bare `ssh <host>: exit status 128`; with the classifier half, `failureClassification.category` = `credential` on the 401 case.

**Run count — flag.** DETERMINISTIC, unit-pinned → the **unit test IS the gate**. But the scripted scenario supplies a VALID token (probe succeeds, no diagnostic shown). Recommendation: **(a) accept the unit test as the gate + use 1 happy-path eval run for no-regression** (git-push-setup still stamps `configured`, walkthrough → confirm → cicd prompt intact). Optionally (b) Karel hand-triggers a bad-token confirm against eval-zcp to eyeball the stderr surfacing. `launch-with-existing-cicd` does NOT re-exercise this path — skip for P3.

### RISK + ROLLBACK
- **Blast radius:** one function, the container probe-failure branch. The two reused helpers are pure/read-only, battle-tested by the deploy path, zero new imports.
- **Could regress:** (1) longer/multi-line message — `truncateStderr` caps at last 5 lines joined by `; ` → bounded. (2) Secret-echo — probe uses `.netrc` (token never in URL), `BuildGitAuthProbeCommand` exports the token, so `.Output` won't contain the PAT; `TokenNeverEchoed` guards it. (3) Classification mislabel — `classifyTransportError` returns nil for unrecognized stderr, `WithFailureClassification(nil)` is a no-op → worst case "no field" (status quo).
- **Rollback:** single-hunk revert + delete the new test. No migration/state-shape/golden; no user-facing on-disk surface touched.

---

## P4 — bootstrap-discover-planless-deadlock (REAL_BUG F35, engine/handler M-fix)

### Root cause (read from code)
`engine.go::BootstrapComplete:459-471` has a `Plan==nil` guard that **exempts** discover:
```go
if stepName != StepDiscover && state.Bootstrap.Plan == nil {
    return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
}
```
The discover checker is **nil** (`workflow_checks.go:40-47`). So `BootstrapComplete(ctx, "discover", attestation, nil)` runs `CompleteStep("discover")` → sets `Status=complete`, advances `CurrentStep` 0→1 — **with `Plan` still nil**. The next `complete step="provision"` hits the guard and returns `step "provision" requires plan from discover step`. Discover is already `complete` and `CurrentStepName()` is now `provision`, so `BootstrapCompletePlan` rejects (`engine.go:556-558`). **Only `action=reset` escapes** — a closed deadlock.

**Trigger:** the only discover case falling through to `engine.BootstrapComplete(ctx,"discover",...)` at handler line 134 is **classic/empty route with `input.Plan == nil`** (agent submitted `attestation`, omitted the `plan` key). The managed-only canonical path submits `plan:[]` (non-nil at the boundary) → `BootstrapCompletePlan` → non-nil `state.Bootstrap.Plan`. `WorkflowInput.Plan` is `json:"plan,omitempty"`: omitting → nil; `plan:[]` → empty-non-nil. The fix keys on exactly this distinction.

**Second defect:** the action string at `workflow_bootstrap.go:136-139` says `"Start bootstrap first with action=start"` — WRONG for an active session, and points at re-start (which auto-resets, destroying the session).

### FIX 1 — reject planless discover at the source (engine.go)

Replace the guard block at `engine.go:468-471`:
```go
	if state.Bootstrap.Plan == nil {
		if stepName == StepDiscover {
			return nil, fmt.Errorf("bootstrap complete: step %q requires a plan — submit it via action=complete step=discover with a plan (classic), or omit the plan with route=adopt/recipe to derive it; attestation-only does not advance discover", stepName)
		}
		// Non-discover step with no plan: defense-in-depth (should be
		// unreachable now that discover always commits a plan).
		return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
	}
```
The structured-plan paths (`BootstrapCompletePlan/AdoptPlan/RecipePlan`) always set a non-nil Plan (managed-only `plan:[]` → non-nil `ServicePlan{Targets:[]}`; adopt/recipe auto-derive → non-nil). A nil Plan ON discover means the agent reached the free-text attestation path for a step requiring a structured plan: reject, never advance.

**Chain check:** only production caller is `workflow_bootstrap.go:134`. The structured methods go through `completePlanWithTargets` → `CompleteStep(StepDiscover,...)` directly (`engine.go:694`) + set Plan immediately (`:698`) — they do NOT call `BootstrapComplete`. The provision fast-path (`:504-515`) reads `Plan` — now guaranteed non-nil when reached. No new nil-deref.

### FIX 2 — correct the misleading action string (workflow_bootstrap.go:134-140)

```go
		return convertError(platform.NewPlatformError(
			platform.ErrBootstrapNotActive,
			fmt.Sprintf("Complete step failed: %v", err),
			"Check status with action=status workflow=bootstrap; if the discover step rejected, re-call action=complete step=discover with a plan (classic) or omit the plan for route=adopt/recipe — do NOT re-run action=start (it resets the active session)."), WithRecoveryStatus()), nil, nil
```
`action=status` is the canonical recovery primitive (CLAUDE.md "Lifecycle recovery is via action=status"). Error-code stays `ErrBootstrapNotActive` (narrowing would ripple into permission allowlists / agent error catalogs); only the human action string changes.

### TDD

**RED (engine) — `TestBootstrapComplete_PlanlessDiscover_Rejected`** near `engine_test.go:1712`: `BootstrapStart` → `BootstrapComplete(ctx, "discover", attestation, nil)` must error; assert step stays `StepDiscover`, `Plan` stays nil, then `BootstrapCompletePlan([]BootstrapTarget{}, nil, nil)` SUCCEEDS (proves the escape is a structured plan, not reset). FAILS before fix (returns nil + advances), PASSES after.

**GREEN — convert the ~8 unit tests relying on the bug** (each `BootstrapComplete(ctx,"discover",…,nil)` → `BootstrapCompletePlan([]BootstrapTarget{...}, nil, nil)`, managed-only `[]` or a single nodejs target matching intent):

| File:line | Convert to |
|---|---|
| `engine_test.go:570` (`_Success`) | single-target plan; still Current=provision, Completed=1 |
| `engine_test.go:785,813,840,1701` | `BootstrapCompletePlan([]BootstrapTarget{}, nil, nil)` |
| `engine_test.go:1176` (`_WrongStep`) | first plan commits discover, THEN second `BootstrapCompletePlan` rejects "current step is provision, not discover" — same assertion, valid path |
| `bootstrap_kind_test.go:70` | `…[]BootstrapTarget{}…` — Kind stays session-active |
| `workflow_test.go:1177` | `engine.BootstrapCompletePlan([]workflow.BootstrapTarget{}, nil, nil)` |

**Checker-mechanism tests** (`engine_test.go:1292,1332,1361,1490`): discover used only as a checker vehicle, but the new guard rejects discover BEFORE the checker runs. **Re-point to `provision`** with a pre-committed plan: first `BootstrapCompletePlan([]BootstrapTarget{{Runtime:{DevHostname:"appdev",Type:"nodejs@22",BootstrapMode:"standard",ExplicitStage:"appstage"}}}, nil, nil)` → then `BootstrapComplete(ctx,"provision",attestation,checker)`. Update assertions: `_Pass`/`_NilChecker` expect `Current.Name=="close"`; `_Fail` expects `CurrentStep==1`.

**Handler test** (`workflow_test.go:718` `_Action_BootstrapComplete`): convert to submit `"plan": []map[string]any{}`, keep `resp.Current.Name == "provision"`. **ADD a sibling case**: `complete step=discover attestation=…` WITHOUT a plan now returns `IsError` + message contains "requires a plan" — the handler-boundary regression guard.

### REGRESSION GUARD

MUST stay green:
- `TestBootstrapComplete_PlanNilCheck_NonDiscoverSteps:1712` — now the else branch; unchanged.
- `TestEngine_BootstrapComplete_FullSequence:585`, `TestBootstrapComplete_WritesServiceMeta` (`bootstrap_outputs_test.go:13`), `TestEngine_BootstrapComplete_CleanupWarningInResponse:1758` — already use `BootstrapCompletePlan`. Untouched, prove happy path.
- **`TestBootstrapCompleteAdoptPlan_*` + `TestHandleBootstrapComplete_Adopt*`** (CLAUDE.md adopt pins) — adopt never reaches `BootstrapComplete`; **explicit "carefully check adopt path" gate** — must stay green to prove auto-derive (`plan:[]` + omitted-plan) is not broken.
- `workflow_bootstrap_adopt_test.go` (49,74,96,125) — adopt returns early at handler line 84.
- `TestWorkflowTool_Action_Start_AutoResetDone:325` + iterate test `:1230` — already use `plan:[]`.
- `scenarios_test.go:983-1013` — `BootstrapSessionSummary` shapes, not `BootstrapComplete` calls.

**Goldens:** NONE regenerate (error path + action string inside `convertError`; `grep` found no golden referencing "Start bootstrap first" or `BOOTSTRAP_NOT_ACTIVE`). No atom edits.

**Verify:**
```
go test ./internal/workflow/... ./internal/tools/... -race
go test ./... -race
make lint-local
```

### EVAL-ZCP

**Scenario:** `discover-adoption-state-resumable-uses-sessionid`.

**Observable:** (1) NO `BOOTSTRAP_NOT_ACTIVE`/"requires plan from discover step" deadlock loop retrying provision. (2) NO message matching `retrospectiveMustNotMention` ("had to delete state", "tried classic", "tried adopt", "asked user for session"). (3) Agent reaches the resume path (`route=resume sessionId=sess-stale-mid-bootstrap-2026-05-27`); final appdev/appstage/db ACTIVE with `noFailedProcesses:true`. (4) Any planless discover yields the "requires a plan … or use action=status" rejection ONCE, then recovery next turn — no multi-turn loop.

**Runs:** DETERMINISTIC engine/handler bug, unit-pinned (deadlock structurally impossible post-fix) → **1 run** for deadlock-absence. The resume-route choice is LLM-shaped and pre-existing — if that primary goal regresses for unrelated reasons, do 2 more and require deadlock-loop / state-deletion escape absent in **3/3** (deadlock-absence is the deterministic bar).

### RISK + ROLLBACK
- **Blast radius:** `engine.BootstrapComplete` (one prod caller) + one action string + ~14 test conversions. Largest test surface of the battery.
- **Could regress:** an un-enumerated test relying on planless-discover advancing — `go test ./... -race` catches any straggler (unexpected non-nil error from a planless discover); convert that site to `BootstrapCompletePlan` too. Checker-vehicle re-points only assert `CheckResult`/`Current.Name`, not guide content.
- **Adopt-path safety (explicit gate):** guard is in `BootstrapComplete`, which adopt/recipe/classic-with-plan NEVER call for discover (early returns at 84/103/116); `plan:[]` and omitted-plan-adopt both produce non-nil `Plan`. Pinned by keeping `TestBootstrapCompleteAdoptPlan_*` green.
- **Rollback:** revert two edits + test conversions. Single-commit, no migration / on-disk-state / golden.

---

## P5 — export-compose-ready + launch-pipeline knowledge propagation

**Cluster theme:** a handler shipped a terminal/state the knowledge + recovery layers never learned. **F54:** `topology.ExportStatusComposeReady` ("compose-ready", shipped e65e182b) exists in handler + topology enum + topology test, but is absent from (a) the `exportStatus:` atom-axis allow-list (so NO atom can carry it) and (b) the two export doc tables — so the agent gets only the inline fallback, never curated atom guidance. **F43:** `action=status` on a launched/active launch returns an envelope with NO pipeline summary, so after compaction the agent can't say which CD was configured; the recovery atom asserts "no further start calls are needed" — wrong when a re-call with launchKey is exactly what re-probes an unconfigured pipeline.

**Single-owner framing:** `topology.ExportStatus` owns the export sub-status vocabulary; the axis allow-list (`internal/workflow/atom.go`) + doc tables are hand-authored copies that drifted. F54 re-converges tell (atom guidance) and check (axis validator) with the owner. F43 derives the recovery's pipeline tell from `state.PipelineConfigurations` (the owner the fresh `launched` response already reads via `pipelineBlockers`).

### F54 — compose-ready propagation

**Edit 1 — add `compose-ready` to the axis allow-list** (`atom.go` `validAtomEnumValues["exportStatus"]:331-339`): add `"compose-ready": {},` between `validation-failed` and `publish-ready`. **Root, not symptom:** without it, `validateAtomFrontmatter:376` REJECTS any atom declaring `exportStatus: [compose-ready]` — Edit 4's atom literally cannot parse until this lands.
> **FLAG (not in brief):** the allow-list still carries `"variant-prompt"` which is NOT in `topology.ExportStatus` (pre-existing dead drift). **Do NOT remove here** (out of scope) — record as backlog candidate (allow-list should eventually derive from the topology enum).

**Edit 2 — `export-intro.md:28`** doc-table "Returns" cell → `compose-ready (bundle delivered) — or publish-ready if git-push is configured, or validation-failed`. Corrects the false implication that call 3 always lands at publish-ready (it lands at compose-ready when git-push isn't set up — the common path).

**Edit 3 — `export-scope-prompt.md:23`** status list → `scaffold-required / classify-prompt / validation-failed / compose-ready / publish-ready`. **Decision:** drops `git-push-setup-required` from this prose menu (compose-ready is the new decoupled terminal; handler comment `workflow_export.go:520-523`). The status stays valid in the enum/allow-list; only the scope-prompt menu changes. **FLAG: if Karel wants both listed, keep `git-push-setup-required` — one-token prose choice.**

**Edit 4 — author `internal/content/atoms/export-compose-ready.md`** mirroring `export-publish.md`. Frontmatter: `priority: 4`, `phases: [export-active]`, `exportStatus: [compose-ready]`, `environments: [container]`, `references-fields: [bundle.ExportBundle.{ImportYAML,ZeropsYAML,RepoURL,Warnings}]` (verified to resolve via `ops/bundle/outputs.go::ExportBundle` + `loadAtomReferenceFieldIndex`). Body (TRIGGER+ACTION+FAILURE, no spec-IDs/handler-verbs/env-only-title-tokens — Axis L safe): lead "You are at `status="compose-ready"`. The bundle is composed and schema-clean: the `importYaml`+`zeropsYaml` ARE the deliverable. Git-push is NOT configured, and that's fine — publishing via git-push is OPTIONAL." + "Write the deliverable" (write the two yamls verbatim, commit) + "Optional — publish via git-push" + "Re-import elsewhere" (`zcli project project-import` round-trip — the user's stated goal). ≤300 lines.

**Edit 5 — add the coverage scenario** (`scenarios_test.go`, export panel ~line 1219): `{"export-active/compose-ready", StateEnvelope{Phase: PhaseExportActive, Environment: EnvContainer, ExportStatus: topology.ExportStatusComposeReady, Services: []ServiceSnapshot{{Hostname:"appdev", TypeVersion:"nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}}}}`. Update comment `Cover all 7` → `Cover all 8`. **REQUIRED** — else `TestCoverageGate` fails (atom uncovered, no `coverageExempt:`).

### F43 — launch pipelineSummary on the recovery envelope

**Edit 6 — add `PipelineSummary` to `launchActiveEnvelope` + derive it** (`internal/tools/launch_status_recovery.go`):
```go
	PipelineSummary []launchPipelineSummaryEntry `json:"pipelineSummary,omitempty"`
```
```go
type launchPipelineSummaryEntry struct {
	ProdHostname string `json:"prodHostname"`
	Configured   bool   `json:"configured"`
	SkipReason   string `json:"skipReason,omitempty"`
	DeepLink     string `json:"deepLink,omitempty"`
}

func pipelineSummaryFrom(state *launchState) []launchPipelineSummaryEntry {
	if len(state.PipelineConfigurations) == 0 {
		return nil
	}
	hosts := make([]string, 0, len(state.PipelineConfigurations))
	for h := range state.PipelineConfigurations {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts) // deterministic order for compaction-stable envelope
	out := make([]launchPipelineSummaryEntry, 0, len(hosts))
	for _, h := range hosts {
		e := state.PipelineConfigurations[h]
		out = append(out, launchPipelineSummaryEntry{
			ProdHostname: h, Configured: e.Configured,
			SkipReason: e.SkipReason, DeepLink: e.DeepLink,
		})
	}
	return out
}
```
Add `"sort"`. Populate in BOTH builders: `renderLaunchActiveRecovery` (`env := launchActiveEnvelope{...}` ~line 63) and `renderLaunchTerminalRecovery` (~line 173) with `PipelineSummary: pipelineSummaryFrom(active/terminal)`. **The terminal path is the primary F43 target** (the eval's "I just launched, which CD is configured?"). **`sort.Strings` mandatory** — CLAUDE.md requires byte-deterministic status envelope; unsorted map-range is nondeterministic JSON.

**Edit 7 — fix `launch-status-recovery.md` "launch-completed" paragraph (46-48)** — replace the absolute "no further `action="start"` calls are needed" with the `pipelineSummary`-aware conditional: `configured: true` everywhere → deploy with `git tag vX.Y.Z && git push --tags`; any `configured: false` → user finishes dashboard setup (`deepLink`), then a single `action="start"` with launchKey re-probes. Kills the false universal that left the agent "flying blind".

### TDD — RED per fix

- **Edit 1:** `atom_test.go::TestParseAtom_ExportStatusAxis` `full_enum:712` — add `compose-ready` to the `exportStatus:` list AND `wantES` (`topology.ExportStatusComposeReady`). FAILS before Edit 1 (`invalid value "compose-ready"`), GREEN after.
- **Edit 4:** `TestCoverageGate:43` FAILS once the atom exists, until Edit 5 adds the scenario. `TestAtomAuthoringLint` + `TestAtomReferenceFieldIntegrity` pass (export-publish.md-mirrored shape).
- **Edit 6:** `launch_status_recovery_test.go::TestRenderLaunchTerminalRecovery_LaunchedConfirmsCompletion:467` — extend `terminal` fixture with `PipelineConfigurations: {"app":{Configured:true}, "worker":{Configured:false, DeepLink:"..."}}`; assert `len(body.PipelineSummary)==2`, sorted (`app` before `worker`), `[0].Configured`. FAILS before (field absent/len 0), GREEN after. `mustExtractEnvelope` already unmarshals into `launchActiveEnvelope`.
- **Edits 2/3/7:** pure-guidance — atom-lint gate + golden regen + eval re-run.

### REGRESSION GUARD

Stay green:
- `topology/types_test.go::TestExportStatusValues` — already 8 values incl. compose-ready (topology untouched → confirms owner unchanged).
- `atom_test.go::TestParseAtom_ExportStatusAxis` (`invalid_enum_value` asserts `publish` still rejects — adding compose-ready must NOT make the validator permissive).
- `coverage_gate_test.go::TestCoverageGate`, `corpus_pin_density_test.go` — green after Edit 5.
- `atom_reference_field_integrity_test.go`, `content/atoms_lint_test.go::{TestAtomAuthoringLint, TestAtomReferenceFieldIntegrity, TestAtomReferencesAtomsIntegrity}` — green for the new atom.
- `tools/workflow_export_test.go::TestHandleExport_GitPushUnconfigured_DeliversComposeReady` — handler already returns compose-ready; no handler edits → MUST stay green.
- `tools/launch_status_recovery_test.go::{_SingleActive, _MultipleActive, _FailedPointsAtReset}` — new field `omitempty`, fixtures pass no PipelineConfigurations → nil/omitted → existing assertions unchanged.
- `tools/launch_pipeline_test.go` — `pipelineSummaryFrom` additive, reads the same map.
- `synthesize_export_test.go` — SYNTHETIC corpora, not the real atom set; adding a real atom doesn't perturb them.

**Goldens that REGENERATE (commit them):** (1) NEW `testdata/atom-goldens/export/compose-ready.md`; (2) `_coverage-map.md` (new atom row + scenario); (3) `testdata/atom-goldens/launch-production/<launch-production-active>.md` (Edit 7 body). Regen: `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/ -run 'TestScenarios_GoldenComparison'` then re-run without the env var. NO `make schema-sync` (atom-goldens, not API schema embed).

**Verify:** `go test ./... -race` (topology/workflow/tools/content layers) + `make lint-local` (atom-tree + golden gates) + `make lint-fast`.

### EVAL-ZCP

**F54 → `export-buildfromgit-self-snapshot`:** Success — when the bundle composes clean and git-push is NOT configured, the response carries `status="compose-ready"` AND renders the `export-compose-ready` atom body ("the importYaml+zeropsYaml ARE the deliverable, write them, git-push is optional"), and the agent does NOT loop back into git-push-setup as if required. Baseline run 20260604-071303 hit `git-push-setup-required` (placeholder repo, no PAT) and dead-ended. Atom-fires is unit/golden-proven; end-to-end LLM behavior → **2 runs**; bar = agent surfaces the deliverable at compose-ready in ≥2/2.

**F43 → `launch-production-pipeline-configured`:** Success — `action=status` on a persisted launched state returns an envelope whose `pipelineSummary` lists each prod runtime with `configured: true`, and the agent answers with `git tag vX.Y.Z && git push --tags` instead of "flying blind" (verbatim failure in the 20260604-234709 retro). Field-presence unit-pinned (Edit 6); LLM reads it → **2-3 runs**; bar = ≥2/3 surface the per-runtime configured state + land on tag-push, "flying blind" absent. **CAVEAT to flag:** the harness must persist a launched state file with populated `PipelineConfigurations` for `action=status` to hit `renderLaunchTerminalRecovery` — the 20260604 run showed `phase: idle` (no persisted state). **If the scenario only runs a fresh launch (not status-recovery), the F43 recovery envelope can't be exercised — raise with Karel; may need a new/adjusted scenario fixture seeding a launched state file.**

### RISK + ROLLBACK

| Fix | Blast radius | Risk | Rollback |
|---|---|---|---|
| Edit 1 (axis allow-list) | atom-parse; widens a closed enum | LOW — purely additive | revert one-line map add |
| Edits 2/3 (doc tables) | prose | LOW — golden regen on covered scenarios | revert prose |
| Edit 4 (new atom) | new corpus member | LOW-MED — MUST land with Edit 5 or CoverageGate fails build | `git rm` atom + revert scenario |
| Edit 5 (scenario) | panel + 2 goldens | LOW — additive | revert panel + `git checkout` goldens |
| Edit 6 (envelope field) | tools launch JSON | LOW — `omitempty`, compat-safe; `sort`+map determinism the only subtlety | revert field + helper + 2 populate sites |
| Edit 7 (atom prose) | recovery body | LOW — regenerates launch-production-active golden | revert prose + golden |

**Cross-cutting:** no topology change, no export handler logic change (shipped e65e182b), no platform-API change. Backward-compat: new atom server-embedded; new envelope field `omitempty` (existing consumers ignore). **One out-of-scope drift flagged for backlog:** `variant-prompt` lingering in the allow-list without a topology enum member.

---

## P6 — recipe nextjs-ssr deployFiles clarification (F16, ships via sync push)

### Problem (one sentence)
The `nextjs-ssr-hello-world` recipe knowledge presents the narrow standalone cherry-pick (`.next/standalone`, `.next/static`, `public`, `migrate.cjs`) as *the* deployFiles pattern with no deploy-class context, so an agent scaffolding a dev/stage pair copies it into the **dev self-deploy** setup and hits a hard `INVALID_ZEROPS_YML` DM-2 retry.

### Evidence (eval transcript, verbatim)
`eval/behavioral/runs/20260605-025825/recipe-nextjs-ssr-frontend-standard/self-review.md`:
> "the recipe knowledge guide I pulled earlier showed the cherry-pick pattern as *the* pattern for Next.js, without distinguishing self-deploy vs cross-deploy context. A future agent should remember: `deployFiles: [.]` for the dev setup (self-deploy), cherry-pick patterns only for the stage setup (cross-deploy from dev→stage)."

Error owner: `ops/deploy_validate.go:96` — `self-deploy setup %q: deployFiles must be [.] or [./] — narrower patterns destroy the target's working tree on artifact extraction (DM-2)`.

### Root-cause owner map (no duplication)
- The **platform mechanic** (why narrower self-deploy is destructive) is SINGLE-OWNED by atom `develop-deploy-files-self-deploy.md`. The recipe MUST NOT re-author it.
- What's MISSING is the **recipe-specific framing** — *which of its two setups is which deploy class*. That class-mapping is framework-coupled → belongs in the recipe per CLAUDE.md "Recipe-specific findings go in recipes."

### THE EDIT (recipe CONTENT — ships via `sync push`, NOT a committed zcp change)
File: `internal/knowledge/recipes/nextjs-ssr-hello-world.md` — **gitignored** (`.gitignore:44`), synced from Strapi, embedded via `//go:embed all:recipes`. Authored locally, pushed upstream; NOT git-committed in zcp.

Append one bullet to `## 2. Key Configuration Points` (after the `**No .next/cache in build cache**:` bullet):
```markdown
**Deploy class picks the deployFiles shape**: the standalone cherry-pick (`.next/standalone`, `.next/static`, `public`, `migrate.cjs`) is for a **cross-deploy** — a separate stage/prod target whose deploy root holds only built output. A **self-deploy** (source == target — the dev half you SSH into and re-push from the mount) MUST use `deployFiles: [.]`; a narrower selection overwrites the mount with just the build subset and is rejected with `INVALID_ZEROPS_YML` (DM-2). Dev setup → `[.]`; stage/prod setup → cherry-pick.
```

Placement verified against `internal/sync/transform.go`: `## 2.` is a numbered heading → `isIntegrationGuideHeading:199` true → the bullet lands in the **Integration Guide** fragment (pushes to the app-repo). The section has NO ```yaml block → `ZeropsYAML` stays empty → **no `zerops.yml` overwritten upstream**; blast radius bounded to a single IG markdown fragment, `+1 / -0`. No new `## zerops.yaml` H2 / yaml block → `TestRecipeLint`'s yaml-validation path stays SKIPPED for this recipe exactly as today. Do NOT restate the destruction mechanism the atom owns; the recipe's job is only the class→shape mapping.

### Sync push flow (the ship)
1. Edit local `nextjs-ssr-hello-world.md` (one bullet).
2. **Preview first (mandatory per sync-amplification rule):** `zcp sync push recipes nextjs-ssr-hello-world --dry-run`. Expect `+N / -0` IG-fragment diff scoped to the bullet and NOTHING else. If larger → STOP, ask Karel.
3. `zcp sync push recipes nextjs-ssr-hello-world` → GitHub PR on `zerops-recipe-apps/nextjs-ssr-hello-world-app`.
4. Merge the PR (human review).
5. `zcp sync cache-clear nextjs-ssr-hello-world`.
6. `zcp sync pull recipes nextjs-ssr-hello-world` → refresh local embedded `.md`. Confirm the bullet survived round-trip (Strapi may reflow; verify the class→shape mapping intact).

### Scope note
`cadence-multiservice-spec-run2-replay` was offered but has ZERO deployFiles/self-deploy references (grep-empty — pure env-var replay) → NOT a P6 gate. Only `recipe-nextjs-ssr-frontend-standard` is relevant.

### Acceptance — `recipe-nextjs-ssr-frontend-standard` (after merge + cache-clear + pull)
Observable: agent scaffolds the dev setup with `deployFiles: [.]` (or `./`) and the stage setup with the standalone cherry-pick — NO `INVALID_ZEROPS_YML`/"deployFiles must be [.]" retry on the dev half's first deploy.

**LLM-shaped/probabilistic** (the DM-2 gate stays as-is — the correct safety net). No unit-pinned guarantee → **2-3 runs**; bar = DM-2 self-deploy retry ABSENT in ≥2 of 3 (vs the 2026-06-05 baseline where it fired). The `sync pull` (step 6) MUST precede the re-run so the rebuilt binary embeds the new recipe content.

### Risk + rollback
- **Blast radius:** one IG markdown bullet in one recipe. No Go, no atom, no golden, no schema, no committed zcp file. DM-2 gate + self-deploy atom + every test untouched.
- **Regression:** near-zero; the dry-run (step 2) guards against amplification. `TestRecipeLint/nextjs-ssr-hello-world` green at baseline, stays green (no yaml block / `## zerops.yaml` added).
- **Round-trip drift:** Strapi may reflow on pull; verify the mapping intact, re-push if mangled (idempotent).
- **Rollback:** revert the bullet locally + `sync push` again (or revert the merged app-repo PR + cache-clear + pull). No zcp git history to unwind.

---

## 3. Combined Regression Strategy

**Full-suite gate — EVERY commit (P1–P5) must pass before it lands:**
```
go test ./... -race -count=1          # all layers green (unit + tool + integration mock)
make lint-local                       # full lint + atom-tree gates + atom-lint + goldens-compare
```
P6 ships no zcp commit, but its `sync pull` must leave `go test ./internal/knowledge/ -run 'TestRecipeLint/nextjs-ssr-hello-world'` + `go test ./internal/sync/...` green.

**Golden regeneration — the complete cross-plan list.** Regen command (single, shared):
```
ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow/ -run TestScenarios_GoldenComparison
```
| Plan | Goldens that regenerate | Nature |
|---|---|---|
| P1 | `bootstrap/adopt/discover-existing-pair.md`; `bootstrap/classic/provision-local.md`; `bootstrap/recipe/provision.md`; `bootstrap/recipe/close.md`; `develop/first-deploy-dev-dynamic-container.md`; `develop/failure-tier-3.md`; `develop/first-deploy-recipe-implicit-standard.md`; `_coverage-map.md` (no-op) | body prose moves; atom IDs unchanged |
| P2 | NONE | Go-state change only |
| P3 | NONE | runtime error message only |
| P4 | NONE | error path + action string in `convertError` |
| P5 | NEW `export/compose-ready.md`; `_coverage-map.md` (new atom row + scenario); `launch-production/<launch-production-active>.md` | new scenario + atom body |
| P6 | NONE (out-of-tree recipe content) | sync fragment |

**Rule — a regenerated golden is reviewed as a `git diff` before commit.** After `ZCP_UPDATE_ATOM_GOLDENS=1 …` regen, run `git diff -- internal/workflow/testdata/atom-goldens/` and confirm the diff matches ONLY the intended prose move / new scenario render. A golden diff wider than the edit (unexpected atom-ID churn, an unrelated body change, a `_coverage-map.md` row beyond the added scenario) is a STOP signal — investigate before committing. Goldens are committed in the SAME commit as the atom edit that produced them (atomic consistency).

**Change-impact layering (CLAUDE.md):** P5's atom-axis is workflow-layer, its atom corpus is content-layer, its envelope field is tools-layer → `go test ./... -race` is the correct combined gate. P2/P3/P4 are within their owning packages but `./...` still runs to catch cross-package stragglers.

---

## 4. Combined Eval-ZCP Acceptance Battery

flow-eval **rebuilds the working tree**, runs **sequentially on the shared eval-zcp container (~15 min each)**, and MUST run **AFTER the plan's commit is in the tree** (P1–P5) or **AFTER the `sync pull`** (P6). The unit RED→GREEN proves the mechanism; the **live re-run proves the friction is observably gone in a fresh transcript** — i.e. the agent, given the corrected tell/handler, no longer reproduces the friction signature. Unit-green alone is NOT acceptance.

| Plan | Scenario(s) | Observable success in transcript | #runs | Baseline (pre-fix friction) |
|---|---|---|---|---|
| **P1 / F1** | `adopt-existing-standard-pair` | First `complete step=discover` resolves the same-type pair in ONE follow-up (single-runtime then explicit-plan, OR direct two-template `plan=[...]`); NO ≥2 consecutive `ErrAdoptPairingChoice`, no treating it as failure | **2-3** | Agent copied `scope=["appdev","appstage"]`, hit `ErrAdoptPairingChoice`, re-read atom, re-submitted same scope, re-hit it (~18×) |
| **P1 / F27** | `classic-php-mariadb-standard`, `recipe-nestjs-minimal-standard` | Project-env set via `project=true` + `variables=["KEY=value"]`, SUCCEEDS first try; no `INVALID_PARAMETER`/"unexpected property scope/key/value" | **1** each | `action=set scope=project key=X value=Y` rejected at SDK boundary, retry |
| **P1 / F22** | `recipe-nestjs-minimal-standard` | Agent does NOT cite the atom to refuse a valid verify of a live buildFromGit runtime; still skips non-HTTP/managed | **2-3** | Blanket "do not run zerops_verify" advised skipping a legitimate recipe verify |
| **P1 / F9** | `classic-php-mariadb-standard` | `build.base: php@X` (bare) + `run.base: php-apache@X`/`php-nginx@X`; no composite-in-build.base validation error | **1** | Agent copied `build.base: php-apache@latest` from a doc → validation reject |
| **P1 / F19** | `greenfield-fullstack-multi-runtime` (sanity only) | Env-in-start uses `run.envVariables` or `env KEY=val`; no inline-`KEY=val` launch failure | **lint+golden-gated; 1 run sanity** | No sharp scenario — preventive guidance (flagged) |
| **P1 / F57** | `greenfield-fullstack-multi-runtime` | Frontend `NEXT_PUBLIC_*`/`VITE_*` URL = public subdomain; none point at `apidev:3000`/internal hostname | **2-3** | Browser-bundled URL wired to internal hostname → SPA API calls fail |
| **P2 / F11** | `recipe-nestjs-minimal-standard` (+ optional `recipe-laravel-minimal-standard`) | Develop-start envelope shows recipe runtime `deployed: true`; agent does NOT scaffold a `zerops.yaml` / redeploy over the ACTIVE recipe app | **1** (+1 optional) | `deployed:false` → develop re-fired first-deploy/scaffold over a working app |
| **P3 / F36** | `git-push-setup-with-cicd-method-prompt` | (Unit IS the gate.) Probe-failure message carries git stderr (`Authentication failed`/`Repository not found`/etc.), not bare `ssh <host>: exit status 128`; classifier → `failureClassification.category=credential`. Happy-path eval = no-regression (still stamps `configured`) | **1** (no-regression; unit-pinned gate) | `%v` dropped `.Output` → agent saw only `exit status 128`, couldn't pick recovery |
| **P4 / F35** | `discover-adoption-state-resumable-uses-sessionid` | No `BOOTSTRAP_NOT_ACTIVE`/"requires plan from discover" deadlock loop; no `retrospectiveMustNotMention` escape; reaches resume path; final appdev/appstage/db ACTIVE `noFailedProcesses:true` | **1** (deadlock-absence; 3/3 if resume-route regresses) | Planless discover advanced with `Plan==nil` → provision deadlocked; only `reset` escaped |
| **P5 / F54** | `export-buildfromgit-self-snapshot` | `status="compose-ready"` co-occurs with the `export-compose-ready` atom body ("importYaml+zeropsYaml ARE the deliverable, git-push optional"); agent does NOT loop into git-push-setup as required | **2** | Run 20260604-071303 dead-ended at `git-push-setup-required` (placeholder repo, no PAT) |
| **P5 / F43** | `launch-production-pipeline-configured` | `action=status` envelope's `pipelineSummary` lists each prod runtime `configured: true`; agent gives `git tag vX.Y.Z && git push --tags`; "flying blind" absent | **2-3** | Run 20260604-234709: agent "flying blind", "zero ability to verify which integration was set up" |
| **P6 / F16** | `recipe-nextjs-ssr-frontend-standard` | Dev setup `deployFiles: [.]` (or `./`), stage setup standalone cherry-pick; NO `INVALID_ZEROPS_YML`/"deployFiles must be [.]" retry on the dev half | **2-3** | Run 20260605-025825: agent copied cherry-pick into self-deploy → DM-2 retry |

**Run-count rationale:** **1 run** = deterministic + unit-pinned mechanism (F27, F9, F11, F36, F35) — the live run confirms the end-to-end observable the unit already guarantees. **2-3 runs** = LLM-shaped behavior (F1, F22, F57, F54, F43, F16) — acceptance bar is the friction signature ABSENT in ≥2 of the runs, because the agent could have recovered probabilistically pre-fix.

---

## 5. Definition of Done — per plan

A plan is **"shipped"** ONLY when ALL of:

| Gate | P1 | P2 | P3 | P4 | P5 | P6 |
|---|---|---|---|---|---|---|
| Unit RED→GREEN demonstrated | golden+lint (+import_test.go assertion for F27) | new `TestBootstrapRecipe_BuildFromGitStampsFirstDeployed` | new `..._SurfacesGitStderr` | new `..._PlanlessDiscover_Rejected` + ~14 conversions | `TestParseAtom` + `TestCoverageGate` + `TestRenderLaunchTerminalRecovery` | n/a (LLM-shaped; DM-2 gate unchanged) |
| `go test ./... -race -count=1` green | ✅ | ✅ | ✅ | ✅ | ✅ | recipe-lint + sync tests green |
| `make lint-local` green (atom-tree + golden gates) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Goldens reviewed as `git diff` (only intended moves) | 6 goldens + coverage-map | none | none | none | 3 goldens | none |
| eval-zcp scenario shows friction GONE in a fresh transcript | per battery (§4) | per battery | per battery (+ optional hand-trigger) | per battery | per battery (incl. F43 fixture caveat) | per battery (after pull) |
| For P6 only: upstream PR merged + cache-clear + pull round-trip verified | — | — | — | — | — | ✅ |

Each plan is **one atomic commit** (P1–P5) — compiles, full suite green, self-consistent. **Release / `make install` is a SEPARATE Karel decision** taken after he reviews what shipped — NOT inferred from any plan's completion (per CLAUDE.local.md).

---

## 6. Open Questions / Karel Decisions

Surfaced explicitly, not silently decided:

1. **P1 / F1 — fix style (example vs note).** Plan chose BOTH: narrow the worked example to `scope=["appdev"]` AND add a forward-pointer sentence routing the pair case to line 38's template. Alternative considered: keep the same-type example + only add a clarifying note. Rejected because the copied example is the trap — narrowing it removes the contradiction at the source. **Confirm both, or example-only / note-only?**

2. **P1 — scope additions beyond the brief (2 extra F27 owners).** `internal/ops/import.go:101` (production IMPORT_HAS_PROJECT hint) + its `import_test.go:107` pin, and `develop-local-env-channels.md` lines 16/31/36 (3 bare-form occurrences). In-scope-correct for single-owner consistency but NOT in the original 3-owner brief. **Fold in (recommended), or keep P1 to the 3 named owners and backlog the rest?**

3. **P1 / F19 — no sharp eval scenario.** Acceptance is lint+golden-gated only; `greenfield-fullstack-multi-runtime` is an opportunistic sanity check, not proof. **Accept lint/golden as the F19 gate, or author a dedicated inline-`KEY=val`-start scenario?**

4. **P1 / F57 — separate guide sync push.** Unlike the other P1 atom edits (in-repo), F57 edits `internal/knowledge/guides/environment-variables.md` and reaches users only via `zcp sync push guides` → PR → merge → cache-clear → pull. **Ship F57 in the same P1 work-session (with the separate guide push), or split it into its own mini-plan?**

5. **P3 / F36 — classifier half (ship or minimal).** Recommended: ship `classifyTransportError` + `WithFailureClassification` (+1 line, free, parity with the deploy path, structured `failureClassification.category=credential`). Minimal alternative: only `detail` (richer message, no classification field). **Ship both (recommended), or message-only?**

6. **P3 / F36 — live failure-path verification.** The scripted scenario supplies a VALID token (probe succeeds, diagnostic never shown). Recommendation (a): unit test is the gate + 1 happy-path eval run for no-regression. Option (b): Karel hand-triggers a bad-token confirm against eval-zcp to eyeball the stderr surfacing. **(a), or do you want (b) too?**

7. **P4 / F35 — test-conversion blast radius.** ~14 unit tests change (8 planless→`BootstrapCompletePlan`, 4 checker-vehicle re-pointed discover→provision, 1 handler convert + 1 new sibling). Each is mechanical but touches assertions (provision→close, CurrentStep 0→1). **OK to convert all in the same commit as the fix (atomic), or do you want the test-conversion diff reviewed separately first?**

8. **P5 / F43 — scenario fixture may not seed a launched state.** The 20260604 run showed `phase: idle` (no persisted state) — `action=status` then can't hit `renderLaunchTerminalRecovery`, so the recovery envelope (and thus F43's observable) isn't exercised. **Does `launch-production-pipeline-configured` seed a persisted launched state file with populated `PipelineConfigurations`? If not, we need a new/adjusted status-recovery fixture — your call on authoring it.**

9. **P5 / F54 — Edit 3 prose menu.** `export-scope-prompt.md:23` drops `git-push-setup-required` from the status menu (compose-ready is the new decoupled terminal). The status stays valid in the enum/allow-list. **Drop it (recommended — leads with the common terminal), or keep both listed?**

10. **P5 — `variant-prompt` allow-list drift (backlog).** The `exportStatus` allow-list carries `variant-prompt`, which has NO `topology.ExportStatus` member (pre-existing dead drift). Plan does NOT touch it (out of scope). **Backlog it (allow-list should eventually derive from the topology enum, killing hand-maintained drift), or leave unrecorded?**