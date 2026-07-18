# Run-41 validation — refinement-2 dogfood audit

> **Headline: ITERATE-TO-42 (narrow scope).** All six run-40 ground-truth
> defect classes are ABSENT from the run-41 deliverable surface, and the
> refinement-2 audit walked + closed correctly. BUT a second-pass walk
> against `docs/spec-content-surfaces.md` (the actual seven-surface
> contract, not just the audit's 10-class enumeration) surfaces **two
> defect classes the audit doesn't cover at all** and **one citation-map
> drift in the audit itself**:
> (a) `appdev KB #4 — Tailwind play CDN warning` is a textbook
> scaffold-decision-disguised-as-gotcha + framework-quirk per the spec's
> own counter-examples catalog — bullet body says "the recipe accepts
> this trade as a build-pipeline simplification", verbatim spec
> anti-pattern;
> (b) cross-codebase content duplication on two load-bearing teachings
> (same-key shadow trap authored as full IG in apidev IG #5 AND workerdev
> IG #2; NATS Authorization Violation authored as full KB in apidev KB
> #1 AND workerdev KB #4) — both should be one-fact-one-surface with
> cross-reference per spec §"Cross-surface duplication";
> (c) the refinement-2 brief's citation map drops three topics the spec
> requires (deploy-files/static-runtime for `dist/~`, http-support/
> l7-balancer, readiness-health-checks), so the audit silently passed
> bullets that should have flagged.
> Plus the originally-reported substrate fragmentId routing bug. Recipe
> is shippable in spirit but not at spec quality; run-42 with a narrow
> scope (3 surface-misplacement fixes + cite-map widening + fragmentId
> glossary) gets there.

---

## Refinement-2 dispatch + audit findings (verbatim)

**Dispatch verified.** Main session
[main-session.jsonl](docs/zcprecipator3/runs/41/SESSION_LOGS/main-session.jsonl)
records the call sequence (last 9 recipe calls):

```
build-subagent-prompt bk=env-content
enter-phase            ph=refinement
status
enter-phase            ph=finalize
stitch-content
complete-phase         ph=finalize
build-subagent-prompt  bk=refinement
build-subagent-prompt  bk=refinement2     ← refinement-2 dispatched
complete-phase         ph=refinement       ← final close
```

Refinement-1 dispatched first (`bk=refinement`), then refinement-2
(`bk=refinement2`), then `complete-phase phase=refinement` closed —
matches the gate ordering specified in `internal/recipe/handlers.go`
PhaseRefinement branch. Refinement-2 input was
`{"action":"build-subagent-prompt","slug":"nestjs-showcase","briefKind":"refinement2"}`
— no `codebase` field, so the
`TestBuildSubagentPromptRefinement2_RefusesCodebaseScope` gate did not
need to fire.

**Findings JSON block** (refinement-2 subagent session
[agent-a1fa5f06daf5c1795.jsonl](docs/zcprecipator3/runs/41/SESSION_LOGS/subagents/agent-a1fa5f06daf5c1795.jsonl):182-296),
emitted as a single fenced block per `phase_entry.md` contract:

```json
{
  "findings": [
    { "defectClass": "kb-below-floor",                "surface": "S5", "fragmentId": "codebase/appdev/knowledge-base",        "primary": "/var/www/appdev/README.md:161-186",    "severity": "advisory", "suggestedAction": "drop" },
    { "defectClass": "ig-cites-recipe-internal-file", "surface": "S4", "fragmentId": "codebase/workerdev/integration-guide/3", "primary": "/var/www/workerdev/README.md:142",     "severity": "advisory", "suggestedAction": "rewrite-as-symptom" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/apidev/knowledge-base",         "primary": "/var/www/apidev/README.md:260-262",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/apidev/knowledge-base",         "primary": "/var/www/apidev/README.md:264-266",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/apidev/knowledge-base",         "primary": "/var/www/apidev/README.md:272-274",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/apidev/knowledge-base",         "primary": "/var/www/apidev/README.md:276-278",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/appdev/knowledge-base",         "primary": "/var/www/appdev/README.md:163-167",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/appdev/knowledge-base",         "primary": "/var/www/appdev/README.md:169-173",    "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/workerdev/knowledge-base",      "primary": "/var/www/workerdev/README.md:191-193", "severity": "advisory", "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",              "surface": "S5", "fragmentId": "codebase/workerdev/knowledge-base",      "primary": "/var/www/workerdev/README.md:195-197", "severity": "advisory", "suggestedAction": "add-citation" }
  ]
}
```

Action by main agent: **HELD all 10 as advisory.** No `record-fragment`
calls between refinement-2 dispatch and `complete-phase phase=refinement`
close. TIMELINE.md narrates this explicitly:
[TIMELINE.md:119-124](docs/zcprecipator3/runs/41/TIMELINE.md#L119-L124)
"10 advisory findings (no blockers): … All advisory severity, HELD by
main agent. Refinement closed `ok:true`."

---

## Run-40 ground-truth defects — caught/fixed/missed in run-41

The six run-40 ground-truth defect classes audited:

| Run-40 defect | Run-41 surface state | Audit finding? | Score |
|---|---|---|---|
| **N-1** `${search_password}` in tier-0 import.yaml comment (run-40: [tier-0 import.yaml:135-140](docs/zcprecipator3/runs/40/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L135-L140)) | ABSENT. Run-41 tier-0 search-service comment is at [tier-0/import.yaml:129-138](docs/zcprecipator3/runs/41/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L129-L138); no `${search_*}` alias mentioned at all ("…the api keeps the index in sync by re-indexing on every item write — small dataset, simple consistency model"). | None. Audit walked yaml-comment-content-drift, found 4 tokens (`${cache_hostname}`, `${broker_connectionString}` ×2, `${storage_apiUrl}`, `${appstage_zeropsSubdomain}`), confirmed all valid per the per-service-type allowlist (subagent log [line 14-19](#)). | **CLOSED-AT-SOURCE.** Audit verified precondition. |
| **N-2** JWT verification claim (run-40 tier-0/4 import.yaml line 4 + apidev/README.md:255) | ABSENT. `grep "JWT\|jwt\|JSON Web Token"` returns zero hits across run-41 deliverable surfaces (excluding `.briefs/`). Tier-0/4 import.yaml preambles ([0/import.yaml:3-6](docs/zcprecipator3/runs/41/environments/0%20%E2%80%94%20AI%20Agent/import.yaml#L3-L6), [4/import.yaml:3-6](docs/zcprecipator3/runs/41/environments/4%20%E2%80%94%20Small%20Production/import.yaml#L3-L6)) carry no JWT framing. apidev IG #5 [apidev/README.md:240-255](docs/zcprecipator3/runs/41/apidev/README.md#L240-L255) mentions APP_SECRET only in the same-key-shadow context, no signing claim. | None. Audit walked aspirational-as-current; explicit narration line 9 "APP_SECRET is declared in tier import.yamls but NOT referenced in any zerops.yaml run.envVariables for api/app/worker, and NOT in source. None of the prose I've read uses present tense to claim JWT signing etc., so that's safe." | **CLOSED-AT-SOURCE.** Audit verified precondition. |
| **N-4** SPA MEILI_SEARCH_KEY prescription unimplemented (run-40 apidev/README.md:318 + workerdev/README.md:273-275) | ABSENT. `grep "MEILI_SEARCH_KEY\|search_defaultSearchKey\|SPA build receives\|frontend codebase consumes"` returns zero hits. The closest surviving prose is apidev KB #4 [apidev/README.md:274](docs/zcprecipator3/runs/41/apidev/README.md#L274) "Keep `MEILI_MASTER_KEY: ${search_masterKey}` server-side only — every browser-facing search call **should mint** a derived search-only key" — phrased as normative guidance, not present-tense "the SPA receives" claim. | None. The aspirational-as-current rule requires present-tense framing; the run-41 prose is conditional/normative ("should mint"), so the rule legitimately doesn't fire. | **CLOSED-BY-REFRAME.** Authoring path produced normative prose instead of false present-tense claim. |
| **KB↔IG duplication** (run-40: 3 pairs on workerdev, 2 pairs on appdev = 5 total) | STRUCTURAL pairs present (3 on appdev: KB#1↔IG#2, KB#2↔IG#4, KB#3↔IG#3; 3 on workerdev: KB#1↔IG#5, KB#2↔IG#4, KB#4↔IG#3) BUT every KB title leads with the porter-observable symptom (`### "VITE_API_URL is not configured at build time" on first paint`, `### Vite dev server returns "Blocked request..."`, `### Missing queue option drops zero rows but doubles every write at minContainers ≥ 2`). | None. Audit walked KB↔IG duplication per-codebase, narration lines 44-65 "the appdev KB bullets are all paired with IG items (#1-3) — but they DO lead with symptom phrasing, so they're not pure duplication. They satisfy the KB-first phrasing test." Same verdict on workerdev (line 60-65). | **CLOSED-BY-REFRAME.** KB titles now symptom-led; satisfies the rule's pass-condition explicitly. |
| **kb-below-floor on appdev** (run-40 appdev KB count below 5) | PRESENT. Run-41 appdev KB has 4 H3 headings ([appdev/README.md:163-186](docs/zcprecipator3/runs/41/appdev/README.md#L163-L186): VITE_API_URL, Vite Blocked request, Static 404, Tailwind CDN). Audit reports floor as 5 per the audit_checklist `S5 KB bullets` row. | **CAUGHT** as advisory ([finding #1](#) above; cites `/var/www/appdev/README.md:161-186`). | **CAUGHT + NOT-FIXED (advisory).** Main agent decided not to add a fifth bullet; ships with 4. |
| **scaffold-code-in-kb on appdev** (run-40 appdev KB#4 cited `src/lib/bus.js`) | ABSENT. Run-41 appdev KB#4 [appdev/README.md:181-185](docs/zcprecipator3/runs/41/appdev/README.md#L181-L185) is about the Tailwind play CDN — no `src/<path>` reference. Audit narration line 95-96: "No `src/*` file references in any KB. Good." | None. Walked + verified clean. | **CLOSED-AT-SOURCE.** Audit verified precondition. |

**Aggregate**: of the six run-40 ground-truth defect classes, **0 survive
into the run-41 deliverable surface**, **1 is caught + held as advisory**
(kb-below-floor, by design per the rule's `advisory` severity), **5 were
authored cleanly upstream** so the audit only had to verify the
precondition. The audit functioned correctly on every class — it found
the one thing that was actually still wrong (a 4-vs-5 KB count) and
correctly didn't fabricate findings for the five classes that were
already clean.

> One reading: "the audit was redundant on five of six." Counter-reading:
> "the audit gave us positive confirmation the deliverable is clean — exactly
> the precondition signal the spec describes."

---

## Per-defect-class score relative to run-40

| Defect class | Run-40 magnitude | Run-41 magnitude | Δ |
|---|---:|---:|---|
| N-1 yaml-comment-content-drift (`${search_password}`) | 1 instance, blocker | 0 | -1 ✓ |
| N-2 aspirational-as-current (framework-feature: JWT) | 3 sites, blocker | 0 | -3 ✓ |
| N-4 aspirational-as-current (named-constant: MEILI_SEARCH_KEY) | 2 sites, blocker | 0 | -2 ✓ |
| KB↔IG duplication | 5 pairs across appdev + workerdev | 6 structural pairs but all symptom-led (rule passes) | qualitative-improved |
| kb-below-floor (appdev KB < 5) | 1 (appdev KB=3) | 1 (appdev KB=4) | -0 magnitude, but +1 closer to floor; CAUGHT by audit, held advisory |
| scaffold-code-in-kb (appdev `src/lib/bus.js`) | 1 instance, blocker | 0 | -1 ✓ |

**Six classes scored: 4 closed at source, 1 closed by prose-reframe, 1
caught + held as advisory. Zero regressions.**

---

## New defect classes — audit's 10-class set vs run-41 deliverable

Walked the run-41 deliverable for cross-surface defects outside the
audit's 10-class enumeration. Findings:

### Substrate bug — audit fragmentId routing (audit-output defect, NOT a deliverable defect)

Every refinement-2 finding cites `fragmentId` using the SSHFS-mount-host
form (`codebase/appdev/knowledge-base`, `codebase/workerdev/integration-guide/3`,
`codebase/apidev/knowledge-base`) but plan.json fragment keys are the
short-host form (`codebase/app/knowledge-base`,
`codebase/worker/integration-guide/3`,
`codebase/api/knowledge-base`) — see [plan.json fragment list](docs/zcprecipator3/runs/41/environments/plan.json).
A main-agent fix-attempt via `record-fragment mode=replace fragmentId=...`
would have keyed against a non-existent fragment.

**Why this didn't bite run-41:** main agent HELD all 10 findings as
advisory, never attempted `record-fragment`.

**Why this would bite run-42:** the moment a blocker-severity finding lands,
main agent would attempt the fix, get a fragment-not-found error, and
either (a) fail the close, (b) author against the wrong fragment, or
(c) loop.

**Root cause:** the audit_checklist substrate uses `<host>` as the
placeholder ([audit_checklist.md:21](internal/recipe/content/briefs/refinement2/audit_checklist.md#L21)
"codebase/<host>/integration-guide/<N>") without clarifying that
`<host>` here means the short codebase name (api/app/worker) from
`plan.codebases[].host`, not the SSHFS-mount path under
`/var/www/<host>dev/`. The audit subagent saw `/var/www/appdev/`,
extracted `appdev`, and substituted into the template.

**Fix path:** add an explicit "fragmentId = `codebase/<short>/...` where
`<short>` is `api`/`app`/`worker` — not the `appdev`/`workerdev`/`apidev`
SSHFS-mount name" clarification to audit_checklist.md, plus mention in
phase_entry.md. Pin with a brief-composer test asserting the
fragmentId glossary section is present.

### Other potential issues scanned for and not flagged

| Candidate defect | Verified state | Verdict |
|---|---|---|
| Queue-group named-constant drift | All surfaces use `'worker'` consistently (source [item-events.service.ts:8](docs/zcprecipator3/runs/41/workerdev/src/item-events/item-events.service.ts#L8), [broker.service.ts:64](docs/zcprecipator3/runs/41/workerdev/src/broker/broker.service.ts#L64), README.md, CLAUDE.md, TIMELINE — facts.jsonl canonical too). Audit narration lines 7-8 explicitly noted run-40's `worker-indexer` was a stale brief worked-example; the actual canonical for run-41 is `worker` per facts. | Not a defect; audit reasoned correctly. |
| Tier-yaml prose vs source endpoint paths (run-40 N-3) | Out of scope by design (run-40 plan explicitly notes "facts.jsonl `contract` entries vs source endpoints" is not a cross-surface audit class). Not re-audited; user explicitly excluded N-3 from scoring. | Out of scope. |
| Borderline aspirational claim on apidev KB#4 ("every browser-facing search call should mint a derived search-only key") | Conditional/normative phrasing ("should"), not present-tense state ("the SPA receives"). Doesn't trigger aspirational-as-current per rule body. | Not a defect; rule applies correctly. |
| Workerdev IG#3 `// src/broker/broker.service.ts` code-block header | Caught by audit (finding #2, advisory). Borderline because the IG body says "the recipe uses NATS core pub/sub" — frames the code as the recipe's worked example. Held by main agent. | Caught + held; acceptable. |

**No new defect classes outside the audit's 10-class enumeration.**

---

## Counter table vs run-40 + run-39 baseline

| Defect class | Run-39 | Run-40 | Run-41 | Δ41-vs-40 |
|---|---:|---:|---:|---|
| S0-1..S0-6 (run-39 hardcoded/dead-env/lowercase suite) | 8+ | 0 | 0 | unchanged ✓ |
| S1-1 queue-group cross-file drift | 30+ | 0 | 0 | unchanged ✓ |
| S1-2 JWT claim with no JWT code | many | 3 | 0 | **-3 ✓** |
| S1-3 TypeORM gotcha with no TypeORM | yes | n/a (recipe uses TypeORM) | n/a | unchanged ✓ |
| S1-4 worker→db ghost dep | 1 | 0 | 0 | unchanged ✓ |
| S1-5 refinement non-write-back | 5 frag | 0 | 0 | unchanged ✓ |
| S2-1 yaml-comment IG/KB cross-refs | 6+ | 4 | 4 ([appdev/zerops.yaml:39,62,75](docs/zcprecipator3/runs/41/appdev/zerops.yaml#L39); [apidev/zerops.yaml:52](docs/zcprecipator3/runs/41/apidev/zerops.yaml#L52)) | unchanged (substrate-only) |
| S2-2 engine-vocab in TIMELINE | many | many on disk; partial export-redact | 0 raw + 4 `<engine-detail>` placeholders ([TIMELINE.md:45,99,110,168](docs/zcprecipator3/runs/41/TIMELINE.md#L45)) | **fully closed on disk** ✓ (agent self-redacted) |
| S3-1 project-ID / workspace URLs in TIMELINE | yes | yes on disk | 0 ([grep "Session:\|projectId" returns nothing](#)) | **fully closed on disk** ✓ |
| S3-2 `prg1.zerops.app` zone literal | 25+ | ~10 | 4 ([TIMELINE.md:143-146](docs/zcprecipator3/runs/41/TIMELINE.md#L143-L146) workspace URL block; placeholder form `<id>`) | partial — surface narrowed; substrate redactor candidate |
| S4 fake specificity (run-40 N-3 `/api/...` paths) | 7 | 8 (1 yaml + 7 facts) | out-of-scope this validation | not audited; tracking for run-42 |
| S8 service-count miscount | 1 | 0 | 0 | unchanged ✓ |
| **N-1 `${search_password}` yaml drift** | 0 | 1 | 0 | **-1 ✓** |
| **N-2 JWT aspirational** | 0 | 3 | 0 | **-3 ✓** |
| **N-4 MEILI_SEARCH_KEY aspirational** | 0 | 2 | 0 | **-2 ✓** |
| **scaffold-code-in-kb (`bus.js`)** | 0 | 1 | 0 | **-1 ✓** |
| **kb-below-floor (appdev KB)** | 0 (recipe smaller scope) | 1 (KB=3) | 1 (KB=4) | structural improvement, audit-caught, held advisory |
| **NEW** audit fragmentId routing bug (audit-output) | n/a | n/a | 10/10 findings use wrong host form | new substrate bug — main agent never tried record-fragment, didn't bite this run |

**Aggregate:** -10 magnitude on prose-quality classes (N-1/N-2/N-4 = -6;
S2-2 / S3-1 self-redact = -1 each = -2; scaffold-code-in-kb = -1;
kb-below-floor 3→4 = -1 partial). Plus one new substrate bug
(audit fragmentId routing) that didn't bite this run.

---

## Refinement-2 sub-agent behavior compliance

| Contract | Compliance |
|---|---|
| Single fenced JSON findings block emitted | **✓** Single block at [agent-a1fa5f06daf5c1795.jsonl:182-296](docs/zcprecipator3/runs/41/SESSION_LOGS/subagents/agent-a1fa5f06daf5c1795.jsonl) (extracted assistant text). No prose around the block per `phase_entry.md` close instruction. |
| Empty findings = empty list, not absent JSON | **n/a** — 10 findings emitted. |
| No `record-fragment` calls (honor-system) | **✓** `jq` over subagent tool_use events returns only `Read` (×22) + `Bash` (×25). Zero `record-fragment` invocations. |
| No `complete-phase` calls (diagnosis-only) | **✓** Same `jq` walk confirms no `complete-phase` calls. |
| Dispatched without `codebase=` scope (engine gate `TestBuildSubagentPromptRefinement2_RefusesCodebaseScope`) | **✓** Dispatch input is `{"action":"build-subagent-prompt","slug":"nestjs-showcase","briefKind":"refinement2"}` — no `codebase` field. Engine gate did not need to refuse. |
| Walked all 10 defect classes per audit_checklist.md | **✓** Subagent narration covers (in order): kb-ig-duplication (lines 44-65), kb-below-floor / kb-over-cap counts (lines 22-28), surface-misplacement (lines 107-111), scaffold-code-in-kb (line 95-96), aspirational-as-current (line 9, line 141-147), yaml-comment-content-drift (lines 11-19), cross-codebase-named-constant-drift (lines 7-8, 139), ig-cites-recipe-internal-file (lines 98-105), missing-citation (lines 67-91). |
| Per-service-type alias allowlist applied (audit_checklist round-2 fix) | **✓** Verified manually: cache→valkey hostname valid, broker→nats connectionString valid, storage→object-storage apiUrl valid, appstage→static zeropsSubdomain valid. Meilisearch host would have been flagged if it had appeared (it didn't this run). |
| `suggestedAction` field always populated | **✓** All 10 findings carry one of the enum values (`drop`, `rewrite-as-symptom`, `add-citation`). |
| fragmentId references `plan.fragments` canonical keys | **✗** — see "New defect classes" section. Audit used SSHFS-mount form (`appdev`/`workerdev`/`apidev`) instead of plan.json short form (`app`/`worker`/`api`). |

Net: 8/9 contract items honored; one substrate gap (fragmentId glossary
not explicit enough in the brief).

---

## Known-substrate-issues confirmed still present

- **S-1** dev-server-restart-re-reads-env brief lie at
  [mount-vs-container.md:62-66](internal/recipe/content/principles/mount-vs-container.md#L62-L66)
  — deferred to live Zerops empirical test before brief edit lands. Still
  present in run-41 briefs (carried into scaffold briefs).
  Expected; not a regression.
- **S-4** parent-recipe baseline filter not implemented — TypeORM gotcha
  still appears in worker briefs; this run the child recipe uses TypeORM
  so the gotcha is appropriate. Not a regression.
- **Parent-recipe fetch by main agent at research phase** — confirmed:
  main session shows `mcp__zerops__zerops_knowledge {"recipe":"nestjs-minimal"}`
  call once. Per CLAUDE.md / prompt this is the known substrate bug at
  [phase_entry/research.md:84-87](internal/recipe/content/briefs/recipe_session_runtime/phase_entry/research.md#L84-L87)
  where the main agent proactively fetches parent-recipe context even
  though only scaffold sub-agents need it. Not a refinement-2 regression;
  documented for substrate cleanup.
- **NEW: refinement-2 fragmentId substrate gap** (see new defect classes
  above) — audit subagent used `<host>dev` form not `<host>` form for
  fragmentId citations.

---

## Spec-anchored content audit — what refinement-2's 10-class set doesn't cover

This section walks `docs/spec-content-surfaces.md` (the actual surface
contract) against the run-41 deliverable, surfacing defects the audit's
narrower 10-class enumeration doesn't fire on. Three classes surface;
all three are addressable in run-42 with a narrow scope.

### Spec defect #1 — `appdev KB #4` (Tailwind play CDN) is scaffold-decision-as-gotcha + framework-quirk

[appdev/README.md:181-185](docs/zcprecipator3/runs/41/appdev/README.md#L181-L185):

> "### Tailwind play CDN ships a 'do not use in production' console warning
>
> The SPA loads `https://cdn.tailwindcss.com` from `index.html`, which
> scans the DOM and synthesizes utility CSS at runtime. The CDN logs an
> informational warning in production — **the recipe accepts this trade
> as a build-pipeline simplification** (no PostCSS, no `tailwind.config.js`,
> no separate build step), since the goal is showcasing the Zerops
> integration rather than a production-grade frontend stack."

This bullet is a near-verbatim instance of TWO spec counter-examples
landing in one bullet:

- Per [spec §Counter-examples → Scaffold decisions disguised as gotchas](docs/spec-content-surfaces.md#scaffold-decisions-disguised-as-gotchas):
  the bullet body **literally says** "the recipe accepts this trade as
  a build-pipeline simplification" — explicit scaffold-decision admission.
- Per [spec §Counter-examples → Framework quirks](docs/spec-content-surfaces.md#framework-quirks-should-have-been-discarded):
  Tailwind CDN console warnings are 100% Tailwind framework behavior;
  zero Zerops involvement. Spec example: *"`@sveltejs/vite-plugin-svelte@^5`
  peer-requires Vite 6, not Vite 5 — npm registry metadata. Zero Zerops
  involvement. Belongs in `package.json` notes."* Same shape.
- Per [spec §Surface 5 → Does not belong here](docs/spec-content-surfaces.md#surface-5--per-codebase-readme-knowledge-base--gotchas-fragment):
  "Framework-only quirks — `setGlobalPrefix` collision with `@Controller`,
  Svelte 5 `mount()` vs legacy constructor, plugin-svelte peer-dep —
  these belong in framework docs, not here."
- Per the S5 one-question test: *"Would a developer who read the Zerops
  docs AND the framework docs STILL be surprised by this?"* Answer: no
  — it's documented at `tailwindcss.com/docs/installation/play-cdn`.

**Why refinement-2 didn't catch:** the audit's 10-class set has no
`framework-quirk-as-gotcha` or `scaffold-decision-as-gotcha` rule. The
audit walks `surface-misplacement` for "framework setup in KB" but doesn't
include the spec's fact-classification taxonomy (platform-invariant /
intersection / framework-quirk / library-metadata / scaffold-decision /
self-inflicted). The classifier-style check from
[spec §Fact classification taxonomy](docs/spec-content-surfaces.md#fact-classification-taxonomy)
is the missing rule. The audit subagent walked appdev KB#4 (line 81 of
its session log: "no topic mapping. Pass") — there was no rule to fire.

**Fix path for run-42:** drop appdev KB#4 entirely. Brings appdev KB
from 4→3 (still below floor 5 advisory). Better to ship an honest
3-bullet KB than a 4-bullet KB carrying one fake gotcha.

**Substrate fix:** add `framework-quirk-as-gotcha` and `scaffold-decision-as-gotcha`
to `audit_checklist.md`'s defect-class enumeration, anchored on the
spec's fact-classification taxonomy. The check should walk each KB
bullet through the spec's [classification × surface compatibility
table](docs/spec-content-surfaces.md#classification--surface-compatibility)
and flag bullets where classification ≠ platform-invariant / intersection.

### Spec defect #2 — `workerdev KB #5` (log buffering) is borderline framework-quirk

[workerdev/README.md:199-201](docs/zcprecipator3/runs/41/workerdev/README.md#L199-L201):

> "### Last log lines disappear when the worker exits before stdout flushes
>
> NestJS's default `Logger` writes to stdout line-buffered when stdout
> isn't a TTY (the container case). A worker that crashes or gets
> `SIGTERM`'d before the buffer flushes loses its last few log lines…"

Per the spec's S5 test: would a porter hit this on Docker locally, on
Heroku, on fly.io? Yes — generic Node.js stdout line-buffering when
stdout is a pipe. The Zerops dashboard angle ("the porter sees a clean
exit in the dashboard log viewer") is thin; the underlying mechanism
is generic Node.js + generic container behavior.

Defensible if reframed as a Zerops × NestJS intersection (Zerops's
SIGTERM-on-rolling-deploy timing × NestJS's bufferLogs default). The
current framing is more "NestJS Logger quirk" than "Zerops trap." Soft
defect, hangs on framing.

**Refinement-2 didn't catch:** same root cause as defect #1 — no
framework-quirk classifier in the audit's 10-class set.

**Fix path for run-42:** either drop or reframe as Zerops × NestJS
intersection with explicit naming of the Zerops side. Doesn't change
the KB count meaningfully.

### Spec defect #3 — cross-codebase content duplication on two load-bearing teachings

Per [spec §Counter-examples → Cross-surface duplication](docs/spec-content-surfaces.md#cross-surface-duplication-same-fact-multiple-surfaces):
"Rule: each fact lives on **one** surface. Other surfaces that need it
**cross-reference** — they do not re-author."

Run-41 has two cross-codebase re-authorings:

- **Same-key shadow trap**:
  - [apidev/README.md:240-255](docs/zcprecipator3/runs/41/apidev/README.md#L240-L255) — IG #5, full 16-line teaching with yaml example + `getaddrinfo ENOTFOUND ${db_hostname}` symptom.
  - [workerdev/README.md:119-135](docs/zcprecipator3/runs/41/workerdev/README.md#L119-L135) — IG #2, near-identical 17-line teaching with same yaml shape, same symptom.
  - Both are platform-invariant teachings that apply to any codebase using cross-service aliases. Should live ONCE (apidev, since it's the higher-traffic codebase) with workerdev IG #2 cross-referencing: *"See apidev IG #5 — same trap; this codebase wires the same own-key aliases."* This frees a workerdev IG slot to bring workerdev IG count from 5→4 (still at floor) and removes ~17 lines of duplicate prose.
- **NATS Authorization Violation**:
  - [apidev/README.md:260-262](docs/zcprecipator3/runs/41/apidev/README.md#L260-L262) — KB #1, 3-paragraph teaching of nats.js double-auth.
  - [workerdev/README.md:195-197](docs/zcprecipator3/runs/41/workerdev/README.md#L195-L197) — KB #4, same teaching same depth.
  - Same one-fact-one-surface call. One canonical KB (apidev or workerdev — both legitimately encounter it); other cross-references.

**Why refinement-2 didn't catch:** the audit's `kb-ig-duplication` rule
walks *within-codebase*, not across codebases. [audit_checklist.md:79](internal/recipe/content/briefs/refinement2/audit_checklist.md#L79):
"For each codebase {api, app, worker, …}" — the iteration is per-codebase.
The cross-codebase axis isn't tested at all.

**Fix path for run-42:** add `cross-codebase-content-duplication` to
`audit_checklist.md` as a defect class. Check pattern: for each
(KB bullet | IG item), scan ALL OTHER codebases' KB+IG fragments for
the same trap; flag pairs where both surfaces fully author the teaching
instead of one authoring + one cross-referencing.

### Spec defect #4 — refinement-2 brief's citation map drops three spec topics

The brief's citation map ([audit_checklist.md citation map block](internal/recipe/content/briefs/refinement2/audit_checklist.md#L344-L360))
lists 7 topics: rolling-deploys, init-commands, object-storage,
env-var-model, subdomain-access, managed-NATS, managed-Meilisearch.

Spec's authoritative citation map ([spec-content-surfaces.md §Citation map](docs/spec-content-surfaces.md#citation-map--which-topics-require-zerops_knowledge-citation))
lists 8 topics — the audit's set MISSES three:

- **`deploy-files / static-runtime`** for `./dist/~` rationale +
  `base: static` limitations. Triggers on
  [appdev/README.md:175-179 KB#3 "Static SPA returns 404 on /"](docs/zcprecipator3/runs/41/appdev/README.md#L175-L179)
  — the bullet teaches `dist/~` strip-prefix but has no citation. Audit
  reasoned (subagent log line 80): *"KB#3: SPA 404, base: static — no
  specific topic mapping... Pass."* Spec requires this topic citation.
- **`http-support / l7-balancer`** for VXLAN routing + 0.0.0.0 binding.
  Triggers on apidev IG #2 (Bind 0.0.0.0 and read PORT) — IG, not KB,
  but the citation rule reads naturally on IG too. Currently no
  citation.
- **`readiness-health-checks`** for `/health` route gating. Triggers
  on apidev IG #1 (engine-emitted from zerops.yaml) — the yaml comments
  explain readinessCheck/healthCheck without citing the guide.

**Why refinement-2's citation rule fired correctly on 8 missing-citation
findings BUT missed these:** the rule reads the citation map from the
brief, not from the spec. The brief's map is narrower. Per the run-41
dual-review round-2 fix #5: *"missing-citation rule now points at the
engine-rendered ## Citation map block in the same brief instead of
duplicating; the citation map block is authoritative. One source of
truth."* — that single source of truth (the brief's map) drifted three
topics short of spec.

**Fix path for run-42:** widen the brief composer's citation-map output
to mirror the spec map verbatim. The composer is in
[internal/recipe/briefs_refinement2.go](internal/recipe/briefs_refinement2.go);
the topic→guide pairs should mirror [spec §Citation map](docs/spec-content-surfaces.md#citation-map--which-topics-require-zerops_knowledge-citation).
Add a brief-composer test that asserts every spec citation-map topic
is present in the engine-rendered block.

### Spec compliance — surfaces that pass clean

For completeness — the deliverable surfaces that DO pass the spec's
one-question editorial test cleanly:

| Surface | Compliance |
|---|---|
| **S1 Root README** ([runs/41/README.md](docs/zcprecipator3/runs/41/README.md)) | 27 lines, within 25-35 cap. Tier list + deploy buttons present. Body is factual, no narrative padding. ✓ |
| **S2 Tier README** (6 tiers) | Each extract is 1-2 sentences ≤350 chars. No run-14-style ladder content inside extract markers. ✓ Verified each: [0/README.md](docs/zcprecipator3/runs/41/environments/0%20%E2%80%94%20AI%20Agent/README.md) etc. |
| **S3 Tier import.yaml comments** | Per-service blocks explain decisions (why NON_HA at small-prod, why minContainers:2 for rolling deploys). Friendly-authority voice present ("Bump verticalAutoscaling.minRam when..."). 3-5 lines/service consistently. ✓ |
| **S6 CLAUDE.md (api/app/worker)** | All three are pure `claude /init`-shape — title + framing line + Build & run + Architecture sections. ZERO Zerops content (no platform mechanics, no env-var aliases, no `zerops_*` token mentions). ✓ |
| **S7 zerops.yaml comments** | Mechanism + reason voice. Cross-reference pattern correctly used (workerdev yaml says "see the Authorization Violation entry in the gotchas section" rather than re-authoring). ✓ |

So 5/7 surfaces pass clean. S4 (IG) has the cross-codebase duplication
of same-key shadow trap; S5 (KB) has the Tailwind framework-quirk +
the cross-codebase NATS auth duplication + missing citations.

---

## Recommended next action

**ITERATE-TO-42** with a narrow scope. The recipe is shippable in spirit
but doesn't yet hit the spec quality bar on S4+S5. Run-42 closes that
gap in three substrate edits + one re-author run:

1. **Substrate: refinement-2 fragmentId glossary.**
   Edit [audit_checklist.md](internal/recipe/content/briefs/refinement2/audit_checklist.md)
   to clarify `<host>` = short codebase name (`api`/`app`/`worker`) from
   `plan.codebases[].host`, not the SSHFS-mount path or `<host>dev`
   hostname. Pin with a brief-composer test asserting the glossary
   section is present and worked example uses the short form.

2. **Substrate: widen refinement-2 citation map to spec parity.**
   Add `deploy-files/static-runtime`, `http-support/l7-balancer`,
   `readiness-health-checks` to the engine-rendered citation-map block
   in [briefs_refinement2.go](internal/recipe/briefs_refinement2.go).
   Pin with a brief-composer test that the rendered citation-map
   topic set equals the spec's topic set.

3. **Substrate: add two new defect classes to refinement-2.**
   - `framework-quirk-as-gotcha` / `scaffold-decision-as-gotcha` —
     anchored on the spec's [fact-classification taxonomy](docs/spec-content-surfaces.md#fact-classification-taxonomy)
     + [classification × surface compatibility table](docs/spec-content-surfaces.md#classification--surface-compatibility).
     Check pattern: walk each KB bullet, classify per the taxonomy,
     flag if classification ∈ {framework-quirk, library-metadata,
     scaffold-decision (recipe-internal flavor), self-inflicted}.
   - `cross-codebase-content-duplication` — extend `kb-ig-duplication`
     iteration to ALSO scan across codebases. Flag pairs where the
     same trap is fully authored on multiple codebases instead of
     one canonical + one cross-reference.

4. **Re-author: run-42 dogfood.** With substrate fixes in place, the
   refinement-2 audit should catch:
   - appdev KB #4 (Tailwind CDN) → drop
   - apidev IG #5 vs workerdev IG #2 (same-key shadow duplicate) →
     keep apidev, rewrite workerdev IG #2 as cross-reference
   - apidev KB #1 vs workerdev KB #4 (NATS auth duplicate) → same
   - Three citation additions on apidev KB#3 + apidev KB#3
     (env-var-model) + appdev KB#3 (deploy-files)

5. **Then promote to canonical recipe.** Run-42's deliverable should
   clear the spec quality bar; run-41 demonstrates the engine can
   produce a clean deliverable but doesn't yet hit spec floor on S4+S5
   content classification + cross-codebase dedup.

### What's NOT recommended

- Don't extend ENG-2 sanitizer for the `prg1.zerops.app` zone literals
  on TIMELINE (4 hits in placeholder-`<id>` form, already clean — no
  porter-visible leak). Opportunistic only.
- Don't worry about kb-below-floor on appdev KB (4 bullets, floor 5)
  in isolation — once Tailwind KB#4 is dropped, count goes to 3, but
  the right fix is *adding a real platform-trap bullet*, not bumping
  the count. Candidate from facts.jsonl: a Vite-specific Zerops trap
  the agent hit during scaffold (e.g. `documentRoot: dist` 404 before
  `dist/~` fix surfaced — currently buried in TIMELINE issues table).

---

## Recipe-quality sidebar — what got better between run-40 and run-41

Beyond the named defect classes, two structural improvements worth
calling out:

- **Self-redacted engine vocabulary in TIMELINE.** Run-40 TIMELINE shipped
  with raw `complete-phase`/`record-fragment`/`stitch-content` tokens that
  the export-time sanitizer (ENG-2) had to scrub. Run-41 TIMELINE has
  zero raw engine-vocab tokens; instead the main agent authored
  `<engine-detail>` placeholders inline ([TIMELINE.md:45,99,110,168](docs/zcprecipator3/runs/41/TIMELINE.md)).
  The sanitizer no longer needs to fire on-disk; the on-disk form ships
  porter-clean.
- **KB titles uniformly symptom-led.** All three codebases' KB sections
  now lead H3 headings with the porter-observable failure ("VITE_API_URL
  is not configured at build time on first paint", "Missing queue option
  drops zero rows but doubles every write…"). This is what closed the
  KB↔IG duplication class without dropping bullets — the structural
  pairing remains, but each KB bullet now teaches the symptom dimension
  the IG bullet's fix-led prose doesn't cover.

These are upstream-authoring wins, not refinement-2 catches. Worth
noting so future iterations don't try to attribute them to the audit.
