# runs/v37 verification checklist

**Machine report SHA**: `10003228c87797f577cc7f2364d9bbb1996ed7d48e1fdf60bb8c6273de49919d`
**Generated at**: 2026-04-21T21:56:12Z
**Tier**: showcase
**Slug**: nestjs-showcase
**Deliverable**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v37`
**Analyst**: <analyst-fill>
**Analyst session start (UTC)**: <analyst-fill>

## Phase reached

- [ ] `close` complete (auto)
- [x] editorial-review dispatched (auto)
- [x] code-review dispatched (auto)
- [ ] close-browser-walk attempted (auto)

If `close` is not complete, downstream cells must be `unmeasurable-valid` with explicit justification. If `close` IS complete, no downstream cell may be `unmeasurable`.

## Structural integrity bars (auto)

- [x] B-15 ghost_env_dirs: threshold 0, observed 0, **status pass**
- [x] B-16 tarball_per_codebase_md: threshold 6, observed 6, **status pass**
- [x] B-17 marker_exact_form: threshold 0, observed 0, **status pass**
- [x] B-18 standalone_duplicate_files: threshold 0, observed 6, **status fail** — evidence: apidev/GOTCHAS.md, apidev/INTEGRATION-GUIDE.md, appdev/GOTCHAS.md, appdev/INTEGRATION-GUIDE.md, workerdev/GOTCHAS.md, workerdev/INTEGRATION-GUIDE.md, session-log Write authorship: /var/www/apidev/GOTCHAS.md, session-log Write authorship: /var/www/apidev/INTEGRATION-GUIDE.md, session-log Write authorship: /var/www/appdev/GOTCHAS.md, session-log Write authorship: /var/www/appdev/INTEGRATION-GUIDE.md, session-log Write authorship: /var/www/workerdev/GOTCHAS.md, session-log Write authorship: /var/www/workerdev/INTEGRATION-GUIDE.md
- [x] B-22 atom_template_vars_bound: threshold 0, observed 0, **status pass**

## Session-metric bars (auto)

- [x] B-20 deploy_readmes_retry_rounds: threshold 2, observed 5, **status fail** — evidence: , , , , 
- [x] B-21 sessionless_export_attempts: threshold 0, observed 2, **status fail** — evidence: zcp sync recipe export /var/www/zcprecipator/nestjs-showcase --app-dir /var/www/apidev --app-dir /var/www/workerdev --app-dir /var/www/appdev --include-timeline 2>&1 | tail -30, zcp sync recipe export /var/www/zcprecipator/nestjs-showcase --app-dir /var/www/apidev --app-dir /var/www/workerdev --app-dir /var/www/appdev --include-timeline 2>&1 | tail -30
- [x] B-23 writer_first_pass_failures: threshold 3, observed 0, **status skip**
- [x] B-24 dispatch_integrity: threshold 0, observed 0, **status **
- sub_agent_count: 13

## Dispatch integrity (analyst-fill for diff_status)

Byte-diff each captured Agent dispatch prompt against `BuildXxxDispatchBrief(plan)` output. Status `clean` or `divergent`. Divergent dispatches must list root cause.

- **Apply editorial CRIT+WRONG fixes**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Author recipe READMEs + CLAUDE.md + manifest**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Close-step static review**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Code-review: NestJS framework expert**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Editorial review of recipe content**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Editorial review: return JSON payload**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Feature sub-agent: implement 5 showcase features**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Final README fix round**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Fix README fragment format + YAML comments**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Re-dispatch editorial review**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold apidev NestJS API**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold appdev Svelte SPA**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold workerdev NestJS worker**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>

## Writer content quality (analyst-fill, required)

### apidev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=6 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=6: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### appdev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=4 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=5: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### workerdev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=4 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=6: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### apidev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4953 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### appdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 3094 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### workerdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4318 bytes)
- [x] base sections present: [Dev Loop Migrations Container Traps Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 2)
- auto **status pass**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

## Retry-cycle attribution (analyst-fill)

| Cycle | Timestamp | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | 2026-04-21T20:47:46.487Z | deploy/ | fragment_integration-guide, fragment_knowledge-base, fragment_intro, fragment... | <analyst-fill> |
| 2 | 2026-04-21T20:49:36.176Z | deploy/ | comment_ratio, fragment_integration-guide_heading_level, fragment_knowledge-b... | <analyst-fill> |
| 3 | 2026-04-21T20:55:47.424Z | deploy/ | app_knowledge_base_authenticity, api_knowledge_base_authenticity, worker_work... | <analyst-fill> |
| 4 | 2026-04-21T20:59:05.039Z | deploy/ | app_gotcha_distinct_from_guide, cross_readme_gotcha_uniqueness | <analyst-fill> |
| 5 | 2026-04-21T20:59:42.755Z | deploy/ | worker_gotcha_distinct_from_guide | <analyst-fill> |
| 6 | 2026-04-21T21:01:39.326Z | finalize/ | 0 — AI Agent_import_factual_claims, 1 — Remote (CDE)_import_comment_ratio... | <analyst-fill> |
| 7 | 2026-04-21T21:05:01.355Z | finalize/ | 3 — Stage_import_comment_ratio, 4 — Small Production_import_comment_ratio... | <analyst-fill> |

## Final verification

- [ ] All cells are non-`pending`
- [ ] Every Read-receipt timestamp is after analyst session start
- [ ] No `unmeasurable-invalid` cells
- [ ] Machine-report SHA matches file content
- [ ] Checklist SHA matches file content

**Analyst sign-off**: <analyst-fill name, timestamp>
