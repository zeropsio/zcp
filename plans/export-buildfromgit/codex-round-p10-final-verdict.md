# Phase 10 FINAL-VERDICT — export-buildFromGit

## 1. Verdict

**NOSHIP from this verifier run.** The implementation is broadly ship-shaped, but the Phase 10 gate is not closed because `make lint-local` failed during this read-and-verify pass, and G2 requires `go test ./... -race -count=1` plus `make lint-local` green (`plans/export-buildfromgit-2026-04-28.md:817-825`). The failure is environmental/DNS, not an observed lint issue: `catalog sync` could not resolve `api.app-prg1.zerops.io`, so lint never reached a 0-issue conclusion.

If `make lint-local` is rerun successfully in a network-capable environment, the verdict should move to **SHIP-WITH-NOTES**, not clean SHIP, because Phase 8 explicitly waived stage-variant and fresh-project re-import verification (`plans/export-buildfromgit/phase-8-tracker.md:67-73`, `plans/export-buildfromgit/phase-8-tracker.md:80-92`).

## 2. G1-G9

| Gate | Verdict | Evidence |
|---|---|---|
| G1 phases closed | **PARTIAL** | Phases 0-9 have trackers with exits: P0 closed at `plans/export-buildfromgit/phase-0-tracker.md:110-119`; P1 at `plans/export-buildfromgit/phase-1-tracker.md:61-66`; P2 at `plans/export-buildfromgit/phase-2-tracker.md:112-121`; P3 at `plans/export-buildfromgit/phase-3-tracker.md:124-133`; P4 at `plans/export-buildfromgit/phase-4-tracker.md:112-122`; P5 at `plans/export-buildfromgit/phase-5-tracker.md:102-114`; P6 at `plans/export-buildfromgit/phase-6-tracker.md:65-74`; P7 at `plans/export-buildfromgit/phase-7-tracker.md:46-53`; P8 at `plans/export-buildfromgit/phase-8-tracker.md:80-92`; P9 at `plans/export-buildfromgit/phase-9-tracker.md:40-47`. Phase 10 cannot be closed until this verdict and final gate handling complete (`plans/export-buildfromgit-2026-04-28.md:774-778`). |
| G2 full test + lint | **FAIL** | `go test ./internal/... -race -count=1 -run TestArchitecture` passed for all internal packages, including `internal/topology`. `make lint-local` failed before lint completion with: `catalog sync: fetch schemas: fetch zerops.yaml schema: Get "https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json": dial tcp: lookup api.app-prg1.zerops.io: no such host`; G2 requires lint green (`plans/export-buildfromgit-2026-04-28.md:818`). |
| G3 X1-X8 addressed | **PASS with notes** | X1: legacy 229-line atom replaced by six topic atoms; scenario pins exact IDs (`internal/workflow/scenarios_test.go:589-630`). X2: generator exists and validates bundle output (`internal/ops/export_bundle.go:128-185`). X3: handler prompts pair variants and tests cover standard/stage/local-stage (`internal/tools/workflow_export.go:213-220`, `internal/tools/workflow_export_test.go:172-210`, `internal/tools/workflow_export_test.go:571-602`). X4: git-push setup chain gates publish (`internal/tools/workflow_export.go:206-208`). X5: env classification is per-request and LLM-driven, with classify prompt redaction (`internal/content/atoms/export-classify-envs.md:10-19`, `internal/tools/workflow_export_test.go:744-792`). X6/G9: schema validators are wired into `BuildBundle` (`internal/ops/export_bundle.go:163-184`, `internal/schema/validate_jsonschema.go:76-130`). X7: documented limitation, not normalized (`plans/export-buildfromgit-2026-04-28.md:780-784`, `plans/export-buildfromgit/SHIP-WITH-NOTES.md:14-16`). X8: canonical filename is `zerops-project-import.yaml` in atoms and response builder (`internal/content/atoms/export-publish.md:11-23`, `internal/tools/workflow_export.go:409-430`). |
| G4 final verdict | **FAIL for current run** | This file records NOSHIP because G2 is not satisfied; G4 allows only SHIP or SHIP-WITH-NOTES (`plans/export-buildfromgit-2026-04-28.md:820`). |
| G5 eval-zcp dev+stage+re-import | **PARTIAL** | Dev eval r3 passed (`plans/export-buildfromgit/phase-8-tracker.md:47-52`, `plans/export-buildfromgit/phase-8-tracker.md:80-92`). Stage variant and fresh-project re-import were explicitly waived (`plans/export-buildfromgit/phase-8-tracker.md:67-73`). |
| G6 atom axes lint-clean | **PASS for targeted check** | `go test ./internal/content -run TestAtomAuthoringLint -count=1` passed. The lint contract covers spec IDs, handler-behavior, invisible state, and axes K/L/M/N (`internal/content/atoms_lint.go:40-70`, `internal/content/atoms_lint_axes.go:36-49`, `internal/content/atoms_lint_axes.go:146-168`, `internal/content/atoms_lint_axes.go:179-249`, `internal/content/atoms_lint_axes.go:256-278`). |
| G7 docs aligned | **PASS with amendment** | `docs/spec-workflows.md` now has §9 export flow and E1-E5 invariants (`docs/spec-workflows.md:1219-1274`); `CLAUDE.md` has the export invariant (`CLAUDE.md:194-208`). Amendment: `internal/tools/workflow.go` JSON schema still says variant dev/stage "re-imports as mode=dev/simple" (`internal/tools/workflow.go:96-103`), contradicting the NON_HA correction in docs (`docs/spec-workflows.md:1241-1245`). |
| G8 no old atom path refs | **PASS** | Exact command: `rg -n "atoms/export\\.md" internal/ docs/`. Exact output: zero stdout, exit code 1. This is ripgrep's zero-hit result and satisfies G8 (`plans/export-buildfromgit-2026-04-28.md:824`). |
| G9 schema-valid generated import | **PASS by implementation/test pins** | `ValidateImportYAML` validates YAML against embedded import schema (`internal/schema/validate_jsonschema.go:76-101`), and `BuildBundle` appends import/zerops validation errors to `ExportBundle.Errors` before publish (`internal/ops/export_bundle.go:163-184`). Tests pin happy/error paths (`internal/schema/validate_jsonschema_test.go:33-93`, `internal/tools/workflow_export_test.go:873-994`). |

