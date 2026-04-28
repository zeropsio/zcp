# Codex Round P2 Postwork Architecture Review

## (1) VERDICT

NEEDS-AMENDMENT: Phase 2 is acceptable as a pure composer, but Phase 3 must clarify preview redaction/classification flow, evidence ownership, managed-service inclusion, and RemoteURL freshness boundaries before handler work starts.

## (2) PHASE 3 INTEGRATION ADJUDICATIONS

### 2a. BundleInputs shape for multi-call flow

Current `BundleInputs` is a clean pure-composition input bag: it carries project/runtime identity, live `zerops.yaml`, repo URL, project envs, and managed services, and the comment explicitly assigns Discover/SSH/git-remote reads to the handler before `BuildBundle` runs (`internal/ops/export_bundle.go:59`, `internal/ops/export_bundle.go:62`, `internal/ops/export_bundle.go:81`, `internal/ops/export_bundle.go:85`, `internal/ops/export_bundle.go:89`, `internal/ops/export_bundle.go:93`). `BuildBundle` takes those inputs plus variant and classifications, and it performs no client/context I/O (`internal/ops/export_bundle.go:115`, `internal/ops/export_bundle.go:120`).

For the three-call handler, the shape supports Phase B only after the handler has probed all required fields. `BuildBundle` fail-fast rejects empty target hostname, repo URL, setup name, and service type (`internal/ops/export_bundle.go:125`, `internal/ops/export_bundle.go:128`, `internal/ops/export_bundle.go:131`, `internal/ops/export_bundle.go:134`). That means a second-call preview cannot use `BuildBundle` unless the handler has already read a live repo URL.

When classifications are absent, `BuildBundle` normalizes nil to an empty map (`internal/ops/export_bundle.go:137`) and `composeProjectEnvVariables` treats each missing key as `SecretClassUnset`, emits the live value verbatim, and adds a not-classified warning (`internal/ops/export_bundle.go:266`, `internal/ops/export_bundle.go:284`, `internal/ops/export_bundle.go:285`, `internal/ops/export_bundle.go:286`). Tests pin that unset behavior with `MYSTERY_VAR` emitted as `abc` and a warning (`internal/ops/export_bundle_test.go:188`, `internal/ops/export_bundle_test.go:192`, `internal/ops/export_bundle_test.go:193`, `internal/ops/export_bundle_test.go:196`).

Ruling: keep Phase 2 behavior for internal composition, but amend Phase 3 so the handler does not show raw unclassified `ImportYAML` as the user preview. The review table may show values/evidence under handler-controlled redaction, but YAML preview before acceptance should use redacted placeholders or omit the env block until classifications are supplied.

### 2b. Per-env evidence surfacing

The current structs do not carry evidence. `ProjectEnvVar` has only `Key` and `Value` (`internal/ops/export_bundle.go:99`, `internal/ops/export_bundle.go:100`), `ExportBundle.Classifications` is only a key-to-bucket map (`internal/ops/export_bundle.go:50`, `internal/ops/export_bundle.go:52`), and `BuildBundle` merely echoes that map onto the bundle (`internal/ops/export_bundle.go:150`, `internal/ops/export_bundle.go:158`). Therefore the handler has enough data for Key, Value, and Classification, but not Source/Evidence/Risk/Override.

Ruling: evidence collection belongs in the Phase 3 handler/agent orchestration, not in `BuildBundle`. If the response schema needs persistence of evidence, add a handler-level review-row DTO; do not overload `BundleInputs` unless the composer must use evidence.

### 2c. Cross-service dollar-brace references

The composer applies exactly the supplied bucket: `SecretClassInfrastructure` drops the env, `SecretClassUnset` emits it verbatim with a warning, and there is no parser that auto-detects `${db_hostname}` (`internal/ops/export_bundle.go:267`, `internal/ops/export_bundle.go:269`, `internal/ops/export_bundle.go:270`, `internal/ops/export_bundle.go:284`). Tests show `${db_hostname}` is dropped only when the test supplies `SecretClassInfrastructure` (`internal/ops/export_bundle_test.go:129`, `internal/ops/export_bundle_test.go:131`, `internal/ops/export_bundle_test.go:133`, `internal/ops/export_bundle_test.go:136`).

Ruling: classification remains the agent's job. The handler should surface dollar-brace provenance as evidence, but should not silently auto-classify before calling `BuildBundle`, because that would introduce the hardcoded heuristic layer Phase 2 intentionally avoided.

