package recipe

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Gate is a mechanical check — file existence, marker form, JSON shape,
// citation timestamp, writer-owned-path authorship. Gates do not judge
// prose quality (that's the editorial-review step); they only attest
// structural invariants.
type Gate struct {
	Name string
	Run  func(ctx GateContext) []Violation
}

// GateContext carries the data gates need: the plan, the recipe output
// tree root on disk, an optional facts-log, and optional parent recipe.
type GateContext struct {
	Plan       *Plan
	OutputRoot string
	FactsLog   *FactsLog
	Parent     *ParentRecipe
}

// RunGates runs every gate and collects violations. Violations do not
// abort — the caller decides whether to block a phase transition.
func RunGates(gates []Gate, ctx GateContext) []Violation {
	out := make([]Violation, 0, len(gates))
	for _, g := range gates {
		out = append(out, g.Run(ctx)...)
	}
	return out
}

// DefaultGates returns the mechanical gate set that runs at every phase
// close. Phase-specific gates (research classification, finalize file
// presence) are added by gatesForPhase on top of this base.
func DefaultGates() []Gate {
	return []Gate{
		{Name: "citations-timestamped", Run: gateCitationsTimestamped},
		{Name: "fact-required-fields", Run: gateFactsValid},
	}
}

// CodebaseScaffoldGates runs at scaffold + feature complete-phase.
// Run-17 §8 (R-16-1 closure) — content-surface validators are NOT
// included here; they run at codebase-content complete-phase via
// CodebaseContentGates. The scaffold/feature sub-agent records facts
// only; authoring IG/KB/zerops.yaml-comments/CLAUDE.md is strictly
// the codebase-content sub-agent's job. Source-comment voice still
// fires here because committed source comments are scaffold-owned
// (the scaffold agent SSH-edits the codebase).
func CodebaseScaffoldGates() []Gate {
	return []Gate{
		{Name: "facts-recorded", Run: gateFactsRecorded},
		{Name: "engine-shells-filled", Run: gateEngineShellsFilled},
		{Name: "source-comment-voice", Run: gateSourceCommentVoice},
		// Run-20 C3 — bare-yaml prohibition enforcement. Refuses scaffold
		// complete-phase when any committed codebase zerops.yaml carries
		// `^\s+# ` causal comments (carve-outs: shebang, trailing data-
		// line comments). Closes the run-19 leakage that hit the engine
		// stitch path's strip-then-inject contract.
		{Name: "scaffold-bare-yaml", Run: gateScaffoldBareYAML},
		// Run-20 C5 — facts-rationale completeness. Refuses scaffold/
		// feature complete-phase when any directive group in the
		// committed yaml lacks an attesting `field_rationale` fact. The
		// agent records one fact per directive group at scaffold/feature
		// time so the codebase-content sub-agent has the rationale stream
		// it needs to author yaml-comment fragments.
		{Name: "fact-rationale-completeness", Run: gateFactRationaleCompleteness},
		// Run-20 C4 — worker dev-server attestation. Refuses scaffold
		// complete-phase when a dev codebase whose start is `zsc noop
		// --silent` lacks a `worker_dev_server_started` fact. Bypass
		// via `worker_no_dev_server` for one-shot batch codebases.
		// Closes the run-19 scaffold-worker gap (zero MCP zerops_dev_server
		// invocations on workerdev — worker behavior was attested only on
		// the compiled-entry workerstage path).
		{Name: "worker-dev-server-started", Run: gateWorkerDevServerStarted},
		// Run-21-prep RC2 — schema-conformance at the producer. Without
		// this, scaffold can ship a yaml with fields invalid under the
		// live zerops-yml schema (e.g. `verticalAutoscaling` placed
		// inside `run:`, when it's an import.yaml service-level field).
		// Pre-fix the violation only surfaced at codebase-content + finalize,
		// long after the agent that authored the bad shape had moved on.
		// Catching at scaffold complete-phase puts the refusal in the
		// authoring agent's same-context window where the fix is cheap.
		{Name: "zerops-yaml-schema", Run: gateZeropsYamlSchema},
	}
}