## 3. Acceptance Delta

Plan Phase 10 accepts SHIP-WITH-NOTES for private-repo auth not confirmed, subdomain drift documented, and multi-runtime out of scope (`plans/export-buildfromgit-2026-04-28.md:780-784`). Current state has all three: private-repo auth remains unconfirmed (`plans/export-buildfromgit/SHIP-WITH-NOTES.md:16`), subdomain drift is documented (`plans/export-buildfromgit/SHIP-WITH-NOTES.md:14`), and multi-runtime is out of scope (`plans/export-buildfromgit-2026-04-28.md:827-833`).

The **stage-variant + fresh re-import waiver does not cleanly fit condition c**. Condition c is multi-runtime scope, while stage/re-import are explicit G5 live-verification requirements (`plans/export-buildfromgit-2026-04-28.md:821`). It needs a separate callout, already recorded in Phase 8 and SHIP notes (`plans/export-buildfromgit/phase-8-tracker.md:67-73`, `plans/export-buildfromgit/SHIP-WITH-NOTES.md:10-16`).

## 4. Regression

Required regression command:

```text
go test ./internal/... -race -count=1 -run TestArchitecture
```

Result: **PASS**. All listed internal packages returned `ok`; only `internal/topology` ran matching tests, and the rest reported `[no tests to run]`.

Required lint command:

```text
make lint-local
```

Result: **FAIL/BLOCKED** before lint completion. Exact failure tail:

```text
2026/04/29 01:37:55 catalog sync: fetch schemas: fetch zerops.yaml schema: Get "https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json": dial tcp: lookup api.app-prg1.zerops.io: no such host
make: *** [catalog-sync] Error 1
```

Phase 9 previously recorded `make lint-local` as 0 issues (`plans/export-buildfromgit/phase-9-tracker.md:28-35`), but this Phase 10 verifier cannot claim current lint green.

## 5. Axis Hygiene

Targeted atom reads:

- `export-classify-envs.md` has no title/heading env-only qualifier: title and headings are content-specific (`internal/content/atoms/export-classify-envs.md:1-7`, `internal/content/atoms/export-classify-envs.md:10-23`, `internal/content/atoms/export-classify-envs.md:66-79`). It contains no `DM-*`, `E*`, or `O*` invariant citations in the checked grep set; bucket examples use M2/M4/M6/M7-style misclassification labels, which are not forbidden by the spec-ID regex (`internal/content/atoms_lint.go:40-45`). It does not use handler-behavior verbs in the forbidden subject patterns, and it does not mention the invisible-state fields forbidden by lint (`internal/content/atoms_lint.go:47-65`). It uses observable response fields and command inputs (`internal/content/atoms/export-classify-envs.md:81-113`).
- `export-publish.md` has no title/heading env-only qualifier (`internal/content/atoms/export-publish.md:1-7`, `internal/content/atoms/export-publish.md:11-63`). It has no forbidden spec invariant IDs in the checked grep set. It mentions `meta.GitPushState` and `meta.RemoteURL` (`internal/content/atoms/export-publish.md:9-41`); those are not covered by the current invisible-state regex (`internal/content/atoms_lint.go:61-65`), but they are internal-ish state names. Keep as a note, not a lint failure, because `TestAtomAuthoringLint` passed and the fields are also response/model concepts in the export flow.

## 6. Noted Limitations

- Private-repo `buildFromGit` auth was not exercised; Phase 8 used a public repo (`plans/export-buildfromgit/phase-8-tracker.md:16-21`, `plans/export-buildfromgit/SHIP-WITH-NOTES.md:16`).
- Subdomain drift is documented rather than normalized (`plans/export-buildfromgit-2026-04-28.md:214-216`, `plans/export-buildfromgit/SHIP-WITH-NOTES.md:14`).
- Stage variant and fresh-project re-import remain waived from live verification (`plans/export-buildfromgit/phase-8-tracker.md:67-73`, `plans/export-buildfromgit/phase-8-tracker.md:91-92`).
- Current Phase 10 lint gate is blocked by DNS, so this verifier cannot attest 0 issues now.

## 7. Amendments

1. Rerun `make lint-local` where `api.app-prg1.zerops.io` resolves; record the exact 0-issue output before moving from NOSHIP to SHIP-WITH-NOTES.
2. Treat stage-variant + fresh re-import waiver as its own SHIP-WITH-NOTES condition, not as multi-runtime scope.
3. Fix stale JSON schema text in `internal/tools/workflow.go` for `variant`: it should not say dev/stage re-import as topology `mode=dev/simple` (`internal/tools/workflow.go:96-103`); docs say import uses platform scaling `NON_HA` (`docs/spec-workflows.md:1241-1245`).