## (3) PHASE 4 ATOM REFERENCES-FIELDS VERIFICATION

The declared atom paths match the actual `ExportBundle` fields exactly and case-sensitively: `ImportYAML`, `ZeropsYAML`, and `Warnings` are present on `ExportBundle` (`internal/ops/export_bundle.go:29`, `internal/ops/export_bundle.go:30`, `internal/ops/export_bundle.go:31`, `internal/ops/export_bundle.go:32`, `internal/ops/export_bundle.go:36`, `internal/ops/export_bundle.go:53`, `internal/ops/export_bundle.go:56`). The source comment also lists the same three reference paths (`internal/ops/export_bundle.go:23`, `internal/ops/export_bundle.go:24`, `internal/ops/export_bundle.go:25`, `internal/ops/export_bundle.go:26`).

## (4) PHASE 5 READINESS

### 4a. Warnings sufficiency

`Warnings []string` is sufficient for Phase 2-style non-fatal hints: warnings are returned from `composeImportYAML` and assigned directly to the bundle (`internal/ops/export_bundle.go:145`, `internal/ops/export_bundle.go:159`), and current warnings are human-readable strings for empty external secrets, unset classification, and unknown buckets (`internal/ops/export_bundle.go:277`, `internal/ops/export_bundle.go:286`, `internal/ops/export_bundle.go:290`). For Phase 5 schema validation, warnings can carry non-fatal text, but blocking validation should not be squeezed into this field.

### 4b. ExportBundle.Errors absence

`ExportBundle` currently ends with `Warnings []string`; there is no `Errors` field in the struct (`internal/ops/export_bundle.go:29`, `internal/ops/export_bundle.go:53`, `internal/ops/export_bundle.go:56`, `internal/ops/export_bundle.go:57`). The code comment explicitly says errors will be added in Phase 5 (`internal/ops/export_bundle.go:53`, `internal/ops/export_bundle.go:55`).

### 4c. Fail-fast composition errors

Phase 2 `BuildBundle` fail-fast returns errors for missing target hostname, repo URL, setup name, service type, invalid/empty `zerops.yaml`, and missing setup (`internal/ops/export_bundle.go:125`, `internal/ops/export_bundle.go:128`, `internal/ops/export_bundle.go:131`, `internal/ops/export_bundle.go:134`, `internal/ops/export_bundle.go:141`, `internal/ops/export_bundle.go:142`). Tests pin the missing repo URL chain wording and other composition errors (`internal/ops/export_bundle_test.go:538`, `internal/ops/export_bundle_test.go:560`, `internal/ops/export_bundle_test.go:563`, `internal/ops/export_bundle_test.go:576`, `internal/ops/export_bundle_test.go:583`). Ruling: acceptable for Phase 2; Phase 5 should add accumulated schema `Errors` without weakening these prerequisite errors.

## (5) PHASE 6 READINESS

`BuildBundle` consumes `inputs.RepoURL` as-is: it rejects empty values, writes the same value to the runtime `buildFromGit`, and echoes it to `ExportBundle.RepoURL` (`internal/ops/export_bundle.go:128`, `internal/ops/export_bundle.go:206`, `internal/ops/export_bundle.go:210`, `internal/ops/export_bundle.go:154`). There is no cache lookup or freshness check in `BuildBundle`; comments assign live `git remote` resolution to Phase A/handler work (`internal/ops/export_bundle.go:85`, `internal/ops/export_bundle.go:115`, `internal/ops/export_bundle.go:117`). Ruling: freshness belongs to the handler or Phase 6 helper, not `BuildBundle`.

## (6) CLASSIFICATIONS FIELD SHAPE

Current shape is `map[string]topology.SecretClassification` on both `ExportBundle` and `BuildBundle` input (`internal/ops/export_bundle.go:50`, `internal/ops/export_bundle.go:52`, `internal/ops/export_bundle.go:120`, `internal/ops/export_bundle.go:123`). That is sufficient for composition because `composeProjectEnvVariables` only needs key-to-bucket lookup (`internal/ops/export_bundle.go:259`, `internal/ops/export_bundle.go:267`).

It is not sufficient as the review-table model. The Phase 3 table needs Key, Bucket, Evidence, Emit, Risk, and Override, while the map carries only Key and Bucket and loses stable row ordering. Ruling: keep the map at the `BuildBundle` boundary, and add a separate handler response slice such as `[]EnvClassificationReviewRow`; do not replace the composer input with a heavier slice unless `BuildBundle` starts consuming evidence.