// CodebaseContentGates runs at codebase-content complete-phase. Owns
// content-surface validation now that codebase-content is the sole
// content-authoring phase for codebase-scoped surfaces (IG, KB,
// CLAUDE, zerops.yaml comments). Cross-surface + cross-recipe
// duplication checks fire here too — both span only codebase
// surfaces and are meaningful once the content phase has authored
// them.
func CodebaseContentGates() []Gate {
	return []Gate{
		{Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators},
		{Name: "deploy-files-narrowness", Run: gateDeployFilesNarrowness},
		{Name: "cross-surface-duplication", Run: gateCrossSurfaceDuplication},
		{Name: "cross-recipe-duplication", Run: gateCrossRecipeDuplication},
		{Name: "zerops-yaml-schema", Run: gateZeropsYamlSchema},
	}
}

// CodebaseGates is retained for callers that pre-date the run-17 split.
// Returns the union of CodebaseScaffoldGates + CodebaseContentGates so
// existing tests and back-compat consumers keep producing the same
// result. New code uses the per-phase variants directly. Deleted in
// run-18 cleanup once the back-compat callers are migrated.
func CodebaseGates() []Gate {
	out := CodebaseScaffoldGates()
	out = append(out, CodebaseContentGates()...)
	return out
}

// gateFactsRecorded — every codebase has at least one porter_change
// or field_rationale fact in scope. Run-17 §8 — catches the scaffold-
// skip-to-finalize case where the agent never recorded anything for a
// codebase and the codebase-content sub-agent has no fact stream to
// synthesize from. Notice severity (not blocking) — some codebases
// genuinely have no platform-forced changes and the agent legitimately
// records zero facts.
func gateFactsRecorded(ctx GateContext) []Violation {
	if ctx.FactsLog == nil || ctx.Plan == nil {
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	for _, f := range records {
		if f.Kind != FactKindPorterChange && f.Kind != FactKindFieldRationale {
			continue
		}
		host := f.Scope
		if i := strings.IndexByte(host, '/'); i > 0 {
			host = host[:i]
		}
		if host != "" {
			seen[host] = true
		}
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		if !seen[cb.Hostname] {
			out = append(out, Violation{
				Code:     "codebase-no-facts-recorded",
				Path:     cb.Hostname,
				Severity: SeverityNotice,
				Message:  fmt.Sprintf("codebase/%s recorded no porter_change or field_rationale facts during scaffold/feature; codebase-content sub-agent will have no fact stream to synthesize from", cb.Hostname),
			})
		}
	}
	return out
}

// gateEngineShellsFilled — Run-17 §6 retracted Class B/C/per-service
// engine-emit shells, so the gate has nothing to enforce. Kept as a
// no-op so the registered name stays referenceable from future
// tranches without re-adding the entry to CodebaseScaffoldGates.
// Deleted in run-18 cleanup along with the back-compat CodebaseGates
// shim.
func gateEngineShellsFilled(_ GateContext) []Violation {
	return nil
}

func gateCrossSurfaceDuplication(ctx GateContext) []Violation {
	return validateCrossSurfaceDuplication(context.Background(), ctx.Plan)
}

func gateCrossRecipeDuplication(ctx GateContext) []Violation {
	return validateCrossRecipeDuplication(context.Background(), ctx.Plan, ctx.Parent)
}

// EnvGates returns the gate set that runs only at finalize close —
// finalize is the only phase that authors root + env surfaces. Adds
// env-imports-present mechanical check.
func EnvGates() []Gate {
	return []Gate{
		{Name: "env-imports-present", Run: gateEnvImportsPresent},
		{Name: "env-surface-validators", Run: gateEnvSurfaceValidators},
	}
}

// FinalizeGates is preserved as a convenience for callers that want
// the union of CodebaseGates + EnvGates at finalize close. Run-12 §G
// — finalize re-runs codebase gates (catches feature appends) plus
// the env gates only finalize cares about.
func FinalizeGates() []Gate {
	out := CodebaseGates()
	return append(out, EnvGates()...)
}

// codebaseSurfaceKinds — surfaces validated at scaffold + feature
// complete-phase.
var codebaseSurfaceKinds = []Surface{
	SurfaceCodebaseIG,
	SurfaceCodebaseKB,
	SurfaceCodebaseCLAUDE,
	SurfaceCodebaseZeropsComments,
}

// envSurfaceKinds — surfaces validated only at finalize close.
var envSurfaceKinds = []Surface{
	SurfaceRootREADME,
	SurfaceEnvREADME,
	SurfaceEnvImportComments,
}

// gateCodebaseSurfaceValidators runs validators for the codebase-
// scoped surface set against the assembler's just-rendered fragment
// bodies (Cluster A.1 — R-13-1). Disk fall-through preserved for
// codebase zerops.yaml, which the sub-agent ssh-edits in place rather
// than authoring through Plan.Fragments.
//
// I/O boundary: bodies derive from in-process state (Plan.Fragments,
// templates embedded in the binary). No filesystem coherence dependency
// for fragment-backed surfaces.
func gateCodebaseSurfaceValidators(ctx GateContext) []Violation {
	bodies, err := collectCodebaseBodies(ctx.Plan)
	if err != nil {
		return []Violation{{Code: "validator-prep-failed", Message: err.Error()}}
	}
	return runSurfaceValidatorsForKinds(ctx, codebaseSurfaceKinds, bodies)
}

// gateEnvSurfaceValidators runs validators for the root + env surface
// set, plus the cross-surface uniqueness check which needs the full
// stitched corpus to be meaningful. Kept distinct so finalize is the
// only phase emitting these violations (root + env are finalize-
// authored; the uniqueness check spans every surface).
//
// I/O boundary: bodies for fragment-backed + emitter-deterministic
// surfaces flow from in-process state (Cluster A.1 symmetric extension
// per plan §7 open question 1). Cross-surface uniqueness consumes the
// union of env + codebase bodies; only codebase zerops.yaml retains
// the disk read because the sub-agent ssh-edits it directly.
func gateEnvSurfaceValidators(ctx GateContext) []Violation {
	envBodies, err := collectEnvBodies(ctx.Plan, ctx.OutputRoot)
	if err != nil {
		return []Violation{{Code: "validator-prep-failed", Message: err.Error()}}
	}
	out := runSurfaceValidatorsForKinds(ctx, envSurfaceKinds, envBodies)
	cbBodies, err := collectCodebaseBodies(ctx.Plan)
	if err != nil {
		return append(out, Violation{Code: "validator-prep-failed", Message: err.Error()})
	}
	union := make(map[string]string, len(envBodies)+len(cbBodies))
	maps.Copy(union, envBodies)
	maps.Copy(union, cbBodies)
	out = append(out, runCrossSurfaceUniqueness(ctx, union)...)
	return out
}

// runCrossSurfaceUniqueness applies the cross-surface duplication
// check against bodies — preferring the in-memory map and falling
// back to disk for surfaces not in the map (codebase zerops.yaml).
// Only meaningful at finalize close, when the full corpus is in hand.
func runCrossSurfaceUniqueness(ctx GateContext, bodies map[string]string) []Violation {
	var facts []FactRecord
	if ctx.FactsLog != nil {
		if all, err := ctx.FactsLog.Read(); err == nil {
			facts, _ = ClassifyLog(all)
		}
	}
	surfaces := map[string]string{}
	for _, s := range Surfaces() {
		for _, p := range resolveSurfacePaths(ctx.OutputRoot, s, ctx.Plan) {
			if body, ok := bodies[p]; ok {
				surfaces[filepath.Base(p)] = body
				continue
			}
			body, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			surfaces[filepath.Base(p)] = string(body)
		}
	}
	return validateCrossSurfaceUniqueness(surfaces, facts)
}

// runSurfaceValidatorsForKinds runs the registered ValidateFn for each
// of the given surface kinds. Bodies are sourced from `bodies[path]`
// when present and from disk otherwise — the in-memory path closes
// the SSHFS write-back race (R-13-1) for fragment-backed surfaces while
// preserving the disk read for surfaces the sub-agent owns directly
// (codebase zerops.yaml). Missing-on-disk-and-not-in-bodies skips
// silently — codebase surfaces during early scaffold close may not
// have stitched yet.
//
// I/O boundary: callers compute `bodies` from in-process state
// (Plan.Fragments + templates). Disk read is reserved for surfaces NOT
// in bodies; those are by-construction non-stitch-race-prone.
func runSurfaceValidatorsForKinds(ctx GateContext, kinds []Surface, bodies map[string]string) []Violation {
	var facts []FactRecord
	if ctx.FactsLog != nil {
		if all, err := ctx.FactsLog.Read(); err == nil {
			facts, _ = ClassifyLog(all)
		}
	}
	inputs := SurfaceInputs{Plan: ctx.Plan, Facts: facts, Parent: ctx.Parent}
	var violations []Violation
	for _, s := range kinds {
		fn := ValidatorFor(s)
		if fn == nil {
			continue
		}
		paths := resolveSurfacePaths(ctx.OutputRoot, s, ctx.Plan)
		for _, p := range paths {
			var content []byte
			if body, ok := bodies[p]; ok {
				content = []byte(body)
			} else {
				disk, err := os.ReadFile(p)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}
					violations = append(violations, Violation{
						Code: "validator-read-failed", Path: p, Message: err.Error(),
					})
					continue
				}
				content = disk
			}
			vs, err := fn(context.Background(), p, content, inputs)
			if err != nil {
				violations = append(violations, Violation{
					Code: "validator-error", Path: p, Message: err.Error(),
				})
				continue
			}
			violations = append(violations, vs...)
		}
	}
	return violations
}

