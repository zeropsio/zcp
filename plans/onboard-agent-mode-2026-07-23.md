# Plan: onboard-agent-mode

## Run State
- `phase:` build
- `base:` d0be678734bb0a9c24d2cb34d2965d2c93f5169c
- `integration:` feat/onboard-agent-mode (to be cut from base; SHA recorded at first landing)
- `approved:` Rev-1, 2026-07-23 — owner approved GATE 1 (register + spec promotion + BUILD start)
- `codex:` APPROVE (verify re-run /tmp/codex-out-1784828280-13267-30851.md; initial RESHAPE /tmp/codex-out-1784827523-6148-30934.md — 6 findings incorporated)
- `next:` OWNER GATE 1 — on approval: write docs/spec-onboarding.md (Promotion §s), set phase: build, start W1 (S1+S2+S6)
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: A user typing "onboard me to Zerops" (or clear meta-onboarding intent) to any ZCP-bound coding agent gets a short state-aware welcome and a three-way fork (Bring an app / Start something new / Take a quick tour, + freeform escape), each branch funneling into EXISTING ZCP workflows — shipped content-only: one user-only AGENTS.md trigger block + one direct-fetch-only playbook document; no new tools, no state, no GUI change.

| obs | evidence |
|---|---|
| BuildAgentsMD composes `# Zerops` → env preamble → agents_shared; authoring/guided blocks append AFTER shared; onboarding must splice INTO the `body :=` concat between preamble and shared | [SELF-VERIFIED via explore-agents-seam: internal/content/build_agents.go:45-90, esp. 66-68] |
| No new parameter needed: `rt.Authoring` (env `ZCP_AUTHORING`) is available in both init and serve-refresh paths; `RefreshAgentContext` picks the block up automatically | [internal/runtime/runtime.go:31; internal/server/server.go:115; internal/init/init.go:132-146; internal/content/refresh_agents.go:52-57] |
| CLAUDE.md is a pointer (`@AGENTS.md`), never a copy — block lives only in AGENTS.md | [internal/content/build_agents.go:105-107] |
| New knowledge family = exactly two edits (go:embed + knowledgeDirs); URI derives mechanically; embed pattern must match ≥1 committed file or build fails | [internal/knowledge/documents.go:10,14,178-181; adversarial R5] |
| Search exclusion is one line at the single all-docs iteration; Store.Search has exactly one production consumer; WITHOUT the exclusion a playbook auto-enters search and threatens TestSearch_GuideSpecificQueries (exact-top-result pins) | [internal/knowledge/engine.go:118; internal/tools/knowledge.go:176; internal/knowledge/engine_doc_test.go:208-245] |
| uri= fetch falls through to Store.Get (only `zerops://atoms/` special-cased) — zero tools-layer change; fetch/search asymmetry is native | [internal/tools/knowledge.go:210-221; internal/knowledge/engine.go:162-168] |
| internal/tools has NO TestMain → free of corpus guard; a committed playbook is embedded even unsynced, so the fetch test lives in internal/tools without skips | [internal/knowledge/corpus_guard_main_test.go:17-23; explore-knowledge-seam §3] |
| Committed playbooks/*.md is not gitignored, not swept by sync push, not a pull target; sync performs no directory wipes | [.gitignore:51-59; internal/sync/push_guides.go:178; internal/sync/pull_guides.go:75-79] |
| adoptionState is a 6-value enum: adopted/resumable/adoptable/managed-dep/zcp-self/bootstrapping; classifier precedence keys zcp-self off `Type=="zcp@1"` | [internal/ops/discover.go:32-82; internal/tools/discover.go:287-316] |
| Asymmetry: `action="status"` EXCLUDES the control-plane (SelfService) → fresh container renders `Phase: idle` + `Services: none`; `zerops_discover` SHOWS it as zcp-self | [internal/workflow/compute_envelope.go:78-81,189-191; internal/workflow/render.go:332-341; internal/tools/discover.go:288] |
| status is ES-backed and can lag (ListServices→PostServiceStackSearch); discover is direct (ListServicesDirect→GET) — discover is the authoritative fresh/populated classifier | [internal/workflow/compute_envelope.go:56 → internal/platform/zerops.go:258; internal/ops/discover.go:164 → internal/platform/zerops_direct.go:24-30; adversarial R6] |
| First bootstrap call (no route) is read-only, opens NO session; recipe confirm-before-provision is spec invariant RCO-5 — the consent stance follows the existing contract | [internal/content/atoms/bootstrap-route-options.md:9-13; docs/spec-workflows.md:285-343,1174] |
| `zerops://themes/model` covers all three newcomer concepts (hierarchy, private network/hostname, build→deploy→run); its pricing table + YAML field mechanics are wrong altitude and must be excluded from narration | [internal/knowledge/themes/model.md:7-15,37-59,86-94 vs 17-26,74-150] |
| Playbook next-step narration must not fight the typed Plan: planIdle already suggests bootstrap on empty / develop on bootstrapped / adopt on unmanaged | [internal/workflow/build_plan.go:204-222,381-388] |
| URI lint scans a fixed dir list that EXCLUDES templates/ today; extending it is one slice-append and no existing template fails it | [internal/content/agent_facing_uri_lint_test.go:20,28-34,93-99; adversarial R3] |
| Ordering landmine: TestBuildAgentsMD_DevelopFirst pins first-occurrence ``- `develop` `` before ``- `bootstrap` `` over the FULL output — the onboarding block (ahead of shared) must not contain a ``- `bootstrap` `` bullet | [internal/content/build_agents_test.go:229-233; internal/content/templates/agents_shared.md:33-34; adversarial R1] |
| Onboarding template inherits the forbidden-string union: env-leak negatives (`/var/www/`, `SSHFS`, `Developer machine`, `zcli vpn up`, `generate-dotenv`, `$db_connectionString`, `{{.SelfHostname}}`), authoring negatives (`zerops_recipe`, `zerops_port`, authoring header), guided marker `## Guided mode (user-only)` | [internal/content/build_agents_test.go NoLocalLeak/NoContainerLeak/:259-265/:293; adversarial R1] |
| No golden/byte-exact pin on AGENTS.md output anywhere (all Contains-based); no other consumer of BuildAgentsMD/GetTemplate; no test asserts every store doc searchable | [adversarial R1/R2/R4] |
| Flow-eval scenarios require front-matter keys id/description/seed/tags/area/retrospective with id==filename | [internal/content/eval_scenario_manifest_test.go:37-44,129-136] |
| Stale flag (repo hygiene, in-scope fix candidate): ops/discover.go:32-35 doc-comment says "five buckets", enum has 6 | [internal/ops/discover.go:14,32-35,75-82] |

- AC1: The exact phrase (case/punct-insensitive) and clear meta-onboarding variants trigger the onboarding conversation; tech-specific "get started with X" requests do NOT — planned evidence: content pins in the new trigger-block test + flow-eval trigger scenarios (positive, variant, negative control).
- AC2: The trigger block renders in AGENTS.md for user inits in BOTH local and container contexts, ordered BEFORE `## Route every user turn`, absent under authoring, present with guided both on and off, and survives `zcp serve` refresh — planned evidence: `TestBuildAgentsMD_Onboarding*` (gate + ordering, modeled on _DevelopFirst/_AuthoringGate) + a `TestRefreshAgentContext_*` assertion.
- AC3: `zerops_knowledge uri="zerops://playbooks/onboarding"` returns the playbook body in an UNSYNCED checkout, and the playbook never appears in `query=` hits — planned evidence: internal/tools embedded-fetch test + `TestSearch_ExcludesPlaybooks` + existing `TestSearch_GuideSpecificQueries` staying green post-sync.
- AC4: The playbook's state resolution is status-first (active work → lead with it) then discover-classified (FRESH iff every non-system row is `zcp-self`, no live activity/warnings) — planned evidence: PROVE probes P1/P2 in the Evidence Ledger + content pins on the fresh-rule wording.
- AC5: No mutation auto-runs from the bare phrase; branches lean only on existing primitives (bootstrap route-menu + RCO-5 recipe confirm, develop, `zerops://themes/model` fetch for the tour) — planned evidence: content pins (no mutating tool-call directive before user choice) + flow-eval no-mutation scenario.
- AC6: The full existing battery stays green (DevelopFirst ordering, env-leak/authoring/guided negatives, URI lint extended over templates/, knowledge search pins) — planned evidence: ASSEMBLE battery run.

**Non-goals**: any welcome-CTA/extension change (no `vscode-bootstrap-*` edit, no BootstrapExtVersion bump); new MCP tools or workflow actions; onboarding state files/flags; a skill subtree; data/DNS migration playbooks; BYO-GitHub import machinery; auto-provisioning; Strapi-synced content. · **Constraints**: content-only (guided G5 precedent); template inherits the forbidden-string union + DevelopFirst ordering landmine; tools-only `zerops://` form everywhere; no hardcoded platform facts/@version; playbook is direct-fetch-only (search-excluded); playbook copy must not contradict planIdle's typed suggestions.

**Risk class**: medium — content-only and reversible, but it ships routing behavior into EVERY user session's AGENTS.md and rests on live platform-shape assumptions. FULL triggers: load-bearing platform-API assumption (fresh/populated discover shape) + owner asked.

**Design decision (content home)**: new committed direct-fetch-only knowledge family `internal/knowledge/playbooks/` (URI `zerops://playbooks/onboarding`, excluded from `Store.Search` at the single all-docs iteration). Rejected: Strapi-synced `guides/` (shared ownership, push sweeps it upstream, untestable unsynced), `themes/` (semantic misfit — platform references), inline-in-AGENTS.md (permanent context tax), new workflow action (violates the content-only G5 precedent).
**Executor decision**: BUILD slices run primarily via Codex (owner instruction) in per-slice git worktrees driven by codex-helper `--cd`; the flow's RED-replay + merge acceptance machinery is unchanged. A slice failing RED replay twice re-briefs to the flow-default Sonnet subagent.

**Assumptions**:
- [VERIFIED] Mechanism set — every row of the obs table above (cites inline).
- [VERIFIED] P1: `zerops_discover` classifies the control-plane row `zcp-self` (type `zcp@1`); `action="status"` excludes it from Services — Evidence Ledger row 1 (live, 2026-07-23).
- [VERIFIED] P2: Non-control-plane services classify populated (adoptable + warning); idle rows carry no activity — Evidence Ledger row 2 (live, 2026-07-23).
- [LOGICAL] The pure-fresh composite (project with ONLY the control-plane ⇒ discover = one zcp-self row, status = `Services: none`) follows from P1 + the initialized-empty-slice code path [internal/ops/discover.go:220-230].
- [ASSUMED] The model recognizes trigger variants sensibly — content-only softness, not deterministically probe-able; covered by flow-eval scenarios later (AC1), not by PROVE.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| Control-plane row classifies `zcp-self` and status excludes it from Services | AC4 / P1 | mcp | `zerops_discover` + `zerops_workflow action="status"` on live project localflow | discover: `{"hostname":"zcp","type":"zcp@1","adoptionState":"zcp-self"}`; status: `Services: app, febridge, localflow` (no `zcp`) | CONFIRMED | playbook content pin (fresh-rule wording) + existing `classifyAdoptionState` tests |
| Non-control-plane services carry populated-classifying adoptionStates + surfaced warnings; activity absent when idle | AC4 / P2 | mcp | same calls | febridge/app: `"adoptionState":"adoptable"`, adopt warning present verbatim; no `activity` field on any idle row | CONFIRMED | playbook content pin |
| status's ES-backed Services list is non-authoritative — live run showed a spurious `localflow ()` row (empty type, project-named) absent from direct discover | AC4 (reinforces discover-as-classifier) | mcp | same `action="status"` call | `Services: app, febridge, localflow` + `- localflow ()` line; discover (direct) shows only febridge/app/zcp | CONFIRMED (side-observation) | playbook must never classify from status's Services line; separate repo-hygiene note for owner |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Tracer: playbooks family plumbing + stub doc + fetch/search-exclusion tests | — | internal/knowledge/documents.go · internal/knowledge/engine.go · internal/knowledge/playbooks/onboarding.md (stub) · internal/knowledge/engine_playbooks_test.go · internal/tools/knowledge_playbooks_test.go | unit, tool | autonomous | pending |
| S2 | AGENTS.md onboarding trigger block (splice before shared, user-only) | — | internal/content/templates/agents_onboarding.md · internal/content/build_agents.go · internal/content/build_agents_test.go · internal/content/refresh_agents_test.go | unit | autonomous | pending |
| S3 | Playbook content v1 + content pins + lint extensions (URI lint over templates/+playbooks/, no-hardcoded-version) | S1, S2 | internal/knowledge/playbooks/onboarding.md · internal/tools/knowledge_playbook_content_test.go · internal/content/agent_facing_uri_lint_test.go · internal/content/templates_lint_test.go | tool, unit | autonomous | pending |
| S5 | Flow-eval scenario pack (trigger positive/variant/negative, populated, guided-on) + existence/parse test | S3 | eval/behavioral/scenarios/onboard-*.md · eval scenario preseed script for guided-on · internal/eval/onboarding_scenarios_test.go | unit (eval) + manifest lint | autonomous | pending |
| S6 | Hygiene rider: ops/discover.go BOTH stale enum comments (header :14-20 five-state list missing bootstrapping; const-block :32-35 "five buckets") | — | internal/ops/discover.go (comments only) | unit (existing) | autonomous | pending |
Waves: W1 = S1+S2+S6 (disjoint) · W2 = S3 · W3 = S5. (S4 merged into S3 per SHAPE-gate finding 1 — lint extensions carry no standalone replayable RED; the playbook pin test is the slice's RED.) S6 is an explicit no-RED comment-only exception (pure-refactor rule).

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `go test ./internal/content -run 'TestBuildAgentsMD_OnboardingTriggerCopy' -short -count=1` (pins: exact phrase, variant examples, negative rule) + S5 scenario files pass manifest lint + `go test ./internal/eval -run TestOnboardingScenarios -short -count=1` | not-run | — |
| AC1 | live behavioral proof via flow-eval run of the S5 scenarios | not-run (follow-up after LAND — eval runs are a separate billed act) | — |
| AC2 | `go test ./internal/content -run 'TestBuildAgentsMD_Onboarding\|TestRefreshAgentContext_Onboarding' -short -count=1` (gate both envs, absent under authoring, present guided on+off, ordered before `## Route every user turn`) | not-run | — |
| AC3 | `go test ./internal/tools -run 'TestKnowledgeTool_Playbook' -short -count=1` (fetch in unsynced checkout) + `go test ./internal/knowledge -run 'TestSearch_ExcludesPlaybooks' -short -count=1` (synced env) | not-run | — |
| AC4 | Evidence Ledger rows 1-2 (live CONFIRMED) + playbook content pin on the fresh-rule wording (classify from discover only) | partially-passed (ledger) / not-run (pin) | ledger rows 1-3 |
| AC5 | playbook content pin: no mutating tool directive before user choice; RCO-5 confirm directive present + S5 negative scenario | not-run | — |
| AC6 | `go test ./... -short -count=1` + `make lint-local` on integration head (incl. DevelopFirst, env-leak negatives, guided gates, TestSearch_GuideSpecificQueries in synced CI) | not-run | — |
| — | negative/regression: authoring init carries NO onboarding block (TestBuildAgentsMD_Onboarding gate case) | not-run | — |
| — | negative/regression: playbook absent from `query=` search hits (TestSearch_ExcludesPlaybooks) | not-run | — |

## Promotion
- Contracts → NEW `docs/spec-onboarding.md`: §1 trigger contract (phrase, variants, negative rule, block position + user-only gates) · §2 state resolution (status-first for active work; discover-only classification; fresh rule = every non-system row `zcp-self`, no activity/warnings) · §3 conversation contract (opening copy fresh/populated, fork wording, escape hatch, consent-before-provision) · §4 branch playbooks (BRING triage lanes, START demo/idea + RCO-5, TOUR = themes/model fetch + three concepts, guided handoff) · §5 content home (playbooks family, direct-fetch-only, tools-only URI form) · §6 authoring/guided interplay · §7 invariants table.
- Invariants → tests: `TestBuildAgentsMD_OnboardingGate_UserOnly`, `TestBuildAgentsMD_OnboardingFirst_BeforeRouting`, `TestRefreshAgentContext_OnboardingPreserved`, `TestSearch_ExcludesPlaybooks_NoHits`, `TestKnowledgeTool_PlaybookURI_FetchesEmbedded`, `TestPlaybookOnboarding_ContentPins_CoreContract`, extended `TestNoBareZeropsURIInAgentContent` + new `TestTemplatesContent_NoHardcodedVersions`.
- CLAUDE.md trap line (≤1): none — the spec sections + tests cover every seam; no cross-cutting new-call-site trap identified.
- This plan → `plans/archive/` on LAND close. plans/onboard-mode-design-2026-07-23.md (pre-flow design doc) archived alongside; its welcome-CTA sections stay backlog (out of scope this run).