## (7) MANAGED-SERVICE INCLUSION RULING

Ruling: (d) handler decides and `BuildBundle` accepts whatever it receives. Code already implements this boundary: `BundleInputs.ManagedServices []ManagedServiceEntry` is optional (`internal/ops/export_bundle.go:93`, `internal/ops/export_bundle.go:96`), `composeImportYAML` allocates space for one runtime plus provided managed services, appends the runtime first, then appends each managed service with hostname/type/priority/mode (`internal/ops/export_bundle.go:217`, `internal/ops/export_bundle.go:218`, `internal/ops/export_bundle.go:219`, `internal/ops/export_bundle.go:220`, `internal/ops/export_bundle.go:228`). Tests cover both one-service output when the slice is empty and runtime-plus-managed output when the slice is populated (`internal/ops/export_bundle_test.go:337`, `internal/ops/export_bundle_test.go:365`, `internal/ops/export_bundle_test.go:366`, `internal/ops/export_bundle_test.go:384`, `internal/ops/export_bundle_test.go:395`, `internal/ops/export_bundle_test.go:408`).

## (8) RECOMMENDED PLAN AMENDMENTS

### 8a. Phase 2 retrospective

- Acknowledge `BundleInputs.ManagedServices []ManagedServiceEntry` as an intentional Phase 2 addition; the composer supports optional managed-service emission supplied by the handler (`internal/ops/export_bundle.go:93`, `internal/ops/export_bundle.go:96`).
- Record the implementation name `composeProjectEnvVariables`, not the planned `composeServiceEnvVariables`; the function classifies project-level env vars from `ProjectEnvs` (`internal/ops/export_bundle.go:254`, `internal/ops/export_bundle.go:259`, `internal/ops/export_bundle.go:260`).
- Record `verifyZeropsYAMLSetup` as a pure parser/validator over an already-read body, not an SSH fetcher (`internal/ops/export_bundle.go:163`, `internal/ops/export_bundle.go:168`, `internal/ops/export_bundle.go:172`).

### 8b. Phase 3 clarification

- State that the handler prepares `BundleInputs`: Discover-derived service metadata, SSH-read `zerops.yaml`, project env snapshot, managed-service discovery, and live repo URL. `BuildBundle` remains pure composition with no further I/O (`internal/ops/export_bundle.go:59`, `internal/ops/export_bundle.go:115`, `internal/ops/export_bundle.go:117`).
- State that second-call classification preview must not expose raw unclassified YAML produced by `SecretClassUnset`; handler preview/redaction owns that response (`internal/ops/export_bundle.go:284`, `internal/ops/export_bundle.go:285`, `internal/ops/export_bundle.go:286`).

### 8c. Other drift to acknowledge

- Phase 2 did not implement `scrubCorePackageDefaults`; current import service entries include hostname/type/mode/buildFromGit/zeropsSetup/subdomain plus managed services only (`internal/ops/export_bundle.go:206`, `internal/ops/export_bundle.go:213`, `internal/ops/export_bundle.go:217`).
- Phase 2 makes empty `RepoURL` a hard composition error, so Phase 3 must decide whether preview requires live remote resolution or uses a handler placeholder path before publish (`internal/ops/export_bundle.go:128`, `internal/ops/export_bundle.go:129`).

## (9) EFFECTIVE VERDICT

NEEDS-AMENDMENT before Phase 3 starts.

1. Define handler-owned review rows for Evidence/Risk/Override; `BuildBundle` only accepts key-to-bucket classifications (`internal/ops/export_bundle.go:50`, `internal/ops/export_bundle.go:52`, `internal/ops/export_bundle.go:259`, `internal/ops/export_bundle.go:267`).
2. Define preview redaction/placeholder behavior for unclassified envs; current composer emits verbatim values with warnings (`internal/ops/export_bundle.go:284`, `internal/ops/export_bundle.go:285`, `internal/ops/export_bundle.go:286`).
3. Confirm managed-service policy as handler-decides; the composer already accepts zero or more managed services (`internal/ops/export_bundle.go:93`, `internal/ops/export_bundle.go:217`, `internal/ops/export_bundle.go:219`).
4. Move RemoteURL freshness into Phase 3/6 handler helpers; `BuildBundle` only consumes `inputs.RepoURL` as supplied (`internal/ops/export_bundle.go:85`, `internal/ops/export_bundle.go:128`, `internal/ops/export_bundle.go:154`).
