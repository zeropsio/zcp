## 1. Verdict

APPROVE with amendments. The Phase 5 validator is structurally sound: the dependency is vendored in `go.mod:9`/`go.sum:19-20`, the embedded schemas declare draft 2020-12 in `internal/schema/testdata/import_yml_schema.json:1-3` and `internal/schema/testdata/zerops_yml_schema.json:1-3`, validators compile once in `internal/schema/validate_jsonschema.go:42-73`, YAML is normalized to JSON-compatible values in `internal/schema/validate_jsonschema.go:140-156`, and `BuildBundle` propagates validation errors into `ExportBundle.Errors` in `internal/ops/export_bundle.go:163-185`.

Top amendments: fix the stale variant prompt in `internal/tools/workflow_export.go:249-252`, amend plan §3.3 and Q7 at `plans/export-buildfromgit-2026-04-28.md:125-135` and `plans/export-buildfromgit-2026-04-28.md:218`, add validation-failed handler tests, and decide whether validation should outrank git-push setup because the current branch order returns setup-required before validation-failed at `internal/tools/workflow_export.go:163-168`.

## 2. Lib choice + integration (A)

The library choice is appropriate for the embedded schema dialect: the schemas declare draft 2020-12 at `internal/schema/testdata/import_yml_schema.json:1-3` and `internal/schema/testdata/zerops_yml_schema.json:1-3`, and the code uses `github.com/santhosh-tekuri/jsonschema/v5` at `internal/schema/validate_jsonschema.go:12`. Compile-time integration is clean: schemas are embedded at `internal/schema/validate_jsonschema.go:16-20`, registered with a compiler at `internal/schema/validate_jsonschema.go:57-63`, compiled at `internal/schema/validate_jsonschema.go:67-72`, and cached behind `sync.Once` at `internal/schema/validate_jsonschema.go:42-56`.

YAML-to-JSON conversion is correct for schema validation: empty input returns one `ValidationError` at `internal/schema/validate_jsonschema.go:140-143`, YAML parse errors return one root error at `internal/schema/validate_jsonschema.go:144-147`, and marshal/unmarshal normalization produces JSON-shaped `map[string]any` / `[]any` / scalar values at `internal/schema/validate_jsonschema.go:148-156`.

Error flattening is sensible for agent use: `formatJSONSchemaErrors` unwraps `*jsonschema.ValidationError` at `internal/schema/validate_jsonschema.go:201-205`, collects deepest leaves at `internal/schema/validate_jsonschema.go:206-219`, and `collectValidationLeaves` drops internal summary nodes unless a node has no causes at `internal/schema/validate_jsonschema.go:223-239`.

## 3. HA/NON_HA structural fix adjudication (B)

The structural fix is right for `import.yaml`: the schema’s service `mode` enum is `HA` / `NON_HA` at `internal/schema/testdata/import_yml_schema.json:199-205`, and `runtimeImportMode` now returns `NON_HA` for every topology mode at `internal/ops/export_bundle.go:336-350`. The tests pin this across standard, stage, dev, simple, local-stage, local-only, and garbage input at `internal/ops/export_bundle_test.go:262-288`.

This does not need to preserve topology in `mode`. Variant and target identity still live in the bundle via `Variant`, `TargetHostname`, and `SetupName` fields at `internal/ops/export_bundle.go:49-56` and are populated at `internal/ops/export_bundle.go:179-181`. If future bootstrap needs to recreate ZCP topology metadata after import, that should be an explicit metadata/import-adoption step, not an invalid `mode` value in platform YAML.

## 4. Plan §3.3 amendment recommendation (C)

Replace §3.3 at `plans/export-buildfromgit-2026-04-28.md:125-135` with:

