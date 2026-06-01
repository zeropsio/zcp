# Plan-schema authoring friction (classic/recipe routes)

**Surfaced**: 2026-06-01 — live Codex transcript adopting `appdev`/`appstage` for a
Laravel dashboard. The agent took ~14 consecutive `complete step="discover"` validation
failures fuzzing the `plan` shape. The adopt half was fixed by auto-deriving the plan
(`plans/adopt-route-auto-derive-plan-2026-06-01.md`); this entry tracks the residual
friction that still bites the routes where the agent genuinely MUST author a plan
(classic, recipe-override).

**Why deferred**: the adopt route — the only one in the transcript — no longer requires
hand-authoring, so the reported pain is resolved. F1–F3 below are a SEPARATE root cause
(the MCP SDK validates the generated JSON schema before the Go handler runs) with wider
blast radius (touches every plan-authoring route + the schema-generation seam). Folding
it into the adopt fix would have widened that change well past its phase boundary.

**Trigger to promote**: the next classic/recipe `flow-eval` (or a live transcript) that
shows an agent fuzzing the `plan` shape — repeated `unexpected additional properties` /
`required: missing` rounds — on a route where the plan can't be auto-derived.

## Sketch — three independent sub-fixes

- **F1 — the flatten diagnostic is structurally unreachable.** `BootstrapTarget.UnmarshalJSON`
  (`internal/workflow/validate.go:58`) returns a paste-and-resend "flattened fields must
  nest inside runtime" message, but the SDK rejects unknown top-level keys with a terse
  `unexpected additional properties` BEFORE the handler unmarshals (same mechanism that
  rejected `launchKey` pre-F1, pinned by `workflow_schema_test.go:108`). Options: (a) relax
  `additionalProperties` on `BootstrapTarget` so `UnmarshalJSON` runs and emits the good
  diagnostic; (b) accept + normalize the flat shape at the boundary; (c) inject the
  corrected-shape hint into the SDK error. Pick after probing how the go-sdk surfaces
  `additionalProperties:false` overrides.
- **F2 — nested fields carry no schema descriptions.** `RuntimeTarget`/`Dependency` struct
  fields have no `jsonschema` tags, so the generated schema gives the agent property names
  with zero inline guidance; only the giant top-level `Plan` description documents the
  shape (`internal/tools/workflow.go:51`), and it evidently didn't reach the agent. Add
  per-field `jsonschema` tags (devHostname, type, bootstrapMode enum, stageHostname,
  isExisting, resolution enum).
- **F3 — route vocabulary ≠ schema vocabulary.** Agents reflexively send
  `bootstrapMode:"adopt"` (no such mode) and `resolution:"EXISTING"` (must be `EXISTS`).
  `resolution` is already upper-cased (`validate.go:364`) but `EXISTING`≠`EXISTS` is a word
  mismatch, not a case one. Consider a small synonym-normalization + a clearer enum error.

## Risks

- F1 option (a) loosens a structural guard — must keep the field-typo catch some other way
  (the `UnmarshalJSON` probe already enumerates known runtime keys; extend it to reject
  genuinely-unknown keys with the same actionable message).
- F2 enlarges the generated schema; check the MCP tool-list size budget.

## Refs

- Adopt fix that resolved the reported case: `plans/adopt-route-auto-derive-plan-2026-06-01.md`
- SDK-validates-before-handler precedent: `internal/tools/workflow_schema_test.go`
</content>
