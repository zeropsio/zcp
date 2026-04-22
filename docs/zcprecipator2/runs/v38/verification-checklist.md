# runs/v38 verification checklist

**Machine report SHA**: `d756a9726cdd4f03b11994b745af47f3154cd47f6943cc0f20a6d681909bcca2`
**Generated at**: 2026-04-22T12:46:19Z
**Tier**: showcase
**Slug**: nestjs-showcase
**Deliverable**: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38`
**Analyst**: <analyst-fill>
**Analyst session start (UTC)**: <analyst-fill>

## Phase reached

- [x] `close` complete (auto)
- [x] editorial-review dispatched (auto)
- [x] code-review dispatched (auto)
- [ ] close-browser-walk attempted (auto)

If `close` is not complete, downstream cells must be `unmeasurable-valid` with explicit justification. If `close` IS complete, no downstream cell may be `unmeasurable`.

## Structural integrity bars (auto)

- [x] B-15 ghost_env_dirs: threshold 0, observed 0, **status pass**
- [x] B-16 tarball_per_codebase_md: threshold 6, observed 6, **status pass**
- [x] B-17 marker_exact_form: threshold 0, observed 0, **status pass**
- [x] B-18 standalone_duplicate_files: threshold 0, observed 0, **status pass**
- [x] B-22 atom_template_vars_bound: threshold 0, observed 0, **status pass**

## Session-metric bars (auto)

- [x] B-20 deploy_readmes_retry_rounds: threshold 2, observed 2, **status pass** — evidence: , 
- [x] B-21 sessionless_export_attempts: threshold 0, observed 0, **status pass**
- [x] B-23 writer_first_pass_failures: threshold 3, observed 9, **status fail** — evidence: api_comment_specificity, comment_ratio, fragment_integration-guide_blank_after_marker, fragment_intro_blank_after_marker, fragment_knowledge-base_blank_after_marker, intro_length, worker_drain_code_block, writer_manifest_honesty_any_as_intro, writer_manifest_honesty_claude_md_as_gotcha
- [x] B-24 dispatch_integrity: threshold 0, observed 0, **status **
- sub_agent_count: 7

## Dispatch integrity (analyst-fill for diff_status)

Byte-diff each captured Agent dispatch prompt against `BuildXxxDispatchBrief(plan)` output. Status `clean` or `divergent`. Divergent dispatches must list root cause.

- **Author recipe READMEs + CLAUDE.md + manifest**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Build showcase feature sections end-to-end**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Code review of recipe scaffold + features**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Editorial review of recipe reader-facing content**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold NestJS API codebase**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold NestJS worker codebase**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>
- **Scaffold Svelte+Vite frontend**:
  - [ ] dispatch_vs_source_diff: Status: <analyst-fill>
  - auto diff_status: unverified
  - [ ] Read-receipt: <analyst-fill>

## Writer content quality (analyst-fill, required)

### apidev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=5 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=3: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### appdev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=4 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=4: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### workerdev/README.md

- **intro fragment** — auto markers_present=true exact_form=true h3=0 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **integration-guide fragment** — auto markers_present=true exact_form=true h3=3 bullets=0: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- **knowledge-base fragment** — auto markers_present=true exact_form=true h3=0 bullets=3: **status pass**
  - [ ] Analyst qualitative grade: <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### apidev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 5439 bytes)
- [x] base sections present: [Migrations Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### appdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 3893 bytes)
- [x] base sections present: [Migrations Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

### workerdev/CLAUDE.md

- [x] size ≥ 1200 bytes: true (observed 4793 bytes)
- [x] base sections present: [Migrations Testing] (want 4)
- [x] custom sections ≥ 2: true (observed 4)
- auto **status fail**
- [ ] Analyst narrative sign-off (≤ 2 sentences): <analyst-fill>
- [ ] Read-receipt: <analyst-fill timestamp>

## Retry-cycle attribution (analyst-fill)

| Cycle | Timestamp | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | 2026-04-22T09:01:41.520Z | research/ | <none> | <analyst-fill> |
| 2 | 2026-04-22T09:04:29.748Z | provision/ | <none> | <analyst-fill> |
| 3 | 2026-04-22T09:25:07.757Z | deploy/deploy-dev | <none> | <analyst-fill> |
| 4 | 2026-04-22T09:26:14.395Z | deploy/start-processes | <none> | <analyst-fill> |
| 5 | 2026-04-22T09:26:43.998Z | deploy/verify-dev | <none> | <analyst-fill> |
| 6 | 2026-04-22T09:30:00.358Z | deploy/init-commands | <none> | <analyst-fill> |
| 7 | 2026-04-22T09:46:30.896Z | deploy/subagent | <none> | <analyst-fill> |
| 8 | 2026-04-22T09:49:22.081Z | deploy/snapshot-dev | <none> | <analyst-fill> |
| 9 | 2026-04-22T09:50:07.656Z | deploy/feature-sweep-dev | <none> | <analyst-fill> |
| 10 | 2026-04-22T10:05:25.807Z | deploy/browser-walk | <none> | <analyst-fill> |
| 11 | 2026-04-22T10:11:38.574Z | deploy/cross-deploy | <none> | <analyst-fill> |
| 12 | 2026-04-22T10:11:47.993Z | deploy/verify-stage | <none> | <analyst-fill> |
| 13 | 2026-04-22T10:12:40.599Z | deploy/feature-sweep-stage | <none> | <analyst-fill> |
| 14 | 2026-04-22T10:29:36.553Z | deploy/readmes | <none> | <analyst-fill> |
| 15 | 2026-04-22T10:29:49.931Z | deploy/ | comment_ratio, fragment_integration-guide_blank_after_marker, fragment_knowle... | <analyst-fill> |
| 16 | 2026-04-22T10:35:26.633Z | deploy/ | api_comment_specificity, comment_ratio, worker_drain_code_block | <analyst-fill> |
| 17 | 2026-04-22T10:40:07.566Z | finalize/ | 0 — AI Agent_import_factual_claims, 1 — Remote (CDE)_import_factual_claim... | <analyst-fill> |
| 18 | 2026-04-22T11:00:36.178Z | close/editorial-review | <none> | <analyst-fill> |
| 19 | 2026-04-22T11:01:13.340Z | close/code-review | <none> | <analyst-fill> |
| 20 | 2026-04-22T11:03:10.970Z | close/close-browser-walk | <none> | <analyst-fill> |
| 21 | 2026-04-22T11:03:27.248Z | close/ | <none> | <analyst-fill> |

## Final verification

- [ ] All cells are non-`pending`
- [ ] Every Read-receipt timestamp is after analyst session start
- [ ] No `unmeasurable-invalid` cells
- [ ] Machine-report SHA matches file content
- [ ] Checklist SHA matches file content

**Analyst sign-off**: <analyst-fill name, timestamp>