// collectCodebaseBodies returns an in-memory body map keyed by the
// on-disk path each fragment-backed codebase surface (IG, KB,
// CLAUDE.md) renders to. Skips codebases without a SourceRoot —
// chain-parent codebases or pre-scaffold states — so the validator's
// disk fall-through still applies for those. zerops.yaml is omitted
// intentionally: the sub-agent ssh-edits it; no fragment-side
// stitch-race exists for it (validator falls through to disk).
func collectCodebaseBodies(plan *Plan) (map[string]string, error) {
	bodies := map[string]string{}
	if plan == nil {
		return bodies, nil
	}
	for _, cb := range plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		readme, _, err := AssembleCodebaseREADME(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble %s README: %w", cb.Hostname, err)
		}
		bodies[filepath.Join(cb.SourceRoot, "README.md")] = readme
		claude, _, err := AssembleCodebaseClaudeMD(plan, cb.Hostname)
		if err != nil {
			return nil, fmt.Errorf("assemble %s CLAUDE.md: %w", cb.Hostname, err)
		}
		bodies[filepath.Join(cb.SourceRoot, "CLAUDE.md")] = claude
	}
	return bodies, nil
}

// collectEnvBodies returns an in-memory body map keyed by the on-disk
// path each root + env surface renders to. Covers root README,
// per-tier README, and per-tier import.yaml (the deliverable yaml
// produced by EmitDeliverableYAML). Symmetric extension of Cluster A.1
// per plan §7 open question 1: prefer the in-memory body uniformly so
// validator inputs derive from the same Plan that the deliverable
// derives from.
func collectEnvBodies(plan *Plan, outputRoot string) (map[string]string, error) {
	bodies := map[string]string{}
	if plan == nil {
		return bodies, nil
	}
	rootBody, _, err := AssembleRootREADME(plan)
	if err != nil {
		return nil, fmt.Errorf("assemble root README: %w", err)
	}
	bodies[filepath.Join(outputRoot, "README.md")] = rootBody
	for i := range Tiers() {
		envBody, _, err := AssembleEnvREADME(plan, i)
		if err != nil {
			return nil, fmt.Errorf("assemble env/%d README: %w", i, err)
		}
		tier, _ := TierAt(i)
		bodies[filepath.Join(outputRoot, tier.Folder, "README.md")] = envBody
		yaml, err := EmitDeliverableYAML(plan, i)
		if err != nil {
			return nil, fmt.Errorf("emit tier %d import.yaml: %w", i, err)
		}
		bodies[filepath.Join(outputRoot, tier.Folder, "import.yaml")] = yaml
	}
	return bodies, nil
}

