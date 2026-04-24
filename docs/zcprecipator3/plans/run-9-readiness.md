# Run 9 readiness — implementation plan

Run 8 was v9.5.7's first live dogfood (`zcprecipator-nestjs-showcase`, 2026-04-24). It cleared research, provision, scaffold, and feature phases; finalize aborted on two `stitch-content` errors plus a user-issued interrupt at 12:20:30 UTC. This plan enumerates what a fresh implementation session ships before run 9 so the next dogfood reaches `complete-phase finalize` green AND produces content matching the reference deliverable shape.

Reference material (all already written):
- [docs/zcprecipator3/runs/8/ANALYSIS.md](../runs/8/ANALYSIS.md) — self-contained run-8 analysis with per-gap root cause + fix direction; every workstream in §2 below cites a gap letter defined there.
- [spec-content-surfaces.md](../../spec-content-surfaces.md) — 7 surface contracts + classification taxonomy + citation map + anti-patterns.
- [run-8-readiness.md](run-8-readiness.md) — prior run's implementation plan; §2 workstreams A–F shipped in v9.5.7 and stay in force. Every run-9 workstream layers on top, never replacing.
- [CHANGELOG.md](../CHANGELOG.md) — 2026-04-24 entry describes v9.5.7.
- `/Users/fxck/www/laravel-showcase-app/` — apps-repo-shape reference (per-codebase `README.md` + `CLAUDE.md` + commented `zerops.yaml`). Run 9's output should match this shape + voice.
- `/Users/fxck/www/recipes/laravel-showcase/` — recipes-repo-shape reference (root README + 6 tier folders).

Run-8 artifacts: [docs/zcprecipator3/runs/8/](../runs/8/)

---

## 0. Preamble — context a fresh instance needs

### 0.1 What v3 is (one paragraph)

zcprecipator3 (v3) is the Go recipe-authoring engine at [internal/recipe/](../../../internal/recipe/). Given a slug (e.g. `nestjs-showcase`), v3 drives a five-phase pipeline (research → provision → scaffold → feature → finalize) that produces a deployable Zerops recipe. The engine never authors prose — it renders templates, substitutes structural tokens, splices in-phase-authored fragments into extract markers, copies committed per-codebase `zerops.yaml` verbatim, classifies facts, and runs surface validators. Sub-agents (Claude Code `Agent` dispatch) author codebase-scoped fragments at the moment they hold the densest context; the main agent authors root + env fragments at finalize. v2 — the older bootstrap/develop workflow engine at [internal/content/](../../../internal/content/) + [internal/workflow/](../../../internal/workflow/) — is frozen at v8.113.0 but carries institutional knowledge several v3 gaps need to port forward.

### 0.2 Where run 8 stopped + what the classes of defect are

Run 8 closed scaffold and feature, then hit three engine defects in finalize AND shipped five content-pipeline defects even in the surfaces that did render. Details + evidence live in [runs/8/ANALYSIS.md §1 + §3](../runs/8/ANALYSIS.md). Summary:

