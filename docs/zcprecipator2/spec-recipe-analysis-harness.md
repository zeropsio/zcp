# spec-recipe-analysis-harness.md — mechanical analysis layer for zcprecipator2 runs

**Purpose**: structurally prevent the v36 analysis failure mode by moving every mechanical measurement into a CLI tool whose output binds the analyst's verdict. Subjective judgment remains (content quality grades) but layers on top of an evidence-enforced mechanical layer.

**Target**: the v37 commission uses this harness from Phase 4 of [`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md). Built in Phase 1 of the same handoff.

**Design principle**: every claim in a verdict must survive `grep` or `jq`. The harness enforces that claims are backed by re-runnable measurements. Subjective judgments get one explicit layer with Read-receipt enforcement.

---

## 1. Architecture — three tiers

### Tier 1: `zcp analyze recipe-run` (mechanical layer)

CLI command that takes a deliverable tree + session logs directory and produces a JSON report covering every measurable bar.

```
zcp analyze recipe-run <deliverable-dir> <sessions-logs-dir> [--tier showcase|minimal] [--out machine-report.json]
```

Output: `machine-report.json` with fixed schema (below). Every bar is computed by a deterministic measurement; re-runs produce byte-identical reports (modulo `generated_at` timestamp).

### Tier 2: `zcp analyze generate-checklist` (worksheet layer)

CLI command that takes a machine-report and emits a Markdown worksheet with rows for every bar + content file. Structural rows pre-filled from the machine-report; content-quality rows left blank for the analyst.

```
zcp analyze generate-checklist <machine-report.json> [--out verification-checklist.md]
```

Output: `verification-checklist.md` — every row has PASS/FAIL status (auto or analyst-filled) + evidence pointer + Read-receipt timestamp.

### Tier 3: commit hook (enforcement layer)

`.githooks/verify-verdict` — pre-commit hook that refuses to commit `runs/vN/verdict.md` unless:
- `runs/vN/machine-report.json` exists and its SHA matches the value in verdict.md front-matter.
- `runs/vN/verification-checklist.md` exists, SHA matches, and has zero `pending` cells.
- Every PASS/FAIL claim in verdict.md regex-matches a `[checklist X-Y]` or `[machine-report.<key>]` citation within 50 chars.
- No self-congratulatory language ("success" / "works" / "clean" / "PROCEED") without citation.

---

## 2. Machine-report schema

```json
{
  "run": "v37",
  "generated_at": "2026-04-XX HH:MM:SS UTC",
  "generator_version": "v8.109.0",
  "tier": "showcase",
  "slug": "nestjs-showcase",
  "deliverable_dir": "<absolute path>",
  "sessions_logs_dir": "<absolute path>",

  "structural_integrity": {
    "B-15_ghost_env_dirs": {
      "description": "Non-canonical directories under environments/",
      "measurement": "find {deliverable}/environments -maxdepth 1 -type d | sed 's|.*/||' | grep -vE '^([0-5] — .+|)$'",
      "threshold": 0,
      "observed": 0,
      "status": "pass",
      "evidence_raw": ["<raw command output>"],
      "evidence_files": []
    },
    "B-16_tarball_per_codebase_md": { ... },
    "B-17_marker_exact_form": { ... },
    "B-18_standalone_duplicate_files": { ... },
    "B-22_atom_template_vars_bound": { ... }
  },

  "session_metrics": {
    "B-20_deploy_readmes_retry_rounds": 2,
    "B-23_writer_first_pass_failures": 3,
    "B-21_sessionless_export_attempts": 0,
    "sub_agent_count": 8,
    "close_step_completed": true,
    "editorial_review_dispatched": true,
    "code_review_dispatched": true,
    "close_browser_walk_attempted": true,
    "retry_cycle_attributions": [
      { "cycle": 1, "failing_checks": ["fragment_intro"], "attribution": "F-14-anomaly", "resolved_on": "round_2" }
    ]
  },

  "writer_compliance": {
    "apidev/README.md": {
      "file_exists": true,
      "size_bytes": 8251,
      "intro_fragment": {
        "markers_present": true,
        "markers_exact_form": true,
        "line_count": 2,
        "in_range_1_to_3": true,
        "status": "pass"
      },
      "integration_guide_fragment": {
        "markers_present": true,
        "markers_exact_form": true,
        "h3_count": 5,
        "h3_in_range_3_to_6": true,
        "every_h3_has_fenced_code_block": true,
        "status": "pass"
      },
      "knowledge_base_fragment": {
        "markers_present": true,
        "markers_exact_form": true,
        "gotchas_h3_present": true,
        "gotcha_bullet_count": 4,
        "bullets_in_range_3_to_6": true,
        "status": "pass"
      }
    },
    "appdev/README.md": { ... },
    "workerdev/README.md": { ... },
    "apidev/CLAUDE.md": {
      "file_exists": true,
      "size_bytes": 4010,
      "size_ge_1200_bytes": true,
      "base_sections_present": ["Dev Loop", "Migrations", "Container Traps", "Testing"],
      "custom_section_count": 2,
      "custom_sections_ge_2": true,
      "status": "pass"
    },
    "appdev/CLAUDE.md": { ... },
    "workerdev/CLAUDE.md": { ... }
  },

  "dispatch_integrity": {
    "writer_1": {
      "dispatch_prompt_size_bytes": 16842,
      "atoms_stitched_in_envelope_order": true,
      "diff_against_BuildWriterDispatchBrief": {
        "status": "clean",
        "divergences": []
      },
      "template_vars_resolved": true,
      "unresolved_template_vars": []
    },
    "feature": { ... },
    "scaffold_apidev": { ... },
    "scaffold_appdev": { ... },
    "scaffold_workerdev": { ... },
    "code_review": { ... },
    "editorial_review": { ... }
  },

  "env_comments": {
    "0 — AI Agent/import.yaml": {
      "preprocessor_directive_present": true,
      "project_comment_block_exists": true,
      "service_blocks": {
        "appdev": { "comment_lines": 6, "in_range_4_to_10": true, "contains_decision_why": true, "status": "pass" },
        "apistage": { ... }
      },
      "status": "pass"
    },
    "1 — Remote (CDE)/import.yaml": { ... }
  },

  "manifest_consistency": {
    "manifest_file_valid_json": true,
    "fact_count": 12,
    "every_fact_has_routed_to": true,
    "routing_matrix_consistency": {
      "discarded_x_published_gotcha": 0,
      "claude_md_x_published_gotcha": 0,
      "integration_guide_x_published_gotcha": 0
    },
    "writer_content_manifest_completeness": true,
    "status": "pass"
  },

  "inefficiency_signals": {
    "agent_browser_timeout_count": 0,
    "parallel_bash_cancellation_count": 0,
    "total_bash_retry_time_seconds": 0,
    "writer_dispatch_wall_seconds": 1200,
    "writer_authored_bytes": 25000,
    "writer_throughput_bytes_per_minute": 1250,
    "writer_throughput_above_1000": true
  },

  "schema_version": "1.0.0"
}
```

**Every key maps to a deterministic measurement.** Re-running the harness on the same inputs produces byte-identical output (modulo the `generated_at` timestamp).

---

## 3. Tier-1 bar definitions

Each bar is implemented as a Go function that takes `(deliverableDir, sessionsLogsDir)` and returns `(BarResult, error)`. The function runs the measurement, captures evidence, and records status.

### B-15: Ghost env directories

```go
func checkB15GhostEnvDirs(deliverableDir string) BarResult {
    envsDir := filepath.Join(deliverableDir, "environments")
    entries, _ := os.ReadDir(envsDir)
    canonical := map[string]bool{
        "0 — AI Agent": true, "1 — Remote (CDE)": true, "2 — Local": true,
        "3 — Stage": true, "4 — Small Production": true,
        "5 — Highly-available Production": true,
    }
    ghosts := []string{}
    for _, e := range entries {
        if !e.IsDir() { continue }
        if !canonical[e.Name()] {
            ghosts = append(ghosts, e.Name())
        }
    }
    return BarResult{
        ID:         "B-15",
        Threshold:  0,
        Observed:   len(ghosts),
        Status:     passOrFail(len(ghosts) == 0),
        EvidenceFiles: ghosts,
        Measurement: "find environments/ -maxdepth 1 -type d ∉ canonical set",
    }
}
```

Threshold 0; any non-canonical dir under `environments/` fails. **v36 retrospective**: observed = 6 (dev-and-stage-hypercde, local-validator, prod-ha, remote-cde-and-stage, small-prod, stage-only).

### B-16: Per-codebase markdown in tarball

```go
func checkB16TarballPerCodebaseMd(deliverableDir string, plan *workflow.RecipePlan) BarResult {
    tarball := findTarballInDeliverable(deliverableDir) // or take as arg
    if tarball == "" { return BarResult{Status: "skip", Reason: "no tarball found"} }
    entries := listTarballEntries(tarball)
    codebaseCount := countCodebases(plan)
    expectedFiles := codebaseCount * 2 // README.md + CLAUDE.md (post-F-13)
    observedFiles := 0
    for _, e := range entries {
        if matchesPerCodebaseMarkdown(e) { observedFiles++ }
    }
    return BarResult{
        ID:         "B-16",
        Threshold:  expectedFiles,
        Observed:   observedFiles,
        Status:     passOrFail(observedFiles == expectedFiles),
        Measurement: "tar -tzf <archive> | grep -E '/<codebase>/(README|CLAUDE).md$'",
    }
}
```

**v36 retrospective**: expected = 6 (3 codebases × 2), observed = 0. FAIL.

### B-17: Marker exact form

```go
var markerExactRe = regexp.MustCompile(`<!-- #ZEROPS_EXTRACT_(START|END):(intro|integration-guide|knowledge-base)# -->`)
var markerBrokenRe = regexp.MustCompile(`<!-- #ZEROPS_EXTRACT_(START|END):(intro|integration-guide|knowledge-base)(?!#)([^-]|-[^-])*-->`)

func checkB17MarkerExactForm(deliverableDir string) BarResult {
    codebaseReadmes := findCodebaseReadmes(deliverableDir)
    failedFiles := []FailedFile{}
    for _, f := range codebaseReadmes {
        data, _ := os.ReadFile(f)
        brokenMatches := markerBrokenRe.FindAllStringIndex(data, -1)
        if len(brokenMatches) > 0 {
            failedFiles = append(failedFiles, FailedFile{Path: f, Count: len(brokenMatches), Lines: lineNumbers(data, brokenMatches)})
        }
    }
    return BarResult{
        ID:         "B-17",
        Threshold:  0,
        Observed:   len(failedFiles),
        Status:     passOrFail(len(failedFiles) == 0),
        EvidenceFiles: failedFiles,
        Measurement: "grep -P '<!-- #ZEROPS_EXTRACT_(START|END):.*(?!#) -->'",
    }
}
```

**v36 retrospective**: 3 READMEs had markers missing trailing `#`. FAIL.

### B-18: Standalone duplicate files

```go
func checkB18StandaloneDuplicates(deliverableDir string) BarResult {
    forbidden := []string{"INTEGRATION-GUIDE.md", "GOTCHAS.md"}
    found := []string{}
    filepath.Walk(deliverableDir, func(path string, info os.FileInfo, err error) error {
        if info == nil || info.IsDir() { return nil }
        for _, f := range forbidden {
            if info.Name() == f {
                found = append(found, path)
            }
        }
        return nil
    })
    return BarResult{
        ID:         "B-18",
        Threshold:  0,
        Observed:   len(found),
        Status:     passOrFail(len(found) == 0),
        EvidenceFiles: found,
        Measurement: "find <dir> -name INTEGRATION-GUIDE.md -o -name GOTCHAS.md",
    }
}
```

**v36 retrospective**: 0 in deliverable (stranded by F-10), 6 on source mount. Report both if possible.

### B-20: Deploy-readmes retry rounds

```go
func checkB20DeployReadmesRetryRounds(sessionsLogsDir string) BarResult {
    jsonl := filepath.Join(sessionsLogsDir, "main-session.jsonl")
    failingRounds := 0
    for _, event := range parseJSONL(jsonl) {
        if event.ToolName == "mcp__zerops__zerops_workflow" &&
           event.ToolInput.Action == "complete" &&
           event.ToolInput.Step == "deploy" &&
           event.ToolInput.Substep == "readmes" {
            // Check if response has checkResult.passed == false AND any failing check is a readme check
            if resp := parseResponse(event); resp.CheckResult.Passed == false {
                for _, check := range resp.CheckResult.Checks {
                    if strings.HasPrefix(check.Name, "fragment_") ||
                       strings.Contains(check.Name, "comment_ratio") ||
                       strings.Contains(check.Name, "integration_guide") ||
                       strings.Contains(check.Name, "knowledge_base") {
                        failingRounds++
                        break
                    }
                }
            }
        }
    }
    return BarResult{
        ID:         "B-20",
        Threshold:  2,
        Observed:   failingRounds,
        Status:     passOrFail(failingRounds <= 2),
        Measurement: "count of complete step=deploy substep=readmes responses with checkResult.passed==false",
    }
}
```

**v36 retrospective**: 4 rounds. FAIL (signal-grade).

### B-23: Writer first-pass compliance failures

```go
func checkB23WriterFirstPassFailures(sessionsLogsDir string) BarResult {
    // Find the first action=complete step=deploy or substep=readmes after writer-1 returns
    // Count distinct failing check names in that response
    failingChecks := findWriterFirstPassFailures(sessionsLogsDir)
    return BarResult{
        ID:         "B-23",
        Threshold:  3,
        Observed:   len(failingChecks),
        Status:     passOrFail(len(failingChecks) <= 3),
        EvidenceFiles: failingChecks,
        Measurement: "distinct failing check names in first complete step=deploy substep=readmes after writer-1 dispatch",
    }
}
```

**v36 retrospective**: 9 failing checks. FAIL.

### B-22: Atom template variables bound

```go
func checkB22AtomTemplateVarsBound() BarResult {
    allowedFields := map[string]bool{
        "ProjectRoot": true, "Hostnames": true, "EnvFolders": true,
        "Framework": true, "Slug": true, "Tier": true,
    }
    // Also needs render-time verification: can Go actually populate these?
    atoms := findAllAtoms()
    unboundRefs := []UnboundRef{}
    for _, atom := range atoms {
        refs := extractTemplateRefs(atom.Body)
        for _, ref := range refs {
            if !allowedFields[ref.Field] || !rendersCleanly(atom, ref) {
                unboundRefs = append(unboundRefs, UnboundRef{Atom: atom.ID, Field: ref.Field, Line: ref.Line})
            }
        }
    }
    return BarResult{
        ID:         "B-22",
        Threshold:  0,
        Observed:   len(unboundRefs),
        Status:     passOrFail(len(unboundRefs) == 0),
        EvidenceFiles: unboundRefs,
        Measurement: "every {{.Field}} in atoms binds to a Go render path",
    }
}
```

This is a **build-time lint** that should run as part of `make lint-local`, not a post-run harness call. But the harness reports its current state for cross-reference.

**v36 retrospective**: `.EnvFolders` appears unbound unless the render path is traced. FAIL before Cx-ENVFOLDERS-WIRED lands.

### B-24: Dispatch prompt vs Go-source diff

```go
func checkB24DispatchIntegrity(sessionsLogsDir string, plan *workflow.RecipePlan) BarResult {
    captures := findDispatchPrompts(sessionsLogsDir) // from flow-dispatches/
    divergent := []DivergentDispatch{}
    for _, cap := range captures {
        expected := buildExpectedDispatchBrief(cap.Role, plan)
        diff := bytedDiff(cap.Prompt, expected)
        if diff.HasNonTemplateSlotDivergence() {
            divergent = append(divergent, DivergentDispatch{Role: cap.Role, Diff: diff})
        }
    }
    return BarResult{
        ID:         "B-24",
        Threshold:  0,
        Observed:   len(divergent),
        Status:     passOrFail(len(divergent) == 0),
        EvidenceFiles: divergent,
        Measurement: "byte-diff each captured Agent dispatch prompt vs BuildXxxDispatchBrief(plan)",
    }
}
```

Catches F-9-class defects (main agent invents content beyond expected template-slot variance).

---

## 4. Tier-2 checklist template

The generated `verification-checklist.md` follows this shape. Rows auto-filled marked `(auto)`; rows requiring analyst input marked `(analyst-fill)`.

```markdown
# runs/v37 verification checklist

**Machine report SHA**: <sha256>
**Generated at**: 2026-04-XX HH:MM:SS UTC
**Tier**: showcase
**Analyst**: <fill>
**Analyst session start**: <fill UTC timestamp>

## Phase reached

- [x] `research` — complete (auto)
- [x] `provision` — complete (auto)
- [x] `generate` — complete (auto)
- [x] `deploy` — complete (auto)
- [x] `finalize` — complete (auto)
- [ ] `close` — (auto) status: in_progress / complete / skipped

If close is not complete, downstream cells must be `unmeasurable-valid` with explicit close-step-status justification. If close IS complete, no downstream cell may be `unmeasurable`.

## Structural integrity bars (auto from machine-report.json)

- [ ] B-15 ghost_env_dirs: threshold 0, observed <N>, <status>
- [ ] B-16 tarball_per_codebase_md: threshold <N>, observed <M>, <status>
- [ ] B-17 marker_exact_form: threshold 0 failing files, observed <N>, <status>
- [ ] B-18 standalone_duplicate_files: threshold 0, observed <N>, <status>
- [ ] B-22 atom_template_vars_bound: threshold 0 unbound, observed <N>, <status>

## Session-metric bars (auto from machine-report.json)

- [ ] B-20 deploy_readmes_retry_rounds: threshold ≤ 2, observed <N>, <status>
- [ ] B-21 sessionless_export_attempts: threshold 0, observed <N>, <status>
- [ ] B-23 writer_first_pass_failures: threshold ≤ 3, observed <N>, <status>
- [ ] B-24 dispatch_integrity (per role): threshold clean, observed <N> divergent, <status>

## Close-phase dispatch (auto, only valid if close complete)

- [ ] editorial_review_dispatched: <true/false>
- [ ] code_review_dispatched: <true/false>
- [ ] close_browser_walk_attempted: <true/false>
- [ ] editorial_review_CRIT_shipped (T-11): threshold 0, observed <N>, <status>
- [ ] editorial_review_reclassification_delta (T-12): threshold 0, observed <N>, <status>

## Content quality per writer-authored file (ANALYST-FILL, REQUIRED)

For each file listed in machine-report `writer_compliance`, analyst fills:
- `pass / fail / unmeasurable-valid / unmeasurable-invalid`
- `read_receipt_timestamp`: the moment the analyst called Read on this file (YYYY-MM-DD HH:MM:SS UTC)
- `evidence`: file:line pointer supporting the grade
- `notes`: optional narrative, ≤ 2 sentences

### apidev/README.md (surface 4: intro + IG + KB fragment)

- Intro fragment test ("names managed services; 1-3 lines"):
  - [ ] Status: <pass/fail>
  - [ ] Read-receipt: <timestamp>
  - [ ] Evidence: `<file>:<line>`
- Integration-guide fragment test ("3-6 H3 items, each with fenced code block, each principle-level not self-referential"):
  - [ ] Status: <pass/fail>
  - [ ] Read-receipt: <timestamp>
  - [ ] Evidence: `<file>:<line>`
- Knowledge-base fragment test ("3-6 gotcha bullets, each `**symptom** — mechanism` form, cites platform topics in Citation Map"):
  - [ ] Status: <pass/fail>
  - [ ] Read-receipt: <timestamp>
  - [ ] Evidence: `<file>:<line>`

### appdev/README.md
(repeat)

### workerdev/README.md (+ showcase worker supplements)
(repeat)

### apidev/CLAUDE.md (surface 6)
- [ ] Dev Loop section present + useful: <pass/fail>
- [ ] Migrations section present + correct: <pass/fail>
- [ ] Container Traps section present + recipe-specific: <pass/fail>
- [ ] Testing section present: <pass/fail>
- [ ] ≥ 2 custom sections beyond template: <pass/fail>
- [ ] Read-receipt: <timestamp>

### appdev/CLAUDE.md
(repeat)

### workerdev/CLAUDE.md
(repeat)

## Env README quality (ANALYST-FILL per-env)

For each `environments/<N — Name>/README.md` (6 canonical only):

- [ ] Audience section tier-appropriate: <pass/fail>
- [ ] Scale-profile section factual: <pass/fail>
- [ ] Promotion-path section points to adjacent tier: <pass/fail>
- [ ] Operational concerns section named: <pass/fail>
- [ ] 40-80 line range: <pass/fail>
- [ ] Read-receipt: <timestamp>

## Env import.yaml comment quality (ANALYST-FILL per-env)

For each `environments/<N — Name>/import.yaml`:

- [ ] `#zeropsPreprocessor=on` first line: <pass/fail>
- [ ] Project comment block explains APP_SECRET + project-level vars: <pass/fail>
- [ ] Every service block has 4-10 lines, each explains a decision (presence/scale/mode): <pass/fail>
- [ ] Adjacent-tier promotion context present: <pass/fail>
- [ ] No templated openings repeated across service blocks: <pass/fail>
- [ ] Read-receipt: <timestamp>

## Manifest integrity (ANALYST-FILL)

- [ ] ZCP_CONTENT_MANIFEST.json valid JSON: (auto from machine-report)
- [ ] Every fact has valid `routed_to`: (auto)
- [ ] Facts-log ↔ manifest title match: <pass/fail>
- [ ] Every published gotcha has corresponding manifest entry: <pass/fail>
- [ ] Read-receipt: <timestamp>

## Inefficiency signals

- [ ] agent_browser_cost: <N> timeouts × <M>s = <total>; accepted as environmental
- [ ] parallel_bash_cancellation_cascades: <N>
- [ ] writer_throughput: <X>B/min, <pass/fail if ≥ 1000>

## Retry-cycle attribution (REQUIRED)

For every failing check round in machine-report, analyst attributes to a defect class:

| Cycle | Timestamp | Substep | Failing checks | Attribution |
|---|---|---|---|---|
| 1 | <ts> | <substep> | <checks> | <F-N or signal-grade or cosmetic> |
| 2 | <ts> | ... | ... | ... |

No cycles may be unattributed.

## Final verification

- [ ] All cells are non-`pending`
- [ ] Every Read-receipt timestamp is after analyst session start
- [ ] No `unmeasurable-invalid` cells
- [ ] Machine-report SHA matches file content
- [ ] Checklist SHA matches file content

**Analyst sign-off**: <name, timestamp>
**Hook verification**: ✅ / ❌ (filled by pre-commit)
```

---

## 5. Tier-3 commit hook

`.githooks/verify-verdict` — Bash or Go, validates:

```bash
#!/bin/bash
set -euo pipefail

# Find runs/vN/ directories changed in this commit
for run_dir in $(git diff --cached --name-only --diff-filter=A | grep -E 'runs/v[0-9]+/verdict\.md$' | xargs -I {} dirname {}); do
  verdict="$run_dir/verdict.md"
  checklist="$run_dir/verification-checklist.md"
  machine_report="$run_dir/machine-report.json"

  # Rule 1: all three files present
  test -f "$verdict" || { echo "FAIL: $verdict missing"; exit 1; }
  test -f "$checklist" || { echo "FAIL: $checklist missing"; exit 1; }
  test -f "$machine_report" || { echo "FAIL: $machine_report missing"; exit 1; }

  # Rule 2: verdict front matter has machine_report_sha + checklist_sha
  fm_machine_sha=$(sed -n 's/^machine_report_sha: \(.*\)$/\1/p' "$verdict")
  fm_checklist_sha=$(sed -n 's/^checklist_sha: \(.*\)$/\1/p' "$verdict")
  actual_machine_sha=$(shasum -a 256 "$machine_report" | awk '{print $1}')
  actual_checklist_sha=$(shasum -a 256 "$checklist" | awk '{print $1}')
  [ "$fm_machine_sha" = "$actual_machine_sha" ] || { echo "FAIL: machine_report SHA mismatch"; exit 1; }
  [ "$fm_checklist_sha" = "$actual_checklist_sha" ] || { echo "FAIL: checklist SHA mismatch"; exit 1; }

  # Rule 3: no pending cells in checklist
  pending_count=$(grep -c 'Status: <pass/fail>' "$checklist" || true)
  [ "$pending_count" -eq 0 ] || { echo "FAIL: $pending_count pending cells in checklist"; exit 1; }

  # Rule 4: every PASS/FAIL claim in verdict has a citation within 50 chars
  python3 tools/hooks/verify_citation_rule.py "$verdict" || exit 1

  # Rule 5: self-congratulatory language tripwire
  suspicious=$(grep -cE '\b(success|works|clean|PROCEED)\b(?![^[]*\[)' "$verdict" || true)
  if [ "$suspicious" -gt 3 ]; then
    echo "FAIL: $suspicious self-congratulatory terms without citation"
    exit 1
  fi
done
```

`tools/hooks/verify_citation_rule.py` scans the verdict for any sentence containing PASS/FAIL/CLOSED/UNREACHED keywords; requires a `[checklist X-Y]` or `[machine-report.<key>]` within 50 characters; fails if missing.

---

## 6. Implementation plan

### Files to create

| File | Purpose | Est. LoC |
|---|---|---|
| `cmd/zcp/analyze/analyze.go` | CLI entry, subcommand dispatch | 80 |
| `cmd/zcp/analyze/recipe_run.go` | `recipe-run` subcommand | 100 |
| `cmd/zcp/analyze/generate_checklist.go` | `generate-checklist` subcommand | 80 |
| `internal/analyze/structural.go` | B-15..B-18 bar implementations | 200 |
| `internal/analyze/session.go` | B-20..B-24 bar impls + JSONL parse | 250 |
| `internal/analyze/writer_compliance.go` | per-file surface-contract checks | 200 |
| `internal/analyze/dispatch_integrity.go` | dispatch prompt diff engine | 150 |
| `internal/analyze/report.go` | machine-report JSON schema + write | 100 |
| `internal/analyze/checklist.go` | checklist Markdown generator | 150 |
| `tools/lint/atom_template_vars.go` | build-time unbound-var lint | 120 |
| `tools/hooks/verify_verdict` | Bash pre-commit hook | 80 |
| `tools/hooks/verify_citation_rule.py` | citation-regex validator | 60 |
| `internal/analyze/*_test.go` | table-driven tests for every bar | 300 |

**Total estimate**: ~1 800 LoC, most of it measurement logic that's straightforward filesystem + JSONL parsing.

### Order of implementation

1. **Schema first**: `report.go` — define the `MachineReport` struct + JSON tags. Freeze before implementing bars.
2. **Structural bars**: B-15, B-16, B-17, B-18, B-22. Simplest — just filesystem walks + greps. Each bar is a ~40 LoC function + a ~20 LoC test.
3. **Session-metric bars**: B-20, B-21, B-23 — JSONL parser + event filters. B-24 needs dispatch-prompt parsing (use the existing `scripts/extract_flow.py` approach, port to Go).
4. **Writer-compliance**: per-file surface-contract tests. ~20 LoC per test × 10 tests.
5. **CLI wiring**: `recipe-run` command composes the bars, emits JSON.
6. **Checklist generator**: Markdown formatter from machine-report.
7. **Commit hook**: Bash + Python helper.
8. **Validation test**: run against v36 deliverable. Expected output embedded in `internal/analyze/testdata/v36_expected_machine_report.json` as golden file.

### Validation against v36

**Pre-commit validation** of the harness:
```bash
cd /Users/fxck/www/zcp
go build -o bin/zcp ./cmd/zcp
./bin/zcp analyze recipe-run \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36 \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/SESSIONS_LOGS \
  --out /tmp/v36-retro-report.json
```

Expected output fields:
- `structural_integrity.B-15_ghost_env_dirs.observed == 6`
- `structural_integrity.B-15_ghost_env_dirs.status == "fail"`
- `structural_integrity.B-17_marker_exact_form.observed >= 3`
- `structural_integrity.B-17_marker_exact_form.status == "fail"`
- `structural_integrity.B-18_standalone_duplicate_files.observed >= 0` (might be 0 since deliverable strips them; check source mount separately if needed)
- `session_metrics.B-20_deploy_readmes_retry_rounds.observed == 4`
- `session_metrics.B-23_writer_first_pass_failures.observed == 9`
- `writer_compliance.apidev/README.md.integration_guide_fragment.every_h3_has_fenced_code_block == false` (on v36 first pass)

If any of these don't match, the bar implementation is wrong. Iterate before calling the harness done.

### Integration with existing code

- Reuse `internal/sync/export.go:knownEnvFolders` for the canonical env list (single source of truth).
- Reuse `internal/workflow/recipe_templates.go:envTiers` if the canonical list needs framework-dependent variance.
- Reuse `internal/workflow/atom_loader.go` for atom enumeration.
- Reuse existing `extract_flow.py` concepts for JSONL parsing, but implement in Go for the harness.

---

## 7. Out of scope for v1

- **Grading subjective quality** (e.g., "is this gotcha folk-doctrine?"): manual analyst judgment with evidence citation. Harness only checks objective surrogates (has citation, has code block, has mechanism explanation).
- **Cross-run regression detection**: v37-vs-v36 diff reports are easy to add later (JSON diffs are cheap) but not v1.
- **Auto-verdict generation**: harness produces machine-report + checklist, not verdict. Verdict remains analyst prose.
- **Live streaming during run**: v1 is post-run analysis. During-run tripwires are a separate layer.
- **Multi-tier comparison**: showcase vs minimal harness runs separately.

---

## 8. Success criteria for harness v1

- [ ] `go test ./cmd/zcp/analyze/... ./internal/analyze/...` green.
- [ ] `make lint-local` green including new `tools/lint/atom_template_vars.go` lint.
- [ ] Running `zcp analyze recipe-run` against v36 deliverable mechanically surfaces F-9, F-10, F-12, F-13 (retrospective validation).
- [ ] `zcp analyze generate-checklist` emits a valid Markdown worksheet with correct SHA metadata.
- [ ] Pre-commit hook installed at `.githooks/verify-verdict`; `git commit` of a verdict without required companions blocks.
- [ ] Documentation added to this spec + implementation notes in `implementation-notes.md`.

Once v1 harness lands and validates against v36, Phase 2 (fix-stack) can proceed. After fix-stack lands as v8.109.0, v37 commission can proceed. After v37 runs, v37 analyst uses this harness per [`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6 rules.

---

## 9. Why this structure prevents v36's failure mode

- **Rule 1 — mechanical bars first**: analyst cannot skip structural audit because the CLI produces it. No "forgot to check env dirs" class of miss.
- **Rule 2 — Read-receipts required**: analyst must have Read tool call on every graded file. No "skimmed the first 80 lines and inferred" skips.
- **Rule 3 — evidence-to-claim binding**: verdict citations are regex-enforced. No "this sounds authoritative" paraphrasing.
- **Rule 4 — no pending cells**: analyst cannot ship with unfilled cells. No "unmeasured — close unreached" alibi when the file is on disk.
- **Rule 5 — unmeasurable-invalid state**: the checklist explicitly names when "unmeasurable" is valid (close didn't run, file doesn't exist) vs invalid (file is on disk). Forces honesty.
- **Rule 6 — SHA binding**: if analyst edits the machine-report or checklist, SHA mismatches, commit blocks. Prevents retroactive fabrication.
- **Rule 7 — commit hook**: rules are enforced at commit time, not just documented. Cannot land a non-compliant verdict.

The v36 analysis failed because nothing made failure costly. The harness makes bypass costly — commit blocks — while making compliance cheap — the bars run themselves, the checklist template auto-populates. The path of least resistance becomes the correct path.