// gateSourceCommentVoice walks every codebase's SourceRoot and flags
// authoring-phase references inside committed source-code comments.
// Skips codebases whose SourceRoot is empty or missing — a
// chain-parent codebase, or a codebase whose scaffold never ran.
func gateSourceCommentVoice(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		if info, err := os.Stat(cb.SourceRoot); err != nil || !info.IsDir() {
			continue
		}
		vs, err := scanSourceCommentsAt(cb.SourceRoot)
		if err != nil {
			out = append(out, Violation{
				Code: "source-comment-scan-failed", Path: cb.SourceRoot, Message: err.Error(),
			})
			continue
		}
		out = append(out, vs...)
	}
	return out
}

// gateEnvImportsPresent — every tier must have an import.yaml file in the
// output tree.
func gateEnvImportsPresent(ctx GateContext) []Violation {
	var out []Violation
	for i, tier := range Tiers() {
		path := filepath.Join(ctx.OutputRoot, tier.Folder, "import.yaml")
		if _, err := os.Stat(path); err != nil {
			out = append(out, Violation{
				Code:    "env-import-missing",
				Path:    path,
				Message: fmt.Sprintf("tier %d: import.yaml not found", i),
			})
		}
	}
	return out
}

// gateCitationsTimestamped — every fact with a citation MUST have a
// RecordedAt in RFC3339 form, so downstream analysis can order facts
// chronologically.
func gateCitationsTimestamped(ctx GateContext) []Violation {
	if ctx.FactsLog == nil {
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		return []Violation{{Code: "facts-read-failure", Message: err.Error()}}
	}
	var out []Violation
	for i, r := range records {
		if r.Citation == "" {
			continue
		}
		if r.RecordedAt == "" {
			out = append(out, Violation{
				Code:    "citation-missing-timestamp",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: "citation present but recorded_at empty",
			})
			continue
		}
		if _, err := time.Parse(time.RFC3339, r.RecordedAt); err != nil {
			out = append(out, Violation{
				Code:    "citation-bad-timestamp",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: err.Error(),
			})
		}
	}
	return out
}

