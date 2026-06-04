# Export workflow: compose-only terminal state + scaling fidelity

**Source:** Wave-6 flow-eval `export-buildfromgit-self-snapshot` (2026-06-04). Scenario `task_completed=yes` (the agent worked around both via a manual file write), so these are improvements, not blockers — deferred from the gate-fix turn because they're meatier than the 3 fixes shipped that turn (gate deletion, healthCheck atom, out-of-scope filter).

## Finding 1 — no compose-only terminal state (medium confidence, workflow handler)

The export workflow gates the whole flow behind `GitPushState=configured` + a working PAT, but the canonical user goal — "give me the re-importable `import.yaml` + `zerops.yaml` pair" — is the GENERATION output (Phase A+B), not the publish. The agent's THINKING noted the bundle was already composed but it had no sanctioned way to deliver it without git-push-setup, so it fell back to writing the files by hand.

**Fix sketch:** add a compose-only / no-publish terminal status to the export workflow — once Phase A+B validate clean, return a `publish-ready`-equivalent that writes the two files to the repo working dir (or returns them in the response) WITHOUT requiring git-push-setup + PAT. Keep the publish path for when the user does want a PR. Persona declined the unauthorized-push prerequisite, so the compose-only path is the common case.

**Risk:** export is `zerops_workflow workflow="export"` (bootstrap-core, NOT Aleš's recipe-authoring scope). Touches the export handler's terminal-state machine + the response shape (`publish-ready` / `validation-failed` → add `compose-ready`). Pinned tests `TestHandleExport_*` will need a new case.

## Finding 2 — composer drops live verticalAutoscaling/scaling config (medium confidence, ops composer)

The export bundle composer does not source per-service scaling configuration (`verticalAutoscaling` / min-max RAM) from the live service state into the generated `import.yaml`. The validation error correctly rejected a hand-edited value, but the self-snapshot silently loses the deployed scaling shape — a re-import won't reproduce the running config. This is a fidelity bug (the self-snapshot is supposed to reproduce the deployed shape).

**Fix sketch:** the composer reads each bundled service's live scaling config from the platform and emits it at the `import.yaml` service level. At minimum, surface a warning when scaling config is present live but omitted from the bundle. Verify the exact import.yaml scaling field names against the live import schema before emitting (schema is the client-side source of truth; platform validates on re-import).

**Risk:** `ops` export bundle composer (`TestBuildBundle_*` / `TestHandleExport_*`). Must verify the scaling field shape against the live import JSON schema.

## Appendix — lower-confidence Wave-6 items (not yet triaged to fix/drop)

- **develop-active over-prescribes a single mandatory cadence** (medium): close-mode=auto → deploy+verify-every-half is framed as the only path; no fast-path for a transient edit-then-handoff session (export validation fix forced a redundant full deploy+verify). Cross-scenario theme with the out-of-scope leak — both are "develop-active guidance not trimmed to the actual session shape."
- **route-menu dumps full multi-service importYaml of over-provisioning recipes at decision time** (low): the reject/pick decision is made on `fit`/`fitExtras`/`why`; the full YAML (~3300 chars) is validation-set-as-presentation-set. Emit full importYaml only for the best-fit option; defer the rest to recipe-route commit.
- **enableSubdomainAccess two-owner** (low): provision runtime-property table tells the agent to author `enableSubdomainAccess=true` while the deploy handler auto-enables (CLAUDE.md "Subdomain L7 activation is the deploy handler's concern"). Pick one owner — drop it from the provision table (cleaner, matches the invariant).
- **bootstrap discover/provision "always confirm with the user"** (low): blanket imperative with no carve-out for the trivial single-service no-ambiguity case. Scope it to genuine ambiguity.
- **"don't skip to edits before first deploy" wording** (low): superficially contradicts the adjacent "scaffold real code" step. Reword to target probing not authoring.