**Engine-delivery defects** (prevent finalize from completing):
- **A1** — `stitch-content` scanner flags literal `{UPPERCASE}` or `${UPPERCASE}` inside fragment code blocks as unbound template tokens ([assemble.go:37](../../../internal/recipe/assemble.go#L37)).
- **A2** — per-codebase `zerops.yaml` copy is silently a NO-OP because `Codebase.SourceRoot` is never populated on live runs ([handlers.go:458-475](../../../internal/recipe/handlers.go#L458)).
- **C** — feature sub-agent's facts land in `legacy-facts.jsonl` (v2 `zerops_record_fact`) instead of v3's `facts.jsonl`, invisible to `Classify` + `ClassifyLog` + validators.

**Content-pipeline defects** (make what DOES get shipped wrong-shaped):
- **G.1** — scaffold-authored `zerops.yaml` has no `zsc noop --silent` dev-server pattern; dev and prod collapse into "same shape, different `deployFiles`" (tier 0 + 1 iteration flow broken).
- **G.2** — sub-agents run framework CLIs (`npm install`, `tsc`, `nest build`) directly against the SSHFS mount from the zcp host, tunneling every file IO through FUSE and producing build artifacts that don't match the in-container deploy's build.
- **H** — scaffold-authored yaml uses `# ---` ASCII divider banners; reference `laravel-showcase-app` has zero. Prohibited at five v2 locations.
- **I** — sub-agent authoring-phase voice leaks into every artifact: `codebase/<h>/intro` fragments narrate "the scaffold ships… the feature phase will wire…"; 19 committed source-code comments reference "pre-ship contract item N", "showcase default", "scaffold smoke test" — meaningless to a porter cloning the apps repo.
- **K** — main agent dispatched the three scaffold sub-agents sequentially against [phase_entry/scaffold.md:7](../../../internal/recipe/content/phase_entry/scaffold.md#L7)'s "parallelize when shape is 2 or 3"; ~23 minutes of wall time lost.

**Engine ergonomics** (compound debugging cost):
- **J** — 22 `record-fragment` calls in run 8 all returned byte-identical `{"ok":true,"action":"record-fragment","slug":"nestjs-showcase"}`; author has no signal of which fragment landed or whether append fired.

### 0.3 Workstream legend

Each workstream letter maps to the analysis gap letter (A1/A2/C/G/H/I/J/K) or inherits a run-8-readiness letter (E for deferred-gate plumbing). New this run: **B** (phase-atom + feature-brief facts routing, covering ANALYSIS §3 gap B + gap C combined), **R** (regression tests).

| Letter | Scope | Tranche |
|---|---|---|
| A1 | Unreplaced-token scan scope fix | 1 |
| A2 | `Codebase.SourceRoot` population on live runs | 1 |
| B  | Feature brief routes facts via v3 tool + browser-verification FactRecord pattern | 1 |
| E  | `surfaceHint` casing normalization across briefs, atoms, error messages, spec | 1 |
| G1 | Dev-loop principles atom port (`zsc noop`, `zerops_dev_server`, dev-vs-prod process model, self-preserving deploy files) | 2 |
| G2 | Mount-vs-container execution-split principles atom | 2 |
| H  | YAML comment-style principle atom + divider-banner validator | 2 |
| I  | Porter-voice rule across every sub-agent output + source-code comment validator | 2 |
| K  | `phase_entry/scaffold.md` parallel-dispatch directive | 2 |
| J  | `record-fragment` response echoes fragmentId + bodyBytes + appended | 2 |
| R  | End-to-end regression fixture for code-block fragment bodies | 3 |

---

## 1. Goals for run 9

A recipe run of `nestjs-showcase` that:

1. **Reaches `complete-phase finalize`** without a `stitch-content` error on any fragment body that contains legitimate `{UPPERCASE}` or `${UPPERCASE}` code syntax.

2. **Ships a per-codebase `zerops.yaml` for every codebase**, copied verbatim from the scaffold's SSHFS workspace so every inline comment survives byte-identical.

3. **Records browser-walk verification as a `surfaceHint: browser-verification` FactRecord** (one per feature tab), visible in `facts.jsonl` so acceptance criterion 2 becomes testable.

4. **Shows feature-phase facts in `facts.jsonl`** (not `legacy-facts.jsonl`) so `Classify`, `ClassifyLog`, and surface validators see them.

5. **Scaffold yamls carry a real dev-vs-prod process-model distinction** — dev uses `start: zsc noop --silent`, no `healthCheck`, install-only `buildCommands`; prod auto-starts on a compiled entry + `readinessCheck` + `healthCheck` + full build + narrow `deployFiles`. Matches `laravel-showcase-app/zerops.yaml` for dynamic runtimes.

6. **Zero `# ---` divider or banner comments** in any scaffold- or feature-authored `zerops.yaml`.

7. **Codebase intros + committed source comments read as porter-facing product descriptions** — no references to "the scaffold", "feature phase", "pre-ship contract", "showcase tier/default/tradeoff", "we chose", "we added".

8. **Scaffold sub-agents dispatched in parallel**; total scaffold wall time bounded by the slowest sub-agent, not the sum.

Stretch: criterion 9 from run-8 (finalize gates all pass on the full deliverable) and criterion 10 (click-deployable end-to-end) become measurable for the first time. If tranche-2 content fixes land cleanly, run 9 hits both.

---

## 2. Workstreams

### 2.0 Guiding principle — engine delivery before content correctness

Tranche 1 (A1, A2, B, E) is the minimum engine change that unblocks finalize. Without it, tranche-2 fixes are unobservable because the pipeline never reaches the validators that would prove them. Tranche 2 (G1, G2, H, I, K, J) determines whether what finalize ships matches the reference deliverable. Tranche 3 (R) pins tranche-1's fix with a regression fixture so the defect can't return silently.

Three invariants the implementation session must hold:

1. **No architectural work.** Every gap in ANALYSIS §3 is a small patch. None of them justify redesigning state, renaming types, or splitting packages.
2. **Root cause, not trigger.** Fix the token scanner's scope — don't special-case `{API_URL}`. Port a compressed `zsc noop` atom — don't hardcode dev-start command detection. Every fix must survive the next framework.
3. **No backwards-compat gymnastics.** Run 9 is a dogfood, not production. If a fix renames a field or tightens a validator, it just breaks forward. No deprecation paths, no adapters.

### 2.A1 — unreplaced-token scan scope fix

**What run 8 showed** ([ANALYSIS §3 gap A1](../runs/8/ANALYSIS.md)): two consecutive `stitch-content` calls errored with `template has unbound tokens: {API_URL}`. `{API_URL}` appeared nowhere in any template or structural data — only inside the feature sub-agent's `codebase/app/integration-guide` fragment as literal JavaScript template-literal syntax `${API_URL}`. The post-render scanner at [assemble.go:240-246](../../../internal/recipe/assemble.go#L240) runs on the body AFTER `substituteFragmentMarkers` has injected fragment content, so every `{UPPER_SNAKE}` token inside a fragment's code block looks indistinguishable from an unbound template token. Fragment bodies routinely contain `{UPPERCASE}` or `${UPPERCASE}` in code examples (Svelte, Vue, JSX, JS template literals, Go template syntax, Handlebars); the scanner cannot tell them apart from engine-bound tokens.

**Root cause named file**: [internal/recipe/assemble.go](../../../internal/recipe/assemble.go):
- Line 37 — `unreplacedTokenRE = regexp.MustCompile(`\{[A-Z][A-Z0-9_]*\}`)` — matches any `{UPPER_SNAKE}`.
- Lines 49, 73, 95, 118 — `checkUnreplacedTokens(body)` is called on the body AFTER `substituteFragmentMarkers(...)`, so the scan sees both template tokens AND fragment-authored code.
- Lines 240-246 — `checkUnreplacedTokens` returns error on ANY leftover match.

**Fix direction**: constrain the scan to engine-bound tokens. Two viable implementations; pick the simpler one:

- **Option A (preferred — known-tokens allowlist)**: change `checkUnreplacedTokens` to accept the set of tokens the caller expected to bind (`{SLUG}`, `{FRAMEWORK}`, `{HOSTNAME}`, `{TIER_LABEL}`, `{TIER_SUFFIX}`, `{TIER_LIST}`). Any remaining match whose key is IN the allowlist is a real defect (unreplaced engine token); any match whose key is NOT in the allowlist is fragment-authored code and passes. Wrapper signature becomes `checkUnreplacedTokens(body string, known []string) error`. Callers pass their respective token key set.

- **Option B (pre-render scan)**: run `checkUnreplacedTokens` BEFORE `substituteFragmentMarkers` so fragment bodies are never scanned. Simpler diff but weaker invariant — a template author who introduces a new `{FOO}` token that isn't bound would still slip through on render.

Use Option A. Option B permits drift at the template layer that Option A catches.

**Improve error text while editing**: current error is `template has unbound tokens: {API_URL}`. Name the surface + fragment id the author can navigate to. Example target: `assemble codebase/app README: template has unbound tokens: {API_URL} (likely inside fragment body — scanner checks only engine-bound keys)`. The run-8 main agent spent ~2 minutes spelunking `find /var/www/recipes` and `grep`ing rendered files because the error didn't name which surface or fragment.

**Changes:**
1. [internal/recipe/assemble.go](../../../internal/recipe/assemble.go) — introduce a package constant `engineBoundKeys = []string{"SLUG", "FRAMEWORK", "HOSTNAME", "TIER_LABEL", "TIER_SUFFIX", "TIER_LIST"}`. Rewrite `checkUnreplacedTokens` to filter matches against that allowlist.
2. [internal/recipe/assemble.go](../../../internal/recipe/assemble.go) — each `AssembleRoot…` / `AssembleEnv…` / `AssembleCodebase…` wraps the scan error with its surface identifier + fragment-search hint.
3. Callers in [handlers.go:400-447](../../../internal/recipe/handlers.go#L400) `stitchContent` — no change needed if the error text already carries the surface name.

**Touches**: one file + test file. <40 LoC net.

**Test coverage** (new tests in [internal/recipe/assemble_test.go](../../../internal/recipe/assemble_test.go)):
- `TestAssemble_FragmentBodyWith_JSTemplateLiteral_NotFlagged` — fragment body contains `fetch(\`${API_URL}/items\`)`; assemble returns no error.
- `TestAssemble_FragmentBodyWith_BareCurlyToken_NotFlagged` — fragment body contains `{FILENAME}` (Svelte/Handlebars-style); assemble returns no error.
- `TestAssemble_UnreplacedEngineToken_IsFlagged` — template carries `{UNKNOWN_ENGINE_TOKEN}` directly (not inside a fragment); assemble returns an error naming the token.
- `TestAssemble_ErrorNamesSurface` — failure message contains the surface identifier (`root`, `codebase/<h>`, or `env/<N>`).

### 2.A2 — `Codebase.SourceRoot` population on live runs

**What run 8 showed** ([ANALYSIS §3 gap A2](../runs/8/ANALYSIS.md)): rendered tree at run close had NO `codebases/<h>/zerops.yaml` for any codebase, even the one whose README + CLAUDE.md rendered cleanly (api). [handlers.go:458-475](../../../internal/recipe/handlers.go#L458) `copyCommittedYAML` soft-fails (returns `nil`) when `cb.SourceRoot == ""`. `grep -n "SourceRoot =" internal/recipe/` finds assignments only in [chain.go](../../../internal/recipe/chain.go) (for parent-recipe codebases loaded during chain resolution) and in test fixtures. No code path populates `SourceRoot` for the live run's own codebases, so `copyCommittedYAML` is a silent NO-OP on every dogfood.

**Root cause (stated plainly)**: scaffold writes `zerops.yaml` to `/var/www/<hostname>dev/zerops.yaml` via the SSHFS mount. Nothing informs the engine where that file lives. The `Codebase` struct reserves the field ([plan.go:58](../../../internal/recipe/plan.go#L58)) but leaves it empty.

**Fix direction**: populate `SourceRoot` at `enter-phase scaffold` from the convention "dev slot SSHFS mount". v3's slot model (ANALYSIS §0.2) says every codebase hostname `<h>` materializes as `<h>dev` (mountable) + `<h>stage` (cross-deploy target). The scaffold-authoring workspace is always the dev slot. Convention-based population fits every live run:

```go
// Pseudocode, in handlers.go enter-phase for Phase("scaffold"):
if sess.Plan != nil {
    for i, cb := range sess.Plan.Codebases {
        if cb.SourceRoot == "" {
            sess.Plan.Codebases[i].SourceRoot = "/var/www/" + cb.Hostname + "dev"
        }
    }
}
```

Override path: if an author explicitly sets `SourceRoot` via `update-plan` before `enter-phase scaffold` (chain-resolver case, or non-standard mount path), the handler leaves the explicit value alone. Only empty-string values get the convention.

**Why enter-phase scaffold, not complete-phase provision**: at `complete-phase provision`, mounts exist — but there's no requirement that the main agent called `zerops_mount` for every codebase before completing provision. Tying to `enter-phase scaffold` means the population happens AT THE MOMENT SCAFFOLD-WORKSPACE PATHS BECOME LOAD-BEARING, regardless of the mount-call order during provision.

**Why not inside `zerops_mount`**: that tool is shared between v2 and v3 workflows; adding a recipe-specific side-effect to it widens coupling for marginal benefit. The convention-based population in the recipe handler is localized and equally correct.

**Fail-loud on copy gap** (related refinement, cheap): `copyCommittedYAML` currently soft-fails on `SourceRoot == ""`. After A2, `SourceRoot` is always populated at scaffold entry, so a missing `zerops.yaml` on disk now means the scaffold sub-agent failed to author it — a hard stitch error. Change the soft-fail to `return fmt.Errorf("codebase %q has no SourceRoot — scaffold did not run or was skipped", cb.Hostname)`. This surfaces the orthogonal case (scaffold skipped entirely) as a gate failure instead of silent empty output.

**Changes:**
1. [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — `enter-phase` case, after `sess.EnterPhase(Phase(in.Phase))` succeeds and the new phase is `scaffold`, populate `SourceRoot` from the convention on any empty codebase.
2. [internal/recipe/handlers.go:458-475](../../../internal/recipe/handlers.go#L458) `copyCommittedYAML` — replace the `cb.SourceRoot == ""` soft-fail with a hard error (now that A2 guarantees population).
3. Optional: expose the convention as a package-level func `DefaultSourceRoot(hostname string) string` so tests + future call sites share it.

**Touches**: [handlers.go](../../../internal/recipe/handlers.go) + handler test. ~20 LoC net.

**Test coverage** (new tests in [internal/recipe/handlers_test.go](../../../internal/recipe/handlers_test.go)):
- `TestEnterPhase_Scaffold_PopulatesSourceRoot` — plan with 3 codebases, `SourceRoot` empty before; after `enter-phase scaffold`, all three have `/var/www/<hostname>dev`.
- `TestEnterPhase_Scaffold_DoesNotOverrideExplicitSourceRoot` — one codebase has `SourceRoot` pre-set; after `enter-phase scaffold`, that one is untouched, others populate.
- `TestCopyCommittedYAML_MissingFileReturnsHardError` — `SourceRoot` points at a real dir but no `zerops.yaml` inside; `copyCommittedYAML` returns error mentioning "scaffold did not author".
- `TestStitch_PerCodebaseYamlCopied` — fixture with scaffold-authored yaml in temp dir, `SourceRoot` set; post-stitch tree contains `codebases/<h>/zerops.yaml` byte-identical to the source, including comments.

### 2.B — feature brief routes facts via v3 tool + browser-verification FactRecord pattern

**What run 8 showed** ([ANALYSIS §3 gaps B + C](../runs/8/ANALYSIS.md)): the feature sub-agent made zero `zerops_recipe action=record-fact` calls and three `zerops_record_fact` calls (v2 tool; `/agent-ac006` lines 234, 236, 298). The v2 tool writes to `legacy-facts.jsonl` when under a recipe session (v9.5.7's E workstream routing) — structurally correct but invisible to v3's `Classify`/`ClassifyLog`. The feature sub-agent ALSO made 5 `zerops_browser` tool calls (`/agent-ac006` lines 210, 221, 241, 269, 295) and zero FactRecords after any of them, despite run-8-readiness §7 Q4 specifying `FactRecord.Type=browser_verification` per tab. The engine has no defect — the brief + atom simply don't teach the wiring.

The scaffold brief DOES teach record-fact: scaffold-api/app/worker made 5 correct `zerops_recipe action=record-fact` calls between them (with two initial-attempt schema-validation retries due to the case issue covered in E). The feature brief doesn't; neither does the feature phase_entry atom.

**Root cause**:
- [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — 15 lines, covers append semantics + typical scale. No section on fact recording.
- [internal/recipe/content/phase_entry/feature.md](../../../internal/recipe/content/phase_entry/feature.md) lines 41-48 — browser-walk step says `record a zerops_record_fact of type browser_verification`. Wrong tool. Should be `zerops_recipe action=record-fact` with `surfaceHint: browser-verification`.

**Fix direction**: two content-only edits.

1. **Feature brief — new "Recording feature-phase facts" section** in [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md):
   - State explicitly: "record every platform-trap / porter-change / scaffold-decision fact via `zerops_recipe action=record-fact`, NOT the legacy `zerops_record_fact` tool. v3 facts land in `facts.jsonl` where the classifier and validators see them."
   - Include the shape: `{ topic, symptom, mechanism, surfaceHint, citation, scope }` — one-line example per field.
   - Port the scaffold brief's classification-before-routing line: self-inflicted findings DISCARD; platform × framework intersections route to KB with citation; genuine platform traps route to KB; operational observations route to CLAUDE.md.

2. **Feature phase_entry atom — browser-walk wiring** in [phase_entry/feature.md](../../../internal/recipe/content/phase_entry/feature.md) step 7:
   - Replace the current `zerops_record_fact of type browser_verification` instruction with: "After EVERY `zerops_browser` tool call, record one FactRecord via `zerops_recipe action=record-fact` with `surfaceHint: browser-verification`. Fill `topic: <codebase>-<tab>-browser`, `symptom: <what you checked and whether the signal was visible>`, `mechanism: zerops_browser`, `citation: none`, `scope: <service>/<tab>`, and stash the screenshot path + console digest in `extra.screenshot` and `extra.console`."
   - Note explicitly: the existing `surfaceHint` taxonomy in the classifier ([classify.go:61-85](../../../internal/recipe/classify.go#L61)) does NOT include `browser-verification`. The Classify function's default branch routes unknown hints to `ClassScaffoldDecision`, which is publishable. That's the correct route for browser verifications (they're an operational / scaffold-decision observation). Optionally add `"browser-verification": ClassOperational` explicitly to classify.go to surface intent at the code level; not strictly required for run 9.

**Brief cap pressure**: [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) is currently ~500 bytes; the new fact-recording section adds ~400. Total feature brief stays well under the 5 KB cap ([briefs.go:26](../../../internal/recipe/briefs.go#L26)).

**Changes:**
1. [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — append the "Recording feature-phase facts" section.
2. [phase_entry/feature.md](../../../internal/recipe/content/phase_entry/feature.md) — rewrite step 7 with the v3 tool + FactRecord shape.
3. (Optional) [classify.go](../../../internal/recipe/classify.go) — add `"browser-verification"` to the switch; map to `ClassOperational`.

**Touches**: 2 `.md` files + optionally [classify.go](../../../internal/recipe/classify.go). Content-only for the required piece.

**Test coverage** (the content atoms themselves aren't test-covered today; add a single pin):
- `TestFeatureBrief_IncludesV3FactRecordingSection` — `BuildFeatureBrief` output contains `zerops_recipe action=record-fact` AND does NOT contain unqualified `zerops_record_fact`.
- `TestPhaseEntry_Feature_BrowserWalkUsesV3Tool` — the feature phase atom embedded in tests or loaded via `loadPhaseEntry` contains `zerops_recipe action=record-fact` in the browser-walk step.
- (If classify.go updated) `TestClassify_BrowserVerificationIsOperational`.

### 2.E — `surfaceHint` casing normalization across briefs, atoms, error messages, spec

**What run 8 showed** ([ANALYSIS §3 gap C second root cause](../runs/8/ANALYSIS.md)): both scaffold-worker (agent-a446 line 134) and scaffold-app (agent-af497 line 198) failed their FIRST `zerops_recipe action=record-fact` with `validating /properties/fact: unexpected additional properties ["surface_hint"]`. The engine's `FactRecord` struct has JSON tag `surfaceHint` (camelCase; [facts.go:22](../../../internal/recipe/facts.go#L22)). Author prose (briefs + spec prose + the engine's own error message at [facts.go:40](../../../internal/recipe/facts.go#L40)) says `surface_hint` (snake_case). Sub-agents read the brief, send snake_case, JSON schema rejects, sub-agent recovers on retry. Each retry costs one round-trip. Two of three scaffold sub-agents hit it.

**Root cause (exact file:line evidence)**:
- [internal/recipe/facts.go:22](../../../internal/recipe/facts.go#L22) — struct tag `json:"surfaceHint"` (the correct shape).
- [internal/recipe/facts.go:40](../../../internal/recipe/facts.go#L40) — `return errors.New("fact record missing required field \"surface_hint\"")` (wrong shape, snake_case).
- [internal/recipe/content/briefs/scaffold/fact_recording.md:8](../../../internal/recipe/content/briefs/scaffold/fact_recording.md#L8) — brief prose says `surface_hint` (wrong).
- [internal/recipe/facts_test.go:14,33](../../../internal/recipe/facts_test.go#L14) — test fixtures use `surface_hint`, pinning the wrong spelling.

**Fix direction**: normalize every reference to `surfaceHint` (camelCase — the wire format the JSON tag defines). Every downstream reader is a sub-agent sending JSON; camelCase wins.

**Changes (all trivial find/replace, independently verify each)**:
1. [internal/recipe/facts.go:40](../../../internal/recipe/facts.go#L40) — error message to `"fact record missing required field \"surfaceHint\""`.
2. [internal/recipe/content/briefs/scaffold/fact_recording.md:8](../../../internal/recipe/content/briefs/scaffold/fact_recording.md#L8) — prose to `surfaceHint`.
3. [internal/recipe/facts_test.go](../../../internal/recipe/facts_test.go) lines 14, 33 — test literals to `surfaceHint`, plus any test-assertion comparing the error string.
4. [docs/spec-content-surfaces.md](../../spec-content-surfaces.md) — grep for `surface_hint`; if any occurrence remains (analysis found none on last sweep but recheck at implementation), normalize.
5. Any `brief_test.go` assertion that greps for `surface_hint` — update.

**Touches**: 2 `.go` files + 1 `.md` + optionally spec. <15 LoC.

**Test coverage**: existing `facts_test.go` catches this once updated. Nothing new to add.

### 2.G1 — dev-loop principles atom port

**What run 8 showed** ([ANALYSIS §3 gap G.1](../runs/8/ANALYSIS.md)): three scaffold-authored `zerops.yaml` files all had dev and prod `run.start` collapsed into "same compiled entry, different `deployFiles` scope":

| Codebase | dev `run.start` | prod `run.start` |
|---|---|---|
| api | `node dist/main.js` | `node dist/main.js` |
| app | `sh -c 'npx vite preview --host 0.0.0.0 --port $PORT'` | same |
| worker | `node dist/main.js` | same |

Zero uses of `zsc noop --silent`. Zero calls to the `zerops_dev_server` MCP tool across all four sub-agents — the tool exists ([internal/tools/dev_server.go](../../../internal/tools/dev_server.go)) but no v3 brief or atom references it. Consequence: dev containers auto-run the compiled app at boot, so iterative code changes force a full redeploy cycle. Tier 0 (AI Agent) and tier 1 (Remote CDE) — both centered on the dev-slot iterative flow — ship broken. scaffold-app lost ~10 minutes in redeploy loops because the dev container kept auto-starting with bad config.

v2 teaches the pattern in 6+ atoms under [internal/content/atoms/develop-*.md](../../../internal/content/atoms/), notably:
- [develop-checklist-dev-mode.md:12](../../../internal/content/atoms/develop-checklist-dev-mode.md#L12) — "Dev entry in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`."
- [develop-manual-deploy.md:23-24](../../../internal/content/atoms/develop-manual-deploy.md#L23) — "Dev services (`zsc noop`): server does not auto-start after deploy. Start via `zerops_dev_server`…"
- [develop-dynamic-runtime-start-container.md:11-65](../../../internal/content/atoms/develop-dynamic-runtime-start-container.md) — full `zerops_dev_server action=start|status|restart|logs|stop` pattern + rationale for detach correctness + 120-second SSH channel pitfall.

v3's [internal/recipe/content/principles/](../../../internal/recipe/content/principles/) currently contains exactly two atoms (`env-var-model.md`, `init-commands-model.md`). No dev-loop coverage.

**Fix direction**: port a compressed single-topic atom into [internal/recipe/content/principles/dev-loop.md](../../../internal/recipe/content/principles/dev-loop.md). Target ~1 KB — covers the "why" + the wiring without re-inlining all 40 v2 atoms.

**Atom contents** (one file, not four):
- **Dev vs prod process model.** Dev slot runs `start: zsc noop --silent`, no `healthCheck`, `buildCommands` installs deps only (no compiled build). Prod slot starts on the compiled entry with a full `buildCommands` chain + `readinessCheck` + `healthCheck`. The split is not cosmetic — dev containers need the agent to own the long-running process so code edits don't require redeploys.
- **`zerops_dev_server` tool.** Start / status / restart / logs / stop. Pass `command` = the prod `run.start` value (what dev would run if it auto-started), `port` = `run.ports[0].port`, `healthPath` = app health route or `/`. Check `action=status` first when uncertain — avoids spawning a second listener on a bound port.
- **Why not `ssh <h> "cmd &"`.** Backgrounded commands hold the SSH channel open until the 120s timeout fires because the child still owns stdio. `zerops_dev_server` detaches with `ssh -T -n` + `setsid` + stdio redirect. The tool is the single canonical primitive for dev-process lifecycle.
- **Self-preserving deploy files on dev.** Dev self-deploys require `deployFiles: .` — narrowing to `[dist, package.json]` wipes the source tree on the next cycle. Cross-deploys (dev → stage) use narrow lists. Cite `laravel-showcase-app/zerops.yaml` as the shape reference.

**Implicit-webserver exception**: Dynamic runtimes (nodejs, php, go, python, deno, bun) follow the rule above. Implicit-webserver runtimes (`php-nginx`, `php-apache`, `nginx`, `static`) omit `run.start` entirely on both dev AND prod — the server is the runtime's own. Atom states the exception explicitly so the brief composer's conditional injection rule is discoverable from the content side.

**Brief injection rule**: [briefs.go:90-97](../../../internal/recipe/briefs.go#L90) currently injects `platform_principles.md`, `preship_contract.md`, `fact_recording.md`, `content_authoring.md` unconditionally + `init-commands-model.md` when any codebase has `HasInitCommands`. Add:

```go
if anyCodebaseIsDynamicRuntime(plan) {
    atoms = append(atoms, "principles/dev-loop.md")
}
```

Helper `anyCodebaseIsDynamicRuntime` inspects `cb.BaseRuntime` and returns true when none of the implicit-webserver prefixes match (`php-nginx`, `php-apache`, `nginx`, `static`). Conservative default: true for any unknown runtime — the atom is strictly useful even for runtimes not yet encountered.

**Brief cap pressure**: scaffold brief currently ~3-4 KB pre-content atoms. Adding ~1 KB dev-loop keeps under the 5 KB cap ([briefs.go:25](../../../internal/recipe/briefs.go#L25)) with ~1 KB margin. Run-8-readiness already raised the cap from 3 KB → 5 KB for exactly this tranche of fixes.

**Changes:**
1. New file: [internal/recipe/content/principles/dev-loop.md](../../../internal/recipe/content/principles/dev-loop.md), ~1 KB.
2. [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — new helper + conditional injection in `BuildScaffoldBrief`.
3. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — add a one-line reference: "Dev vs prod process model + `zerops_dev_server` live in `principles/dev-loop.md` (injected when your runtime is dynamic)."
4. [internal/recipe/content/phase_entry/scaffold.md](../../../internal/recipe/content/phase_entry/scaffold.md) — step between 4 (verify dev deploy) and 5 (verify initCommands): "Start the dev server via `zerops_dev_server action=start` (dynamic runtimes only) before running the preship contract. `zsc noop` dev does not auto-start — the sub-agent's brief carries the full tool shape."

**Touches**: 1 new atom + 1 Go file + 2 existing `.md` edits. <100 LoC of new content.

**Test coverage**:
- `TestBrief_Scaffold_InjectsDevLoopAtom_ForDynamicRuntime` — plan with one `nodejs@22` codebase; brief contains `zsc noop`.
- `TestBrief_Scaffold_OmitsDevLoopAtom_ForImplicitWebserver` — plan with one `php-nginx@8.4` codebase; brief does NOT contain `zsc noop`.
- `TestBrief_Scaffold_UnderCap_WithDevLoop` — synthesized plan representative of nestjs-showcase (3 codebases, all dynamic); brief.Bytes < 5 KB.

### 2.G2 — mount-vs-container execution-split principles atom

**What run 8 showed** ([ANALYSIS §3 gap G.2](../runs/8/ANALYSIS.md)): six direct-on-mount build-tool executions, zero ssh-into-container equivalents:

| # | Sub-agent | Command |
|---|---|---|
| 1 | scaffold-app | `cd /var/www/appdev && npm install --package-lock-only` |
| 2-3 | feature | `cd /var/www/{api,worker}dev && npm install --no-audit --no-fund --silent` |
| 4-5 | feature | `cd /var/www/apidev && npx nest build` / `cd /var/www/workerdev && npx tsc -p tsconfig.build.json` |
| 6 | feature | `cd /var/www/appdev && VITE_API_URL=https://apidev-21f2-3000.prg1.zerops.app npm run build` |

All six executed on the zcp host against the FUSE mount — every file IO tunneled through SSHFS (10-100× slower than native), every process ran with the host's Node/npm version (wrong runtime cache, no access to platform-injected env vars like `${apidev_zeropsSubdomain}`). The local builds also don't match the deploy's actual build (command #6 hardcoded a subdomain URL the container's build uses as a build-time alias); the `https://https://` double-prefix bug escaped the local build entirely.

v2 teaches the split in [develop-platform-rules-container.md:11-35](../../../internal/content/atoms/develop-platform-rules-container.md#L11):
> Read and edit directly on the mount (editor tools = transparent FUSE). Long-running dev processes → `zerops_dev_server`. One-shot commands over SSH:
> ```
> ssh {hostname} "cd /var/www && npm install"
> ssh {hostname} "cd /var/www && php artisan migrate"
> ssh {hostname} "curl -s http://localhost:{port}/api/health"
> ```

v3 has no such atom.

**Fix direction**: port a compressed atom into [internal/recipe/content/principles/mount-vs-container.md](../../../internal/recipe/content/principles/mount-vs-container.md).

**Atom contents** (target ~700 bytes — single page):
- **Editor tools on the mount.** Local `Read`, `Edit`, `Write`, `Glob`, `Grep` against `/var/www/<hostname>dev/` work transparently — SSHFS bridges every read/write. No ssh indirection needed; it's a normal filesystem.
- **Framework CLIs via ssh to the container.** `npm install`, `npx build`, `tsc`, `artisan`, `composer`, `pip`, `bun`, `curl localhost` — all run in-container via `ssh <hostname>dev "cd /var/www && <cmd>"`. Two reasons: (1) container has the right runtime version + package-manager cache + platform-injected env vars (like `${apidev_zeropsSubdomain}` for build-time URL aliases); (2) running the process locally tunnels every file IO through FUSE, 10-100× slower than native and semantically wrong because the process is on the wrong host.
- **Don't run local builds to "pre-verify".** `zerops_deploy` runs `npm ci` + `npm run build` on a fresh in-container filesystem. A local build on the mount gates nothing the deploy won't also check, and its artifacts don't match (different env, different cache, different runtime).
- **One-shot vs long-running.** Framework CLIs are one-shot — they exit in seconds, no channel-lifetime concern. Dev servers are long-running — they need `zerops_dev_server` (see `dev-loop.md`), not ssh.

**Brief injection rule**: include unconditionally in scaffold + feature briefs. The rule is platform-invariant, not runtime-gated.

**Changes:**
1. New file: [internal/recipe/content/principles/mount-vs-container.md](../../../internal/recipe/content/principles/mount-vs-container.md), ~700 bytes.
2. [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) — add `"principles/mount-vs-container.md"` to the `atoms` slice in `BuildScaffoldBrief` AND `BuildFeatureBrief` (unconditional).
3. [internal/recipe/content/briefs/scaffold/content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) — one-line cross-reference.
4. [internal/recipe/content/briefs/feature/content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — one-line cross-reference.

**Touches**: 1 new atom + 1 Go file edit + 2 existing `.md` edits. <50 LoC.

**Test coverage**:
- `TestBrief_Scaffold_IncludesMountVsContainerAtom` — brief body contains `ssh <hostname>dev "cd /var/www`.
- `TestBrief_Feature_IncludesMountVsContainerAtom` — same for feature brief.
- `TestBrief_Scaffold_UnderCap_WithAllPrinciples` — full injection (platform_principles + preship + fact_recording + content_authoring + dev-loop + mount-vs-container + init-commands-model) stays under 5 KB on a nestjs-showcase-shaped plan.

**Optional warn-lint** (ANALYSIS §3 gap G.2 final paragraph suggested this; defer to run 10 if run 9 is busy): add a pre-call hook on Bash tool invocations that regex-matches `cd /var/www/[^/]+dev && (npm|yarn|pnpm|composer|bun|pip|tsc|vite|nest|npx) ` and surfaces a warning. Warn-only because `cd /var/www/apidev && ls` is fine. If included, touches the MCP harness not the recipe engine. Defer.

### 2.H — YAML comment-style principle atom + divider-banner validator

**What run 8 showed** ([ANALYSIS §3 gap H](../runs/8/ANALYSIS.md)): scaffold-api's [zerops.yaml](../runs/8/apizerops.yml) opens with a 61-character `# -------------------------------------------------------------` ASCII divider banner, with both dev AND prod `setup` blocks framed by matching dividers. Reference [laravel-showcase-app/zerops.yaml](/Users/fxck/www/laravel-showcase-app/zerops.yaml) has zero such dividers. v2 prohibits decoration at five places — notably [workflows/recipe/principles/comment-style.md](../../../internal/content/workflows/recipe/principles/comment-style.md) and [workflows/recipe/phases/finalize/env-comment-rules.md:71](../../../internal/content/workflows/recipe/phases/finalize/env-comment-rules.md#L71) which says verbatim:

> Comments are ASCII `#` prefixed, one line, natural prose. Section transitions use a single blank-comment line (`#`) followed by the first comment of the next section. That is the full vocabulary — no dividers, no banners, no decoration.

v3's [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) teaches comment *content* ("Why-not-what. Use `because`, `so that`, `otherwise`, `trade-off`.") but not comment *visual style*.

**Fix direction** (two parts — brief atom + engine validator):

**Part 1: brief-side atom.** New file [internal/recipe/content/principles/yaml-comment-style.md](../../../internal/recipe/content/principles/yaml-comment-style.md), ~500 bytes. Port the v2 rule:
- ASCII `#` only, one hash per line, one space after the hash, then prose.
- Section transitions: a single bare `#` blank-comment line.
- No dividers (runs of `-`, `=`, `*`, `#`, `_`).
- No banners (multi-line boxes, `# === Section ===`).
- No decoration.
- Reference the reference `laravel-showcase-app/zerops.yaml` shape.

Inject unconditionally in scaffold + feature briefs ([briefs.go:90-110](../../../internal/recipe/briefs.go#L90)). The rule is platform-invariant.

**Part 2: engine-side validator.** The existing [validators_codebase.go:132-156](../../../internal/recipe/validators_codebase.go#L132) `validateCodebaseYAML` checks every comment has a causal word but does NOT scan for divider decoration. Extend it:

- New package-level regex: `yamlDividerRE = regexp.MustCompile(`(?m)^\s*#\s*([-=*#_])\1{3,}\s*$`)` — matches a comment whose body (after the `#`) is a run of 4+ identical decorative characters.
- At the start of `validateCodebaseYAML`, before the per-comment causal-word loop, scan for dividers and emit one violation per match: `yaml-comment-divider-banned`, message `decorative divider line violates yaml-comment-style (no dividers, no banners): <line>`.

The existing `yamlCommentRE` at [validators.go:56](../../../internal/recipe/validators.go#L56) matches every comment including dividers — so the current causal-word check would ALSO flag a divider (missing causal word). The new check is earlier-firing + more specific so the author sees the right diagnostic first.

**Changes:**
1. New file: [internal/recipe/content/principles/yaml-comment-style.md](../../../internal/recipe/content/principles/yaml-comment-style.md).
2. [internal/recipe/briefs.go](../../../internal/recipe/briefs.go) `BuildScaffoldBrief` + `BuildFeatureBrief` — append the atom unconditionally.
3. [internal/recipe/validators.go](../../../internal/recipe/validators.go) — add `yamlDividerRE` regex.
4. [internal/recipe/validators_codebase.go](../../../internal/recipe/validators_codebase.go) `validateCodebaseYAML` — add the divider scan pre-loop.

**Touches**: 1 new atom + 3 file edits. ~50 LoC.

**Test coverage** (new tests in [validators_test.go](../../../internal/recipe/validators_test.go)):
- `TestValidateCodebaseYAML_DividerBanned` — yaml body with `# ----` or `# ====` or `# ****`; validator returns a `yaml-comment-divider-banned` violation.
- `TestValidateCodebaseYAML_BlankCommentAllowed` — single bare `#` as a section transition passes (not a divider).
- `TestValidateCodebaseYAML_ShortRunsNotFlagged` — `# --` (2-char) is allowed; 4+ chars flags.
- `TestBrief_Scaffold_IncludesYamlCommentStyle` — brief contains the atom reference.

### 2.I — porter-voice rule across every sub-agent output + source-code comment validator

**What run 8 showed** ([ANALYSIS §3 gap I.1 + I.2](../runs/8/ANALYSIS.md)): the leak spans three artifact kinds.

1. **Intro fragment voice** (rendered `codebases/api/README.md` line 4):
   > The **api** codebase is the NestJS HTTP service… **The scaffold ships** a health probe and a short-lived debug controller… **The feature phase will wire** Postgres…

   Reads as a recipe-authoring journal. The audience is a porter on `zerops.io/recipes`; by the time they read the README, the feature phase is DONE. "The scaffold ships" / "The feature phase will wire" references the authoring-phase model the sub-agent was mid-run-with when it recorded the fragment.

2. **Integration-guide voice** (feature sub-agent's `codebase/app/integration-guide` extension):
   > Step 4 — Showcase feature tabs / The SPA grew from a single health probe into a tabbed demo…

   "Grew from" references scaffold → feature progression a porter has no "before" to compare against.

3. **Committed source-code comment voice** (19 hits across api + worker `.ts` + storage/broker/items modules):
   - `// unless something actually fails — pre-ship contract item 5`
   - `* (pre-ship contract item 2). MUST be removed before promoting`
   - `* Bucket policy is private (showcase default)…`
   - `* 15 min is a showcase-tier tradeoff…`
   - `// Ping loop — scaffold smoke test. Kept for backwards compat.`
   - `// showcase demo — the UI polls once per click.`
   - …13 more.

   Reference `laravel-showcase-app/` source tree: zero such hits. "Pre-ship contract item N", "showcase default", "scaffold smoke test" are authoring concepts — they mean nothing to a porter who cloned the apps repo expecting production code.

**Root cause**: [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) and [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) tell the sub-agent HOW to place content (rubric) and WHAT tone for comments (causal words). Neither says WHO the reader is. The sub-agent's entire context at authoring time is the recipe-authoring phase model, so that model leaks into every artifact.

**Fix direction** (two parts — brief content + engine validator):

**Part 1: brief-side voice rule.** Extend [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) AND [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) with a SINGLE TOP-LEVEL voice rule, placed early in the atom (before the placement rubric):

> **Voice — the reader is a porter, never another recipe author.**
>
> Everything you write — fragment bodies, `zerops.yaml` inline comments, committed source-code comments, README prose — is read by someone deploying this recipe into their own project. They cloned the apps repo and are reading your code to understand how it works, not how you built it.
>
> **Never write:** "the scaffold", "feature phase", "pre-ship contract item N", "showcase default", "showcase tradeoff", "the recipe", "we chose", "we added", "grew from", "kept for backwards compat" (when the "back" is your own scaffold), "scaffold smoke test".
>
> **Always write:** descriptions of the finished product. The product IS wired. The product HAS the health probe. The product HANDLES the upload. There is no authoring-phase "before" for a porter to compare against.
>
> Good: `// Bucket policy is private — signed URLs give callers time-bounded access without exposing the bucket to the public internet.`
> Bad: `// Bucket policy is private (showcase default) — 15 min is a showcase-tier tradeoff.`

Include 4 good/bad pairs — one per surface kind (intro fragment, yaml inline comment, ts docstring, svelte comment). Compressed versions — the atom adds ~600 bytes total.

**Part 2: engine-side validator — source-code comment scanner.** This is the teeth. Run-8 proved the brief-only approach fails (the atom-less brief still produced the leak, but the leak is ALSO inside source code that no existing validator scans).

A new validator registered against a new `Surface` constant `SurfaceCodebaseSourceComments` walks every `Codebase.SourceRoot` directory, opens every file matching `*.{ts,tsx,js,jsx,mjs,svelte,vue,go,php,py,rb}`, scans comment lines for a forbidden-phrase allowlist. Flags one violation per hit with file:line and the offending phrase. Skips library paths (`node_modules/`, `vendor/`, `.venv/`, `dist/`, `build/`).

Forbidden phrase list (case-insensitive, same set as the brief voice rule):
- `pre-ship contract`, `preship contract`
- `scaffold`, `the scaffold`, `scaffolded`, `scaffold smoke test`
- `feature phase`, `feature-phase`
- `showcase default`, `showcase tier`, `showcase tradeoff`, `showcase-tier`
- `for the recipe`, `the recipe`
- `we chose`, `we added`, `we decided`
- `grew from`

Comment syntaxes to recognize: `//`, `/* */`, `<!-- -->`, `#` (Python/Ruby), `*` (inside block comment lines).

**Scoping**: the validator runs ONLY if `Codebase.SourceRoot` is populated (i.e. A2 landed) and the directory exists. Missing SourceRoot → skip silently (covers chain-parent codebases + not-yet-scaffold-complete states). The validator emits `Violation{Code: "source-comment-authoring-voice-leak", Path: <file>, Message: "<line>: contains authoring-phase reference \"<phrase>\""}`.

**Registration**: new entry in `validators.go::init()`:
```go
RegisterValidator(SurfaceCodebaseSourceComments, validateCodebaseSourceComments)
```
Plus `resolveSurfacePaths` gains a case for `SurfaceCodebaseSourceComments` that returns the list of per-codebase `SourceRoot` directory roots. Because the validator needs to walk subtrees (not read a single file), it's implemented slightly differently from existing validators — it does the walk internally and emits violations per hit. The `RunSurfaceValidators` loop at [validators.go:110-128](../../../internal/recipe/validators.go#L110) reads the content for standard validators; for this one, it can either (a) be special-cased to skip the read + pass the SourceRoot as path, or (b) pass the SourceRoot directory as `path` and `nil` as `body`, letting the validator do its own walk. Option (b) is less invasive.

**Changes:**
1. [content_authoring.md](../../../internal/recipe/content/briefs/scaffold/content_authoring.md) + [content_extension.md](../../../internal/recipe/content/briefs/feature/content_extension.md) — prepend the voice rule + 4 good/bad pairs.
2. [internal/recipe/surfaces.go](../../../internal/recipe/surfaces.go) — add `SurfaceCodebaseSourceComments` constant + include it in `Surfaces()` output.
3. [internal/recipe/validators.go](../../../internal/recipe/validators.go) — new case in `resolveSurfacePaths` returning `cb.SourceRoot` (directory path). Handle the special "body is nil, validator walks internally" case in `RunSurfaceValidators` (or make the new validator signature tolerant of nil body).
4. New file: [internal/recipe/validators_source_comments.go](../../../internal/recipe/validators_source_comments.go) — `validateCodebaseSourceComments` implementation. Walk-based, ~100 LoC.
5. [internal/recipe/validators.go](../../../internal/recipe/validators.go) — `init()` registers the new validator.

**Touches**: 2 `.md` + 3 Go files. ~150 LoC of new code + ~600 bytes of new content.

**Test coverage** (new tests in [internal/recipe/validators_source_comments_test.go](../../../internal/recipe/validators_source_comments_test.go)):
- `TestValidateSourceComments_FlagsPreShipContractReference` — temp dir with `api/src/main.ts` containing `// pre-ship contract item 5`; validator emits violation.
- `TestValidateSourceComments_FlagsScaffoldReference` — `// scaffold smoke test`; flagged.
- `TestValidateSourceComments_FlagsShowcaseDefault` — `// showcase default`; flagged.
- `TestValidateSourceComments_IgnoresNodeModules` — same pattern inside `node_modules/foo.js`; not flagged.
- `TestValidateSourceComments_IgnoresLegitimateCausalComments` — `// Bucket policy is private — signed URLs...`; not flagged.
- `TestValidateSourceComments_SkipsMissingSourceRoot` — `SourceRoot` empty; validator returns zero violations, no error.
- `TestContentAuthoring_IncludesVoiceRule` — `content_authoring.md` content contains "porter", "never another recipe author", "we chose".

### 2.K — `phase_entry/scaffold.md` parallel-dispatch directive

**What run 8 showed** ([ANALYSIS §3 gap K](../runs/8/ANALYSIS.md)): main agent at [main-session.jsonl:74](../runs/8/SESSION_LOGS/main-session.jsonl) narrated:
> Deploys serialize on a shared channel, so I'll run the three scaffold sub-agents sequentially — api first, then app, then worker.

Against [phase_entry/scaffold.md:7](../../../internal/recipe/content/phase_entry/scaffold.md#L7):
> ## For each codebase (parallelize when shape is 2 or 3)

Dispatch timeline: api 10:56:38 → 11:07:35 (11m), app 11:08:47 → 11:34:04 (25m), worker 11:35:26 → 11:47:49 (12m). Serial total: 48m. Parallel max would have been ~25m (bounded by the slowest — scaffold-app, which hit three self-inflicted retry loops). Lost: ~23 minutes.

The parenthetical "parallelize when shape is 2 or 3" is under-explained. The main agent separately remembered that `zerops_*` MCP calls serialize at a shared session mutex, and extrapolated (wrongly) to "therefore serialize the whole dispatch". The extrapolation is wrong because each sub-agent's deploys serialize naturally at call time — they don't force serial dispatch. File authoring, `Bash`/`ssh`/`npm install`, local reads, `zerops_knowledge` lookups all run concurrently across sub-agent sidechains.

**Fix direction**: rewrite [phase_entry/scaffold.md](../../../internal/recipe/content/phase_entry/scaffold.md#L7) lines 7-12 to prescribe parallel dispatch with an explicit one-paragraph rationale. Replace:

```
## For each codebase (parallelize when shape is 2 or 3)
```

with:

```
## Dispatch every codebase scaffold IN PARALLEL

With 2 or 3 codebases, dispatch all sub-agents in a single message (one
Agent tool call per codebase, emitted in parallel). Each sub-agent's
`zerops_deploy` + `zerops_verify` calls queue naturally at the recipe
session mutex — you do NOT need to serialize the dispatch to serialize
the deploys. File authoring, `Bash` and `ssh` commands, `npm install`,
local builds, and `zerops_knowledge` consults run concurrently across
sidechains.

Net savings for a 3-codebase scaffold: 15-30 minutes. Serializing
dispatch is the wrong optimization — the sub-agents block on their own
framework work, not on each other.
```

Also update [CLAUDE.md](../../../CLAUDE.md) reference if the project's CLAUDE.md has a dispatch-parallelism note the atom should cite by name. (Quick check during implementation — if not, skip the cross-reference.)

**Changes**: 1 `.md` edit. ~15 lines.

**Test coverage**: existing [internal/recipe/phase_entry_test.go](../../../internal/recipe/phase_entry_test.go) (if present) or a new pin:
- `TestPhaseEntry_Scaffold_PrescribesParallelDispatch` — loaded scaffold atom contains "IN PARALLEL" and "in a single message".

### 2.J — `record-fragment` response echoes fragmentId + bodyBytes + appended

**What run 8 showed** ([ANALYSIS §3 gap J](../runs/8/ANALYSIS.md)): all 22 `record-fragment` calls returned byte-identical `{"ok":true,"action":"record-fragment","slug":"nestjs-showcase"}` (103 bytes). When the main agent batched 7 env-fragment calls in one message (main-session.jsonl lines 108-121), the transcript showed 7 identical success payloads. Author has no signal of which fragment landed, whether append semantics fired, or the running plan.Fragments count.

**Root cause**: [handlers.go:244-253](../../../internal/recipe/handlers.go#L244) `case "record-fragment"` sets only `r.OK = true`; the response struct ([handlers.go:126-139](../../../internal/recipe/handlers.go#L126)) has no field for fragment metadata.

**Fix direction**: extend the response with three fields populated on success:
- `FragmentID string` — echo of the id.
- `BodyBytes int` — post-write total size of the stored body (for append ids, this is the sum of scaffold-author body + all feature extensions).
- `Appended bool` — true when append semantics fired (prior non-empty body existed for an append-class id).

Add to `RecipeResult`:
```go
type RecipeResult struct {
    // ...existing fields...
    FragmentID string `json:"fragmentId,omitempty"`
    BodyBytes  int    `json:"bodyBytes,omitempty"`
    Appended   bool   `json:"appended,omitempty"`
}
```

Update `recordFragment` ([handlers.go:480-506](../../../internal/recipe/handlers.go#L480)) to return `(bodyBytes int, appended bool, err error)` so the handler can populate the response.

**Changes:**
1. [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — `RecipeResult` struct gains 3 fields.
2. [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — `recordFragment` signature + return.
3. [internal/recipe/handlers.go](../../../internal/recipe/handlers.go) — `case "record-fragment"` populates the new fields.

**Touches**: 1 file. <30 LoC.

**Test coverage** (new tests in [handlers_test.go](../../../internal/recipe/handlers_test.go)):
- `TestRecordFragment_ResponseEchoesID` — response JSON contains `fragmentId` matching the input.
- `TestRecordFragment_AppendSetsAppendedTrue` — first call to an append-class id: `appended: false`. Second call: `appended: true`, `bodyBytes` = sum of both bodies + 2 (the `\n\n` separator).
- `TestRecordFragment_OverwriteSetsAppendedFalse` — first and second calls to a root/env id (overwrite class): both `appended: false`, `bodyBytes` = just the last body.

### 2.R — regression fixture for code-block fragment bodies

**Why this exists**: tranche-1 commit 1 (A1) is a focused fix on a focused class. The next time someone touches [assemble.go](../../../internal/recipe/assemble.go), they could inadvertently revert to the broader scan. Pin the invariant with a fixture that exercises every fragment-body token shape runs have produced or might plausibly produce.

**Fix direction**: a single `TestAssemble_FragmentBody_CodeTokens_E2E` test that builds a realistic plan (3 codebases, all 5 managed services, a full set of FactRecords) and authors a codebase IG fragment containing:
- `${API_URL}` (JavaScript template literal)
- `{API_URL}` (bare uppercase token — Handlebars / Go template)
- `{{template}}` (double-brace — Vue / Svelte)
- `<slot />` (Svelte slot syntax)
- `{#if cond}…{/if}` (Svelte conditional)
- `{{ .FieldName }}` (Go html/template)
- Backtick-literal code block containing `` `${PLACEHOLDER}` ``.

Assert:
- `AssembleCodebaseREADME(plan, hostname)` returns nil error.
- The rendered body contains the exact fragment body byte-range (round-trip preserved).
- Missing list is empty.

**Touches**: 1 new test in [assemble_test.go](../../../internal/recipe/assemble_test.go). ~80 LoC.

---

## 3. Ordering + commits

Dependencies:
- **A1** is foundational for any run 9 — the entire stitch path is gated on it. Land first.
- **A2** depends on nothing inside this plan; independent one-commit change. Sequence with A1 either order; A1 first by default.
- **B**, **E** depend on nothing inside this plan; independent content-only edits (E is trivial case normalization).
- **G1**, **G2**, **H** are independent principle-atom ports + brief-composer edits + (for H) a validator. No shared state between them.
- **I** depends on **A2** (the source-code comment validator reads from `Codebase.SourceRoot`, which A2 guarantees is populated).
- **K** is standalone (one `.md` edit).
- **J** is standalone (one handler change).
- **R** pins **A1**; commit after A1 lands.

### Commit order

Tranche 1 — serial, A1 first because the rest of the stack is unusable without it:

1. **fix(recipe): scope unreplaced-token scan to engine-bound keys only** (A1) — ~40 LoC + 4 regression tests.
2. **feat(recipe): populate `Codebase.SourceRoot` at `enter-phase scaffold`** (A2) — ~20 LoC + 4 tests + hard-fail on missing source yaml.
3. **fix(recipe): feature brief + atom route facts via v3 tool + browser-verification pattern** (B) — 2 `.md` edits + optional classify.go case + 2 pin tests.
4. **chore(recipe): normalize `surfaceHint` casing across briefs, atoms, error messages, tests** (E) — <15 LoC find/replace.

Tranche 2 — parallelizable after tranche 1 lands:

5. **feat(recipe): port dev-loop principles atom + conditional brief injection** (G1).
6. **feat(recipe): port mount-vs-container execution-split atom + unconditional brief injection** (G2).
7. **feat(recipe): yaml-comment-style principle atom + divider-banner validator** (H).
8. **feat(recipe): porter-voice rule in content briefs + source-code comment validator** (I).
9. **feat(recipe): `phase_entry/scaffold.md` parallel-dispatch directive** (K).
10. **feat(recipe): `record-fragment` response echoes fragmentId + bodyBytes + appended** (J).

Tranche 3 — after tranche 2:

11. **test(recipe): e2e assemble regression with code-block fragment bodies** (R).

Final milestone commit: **docs(recipe): run-9-readiness CHANGELOG entry** — update [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) with the story, keep engine-delivery fixes (A1/A2/B/E) separately grouped from content-pipeline fixes (G1/G2/H/I/K/J) so the narrative matches the tranche structure.

Between every commit: `go test ./... -count=1 -short` green + `make lint-local` green. CLAUDE.md's "Max 350 lines per .go file" still applies — [handlers.go](../../../internal/recipe/handlers.go) is 688 lines today; do not grow it further. Split J's struct additions into the existing file but watch the ceiling. If it crosses 700, refactor record-fragment handling into its own file `handlers_fragments.go` as part of commit 10.

---

## 4. Acceptance criteria for run 9 green

Run 9 is "finalize actually finishes AND ships correctly-shaped content" when:

### Inherited from run 8 (measured the same way)

1. **Stage deploys green** — every codebase passes `zerops_verify` on both dev and stage.
2. **Browser verification recorded** — `zerops_browser` step executed; FactRecord with `surfaceHint: browser-verification` present in `facts.jsonl` per feature tab.
3. **Seed script ran once, data visible** — `GET /items` returns ≥ 3 items before any manual create.
4. **Stitched output has canonical structure** — root README + 6 tier READMEs + 6 tier import.yamls + per-codebase `README.md` + `CLAUDE.md` + `zerops.yaml` for every codebase.
5. **Factuality lint passes** — no README claims a framework absent from any codebase's manifest.
6. **Fragments authored in-phase** — `record-fragment` calls by scaffold sub-agent at scaffold close, feature sub-agent at feature close, main agent at finalize. Classification counts visible in `status` output.
7. **Citation map attachment** — every KB gotcha with a citation-map topic carries its guide-id.
8. **Cross-surface uniqueness** — no topic keyword appears in > 1 surface body.
9. **Finalize gates all pass** — validator output shows pass on every `ValidateFn` (including H's divider check + I's source-comment scanner), not just structural.
10. **Recipe click-deployable end-to-end** — `<outputRoot>/4 — Small Production/import.yaml` imports into a fresh project (manual validation at end of run 9).

### New for run 9

11. **Scaffold yamls show the dev-vs-prod distinction** — dev `start: zsc noop --silent` + no `healthCheck` + install-only `buildCommands`; prod `start: <compiled-entry>` + `readinessCheck` + full build + narrow `deployFiles`. Applied to every dynamic-runtime codebase; implicit-webserver runtimes correctly omit `run.start` on both.
12. **Zero `# ---` divider comments** in any scaffold- or feature-authored yaml. Validator H catches any that slip through.
13. **Every `codebase/<h>/intro` fragment passes the porter-voice test** — remove all authoring-phase references; it still reads as a coherent product description. Validator I catches source-code leaks.
14. **Scaffold sub-agents dispatched in parallel** — main session log shows a single message with all `Agent` tool calls, dispatch wall-time is bounded by the slowest sub-agent (not sum).
15. **`record-fragment` responses carry `fragmentId` + `bodyBytes` + `appended`** — observable via any batched record-fragment call in the main-session transcript.
16. **Feature-phase facts visible in `facts.jsonl`** — count > 0; every fact has `surfaceHint` (camelCase) and is readable by `Classify`.

---

## 5. Non-goals for run 9

Keep out of scope, ship separately or deferred:

- **Automated click-deploy verification harness** — still separate work. Criterion 10 remains manual for run 9.
- **LLM-based editorial review** — the run-analysis loop. Separate from in-engine gates.
- **`verify-subagent-dispatch`** — dispatch-integrity SHA check was on run-8-readiness as stretch; still deferred. Ships after run 9 confirms the content pipeline is clean.
- **Chain-resolution delta yaml emission** — `nestjs-showcase` diffs against `nestjs-minimal`'s import.yaml. Current emitter emits full yaml per tier. Defer until nestjs-minimal gets re-run via v3.
- **`requireAdoption` gate fix** — noted in v9.5.5 CHANGELOG. Didn't bite run 6/7/8. Defer until it does.
- **Rehydrating feature-phase v2-tool facts** — a dual-write path that routes `zerops_record_fact` into v3's `facts.jsonl` when a recipe session is open. The brief fix in B is strictly sufficient for run 9; the rehydrate is an engine complexity hedge that costs more than it saves. Revisit if a future run ships content that bypasses the brief.
- **SESSION_LOGS credential redaction on ingest** — ANALYSIS §4 last bullet; the nats.js password leaked into `agent-a44602100f655b4e9.jsonl`. Not v3-specific; any managed-service client that embeds credentials in error traces leaks. Route as infra concern, not recipe-engine.
- **G.2 optional warn-lint at Bash pre-call hook** — flagged in ANALYSIS §3 G.2 last paragraph. Useful but a harness change, not a recipe change. Defer.
- **Browser-verification as a distinct `surfaceHint` class** — if the optional addition in B is skipped, the default-branch routing to `ClassScaffoldDecision` is publishable and behaviorally correct. Make it explicit in a later pass only if validators need it.

---

## 6. Risks + watches

- **Brief cap pressure** — scaffold brief after tranche 2 carries: role contract + 4 content atoms (platform_principles, preship_contract, fact_recording, content_authoring) + dev-loop (when dynamic runtime) + mount-vs-container + yaml-comment-style + init-commands-model (when HasInitCommands). For a nestjs-showcase-shaped plan (3 dynamic runtimes, init commands present), that's ~4.5 KB on pre-run-9-readiness composition. Run-8-readiness raised the cap from 3 → 5 KB; add a `TestBrief_Scaffold_UnderCap_WithFullInjection` pin so run 10 doesn't silently cross. If cap pressure surfaces, compress `platform_principles.md` first (it duplicates some content now covered by dev-loop + mount-vs-container).
- **A1 allowlist drift** — if a future template introduces a new engine-bound token (say `{PROJECT_NAME}`), A1's allowlist needs to grow with it OR the scan becomes a false negative. Mitigation: the allowlist should live as a single exported `engineBoundKeys` slice in `assemble.go`, and any new template must reference it via a helper (`assertTemplateTokensBound(tpl, engineBoundKeys)`) at init time. Optional follow-up for run 10 — not required for run 9 correctness.
- **A2 convention assumes dev-slot naming** — if a future tier model or slot model changes the convention (e.g. v4 introduces `<h>local` for tier 2), A2's hardcoded `<hostname>dev` becomes wrong for that tier. Run 9's scope is tier 0/1 scaffold only, so the convention is safe. Revisit if slot naming changes.
- **I's forbidden-phrase list false positives** — `scaffold` appears in legitimate library names (React "scaffold", CLI "scaffolding"). The source-comment scanner looks only at comment lines, not code identifiers, which narrows the risk substantially. But a legitimate comment like `// Uses next.js scaffolding for the app router` would flag. Mitigation: the list is tight (`the scaffold`, `scaffold smoke test`, `scaffold default` — phrases, not bare words where possible); review the test fixtures post-run-9 and iterate. Warn-level first; gate-blocking only after the list is proven stable.
- **H's divider validator collides with v2 recipes mounted for chain resolution** — chain resolver reads parent `zerops.yaml` from `zeropsio/recipes` clones. If a v2-era recipe has `# ---` dividers (likely: pre-convention), the validator would flag them in the CHAIN parent. Scope: the validator only runs on `resolveSurfacePaths(SurfaceCodebaseZeropsComments, plan)` which returns `codebases/<h>/zerops.yaml` inside `outputRoot`. Chain parents live outside `outputRoot`. No cross-contamination. Confirm during implementation — if wrong, narrow the walk explicitly.
- **K's parallel-dispatch directive undercounts the serialization layer** — if `zerops_deploy` EVER serializes per-hostname at some future platform constraint (say, a shared build cache lock), parallel dispatch might quietly regress. Leave the directive in place — parallel dispatch is still correct for file authoring + `Bash` + `zerops_knowledge`, which dominate wall time — but watch run-9's timeline vs run-8 for net savings. Expect 15-30 min saved on nestjs-showcase if no new serialization emerges.
- **Mount-vs-container atom collides with local-dev runs** — [docs/spec-local-dev.md](../../spec-local-dev.md) describes container-vs-local-machine behavior. G2's atom says "ssh into the container" unconditionally; on a local dev machine the same commands would run on the host (no container to ssh into). Verify during implementation: does v3 scaffold ever target local-machine runtime? If yes, atom needs a "container-mode only" scoping gate (similar to v2's `environments: [container]` frontmatter). If no (run 8 was container-only, nestjs-showcase is container-targeted), atom can stay unconditional for now.
- **I's source-code scanner walk cost** — walking every `SourceRoot` on every `complete-phase finalize` call re-scans N source trees. For nestjs-showcase (3 codebases × ~100 files each), this is trivial. If a future recipe has large codebases, memoize or cap the walk. Run 9 scope: don't worry about it.
- **Commit 10 pushes handlers.go past 350-line advisory cap** — [handlers.go](../../../internal/recipe/handlers.go) is at 688 lines today (already past the 350 line cap, pre-existing). Adding J's 3 fields + 1 helper is small. If the file stays manageable, leave it. If it becomes a review barrier, split `recordFragment` + `isValidFragmentID` + `applyEnvComment` + `isAppendFragmentID` into a new `handlers_fragments.go` at commit 10 — a pure-refactor move with no behavioral change. Existing tests pin behavior.

---

## 7. Open questions

1. **A2 — `enter-phase scaffold` or `complete-phase provision` for SourceRoot population?** Scaffold entry is strictly safer (mounts are definitely settled by then), but provision-complete is where the plan transitions and feels more "right". Lean enter-phase scaffold because the handler has clean access at that point and the guarantee is stronger. Decide at implementation.

2. **B — explicit `browser-verification` classification, or default-branch routing to scaffold-decision?** Current classify.go default maps unknown hints to `ClassScaffoldDecision`, which is publishable. Adding the case explicitly surfaces intent but doesn't change behavior. Lean add-the-case (costs 2 lines, documents intent). Decide at implementation.

3. **I — gate-blocking or warn-only for source-comment leaks?** Run-8 produced 19 hits in 3 codebases. Gate-blocking means run 9 can't close finalize if ANY source comment leaks. Warn-only means run 9 closes and the author iterates. Lean gate-blocking: the voice rule is absolute (a porter never wants to read "the scaffold" in their cloned repo), and the forbidden list is tight enough. Revisit if false positives bite.

4. **H — should the divider-banner rule also apply to README prose?** Run-8 decoration was yaml-comment-only. Markdown README dividers (`---` horizontal rules) are normal markdown. Scope H to yaml only; don't apply to README. If README decoration surfaces later, extend then.

5. **G1 — should implicit-webserver runtimes skip dev-loop atom entirely, or get an inverted version?** Scope of dev-loop is dynamic-runtime only; `php-nginx` etc. have no `run.start` at all. The injection rule above says "skip for implicit webserver". Correct, but means the sub-agent on `php-nginx` has no explicit guidance about dev iteration. It's fine — the build + deploy path is the iteration path — but consider adding one-line prose to `content_authoring.md` explaining "implicit-webserver runtimes iterate via redeploy; the dev loop atom doesn't apply to them." Defer to implementation.

6. **K — should `phase_entry/feature.md` also say "dispatch in parallel"?** Feature phase runs ONE sub-agent across all codebases (per [feature.md:17-18](../../../internal/recipe/content/phase_entry/feature.md#L17) "One dispatch for the whole feature suite"), so parallelism doesn't apply the same way. K scopes to scaffold.md only. Don't touch feature.md for K.

7. **R — should the regression fixture also cover env README + root README token shapes?** Those surfaces render BEFORE any codebase with fragment-body `{UPPER}` code. A1's scope is "fragment bodies can contain `{UPPER}` tokens" which applies to every assemble surface. Extend the fixture to one sample per surface kind (root, env, codebase README, codebase CLAUDE.md) — ~30 extra LoC, pins the invariant for the whole surface set.

8. **Post-run-9: where does the engine-vs-content tranche boundary go next?** Tranche 2 is heavy on principles-atom ports. If run 9 shows more missing atoms (e.g. deploy-files-first-time semantics, schema-validation-before-deploy), run-10-readiness should consider a systematic v2 atom audit — enumerate every atom under [internal/content/atoms/develop-*.md](../../../internal/content/atoms/) and mark port / adapt / drop. Don't do that audit pre-run-9 — it's pre-optimizing.