// gateFactsValid — every fact record round-trips its own Validate(). Any
// fact missing a required field is a writer-brief routing risk.
func gateFactsValid(ctx GateContext) []Violation {
	if ctx.FactsLog == nil {
		return nil
	}
	records, err := ctx.FactsLog.Read()
	if err != nil {
		return []Violation{{Code: "facts-read-failure", Message: err.Error()}}
	}
	var out []Violation
	for i, r := range records {
		if err := r.Validate(); err != nil {
			out = append(out, Violation{
				Code:    "fact-invalid",
				Path:    fmt.Sprintf("facts[%d]", i),
				Message: err.Error(),
			})
		}
	}
	return out
}

// MainAgentRewroteWriterPath reports whether the main agent edited a path
// that belongs to a writer-owned surface. Inputs: the file's recorded
// author, the path, and the registry. Returns a Violation if true.
// Rule: writer-owned paths are locked at the engine boundary; any edit
// by the main agent after writer completion is a violation.
func MainAgentRewroteWriterPath(path, author string) *Violation {
	if author != "main" {
		return nil
	}
	for _, s := range Surfaces() {
		c, _ := ContractFor(s)
		if c.Author != AuthorWriter {
			continue
		}
		for _, pat := range c.Owns {
			if matchOwnedPath(pat, path) {
				return &Violation{
					Code:    "main-agent-rewrote-writer-path",
					Path:    path,
					Message: fmt.Sprintf("surface %q is writer-owned", s),
				}
			}
		}
	}
	return nil
}

// matchOwnedPath does a simple glob-style check. Supports `*/foo.md` (one
// path segment wildcard) and literal suffixes. Good enough for the
// surface registry's patterns; extend if new surface globs need finer
// matching.
func matchOwnedPath(pattern, path string) bool {
	pattern = strings.TrimSuffix(strings.SplitN(pattern, "#", 2)[0], "/")
	if pattern == "" {
		return false
	}
	if !strings.Contains(pattern, "*") {
		return strings.HasSuffix(path, pattern)
	}
	// Reduce one-star glob: "*/foo.md" matches any "x/foo.md" segment.
	parts := strings.Split(pattern, "/")
	segs := strings.Split(path, "/")
	if len(parts) > len(segs) {
		return false
	}
	segs = segs[len(segs)-len(parts):]
	for i, p := range parts {
		if p == "*" {
			continue
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}
