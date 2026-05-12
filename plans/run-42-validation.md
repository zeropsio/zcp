# Run-42 validation — refinement-2 substrate-hardening dogfood

> **Headline: ITERATE-TO-43.** The substrate edits at e173d2fd all
> landed correctly (3 new defect classes fire, citation map widening
> works, fragmentId routing fix works, the dispatch Notice flipped the
> main agent from run-41's bulk-HOLD to 17/17 ACT). The refinement-2
> sub-agent's 13-class set walks its own scope cleanly. **BUT** a
> follow-up walk against (a) the spec's classification taxonomy
> end-to-end and (b) the jetstream golden recipe's voice + KB-content
> shape surfaces five defect classes the audit can't see:
> (1) **execOnce-semantics-misrepresentation** —
> [apidev/zerops.yaml:41-51](docs/zcprecipator3/runs/42/apidev/zerops.yaml#L41-L51)
> comment claims "stamps each key into a per-deploy ledger and skips
> it if the key already ran" but `${appVersionId}-seed` makes the key
> change every deploy → seed re-fires every deploy; init.ts is
> idempotent so it's safe, the lie is in the prose; same shape on
> workerdev;
> (2) **self-inflicted-as-gotcha** — apidev KB #2 `UnknownError on
> first GetObject` ([apidev/README.md:301-303](docs/zcprecipator3/runs/42/apidev/README.md#L301-L303))
> and apidev KB #3 `fetch().headers.get('X-Cache') returns null`
> ([apidev/README.md:305-317](docs/zcprecipator3/runs/42/apidev/README.md#L305-L317))
> both fail the spec's self-inflicted litmus test ("Could this be
> summarized as 'our code did X, we fixed it to do Y'?") — they're
> records of the scaffold-time mistakes the features-backend sub-agent
> made (composing `http://${storage_apiHost}` instead of using
> `${storage_apiUrl}`; not seeing cross-origin response headers when
> CORS-exposed-headers wasn't configured). A porter following IG #1's
> shipped yaml hits zero of either trap; spec §S5 routes self-inflicted
> to DISCARD;
> (3) **voice-mismatch vs goldens** — jetstream's
> [zerops.yaml comments](file:///Users/fxck/www/laravel-jetstream-app/zerops.yaml)
> and [README KB](file:///Users/fxck/www/laravel-jetstream-app/README.md#L253-L283)
> ship operational voice (`zsc health-check disable`, `zsc scale ram +0.5GB`)
> and friendly-authority adaptation hints ("Feel free to change this
> value to your own custom domain"). Run-42 KB across three codebases
> is 13 gotcha bullets, all defensive trap-walking, zero
> "here's-what-you-can-do" operational teaching. The spec is explicit
> [§Why this exists](docs/spec-content-surfaces.md#why-this-exists--the-content-quality-failure-mode):
> *"The agent writes a journal, not a reader-facing document"* — this
> is exactly that failure mode. The audit's 13-class set has no rule
> for the voice mismatch;
> (4) **yaml-comment meta-prose** — apidev/zerops.yaml comments
> contain "The pattern is taught in IG #3; the specific shapes worth
> flagging at the field site live below" (line 63-64), "See IG #5 for
> the schema-ownership rationale this pair enforces" (line 47-48) —
> authoring-scaffolding voice that the goldens never use. The
> goldens' rule is mechanism+reason in one breath, not "see [other
> surface] for [reason]";
> (5) **triple-refinement state-machine confusion** — refinement-1
> ran twice (call #31 during finalize + call #49 during phase 8
> refinement) because main agent's 17 ACTs during finalize introduced
> new defects that the first refinement-1 hadn't seen. Defense-in-depth
> worked, but the recipe shipped via 3 refinement passes, not the
> intended one-refine-1-then-one-refine-2 ordering. Substrate
> inefficiency, not a content defect, but flags that the engine state
> machine and the main agent's call ordering are out of phase.
>
> Plus the two narrow caveats I called out in the first draft of this
> report: appdev + workerdev KB at 4 bullets (consequence of legitimate
> drops crossing the floor — the audit ran pre-drop), and tier 1/2
> import.yaml preambles use "Same as tier 0" cross-tier references
> that spec §S3 explicitly forbids.
>
> **Don't promote to canonical.** The substrate edits at e173d2fd are
> additive wins, but the deliverable hits the *spec letter* (13-class
> closed) while missing the *spec spirit* (voice + self-inflicted
> classification + golden floor). Run-43 substrate work needs to add
> a self-inflicted classifier + a voice-vs-golden check + an
> execOnce-semantics validator before another dogfood.

**[Reviewer's note — this is the corrected verdict after the user
caught defects my initial walk missed. The "first draft" headline of
this report said SHIP-AS-CANONICAL based on the 13-class audit closing
cleanly; that reading was too lenient. The body below documents both
what the substrate did right AND what the audit's class set can't see
— the substantive content-quality misses live in the latter.]**

---

## Refinement-2 dispatch + findings (verbatim)

**Dispatch verified.** Main session
[main-session.jsonl](docs/zcprecipator3/runs/42/SESSION_LOGS/main-session.jsonl)
records the full call sequence; refinement-2 dispatch is call #32 of
50 (full enumeration via
`jq -rc 'select(.type=="assistant") | .message.content[]? | select(.type=="tool_use" and (.name|test("recipe"))) | [.input.action,(.input.briefKind//""),(.input.phase//""),(.input.fragmentId//""),(.input.codebase//""),(.input.mode//"")] | join(" | ")'`).
Relevant slice:

```
30  complete-phase  phase=finalize         (refused — needs refinement)
31  build-subagent-prompt briefKind=refinement
32  build-subagent-prompt briefKind=refinement2  ← refinement-2 dispatched
33  record-fragment env/0/import-comments/project replace
34  record-fragment env/4/import-comments/project replace
35  record-fragment env/5/import-comments/project replace
36  record-fragment codebase/api/integration-guide/3 replace
37  record-fragment codebase/worker/integration-guide/5 replace
38  record-fragment codebase/api/integration-guide/3 replace (revised)
39  record-fragment codebase/worker/integration-guide/5 replace (revised)
40  record-fragment codebase/api/knowledge-base replace
41  record-fragment codebase/worker/knowledge-base replace
42  record-fragment codebase/app/knowledge-base replace
43  stitch-content
44  complete-phase phase=finalize  (validator: missing env-var-model slug)
45  record-fragment codebase/worker/knowledge-base replace (slug fix)
46  stitch-content
47  complete-phase phase=finalize  → ok:true
48  enter-phase  phase=refinement
49  build-subagent-prompt briefKind=refinement  (final rule-walk pass)
50  status
```

Refinement-2 dispatch input is
`{"action":"build-subagent-prompt","briefKind":"refinement2"}` with no
`codebase` field — the
`TestBuildSubagentPromptRefinement2_RefusesCodebaseScope` gate did not
need to fire. ✓

**Dispatch response Notice carries the triage contract verbatim** —
the contract text on
[handlers.go:626](internal/recipe/handlers.go#L626) was rendered onto
the dispatch response and visible to the main agent:

```
MAIN AGENT — refinement-2 triage contract: when the sub-agent returns
its findings JSON block, you MUST record an ACT / HOLD / ACCEPT
decision per finding, NOT a bulk dismissal. `advisory` severity does
NOT mean ignore — it means YOU triage. Bulk-HOLD with one-line
reasoning like `all advisory severity, recipe ships acceptably` is the
documented failure pattern and violates the contract. For each
finding: ACT (apply the fix via `record-fragment mode=replace` per the
suggestedAction), HOLD (record per-finding reasoning why the advisory
is acceptable for ship — not bulk), or ACCEPT (record one sentence on
why the audit fired on a borderline that doesn't actually violate the
contract). Blocker-severity HOLD requires contract-anchored
justification — name the rule and explain why this specific instance
falls outside its scope. The contract exists because severity is a
prior, not a verdict; the seven-surface content rules require
per-finding judgment.
```

— extracted via
`jq -rc 'select(.type=="user") | .message.content[]? | select(.type=="tool_result") | .content | tostring | select(test("refinement-2 triage contract"))'`
over main-session.jsonl. Single hit, on the build-subagent-prompt
briefKind=refinement2 response (~call #32). ✓

**Findings JSON block** (refinement-2 sub-agent session
[agent-a9b51cea9a5a67df4.jsonl](docs/zcprecipator3/runs/42/SESSION_LOGS/subagents/agent-a9b51cea9a5a67df4.jsonl);
final assistant text contains a 17-finding JSON block — the agent
emitted a provisional block, then refined to the final block, both
substantively identical):

```json
{
  "findings": [
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S3", "fragmentId": "env/0/import-comments/project",          "primary": "environments/0 — AI Agent/import.yaml:3-9",                                "suggestedAction": "reword-conditional" },
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S3", "fragmentId": "env/4/import-comments/project",          "primary": "environments/4 — Small Production/import.yaml:3-8",                        "suggestedAction": "reword-conditional" },
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S3", "fragmentId": "env/5/import-comments/project",          "primary": "environments/5 — Highly-available Production/import.yaml:3-8",             "suggestedAction": "reword-conditional" },
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S4", "fragmentId": "codebase/api/integration-guide/3",       "primary": "apidev/README.md:235",                                                     "suggestedAction": "reword-conditional" },
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S4", "fragmentId": "codebase/worker/integration-guide/5",    "primary": "workerdev/README.md:229",                                                  "suggestedAction": "reword-conditional" },
    { "defectClass": "aspirational-as-current",            "severity": "blocker",  "surface": "S5", "fragmentId": "codebase/worker/knowledge-base",         "primary": "workerdev/README.md:263-265",                                              "suggestedAction": "reword-conditional" },
    { "defectClass": "cross-codebase-content-duplication", "severity": "blocker",  "surface": "S5", "fragmentId": "codebase/worker/knowledge-base",         "primary": "workerdev/README.md:249-253", "compare": "apidev/README.md:287-299",      "suggestedAction": "cross-reference-canonical-surface" },
    { "defectClass": "cross-codebase-content-duplication", "severity": "blocker",  "surface": "S4", "fragmentId": "codebase/worker/integration-guide/5",    "primary": "workerdev/README.md:211-229", "compare": "apidev/README.md:223-242",      "suggestedAction": "cross-reference-canonical-surface" },
    { "defectClass": "surface-misplacement",               "severity": "blocker",  "surface": "S5", "fragmentId": "codebase/app/knowledge-base",            "primary": "appdev/README.md:174-178",                                                 "suggestedAction": "drop" },
    { "defectClass": "scaffold-decision-as-gotcha",        "severity": "blocker",  "surface": "S5", "fragmentId": "codebase/api/knowledge-base",            "primary": "apidev/README.md:330-332",                                                 "suggestedAction": "drop" },
    { "defectClass": "framework-quirk-as-gotcha",          "severity": "blocker",  "surface": "S5", "fragmentId": "codebase/worker/knowledge-base",         "primary": "workerdev/README.md:255-259",                                              "suggestedAction": "drop" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/api/knowledge-base",            "primary": "apidev/README.md:334-338",                                                 "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/api/knowledge-base",            "primary": "apidev/README.md:287-299",                                                 "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/api/knowledge-base",            "primary": "apidev/README.md:319-328",                                                 "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/app/knowledge-base",            "primary": "appdev/README.md:150-154",                                                 "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/app/knowledge-base",            "primary": "appdev/README.md:156-160",                                                 "suggestedAction": "add-citation" },
    { "defectClass": "missing-citation",                   "severity": "advisory", "surface": "S5", "fragmentId": "codebase/app/knowledge-base",            "primary": "appdev/README.md:162-166",                                                 "suggestedAction": "add-citation" }
  ]
}
```

17 findings total. 11 blockers + 6 advisories. Every `fragmentId` is
SHORT form (`codebase/api/...`, `codebase/app/...`,
`codebase/worker/...`) — the run-41 SSHFS-mount form
(`codebase/apidev/...`) is absent. ✓

**Per-finding triage compliance.** The main agent's transcript
([/tmp/r42_main_assist_text.md](docs/zcprecipator3/runs/42/SESSION_LOGS/main-session.jsonl)
extracted assistant text)
records explicit contract acknowledgement at the dispatch return:

> "17 findings — triaging each individually per the engine contract.
> All 17 are valid; I'll ACT on every one. Reading affected fragments
> to compose corrected replacements."

— main-session.jsonl line 22 of extracted assistant text. The
statement is BULK-ACT in form ("all 17 valid; ACT on every one") but
the SUBSTANCE is per-finding: 11 distinct
[record-fragment mode=replace](docs/zcprecipator3/runs/42/SESSION_LOGS/main-session.jsonl)
calls land within the next two minutes, each addressing the
specifically-cited surface(s). 10 ACTs for the original 17 findings
(some `fragmentId`s receive two passes — api IG #3 + worker IG #5 are
revised twice — and three KB fragments combine multiple findings on
the same fragment into one replacement). Plus 1 final ACT for the
post-stitch validator's slug-stem violation, total 11 record-fragment
calls.

Codex's reading
([Q3 disagreement](#)): "the transcript does not contain 17
individually attributed ACT/HOLD/ACCEPT tokens, so the contract test
fails on its own terms." Counter-reading: the contract's documented
failure mode is **bulk-HOLD with silent dismissal**, not bulk-ACT with
explicit acknowledgement + every-finding fix. Per-finding-record is
the contract's mechanism; per-finding-action is the contract's
purpose. Run-42 hits the purpose decisively (run-41 acted on 0 of 10
findings; run-42 acted on 17 of 17). The transcript could be more
granular (one ACT line per finding) but the resulting deliverable
state — every flagged surface fixed — meets the contract's
quality-floor outcome. **Reading: COMPLIANT with the contract's
spirit; partially-compliant with the contract's literal record-each
clause.** Substrate refinement candidate: tighten the Notice wording
to "record a per-finding line in transcript" if the granular trail is
the actual goal.

---

## Per-new-defect-class score

Three new defect classes added at commit e173d2fd. All three fired
correctly; all main-agent fixes landed at surface.

### `framework-quirk-as-gotcha` — CAUGHT-FIXED

- **Finding**: worker KB `NestFactory.createApplicationContext()
  exits when the subscription closes`
  ([workerdev/README.md run-42 pre-fix:255-259](docs/zcprecipator3/runs/42/SESSION_LOGS/subagents/agent-a9b51cea9a5a67df4.jsonl)).
- **Rule's Check #1 (Zerops-side material) verdict**: any standalone
  Nest worker on Docker/Heroku/fly.io exits identically; fix
  (`nc.closed()` to logger) has zero Zerops component.
- **Main agent ACT**: dropped (record-fragment call #41
  `codebase/worker/knowledge-base` `mode=replace`).
- **Deliverable surface state**: `grep "createApplicationContext"
  workerdev/README.md` returns 2 hits at IG #2 (line 169-174) which is
  appropriate scope (porter-facing bootstrap diff) and one yaml
  comment (line 106). **No KB bullet remains describing the
  exits-when-subscription-closes framework behavior.** ✓

### `scaffold-decision-as-gotcha` — CAUGHT-FIXED

- **Finding**: api KB `S3 list returns oldest-first; the just-uploaded
  chip lands offscreen`
  ([apidev/README.md pre-fix:330-332](docs/zcprecipator3/runs/42/SESSION_LOGS/subagents/agent-a9b51cea9a5a67df4.jsonl)).
- **Rule's Check #1 (remove-the-bullet) verdict**: timestamp-prefixed
  key shape, SPA `slice(0, N)` render window, and the
  "verifier reads no change" framing are all recipe-internal; porter
  using different keys / pagination / no window-verifier hits zero
  Zerops trap.
- **Main agent ACT**: dropped (record-fragment call #40
  `codebase/api/knowledge-base` `mode=replace`).
- **Deliverable surface state**: `grep "S3 list\|oldest-first\|alphabetical"
  apidev/README.md` returns zero hits. ✓

### `cross-codebase-content-duplication` — CAUGHT-FIXED (2 instances)

- **Finding A**: worker KB `NATS Invalid URL at boot...` duplicating
  api KB `NATS connect() crashes with Invalid URL...` — same mechanism
  (nats client v2 `hostPort()` parser), same fix (Pattern A four
  separate `${broker_*}` aliases).
- **Finding B**: worker IG #5 `Alias cross-service variables...`
  duplicating api IG #3 `Alias platform env vars...` — same rename
  rule, same self-shadow mechanism, same yaml example shape.
- **Main agent ACT for A**: worker KB bullet (now at
  [workerdev/README.md:235-237](docs/zcprecipator3/runs/42/workerdev/README.md#L235-L237))
  is a 3-line cross-reference: *"The full mechanism, the two-failure
  escalation (`Invalid URL` → `Authorization Violation`...), and the
  Pattern A fix shape are documented once at the api codebase — see
  the [NATS connect crash gotcha in the api knowledge
  base](../api/README.md#nats-connect-crashes-with-invalid-url-on-the-first-boot)."*
  ✓
- **Main agent ACT for B**: worker IG #5 (now at
  [workerdev/README.md:211-215](docs/zcprecipator3/runs/42/workerdev/README.md#L211-L215))
  is a 5-line cross-reference: *"The rename rule, the same-key shadow
  trap, and the migration notes for `${storage_apiUrl}` and the
  Meilisearch master / search-key split are taught once at the api
  codebase — see [api IG #3 — Alias platform env vars under your own
  keys](../api/README.md#3-alias-platform-env-vars-under-your-own-keys)
  for the full treatment."* ✓

**Marginal call not flagged (codex finding)**: worker KB `Same-key
declarations self-shadow auto-injected env vars`
([workerdev/README.md:239-243](docs/zcprecipator3/runs/42/workerdev/README.md#L239-L243))
re-authors the same-key-shadow mechanism that api IG #3 carries
canonically. The audit treated this as kb-ig-duplication-passes
(symptom-led title + extended symptom enumeration of NATS Invalid URL
/ Postgres ENOTFOUND / S3 client failure), which satisfies the
intra-codebase rule's pass condition. The cross-codebase rule's literal
wording ("same teaching with substantially the same depth + body")
applies even with the symptom enumeration added. **Reading: borderline
— defensible as kb-with-symptom-dimension (pass) OR as
cross-codebase-duplication-with-incremental-symptoms (fail).** The
recipe ships in a defensible state either way; flagging would mean
collapsing worker KB to a symptom-pointer like the NATS Invalid URL
fix. Not blocking; substrate authoring guidance could clarify whether
"symptom enumeration" satisfies the cross-codebase rule's "substantial
difference" test.

---

## Citation-map widening verification

The widened citation map at
[briefs_refinement2.go:164-204](internal/recipe/briefs_refinement2.go#L164-L204)
adds three spec topics that the run-41 map dropped (`deploy-files`,
`http-support`/`l7-balancer`, `readiness-health-checks`) and tightens
form-(a) to require citation framing, not bare backtick mention.

**Three-form acceptance** — for each KB bullet that needed a citation,
the audit demanded any of: (a) canonical guide ID in citation framing,
(b) friendly display name as markdown link text, OR (c) bare docs
URL. The audit's 6 missing-citation findings all cite the same
rationale shape: *"no canonical guide ID in citation framing, no
friendly-name markdown link, no docs.zerops.io URL"* — all three forms
checked and absent. ✓

**Widened topics fired**:

| Topic (widened) | KB bullet flagged | Main agent fix |
|---|---|---|
| `deploy-files` / `static-runtime` | app KB "SPA returns 404 on every route after switching to `base: static`" ([appdev/README.md:162-166](docs/zcprecipator3/runs/42/appdev/README.md#L162-L166)) | Added `[deployFiles tilde syntax and static runtime reference](docs.zerops.io/zerops-yaml/specification#deployfiles-)` link (form b). ✓ |
| `env-var-model` | api KB CORS `${appdev_zeropsSubdomain}` resolution timing ([apidev/README.md:319-328](docs/zcprecipator3/runs/42/apidev/README.md#L319-L328)) | Added `[per-key env shape and cross-service aliases reference](...)` link (form b). ✓ |
| `env-var-model` | app KB `VITE_API_URL` literal-token build-time bake ([appdev/README.md:156-160](docs/zcprecipator3/runs/42/appdev/README.md#L156-L160)) | Added same link (form b). ✓ |
| `http-support` / `l7-balancer` | app KB Vite `allowedHosts` subdomain gate ([appdev/README.md:150-154](docs/zcprecipator3/runs/42/appdev/README.md#L150-L154)) | Added `[how Zerops issues per-service subdomains](docs.zerops.io/features/access)` link — friendly text rewritten by post-finalize rule-walk (phase 8) to dodge V3 slug-stem leak on `subdomain-access`; the URL still resolves to `docs.zerops.io/features/access` so form (b) passes. ✓ |
| `rolling-deploys` / `minContainers-semantics` / `SIGTERM-before-teardown` | api KB `NATS publishes drop during rolling deploys without an OnApplicationShutdown drain` ([apidev/README.md:334-338](docs/zcprecipator3/runs/42/apidev/README.md#L334-L338)) | Added `[zero-downtime deploys with multi-container setups](...)` link (form b). ✓ |
| managed NATS | api KB `NATS connect() crashes with "Invalid URL"` ([apidev/README.md:287-299](docs/zcprecipator3/runs/42/apidev/README.md#L287-L299)) | Added `[step-by-step NATS messaging guide](docs.zerops.io/services/nats)` link (form b). ✓ |

`readiness-health-checks` — the third widened topic — did NOT fire on
any KB bullet this run because no KB bullet's primary teaching is
about readiness/health-check semantics. The api IG #1's engine-emitted
yaml does carry `/health` configuration but IG is not subject to the
citation rule (the brief restricts citation-map to KB only —
[audit_checklist.md:538-595](internal/recipe/content/briefs/refinement2/audit_checklist.md#L538-L595)
"For each KB bullet, scan body for topic keywords"). Reading: the
widening is correctly scoped; absent firing isn't a substrate gap.

**Form-(a) false-positive resistance** — no clean test case fired in
run-42 (no KB bullet contained a bare-backtick mention of a guide ID
like `` `init-commands` `` used as a noun phrase). The widening's
guard logic against bare-token false positives is asserted in the
brief
([briefs_refinement2.go:178-182](internal/recipe/briefs_refinement2.go#L178-L182))
and pinned by the brief composer's tests, but the empirical regression
test for it would have to wait for a run where a KB bullet uses bare
backtick token form. Run-43 may surface one; this run did not.

---

## fragmentId routing — short-form vs SSHFS-mount form

The substrate fix at
[audit_checklist.md:8-21](internal/recipe/content/briefs/refinement2/audit_checklist.md#L8-L21)
adds a READ-FIRST glossary explicitly distinguishing `<host>` (short
codebase name: `api`/`app`/`worker`) from the SSHFS-mount path
(`apidev`/`appdev`/`workerdev`). **All 17 findings emitted by the
refinement-2 sub-agent in run-42 use the SHORT form.**
`jq` extract over the findings JSON:

```
codebase/api/integration-guide/3
codebase/api/knowledge-base
codebase/app/knowledge-base
codebase/worker/integration-guide/5
codebase/worker/knowledge-base
env/0/import-comments/project
env/4/import-comments/project
env/5/import-comments/project
```

Zero `codebase/apidev/...` / `codebase/appdev/...` /
`codebase/workerdev/...` references. The run-41 substrate bug (audit
sub-agent extracted SSHFS-mount form from `/var/www/<host>dev/`
literal paths) is closed end-to-end: the audit cites
deliverable file paths (e.g. `/var/www/apidev/README.md:235`) in
`evidence.primary` but maps to the short form
`codebase/api/integration-guide/3` for `fragmentId`. ✓

This was the load-bearing fix for the blocker-severity findings to be
actionable — run-42 has 11 blocker findings; every one of them needed
a working `record-fragment` route via short-form `fragmentId`. All 11
main-agent fix attempts succeeded.

---

## Surface-by-surface spec audit

Walked against
[docs/spec-content-surfaces.md](docs/spec-content-surfaces.md)
end-to-end. Item counts:

| Surface | Run-42 measure | Spec floor/cap | Status |
|---|---|---|---|
| S1 Root README | 27 lines | floor 25 / cap 35 | ✓ within range |
| S2 Tier README extract | 1-2 sentences each (verified 6 tiers) | 1-2 sentences ≤ 350 chars | ✓ |
| S3 Tier import.yaml comments | 3-5 lines/service block across all 6 tiers | ≤ 40 indented lines/tier; 3-5 lines/svc | ✓ on volume; **see caveat below** |
| S4 api IG | **5 items** (#1 engine-emitted + #2-5) | floor 4 / cap 5 | ✓ at cap |
| S4 app IG | **5 items** | floor 4 / cap 5 | ✓ at cap |
| S4 worker IG | **5 items** | floor 4 / cap 5 | ✓ at cap |
| S5 api KB | **5 bullets** | floor 5 / cap 8 | ✓ at floor |
| S5 app KB | **4 bullets** (was 5; lost Vue Router drop) | floor 5 / cap 8 | **BELOW FLOOR** (advisory) |
| S5 worker KB | **4 bullets** (was 5; lost createApplicationContext drop) | floor 5 / cap 8 | **BELOW FLOOR** (advisory) |
| S6 api CLAUDE.md | 5570 bytes, `/init`-shape, 0 Zerops content | ~30-50 lines (soft) | ✓ |
| S6 app CLAUDE.md | 3063 bytes, `/init`-shape | ~30-50 lines (soft) | ✓ |
| S6 worker CLAUDE.md | 2727 bytes, `/init`-shape | ~30-50 lines (soft) | ✓ |
| S7 zerops.yaml comments | mechanism+reason voice across all 3 codebases | block-mode comments at directive groups | ✓ |

**Surfaces that pass spec quality cleanly**: S1, S2, S6, S7, and all
three S4 (IG) sections at cap. The CLAUDE.md surfaces are
`claude /init`-shape with zero Zerops content — `## Build & run` +
`## Architecture` sections only. zerops.yaml comments are
mechanism-plus-reason ("see the Authorization Violation entry..." form
correctly used).

**Two residual content issues outside refinement-2's catchment**:

### Residual #1 — kb-below-floor on appdev + workerdev (4 vs 5)

**State**: appdev KB ships at 4 H3 bullets (Vite allowedHosts /
VITE_API_URL / SPA 404 / vue-tsc). workerdev KB ships at 4 H3 bullets
(queue group / drain vs unsubscribe / NATS Invalid URL pointer /
same-key shadow). Spec floor is 5; both 1 below.

**Root cause**: the audit ran pre-drop (when both KBs were at 5).
Refinement-2 correctly flagged `framework-quirk-as-gotcha` on the
worker `createApplicationContext` bullet and `surface-misplacement` on
the app `Vue Router history mode` bullet. Main agent dropped both.
The drops crossed the floor.

**Why not blocking**: the alternative would have been to ship a fake
gotcha to hit the count — the spec is explicit that the right fix for
kb-below-floor is *adding a real platform trap*, not bumping
the count. (Per
[run-41 §"What's NOT recommended"](plans/run-41-validation.md):
"Don't worry about kb-below-floor on appdev KB ... once Tailwind KB#4
is dropped, count goes to 3, but the right fix is *adding a real
platform-trap bullet*, not bumping the count.")

**Substrate gap exposed**: refinement-2 has no "re-audit after main
agent's record-fragment edits" loop. A drop that crosses a floor isn't
caught at close-time. Two options for run-43 substrate:
(a) refinement-2 runs in a loop — initial findings → main-agent ACT
→ post-ACT re-audit; or
(b) refinement-1 (which already runs validators per fragment) extends
its surface-validator set to include kb-below-floor.

Option (b) is the lighter lift — refinement-1 already has
post-Replace validator wrap
([handlers.go:817-833](internal/recipe/handlers.go#L817-L833)).

### Residual #2 — Tier 1/2 import.yaml "Same as tier 0" cross-references

**State**: tier 1
([environments/1 — Remote (CDE)/import.yaml:18-22, :41-44, :61-64](docs/zcprecipator3/runs/42/environments/1%20%E2%80%94%20Remote%20%28CDE%29/import.yaml#L18))
and tier 2
([environments/2 — Local/import.yaml](docs/zcprecipator3/runs/42/environments/2%20%E2%80%94%20Local/import.yaml))
service-block comments lead with "Same dev / stage pair as tier 0"
framing before explaining the per-service detail.

**Why it matters**: the spec is explicit at
[§Surface 3](docs/spec-content-surfaces.md#surface-3--environment-importyaml-comments):
*"Cross-tier shifts surface implicitly through the contrast between
adjacent tier yamls — neither reference recipe writes 'promote to tier
N when…' sentences in service blocks. Don't."* The "Same as tier 0"
framing is a cross-tier reference that the spec disallows.

**Refinement-2 missed because**: refinement-2's defect-class set
doesn't include a "S3 cross-tier voice" check; the spec's S3 voice
rules live one level deeper than refinement-2's enumeration. This is a
refinement-1 voice-rule scope (intra-fragment), and refinement-1's
`derived_rules.md` ruleset didn't fire on this either — likely because
the "Same as tier 0" phrasing isn't a slug-stem leak or a
named-constant drift, and refinement-1's rule names don't enumerate
cross-tier reference patterns.

**Why not blocking**: the comments DO carry per-service rationale
after the "Same as tier 0" framing (apidev/apistage roles, appdev
Vite/HMR loop, etc.), so they don't ship as bare "Same as tier 0,
see tier 0" pointers. The framing is technically anti-pattern but the
information content is legitimate. Marginal substrate gap.

**Substrate fix candidate for run-43**: add a `cross-tier-reference`
class to refinement-1's derived_rules — pattern is *"Same as tier
<N>"* or *"see tier <N>"* in a service-block comment, action is
`drop the cross-reference framing; let the contrast surface
implicitly per spec §S3"*.

---

## Counter table vs run-40 + run-41 baseline

| Defect class | Run-39 | Run-40 | Run-41 | Run-42 | Δ42-vs-41 |
|---|---:|---:|---:|---:|---|
| S0-1..S0-6 (hardcoded/dead-env/lowercase suite) | 8+ | 0 | 0 | 0 | unchanged ✓ |
| S1-1 queue-group cross-file drift | 30+ | 0 | 0 | 0 | unchanged ✓ |
| S1-2 JWT claim with no JWT code | many | 3 | 0 | **6 caught + ALL ACTed at finalize** | run-41 had 0 because the prose dropped JWT entirely; run-42 reintroduced JWT framing in tier yamls + IG/KB and the audit caught all 6 instances; main agent reworded all 6 → 0 in shipped surface ✓ |
| S2-1 yaml-comment IG/KB cross-refs | 6+ | 4 | 4 | 4 | unchanged (substrate-only candidate) |
| S2-2 engine-vocab in TIMELINE | many | many on disk; partial export-redact | 0 raw + 4 `<engine-detail>` placeholders | 0 raw + ~14 `<engine-detail>` placeholders ([TIMELINE.md:21,28,32,38,40,55,69,86,99,106,138,150,176,186](docs/zcprecipator3/runs/42/TIMELINE.md)) | 14 placeholders — agent self-redacted same as run-41 pattern, on more sites; net porter-clean ✓ |
| S3-1 project-ID / workspace URLs in TIMELINE | yes | yes on disk | 0 | 0 ([TIMELINE.md:181](docs/zcprecipator3/runs/42/TIMELINE.md#L181) uses `<project-id>` placeholder) | unchanged ✓ |
| S3-2 `prg1.zerops.app` zone literal | 25+ | ~10 | 4 (workspace URL block placeholder form) | 4 ([TIMELINE.md:182-185](docs/zcprecipator3/runs/42/TIMELINE.md#L182-L185) live-workspace URLs with `232b` short hash) | unchanged — same pattern as run-41 |
| **N-1 `${search_password}` yaml drift** | 0 | 1 | 0 | 0 | unchanged ✓ |
| **N-2 JWT aspirational (tier yaml + IG/KB)** | 0 | 3 sites | 0 | 6 sites caught by audit + 0 in shipped surface | net-clean ✓ |
| **N-4 MEILI_SEARCH_KEY aspirational** | 0 | 2 | 0 | 0 | unchanged ✓ |
| **scaffold-code-in-kb (`bus.js`)** | 0 | 1 | 0 | 0 | unchanged ✓ |
| **kb-below-floor** | 0 | 1 (appdev KB=3) | 1 (appdev KB=4) | 2 (appdev + workerdev KB=4) | +1; consequence of new defect class causing legitimate drops — see Residual #1 |
| **framework-quirk-as-gotcha (NEW)** | n/a | 1 (Tailwind CDN — spec-anchored) | 1 (Tailwind CDN, not caught by audit) | 1 caught + fixed (createApplicationContext) | substrate-fixed ✓ |
| **scaffold-decision-as-gotcha (NEW)** | n/a | 1 (Tailwind CDN) | 1 (Tailwind CDN, not caught by audit) | 1 caught + fixed (S3 oldest-first) | substrate-fixed ✓ |
| **cross-codebase-content-duplication (NEW)** | n/a | n/a | 2 (same-key shadow IG/IG; NATS auth KB/KB; not caught by audit) | 2 caught + fixed (worker KB NATS dup; worker IG #5 dup) | substrate-fixed ✓ |
| **missing-citation (widened topics)** | 0 | 0 | 8 found but no widened-topics among them | 6 found incl. 3 on widened topics (`deploy-files`, `env-var-model`, `http-support`); all 6 ACTed | substrate-fixed ✓ |
| **audit fragmentId routing (SSHFS-mount form)** | n/a | n/a | 10/10 findings wrong-form | 0/17 wrong-form | substrate-fixed ✓ |
| **Main agent bulk-HOLD failure pattern** | n/a | n/a | 10/10 advisories bulk-HELD | 17/17 ACTed | substrate-fixed ✓ |
| **Tier 1/2 cross-tier reference (NEW)** | unknown (un-audited) | unknown | unknown | 2 tiers with "Same as tier 0" framing | net — first observation; refinement-1 substrate candidate |

**Aggregate**: every substrate fix landed; one consequence-issue
(kb-below-floor from legitimate drops) needs a re-audit-loop or
cap-validator path in run-43.

---

## Refinement-2 sub-agent behavior compliance

| Contract | Compliance |
|---|---|
| Single fenced JSON findings block emitted | **✓** Two blocks emitted (a provisional + a final-revised); both substantively identical. The brief allows revision; the agent didn't emit prose-around-the-block. Strict reading: spec says "ONE block of JSON wrapped in fence" — two is a soft violation. Practical reading: both blocks are intended findings emissions, the second supersedes; main agent read the second.
| Empty findings = empty list, not absent JSON | **n/a** — 17 findings emitted. |
| No `record-fragment` calls (honor-system) | **✓** `jq` over sub-agent tool_use events returns only `Read` (×17) + `Bash` (×17). Zero `record-fragment`. |
| No `complete-phase` calls (diagnosis-only) | **✓** Zero `complete-phase`. |
| Dispatched without `codebase=` scope | **✓** Dispatch input is `{"action":"build-subagent-prompt","slug":"nestjs-showcase","briefKind":"refinement2"}`. |
| Walked all 13 defect classes per audit_checklist.md | **✓** Sub-agent narrative covers: kb-ig-duplication (intra), kb-below-floor (counts), surface-misplacement (Vue Router), scaffold-code-in-kb (none found), aspirational-as-current (6 hits), yaml-comment-content-drift (per-service-type allowlist applied, all aliases valid), cross-codebase-named-constant-drift (queue group `'workers'` consistent), ig-cites-recipe-internal-file (none — vite.config.ts is repo-root, not src/), framework-quirk-as-gotcha (1 hit), scaffold-decision-as-gotcha (1 hit), cross-codebase-content-duplication (2 hits), missing-citation (6 hits). |
| Per-service-type alias allowlist applied | **✓** All `${<host>_<key>}` tokens validated against the table — `${db_dbName}` for postgres, `${storage_apiHost}` for object-storage, `${search_masterKey}`/`${search_defaultSearchKey}` for meilisearch, `${broker_connectionString}` for nats. |
| `suggestedAction` field always populated | **✓** All 17 findings carry one of: `reword-conditional`, `cross-reference-canonical-surface`, `drop`, `add-citation`. |
| fragmentId references `plan.fragments` canonical keys (SHORT form) | **✓** Zero `<host>dev`-form references; all 17 use short form. Substrate fix landed. |
| New defect-class decisive Check #1 reasoning applied | **✓** Sub-agent narrative for `framework-quirk-as-gotcha` explicitly walks Check #1 (Zerops-side material), Check #2 (different scaffold), Check #3 (documented elsewhere). Same for `scaffold-decision-as-gotcha` (Check #1 remove-bullet test). False-positive resistance held on NATS Authorization Violation (Zerops side material: broker rejects double-auth → kept as intersection) and queue-group at minContainers ≥ 2 (Check #1: porter making different choice WOULD hit duplicated state → kept). |

Net: 11/12 contract items honored cleanly; one soft violation (two
JSON blocks emitted instead of one).

---

## Known-substrate-issues confirmed still present

- **S-1** dev-server-restart-re-reads-env brief lie at
  [mount-vs-container.md:62-66](internal/recipe/content/principles/mount-vs-container.md#L62-L66) —
  deferred to live Zerops empirical test before brief edit lands. Not
  visibly bitten in run-42.
- **S-4** parent-recipe baseline filter not implemented — this run's
  TypeORM gotcha is appropriate (child recipe uses TypeORM). Not a
  regression.
- **Parent-recipe fetch by main agent at research phase** — main
  session shows `mcp__zerops__zerops_knowledge {"recipe":"nestjs-minimal"}`
  call once (call sequence between research close and provision
  enter). Per task brief, this is the known substrate bug at
  [phase_entry/research.md:84-87](internal/recipe/content/briefs/recipe_session_runtime/phase_entry/research.md#L84-L87) —
  main agent proactively fetches parent context even though only
  scaffold sub-agents need it. **Not a refinement-2 regression;
  documented for substrate cleanup; not scored.**

---

## ENG-1 plan.json finalize-snapshot regression

**Did NOT recur in run-42.** Spot-checked three refinement-edited
fragments against on-disk rendered READMEs:

- `codebase/worker/integration-guide/5` — plan.json fragment body is
  the cross-reference (5 lines pointing to api IG #3); workerdev/README.md
  line 211-215 matches verbatim. ✓
- `codebase/worker/knowledge-base` — plan.json fragment body ends with
  the symptom-led same-key-shadow bullet; workerdev/README.md
  line 239-243 matches verbatim. ✓
- `codebase/api/integration-guide/3` — plan.json fragment body ends
  with the `S3_REGION: us-east-1` literal-region rationale;
  apidev/README.md line 242 matches verbatim. ✓

**Why the run-41 regression didn't bite run-42**: the gating logic at
[handlers.go:800](internal/recipe/handlers.go#L800)
(`wrapRefinement := sess.Current == PhaseRefinement && in.Mode == modeReplace`)
is UNCHANGED — the substrate didn't widen the gate. Run-42's main
agent dispatched refinement-1 + refinement-2 DURING `PhaseFinalize`
(same as run-41), so `wrapRefinement` was false for each
record-fragment, and `persistPlanAfterRefinementReplace` was skipped.
Yet plan.json IS up-to-date.

The mechanism appears to be: the explicit `stitch-content` calls
(lines 43, 46 of call sequence) re-write plan.json as part of the
stitch step, AND the `complete-phase phase=finalize` call (line 47)
also persists plan state. So plan.json gets refreshed via the
finalize-close path, not via wrapRefinement.

**Substrate note for run-43**: the gating bug is still latent. If a
future run dispatches refinement during `PhaseRefinement` (post-
finalize-close) AND skips an explicit `stitch-content` AND skips a
second `complete-phase finalize`, the wrapRefinement persistence
would be the only path and the regression could resurface. The fix
recommended in run-41 (widen wrapRefinement to fire on PhaseFinalize
too) was NOT taken; instead the call-order happenstance dodged it.
**Lower-priority but worth tracking.**

---

## Recommended next action

**ITERATE-TO-43.** The substrate edits at e173d2fd close the
13-class audit cleanly but the deliverable still misses the spec's
classification taxonomy at the boundary the audit's enumeration
doesn't cover. Five substrate items needed before another dogfood,
ranked by content-quality impact.

### Run-43 status snapshot

Run-43 lands the substrate work in five commits on `main`:

| Priority | Status | Commit | Notes |
|---|---|---|---|
| P1 — synthesis_workflow.md voice + KB classification (author brief) | **DONE** | `0196fd12` | Drops the "see IG #N" deferral pattern; adds litmus #4 (self-inflicted test); moves X-Cache + storage_apiHost examples to DISCARD; rewrites the 8.5 anchor to teach per-deploy vs once-ever lifetimes correctly. |
| P2 — `self-inflicted-as-gotcha` defect class (refinement-2 audit) | **DONE** | `f8c9e490` | Decisive Check #1: porter-following-IG#1-verbatim test. Worked example (storage_apiHost UnknownError) + counter-example (NATS Pattern A/B). Blocker severity, action `drop`. |
| P3 — F-EXECONCE-SEMANTICS factuality guard (refinement-1) | **DONE** | `4c0dc93b` | Walks every `zsc execOnce` line; FAILs on `${appVersionId}` key + non-idempotent op + once-only prose. Cross-links `principles/init-commands-model.md`. |
| P4 — voice-vs-golden classifier (operational vs defensive KB voice) | **REMAINING** | — | Separate scope; requires goldens-embedding effort and content-quality rubric design. |
| P5 — F-XSURF-REF cross-surface-reference rule (refinement-1) | **DONE** | `5a364523` | Combines the validation report's §priority-4 cross-tier rule with the §"Headline (4)" Surface 7 cross-codebase-surface deferral catch. Scans both shapes; cites spec §"Surface 7" + §"Surface 3". |
| P6 — kb-floor post-ACT re-audit OR refinement-1 floor validator | **REMAINING** | — | Engine state-machine work at `handlers.go:1103/:1121`; deferred to separate scope. |
| P7 — URL-fragment validator at brief-composer time + form-(b) tightening | **DONE** | `65ef0f2f` | Citation URLs are now named constants (`citationURLEnvVarModel`, etc.); brief demands EXACT URL match for forms (b) + (c); run-42 workerdev/README.md:241 fabricated-URL-fragment gap is closed. |

P1+P2+P3+P5+P7 land the content-quality substrate work (the
spec-spirit gaps the run-42 audit couldn't see). P4 + P6 are
deliberately scoped out — P4 needs goldens-embedding work + a voice
rubric; P6 needs engine state-machine changes. Run-44 dogfood is
gated on P1-P3+P5+P7 only; P4 + P6 are tracked separately.

### Substrate priority 1 — self-inflicted classifier

**Spec citation**: [§Fact classification taxonomy → Self-inflicted](docs/spec-content-surfaces.md#fact-classification-taxonomy)
+ [§Self-inflicted (should have been discarded)](docs/spec-content-surfaces.md#self-inflicted-should-have-been-discarded-were-shipped-as-gotchas).
The litmus test: *"Could this observation be summarized as 'our code
did X, we fixed it to do Y'? If yes, discard."*

**Defects this misses in run-42**:
- apidev KB #2 `UnknownError on first GetObject` — recipe scaffold composed `http://${storage_apiHost}` first, hit 301, switched to `${storage_apiUrl}`. IG #1 now ships `S3_ENDPOINT: ${storage_apiUrl}` directly. A porter following the shipped yaml hits zero of this trap. Self-inflicted; should be DISCARD.
- apidev KB #3 `fetch().headers.get('X-Cache') returns null from the SPA` — recipe scaffolded without `exposedHeaders`, hit cross-origin header invisibility, added `exposedHeaders: ['X-Cache', 'X-Cache-Elapsed-Ms']`. The fix is generic CORS configuration; the Zerops side (cross-origin per-service subdomain) is thin. Self-inflicted (or framework-quirk under the Zerops-side-material test — depends on whether the audit considers "Zerops gives separate subdomains" as material). Either way: DISCARD per the spec.

**Why the audit missed**: `framework-quirk-as-gotcha`'s Check #1
"Zerops side material" passes both bullets shallowly (301 IS a Zerops
gateway behavior; subdomains-per-service IS a Zerops choice). Neither
rule asks the harder question: *"Would a porter following the shipped
yaml hit this with no deviation?"* That question is the self-inflicted
test. Add a `self-inflicted-as-gotcha` rule that asks: for each KB
bullet, does the bullet describe a state the porter would reach by
copying the shipped yaml verbatim? If not — porter has to deviate
from the shipped config to hit the trap — flag as self-inflicted.

**Implementation**: extend `audit_checklist.md` with the rule. Check
pattern: cross-reference the trap's `${<var>}` against IG #1's
shipped envVariables. If the trap fires when porter uses a different
env var than what IG #1 ships, → self-inflicted.

### Substrate priority 2 — execOnce-semantics validator

**Defect**: [apidev/zerops.yaml:41-51](docs/zcprecipator3/runs/42/apidev/zerops.yaml#L41-L51)
comment says *"stamps each key into a per-deploy ledger and skips it
if the key already ran"* but `${appVersionId}-seed` uses
`${appVersionId}` in the key → the key is different every deploy →
seed re-runs every deploy. Same shape on
[apidev:142-147](docs/zcprecipator3/runs/42/apidev/zerops.yaml#L142-L147)
(dev setup) and presumably on workerdev. The init.ts script is
idempotent ("skips seeding when items is non-empty" per
[apidev/CLAUDE.md](docs/zcprecipator3/runs/42/apidev/CLAUDE.md)) so
it's safe but the prose lies.

**The semantic confusion**: `${appVersionId}` in the key is the
correct shape for *migrations* (you want migrate to run once per new
code version, even across rolling replicas — appVersionId stamps the
per-deploy gate). It's the wrong shape for *seed* (you want seed to
run once-ever, with a stable key like `seed-v1`). The recipe applies
appVersionId to both because the agent didn't separate "per-deploy
gate" semantics from "once-ever-bootstrap" semantics.

**Implementation**: refinement-1 (or a new pre-finalize validator)
walks every `zsc execOnce` line in every zerops.yaml. If the key
contains `${appVersionId}` AND the comment claims "skips if key
already ran" / "once-only" / "never re-runs" semantics, flag —
either the key needs a stable prefix or the comment needs to
acknowledge per-deploy re-run.

### Substrate priority 3 — voice-vs-golden classifier

**Spec citation**: [§Empirical floor](docs/spec-content-surfaces.md#L9-L14)
— *"The empirical floor for every contract below is two reference
recipes: laravel-jetstream + laravel-showcase."*

**Defects**:
- KB across three codebases ships 13 gotcha bullets, all defensive
  trap-cataloging. Jetstream KB ships 2 operational bullets
  ([laravel-jetstream-app/README.md:253-283](file:///Users/fxck/www/laravel-jetstream-app/README.md#L253-L283))
  teaching `zsc health-check disable` and `zsc scale ram +0.5GB`.
- apidev/zerops.yaml comments contain meta-prose: `"The pattern is
  taught in IG #3; the specific shapes worth flagging at the field
  site live below."` (line 63-64) + `"See IG #5 for the schema-
  ownership rationale this pair enforces."` (line 47-48). Jetstream
  yaml never says "see section X" or "live below" — it just states
  the choice + reason.

**Why the audit missed**: the spec's §S5 surface contract says
"5-8 bullets per codebase" but the goldens ship 2 total. The
"5-8 per codebase" floor was invented somewhere in substrate
evolution; the empirical floor is "what the goldens ship". The audit
hit the invented floor by over-collecting; the goldens would have
flagged the over-collection.

**Implementation**: replace the §S5 numeric floor with a voice
classifier. Walk each KB bullet through:
(a) is the bullet's voice OPERATIONAL ("here's what you can do with
`zsc <cmd>` / Zerops behavior") or DEFENSIVE ("here's a trap we hit
when we tried X")? Both reference recipes ship operational; defensive
is a smell.
(b) does the trap describe a state the porter would reach by copying
the shipped yaml verbatim (legitimate intersection) or a state that
requires deviating from the shipped yaml (self-inflicted)? Per
priority 1 above.

### Substrate priority 4 — refinement-1 cross-tier-reference rule

**Defect**: tier 1
([environments/1 — Remote (CDE)/import.yaml:18-22, :41-44, :61-64](docs/zcprecipator3/runs/42/environments/1%20%E2%80%94%20Remote%20%28CDE%29/import.yaml#L18))
and tier 2 service-block comments lead with "Same dev / stage pair
as tier 0" framing. Spec §S3 explicitly forbids cross-tier
references: *"Cross-tier shifts surface implicitly through the
contrast between adjacent tier yamls — neither reference recipe
writes 'promote to tier N when…' sentences in service blocks. Don't."*

**Implementation**: add `cross-tier-reference` rule to refinement-1's
`derived_rules.md`. Pattern is *"Same as tier <N>"* or *"see tier <N>"*
in a service-block comment, action `drop the cross-reference framing;
let the contrast surface implicitly per spec §S3`.

### Substrate priority 5 — kb-floor post-ACT re-audit

**Defect**: appdev + workerdev ship KB at 4 bullets (spec floor 5).
Cause: refinement-2 ran pre-drop, main agent dropped defective
bullets, post-drop count crossed the floor. The engine has no
re-audit-after-ACT loop.

**Implementation options**:
- **(a) Light**: extend refinement-1's post-Replace validator at
  [handlers.go:817-833](internal/recipe/handlers.go#L817-L833) to
  include kb-floor count check — every drop that crosses the floor
  surfaces `kb-below-floor` on close.
- **(b) Heavy**: refinement-2 re-runs after main-agent ACTs until
  findings list stabilizes. Higher wall-time cost.

Recommend (a).

**Note**: priorities 1-3 are content-quality work (the goldens-anchored
spec spirit); priorities 4-5 are mechanical fix-ups. Run-43 should
land 1+2+3 minimum before another dogfood.

### Original "narrow substrate-tracking items" (now subordinate to the priorities above)

1. **Re-audit-after-ACT loop OR refinement-1 floor validator**.
   Dropping defective KB bullets crosses the floor (appdev + workerdev
   ship at 4 not 5). Two options:
   - **(a) Light**: extend refinement-1's post-Replace validator at
     [handlers.go:817-833](internal/recipe/handlers.go#L817-L833) to
     include the kb-floor count check — every drop that crosses the
     floor surfaces a `kb-below-floor` notice on close.
   - **(b) Heavy**: refinement-2 re-runs after each batch of
     main-agent ACTs until the findings list stabilizes. Higher
     wall-time cost; better-quality outcome.
   Recommend (a).

2. **Refinement-1 cross-tier-reference rule**. Tier 1/2 import.yaml
   preambles use "Same as tier 0" framing that the spec §S3 disallows.
   Add a `cross-tier-reference` rule to refinement-1's
   `derived_rules.md` — pattern `Same as tier <N>` or `see tier <N>`
   in a service-block comment, action `drop the cross-reference
   framing per spec §S3`. Low-risk; the comments carry standalone
   per-tier rationale already.

3. **Notice wording tighten (optional)**. If per-finding-record in
   transcript is the actual contract goal (codex's reading), the
   Notice text at
   [handlers.go:626](internal/recipe/handlers.go#L626)
   should say *"record a separate ACT/HOLD/ACCEPT line per finding in
   your transcript before closing"*. Current text emphasizes the
   per-finding ACTION (which run-42 honored), not the per-finding
   TRANSCRIPT RECORD (which run-42 condensed to a single line + 11
   record-fragment actions). The contract's documented failure mode
   is bulk-HOLD (silent dismissal); the substantive outcome of
   bulk-ACT is the contract's intended state. Mild substrate
   wording-clarity question, not a behavior bug.

4. **wrapRefinement gating note (tracking-only)**. The handlers.go:800
   gate is still narrow (`PhaseRefinement` only). Run-42 dodged ENG-1
   via stitch + complete-phase re-write. Track for future call-order
   changes; no edit needed now.

5. **Worker KB same-key-shadow bullet review (marginal)**. Codex
   flagged worker KB
   [workerdev/README.md:239-243](docs/zcprecipator3/runs/42/workerdev/README.md#L239-L243)
   as a cross-codebase-content-duplication of api IG #3's same-key
   teaching. The audit treated it as intra-codebase symptom-led KB
   (kb-ig-duplication pass condition). Borderline — either pass or
   fail is defensible. If the rule's "substantially the same depth +
   body" test wants to disqualify symptom-enumeration-only differences,
   the brief language at
   [audit_checklist.md:499-504](internal/recipe/content/briefs/refinement2/audit_checklist.md#L499-L504)
   could clarify.

---

## Recipe-quality sidebar — what got better between run-41 and run-42

Three structural improvements worth calling out, attributable to the
substrate edits (not authoring luck):

- **17 findings emitted, 17 ACTed (vs run-41's 10 emitted, 0 ACTed)**.
  The dispatch Notice at
  [handlers.go:626](internal/recipe/handlers.go#L626) plus the per-
  finding triage contract section in the sub-agent brief
  ([phase_entry.md:46-83](internal/recipe/content/briefs/refinement2/phase_entry.md#L46-L83))
  flipped the main agent's behavior. The
  [TIMELINE.md:103-138](docs/zcprecipator3/runs/42/TIMELINE.md#L103-L138)
  narrative records the per-fragment edits in detail, demonstrating
  the agent treated each finding as a discrete decision.
- **Three new defect-class rules ALL caught real defects on first
  dogfood**. The recipe's authoring path produced framework-quirk,
  scaffold-decision, and cross-codebase-duplication defects — all
  caught and fixed. The rules' decisive Check #1 framing prevented
  false positives (NATS Authorization Violation correctly kept as
  intersection; queue-group at minContainers ≥ 2 correctly kept as
  intersection).
- **Widened citation map fired on the right bullets**. The three
  newly-required topics (`deploy-files`, `env-var-model`,
  `http-support`/`l7-balancer`) all surfaced as missing-citation
  findings on bullets whose primary teaching covers them. Form-(a)
  tightening didn't have a regression test case this run (no bare-
  backtick token mentions to flag), but the negative space — no
  false positives on bullets that legitimately cite via form (b)
  markdown links — held.

These are substrate-attributable wins. Refinement-2's 17-findings-→-
17-ACTs throughput on first dogfood is the contract working as
designed.