```md
### 3.3 Import service scaling mode and topology metadata

`services[].mode` in `zerops-project-import.yaml` is the Zerops platform scaling enum, not ZCP topology. The schema accepts only `HA` / `NON_HA`; single-runtime export bundles emit `mode: NON_HA` for every source topology.

| Source half | Import `services[].mode` | Preserved bundle metadata |
|---|---|---|
| dev half of standard pair | `NON_HA` | `variant=dev`, chosen hostname, matched `zeropsSetup` |
| stage half of standard pair | `NON_HA` | `variant=stage`, chosen hostname, matched `zeropsSetup` |
| dev / simple / local-only | `NON_HA` | chosen hostname, matched `zeropsSetup` |
| local-stage dev / stage | `NON_HA` | `variant=dev` or `variant=stage`, chosen hostname, matched `zeropsSetup` |
```

Also replace Q7 at `plans/export-buildfromgit-2026-04-28.md:218` and Phase 2 line `plans/export-buildfromgit-2026-04-28.md:387`, and rewrite the anti-pattern at `plans/export-buildfromgit-2026-04-28.md:846` so it forbids conflating topology with platform scaling mode.

## 5. Validator correctness (D)

The requested cases are covered. Empty content returns a single error in `internal/schema/validate_jsonschema.go:140-143` and is asserted for import and zerops YAML at `internal/schema/validate_jsonschema_test.go:41-50` and `internal/schema/validate_jsonschema_test.go:101-110`. Malformed YAML returns a parse error at `internal/schema/validate_jsonschema.go:144-147`, with tests at `internal/schema/validate_jsonschema_test.go:52-61` and `internal/schema/validate_jsonschema_test.go:135-141`. Missing `project.name` is tested to mention `name` at `internal/schema/validate_jsonschema_test.go:63-81`. Missing required setup yields `/zerops` plus a named message at `internal/schema/validate_jsonschema.go:172-193`, with tests at `internal/schema/validate_jsonschema_test.go:112-125`. Empty `requiredSetup` skips that check at `internal/schema/validate_jsonschema.go:125-129`, tested at `internal/schema/validate_jsonschema_test.go:127-133`. The preprocessor header is a YAML comment and the no-op wrapper documents that at `internal/schema/validate_jsonschema.go:159-165`, tested at `internal/schema/validate_jsonschema_test.go:84-91`.

## 6. Testdata refresh cadence (E)

Local copies are currently identical: `cmp` returned zero for `plans/export-buildfromgit/import-schema.json` and `internal/schema/testdata/import_yml_schema.json`; both are 31,103 bytes, while zerops schema testdata is 23,839 bytes. The embedded-schema test only proves embedded bytes are non-empty at `internal/schema/validate_jsonschema_test.go:158-166`; it does not compare against live.

Cadence: refresh on any platform import rejection that contradicts local validation, before each release touching export/import behavior, and during a scheduled schema-maintenance pass. The atom already tells agents that platform rejection after local acceptance means embedded testdata needs refresh at `internal/content/atoms/export-validate.md:57`.

## 7. bundle.Errors propagation (F)

Core propagation is clean: `ExportBundle.Errors []schema.ValidationError` exists at `internal/ops/export_bundle.go:64-69`, `BuildBundle` appends import and zerops validation errors at `internal/ops/export_bundle.go:168-172`, and returns them at `internal/ops/export_bundle.go:174-185`. The handler returns `validation-failed` when it reaches the error gate at `internal/tools/workflow_export.go:167-168`, and `validationFailedResponse` serializes `errors` plus `preview` at `internal/tools/workflow_export.go:351-364`.

Missed/ambiguous paths: classification prompt runs before the error gate at `internal/tools/workflow_export.go:159-160`, and git-push setup also runs before the error gate at `internal/tools/workflow_export.go:163-168`. The git-push branch includes preview errors via `bundlePreview` at `internal/tools/workflow_export.go:290-292` and `internal/tools/workflow_export.go:413-415`, but its status remains `git-push-setup-required`, not `validation-failed`.

## 8. Atom prose alignment (G)

