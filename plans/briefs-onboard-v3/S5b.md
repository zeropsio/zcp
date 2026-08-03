# Slice brief: S5b — Structured runtime URLs on the bootstrap response, resolved at L4

Self-contained: no other file is required to execute this. Cite spec §s,
never the plan.

**Outcome** (observable): after a recipe provision, the bootstrap response carries a STRUCTURED runtime-URL collection — per subdomain-enabled service: `{hostname, role (dev|stage|other), url, handoff (bool)}` — present on (a) the successful post-provision/close response, (b) `action="status"` while close remains active, (c) the terminal close response. The close/status Markdown guidance is DERIVED from this data (stage URL named as the handoff), never hand-composed elsewhere. URL resolution failure is best-effort: the collection omits the failed entry and the guidance says the URL could not be resolved yet — never a fabricated URL, never a hard error that blocks close.

**Allowed scope**
- Files: `internal/workflow/bootstrap.go` (add the collection to `BootstrapResponse` — around :60), `internal/workflow/bootstrap_guide_assembly.go` (render guidance FROM the collection), `internal/workflow/bootstrap_guide_assembly_test.go`, `internal/tools/workflow_bootstrap.go` (populate the collection at L4 via `ops.ResolveSubdomainURL` — around :217), its tool tests, one integration test file under `integration/`
- Explicitly excluded: `internal/ops/verify_checks.go` (reuse `ResolveSubdomainURL`, don't modify), e2e (S6), playbooks.

**HARD LAYERING RULE** (depguard + `internal/topology/architecture_test.go`): `internal/workflow` MUST NOT import `internal/ops` — they are L3 peers. The workflow layer defines the STRUCT and renders from it; the TOOLS layer (L4) resolves URLs via ops and populates the struct. If you find yourself importing ops in workflow, stop — the seam is wrong.

**Spec citations**: `docs/spec-workflows.md` §8 RCO amendment (promoted at GATE 1: post-provision/status/close responses carry composed subdomain URLs, stage = handoff; service-level subdomain fields are null even when enabled — compose from project-level `zeropsSubdomainHost`) · `docs/spec-architecture.md` (layer map) · O6 wording: no NEW onboarding-specific tool/action/state — this amends the existing bootstrap workflow response, which is in-contract under RCO.

**RED test list**
- `TestBootstrapResponse_RuntimeURLs_StructPresence` — layer: unit — struct present on post-provision, status-during-close, terminal close; absent/empty pre-provision.
- `TestBootstrapGuide_CloseGuidance_RendersFromRuntimeURLs` — layer: unit — guidance derives from the collection; stage entry marked handoff; dev never presented as the app URL.
- `TestWorkflowBootstrap_PopulatesRuntimeURLs` — layer: tool — L4 populates via ops resolver (mock client returning project `zeropsSubdomainHost`); failure path: resolver error → entry omitted + guidance note, close not blocked.
- One integration case exercising status-during-close carrying the URLs — layer: integration.

**Protocol**: RED → GREEN → REFACTOR.
1. `go test ./internal/workflow -run 'TestBootstrapResponse_RuntimeURLs|TestBootstrapGuide_CloseGuidance' -short -count=1 -v` and `go test ./internal/tools -run TestWorkflowBootstrap_PopulatesRuntimeURLs -short -count=1 -v` — RED (missing-symbol RED is acceptable for the new struct).
2. Implement; full ladder: `go test ./internal/workflow/... ./internal/tools/... -short -count=1`, `go test ./integration/ -short -count=1`.
3. `make lint-fast` (depguard proves the layering); REFACTOR.

**Report contract**: RED + GREEN outputs with exit codes · files touched · layer-matrix lines (unit + tool + integration) · independent-oracle note (expected URL literal from the live observation `https://tmponb3astage-24cb-3000.prg1.zerops.app` — project host `24cb`, port 3000 — not recomputed via the implementation).

**Stop conditions**: scope drift · material unknown · AC change · repeated unexplained failure · any need to import ops from workflow (seam violation — halt).

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass with `-count=1 -v`
- [ ] `make lint-fast` clean (incl. depguard)
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
