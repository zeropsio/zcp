# Slice brief: S5a — Provision rendering: services-only YAML + executable env pre-steps + atom truth fix

Self-contained: no other file is required to execute this. Cite spec §s,
never the plan.

**Outcome** (observable): the recipe-route bootstrap PROVISION guide renders its import YAML services-only — no `project:` key in any fenced YAML block (live-verified defect: guide says "services section ONLY" while rendering `project: {name: ...}`) — and any `project.envVariables` are rendered as EXECUTABLE pre-steps (key AND value, e.g. `zerops_env action="set" scope="project" key="APP_KEY" value="<@generateRandomString(<32>)>"` with the note to expand generator expressions via `zerops_preprocess` first), so generated secrets like Laravel's APP_KEY are not lost. The `bootstrap-recipe-import` atom states the true importer contract.

**Allowed scope**
- Files: `internal/workflow/bootstrap_guide_assembly.go`, `internal/workflow/bootstrap_guide_assembly_test.go`, `internal/content/atoms/bootstrap-recipe-import.md`, its bootstrap atom golden file (locate via `grep -rl "bootstrap-recipe-import" internal/ --include="*.golden"` or the atoms test fixtures)
- Explicitly excluded: `ops/import.go` validation behavior (importer already accepts `project.envVariables` and rejects other project keys — `internal/ops/import.go:53`; do not change it), URL surfacing (S5b), playbooks.

**Spec citations**: `docs/spec-workflows.md` §8 RCO amendment (promoted at GATE 1: provision-step YAML rendered services-only; project.envVariables surfaced as executable pre-steps) · `docs/schema-integration.md` (platform is the sole import validator — add none) · `docs/spec-knowledge-distribution.md` §11 / CLAUDE.md atom rules (atom describes observable orchestration, single-owner).

**Mechanics (verified pointers)**
- `internal/workflow/bootstrap_guide_assembly.go:280` (strip-intent comment), `:303-307` (instruction lines + canonical YAML injection beneath). The discover-step confirm view keeps the full canonical YAML for rename/confirm (per promoted RCO wording); the PROVISION-step rendered YAML is the one that must be services-only.
- Atom `internal/content/atoms/bootstrap-recipe-import.md:14` currently overstates rejection ("imports reject project-level blocks"); truth: `project.envVariables` accepted, all other `project.*` keys rejected (`internal/ops/import.go:113-124` per plan-evidence; verify at implementation time). Fix the atom wording + its golden.

**RED test list**
- `TestBootstrapGuide_ProvisionYAML_ServicesOnly` — layer: unit — for a recipe whose canonical YAML carries `project:` (with envVariables), the rendered provision guide contains NO `project:` key inside any fenced YAML block.
- `TestBootstrapGuide_ProvisionEnvPresteps_ExecutableKV` — layer: unit — the pre-step block carries key AND value (fixture with a generator expression) + the preprocess note.
- Atom golden update — layer: unit (`go test ./internal/content/... -run Atom -short`).

**Protocol**: RED → GREEN → REFACTOR.
1. `go test ./internal/workflow -run TestBootstrapGuide -short -count=1 -v` — RED.
2. Implement; then change-impact ladder: `go test ./internal/workflow/... ./internal/tools/... -short -count=1`, `go test ./integration/ -short -count=1`, `go test ./internal/content/... -short -count=1`.
3. `make lint-fast`.

**Report contract**: RED + GREEN outputs with exit codes · files touched · layer-matrix lines (unit + tool + integration + content) · independent-oracle note (expected YAML fixtures from the live-captured canonical recipe YAML, not recomputed).

**Stop conditions**: scope drift · material unknown · AC change · repeated unexplained failure.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