`export-intro` is aligned on `NON_HA` and topology-outside-import at `internal/content/atoms/export-intro.md:18`. `export-classify-envs` is mostly aligned with the validation flow at `internal/content/atoms/export-classify-envs.md:111-113`. `export-validate` is aligned on error shape and `NON_HA` spot-checks at `internal/content/atoms/export-validate.md:55-65`.

Drift: `export-validate` references `bundle.errors` but its `references-fields` omits `ops.ExportBundle.Errors` at `internal/content/atoms/export-validate.md:7`; add it for the same AST-pinned protection used for ImportYAML/ZeropsYAML/Warnings. Also, handler prose still says variant dev re-imports as `mode=dev` and stage as `mode=simple` at `internal/tools/workflow_export.go:249-252`, contradicting the atoms and generator.

## 9. Test coverage gaps (H)

The existing happy-path handler test covers publish-ready at `internal/tools/workflow_export_test.go:426-501`, and bundle tests cover `NON_HA` mode at `internal/ops/export_bundle_test.go:262-288` plus BuildBundle happy/error paths at `internal/ops/export_bundle_test.go:463-604`.

Missing tests: add `TestHandleExport_ValidationFailed` that feeds a schema-invalid but setup-present `zerops.yaml` or invalid import shape and asserts `status="validation-failed"` plus serialized `errors`. Add a second handler test with `GitPushConfigured` that asserts `bundle.Errors` from `BuildBundle` flows through `validationFailedResponse`/`preview.errors`. Consider a branch-order test for `GitPushUnconfigured` plus validation errors, because current code returns setup-required before validation-failed at `internal/tools/workflow_export.go:163-168`.

## 10. Compaction acceptable? (I)

Acceptable for now, but trim soon. The enforced cap is synthesized body output, not raw markdown: `TestCorpusCoverage_OutputUnderMCPCap` joins synthesized bodies at `internal/workflow/corpus_coverage_test.go:888-893` and asserts 28 KiB at `internal/workflow/corpus_coverage_test.go:883-909`. Frontmatter is stripped during atom parse/render at `internal/workflow/atom.go:340-359`, so 28,719 raw bytes being 47 bytes over the raw cap does not violate the current executable gate. Still, `plans/export-buildfromgit/phase-5-tracker.md:46` records the raw overage; trim one sentence to recover raw margin.

## 11. ValidateZeropsYmlRaw deprecation (J)

Do not remove it in Phase 5. `ValidateZeropsYmlRaw` only checks unknown zerops.yaml fields and silently skips nil, parse errors, and missing lists at `internal/schema/validate.go:54-69`; the new validator performs full schema validation and parse-error reporting at `internal/schema/validate_jsonschema.go:110-130`. They are distinct today because recipe checks still call the legacy path through `internal/ops/checks/yml_schema.go:21-32`, `internal/tools/workflow_checks_recipe.go:42-48`, and `cmd/zcp/check/yml_schema.go:47-70`.

Deprecate later by moving recipe/shim checks to `ValidateZeropsYAML` or a shared schema validator wrapper, then delete `ValidFields`, `FieldError`, `ExtractValidFields`, and legacy tests at `internal/schema/validate_test.go:8-290`.

## 12. Recommended amendments

1. Update plan §3.3, Q7, Phase 2 work-scope line 3, and the anti-pattern text cited above.
2. Fix `variantPromptResponse` at `internal/tools/workflow_export.go:249-252` to describe `variant` as bundle selection only and `services[].mode=NON_HA`.
3. Add handler tests for validation-failed and `bundle.Errors` serialization.
4. Decide and test branch order: validation before git-push setup if blocking YAML errors should be primary; otherwise explicitly document that git-push setup can mask validation-failed status while preview carries errors.
5. Add `ops.ExportBundle.Errors` to `export-validate` `references-fields` at `internal/content/atoms/export-validate.md:7`.
6. Trim at least 47 raw bytes from export atoms even though synthesized-output tests pass.
