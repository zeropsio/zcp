# Audit — service-context decision for `BuildExportEnvelope`

**Step**: Phase 0a.0 of `plans/atom-corpus-verification-2026-05-02.md`.
**Date**: 2026-05-02.
**Question**: Does `BuildExportEnvelope` need to populate ALL project
services in `env.Services` (with an `ExportTarget` discriminator) so atoms
can reason about non-target services, or does single-entry-Services
suffice (one snapshot for the export target only, plus empty for
`scope-prompt` where target is unknown)?

## Method

For each of 6 existing export atoms PLUS 2 planned new atoms, three
questions per the plan §0a.0 decision rule:

1. Does the atom prose reference any service other than the export target
   by name, role (managed dep), or per-service axis (`runtimes:`,
   `serviceStatus:`, etc.)?
2. If YES: which non-target services and why?
3. Does the atom semantically need to fire in `scope-prompt` state where
   target is unknown?

Decision rule:

- **All Q1 = "no non-target reference"** → commit decision is
  **single-entry Services** (default). Phase 0a.6 helper takes one
  hostname + one status; emits `Services=[snapshot]` for the target,
  `Services=[]` for scope-prompt.
- **Any atom requires non-target reasoning** → escalate, do NOT proceed
  to 0a.6 implementation; plan upgrades to full Services + `ExportTarget`
  field on `StateEnvelope`.

## Findings

| Atom | Q1: non-target ref? | Q2: which / why | Q3: fires in scope-prompt? | Notes |
|---|---|---|---|---|
| `export-intro` (existing) | No | — | Yes (universal framing across all export-active statuses; plan default for this atom is no `exportStatus:` filter) | References "managed deps" generically as bundle CONTENT (`${db_*}`/`${redis_*}` resolve at re-import). This is structural description of what the bundle holds, not a per-service axis claim about a specific non-target service. Discusses the runtime list ("If the project has multiple runtime services") but only as workflow narrative — no axis match against another service's snapshot. |
| `export-publish` (existing) | No | — | No (publish-ready only) | Target-focused. References "the destination project" for re-import — that's the destination AFTER bundle ships, not a current sibling service in this project. `failureClassification` cases all describe target-runtime / remote / build state. No per-service axis on a non-target. |
| `export-publish-needs-setup` (existing) | No | — | No (git-push-setup-required only) | Entirely target-focused: `meta.GitPushState`, `meta.RemoteURL`, GIT_TOKEN setup on the target runtime container. Mentions `setup-git-push-container` / `setup-git-push-local` chain atoms — those are environment-scoped (container vs local), not non-target services. |
| `export-classify-envs` (existing) | No | — | No (classify-prompt only) | References `${db_*}`/`${redis_*}` ENV-VAR PATTERNS (managed-service env-name conventions), not specific managed services by hostname. Bucket protocol describes detection signals against the source tree — never against a non-target service's `ServiceSnapshot`. The "Non-default managed-service prefixes" trap (M7) talks about resolving via `zerops_discover` response shape; that's a runtime-time per-call discover, not an atom-axis claim. |
| `export-validate` (existing) | No | — | No (validation-failed; classify-prompt for warning preview, both target-scoped) | Bundle-/preview-/error-focused. `services[].mode` checks are about the BUNDLE rendering for the target, not about live non-target services. M2/M4 discussions reference env keys, not other-service snapshots. |
| `scaffold-zerops-yaml` (existing) | No | — | No (scaffold-required only) | Target's `/var/www/zerops.yaml` only. Discover lookup is on `{targetHostname}`. Migration matrix is runtime-type → setup block, not a per-service-snapshot axis. |
| `export-scope-prompt` (planned, derived from `scopePromptResponse` line 273 of `internal/tools/workflow_export.go`) | No | — | Yes (this atom IS the scope-prompt) | By construction, target is unknown when this atom fires. Inline guidance prose ("Pick the runtime service to export. Pass targetService=<hostname>") references the runtime list as a list, not as per-service axis matches. The atom's own frontmatter axes per the plan: `phases: [export-active], exportStatus: [scope-prompt], priority: 2` — explicitly NO service-scoped axes (modes, runtimes, serviceStatus etc.). Single-entry Services with `Services=[]` (empty) is correct. |
| `export-variant-prompt` (planned, derived from `variantPromptResponse` lines 282-294 of `internal/tools/workflow_export.go`) | No | — | No (variant-prompt only; target IS known by then) | Talks about "dev half / stage half" of a `ModeStandard` / `ModeLocalStage` PAIR. The pair's two halves share a single `ServiceMeta` per `internal/workflow/compute_envelope.go::buildOneSnapshot` (StageHostname is a field on the dev meta) and produce ONE `ServiceSnapshot` keyed by the dev hostname with `StageHostname` populated. So the "stage half" is part of the SAME snapshot, not a sibling non-target snapshot. No non-target reasoning. |

## Decision

**All eight atoms answer Q1 = "no non-target reference".** Single-entry
Services semantics is correct.

`BuildExportEnvelope` populates `env.Services`:

| Status | `Services` | Rationale |
|---|---|---|
| `scope-prompt` (target unknown) | `[]` | UX is asking which service. Atoms for scope-prompt MUST NOT use service-scoped axes (only `phases: [export-active], exportStatus: [scope-prompt]`). |
| Other 6 statuses (target known) | `[snapshot for targetService]` | Service-scoped axes (`runtimes:`, `serviceStatus:`, `closeDeployModes:`, `gitPushStates:`, `buildIntegrations:`, `modes:`) fire on the actual service being exported. |

No `ExportTarget` discriminator field is needed on `StateEnvelope` — the
single Services entry IS the export target by construction.

Phase 0a.6 helper signature is therefore the plan's default form:

```go
type ExportEnvelopeOpts struct {
    Client    platform.Client
    ProjectID string
    StateDir  string
}
func BuildExportEnvelope(targetServiceHostname string, status ExportStatus, opts ExportEnvelopeOpts) (StateEnvelope, error)
func RenderExportGuidance(env StateEnvelope, corpus []KnowledgeAtom) (string, error)
```

## Reviewer notes / out of scope

- The bundle CONTENT (`bundle.importYaml`'s `services:` list) does carry
  managed deps so `${db_*}`/`${redis_*}` resolve at re-import. That's
  payload data composed by `ops.BuildBundle`, not atom-axis filtering;
  atoms describe how the bundle behaves, never assert "service `db` has
  status `RUNNING`" against a sibling snapshot.
- If a future atom needs to reason about non-target services (e.g.
  "warn when a sibling unbootstrapped runtime exists in the same
  project"), this audit must be revisited and the helper signature
  upgraded to the full-Services + `ExportTarget` form. Plan §0a.0
  documents that escalation path.
