package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// gate_fact_rationale.go — Run-20 C5 closure.
//
// Run-19 facts.jsonl had 13 facts with no classification and 7
// `field_rationale` facts missing `fieldPath`; the codebase-content
// sub-agent had only 3 well-formed rationale facts to attach
// yaml-comment fragments to (vs the simulation's 7 and the goldens'
// full coverage). B1 (consumer side, brief composer) can't fix this
// because the consumer can't materialize comments without source
// rationale.
//
// This gate is the producer-side complement: at scaffold + feature
// complete-phase, parse `<SourceRoot>/zerops.yaml` and enumerate
// directive groups + env-var families derived from
// plan.Services/Codebases. Refuse when any directive group lacks
// an attesting `field_rationale` fact whose `FieldPath` matches.
//
// Bypass mechanism: a `field_rationale` fact whose `Why` field
// contains the literal `"intentionally skipped:"` prefix suppresses
// the requirement for that FieldPath. The agent must still record
// the fact + reason; a missing fact is a refusal.

// directiveGroupsForGate enumerates the canonical top-level
// directive groups under each `zerops[i].run` / `zerops[i].build` /
// `zerops[i].deploy` block whose presence in the yaml triggers a
// rationale requirement. Each entry is a dot-path matching the
// FieldPath shape `field_rationale` facts use; the path is matched
// suffix-wise (any FieldPath that ends with `<group>` counts).
//
// The list is derived from spec-content-surfaces.md §"Surface 7"
// + `synthesis_workflow.md`'s yaml-comment block-name examples
// (`run.envVariables`, `run.initCommands`, `build`, `readinessCheck`).
// Adding a new group means scaffold/feature must record one
// rationale per occurrence in the yaml.
var directiveGroupsForGate = []string{
	"build",
	"run.start",
	"run.ports",
	"run.envVariables",
	"run.initCommands",
	"run.prepareCommands",
	"run.healthCheck",
	"run.readinessCheck",
	"deploy.readiness",
	"deploy.healthCheck",
}

// gateFactRationaleCompleteness refuses scaffold/feature complete-phase
// when any directive group present in `<SourceRoot>/zerops.yaml` lacks
// an attesting `field_rationale` fact. Per-codebase scoping; one
// violation per missing (codebase, group) pair.
func gateFactRationaleCompleteness(ctx GateContext) []Violation {
	if ctx.Plan == nil || ctx.FactsLog == nil {
		return nil
	}
	allFacts, err := ctx.FactsLog.Read()
	if err != nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.Hostname == "" || cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		raw, err := os.ReadFile(yamlPath)
		if err != nil {
			// No yaml = pre-scaffold; gateFactsRecorded handles that path.
			continue
		}
		present := directiveGroupsPresent(raw)
		if len(present) == 0 {
			continue
		}
		// Index facts by FieldPath suffix on this codebase's scope.
		attested := factsAttestingDirectives(allFacts, cb.Hostname)
		bypassed := factsBypassingDirectives(allFacts, cb.Hostname)
		for _, group := range present {
			if attested[group] || bypassed[group] {
				continue
			}
			out = append(out, Violation{
				Code: "fact-rationale-missing",
				Path: cb.Hostname,
				Message: fmt.Sprintf(
					"codebase/%s zerops.yaml directive group %q has no attesting `field_rationale` fact (FieldPath suffix %q). Record one with `record-fact kind=field_rationale fieldPath=<...>%s why=<rationale>`. Bypass intentionally with `why=\"intentionally skipped: <reason>\"` for directives the porter doesn't need a rationale for.",
					cb.Hostname, group, group, group,
				),
			})
		}
	}
	return out
}

// directiveGroupsPresent returns the subset of directiveGroupsForGate
// that actually appear in the yaml body. Uses gopkg.in/yaml.v3 — same
// parser already used by validators_import_yaml.go.
func directiveGroupsPresent(raw []byte) []string {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil
	}
	paths := collectYAMLPaths(&root)
	// Strip leading `zerops.<i>.` prefix so the comparison is against
	// directive-group leaves (`run.envVariables`, `build`, etc.).
	stripped := map[string]bool{}
	for p := range paths {
		stripped[stripZeropsPrefix(p)] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, group := range directiveGroupsForGate {
		// Group present iff any stripped yaml path equals it OR has it
		// as a prefix (e.g. `run.envVariables.NODE_ENV` attests
		// `run.envVariables`).
		if stripped[group] || hasPathPrefix(stripped, group+".") {
			if !seen[group] {
				seen[group] = true
				out = append(out, group)
			}
		}
	}
	return out
}

// hasPathPrefix reports whether any path in paths starts with prefix.
func hasPathPrefix(paths map[string]bool, prefix string) bool {
	for p := range paths {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// stripZeropsPrefix removes the leading `zerops.` prefix from a dot-
// path so directive-group comparisons are against the directive-leaf
// shape. The collector walks sequence nodes without inserting array
// indices into the path, so the actual yaml-AST output for
// `zerops[0].run.envVariables.NODE_ENV` is `zerops.run.envVariables.NODE_ENV`,
// stripping to `run.envVariables.NODE_ENV`.
// Paths without the `zerops.` prefix pass through unchanged.
func stripZeropsPrefix(path string) string {
	rest, ok := strings.CutPrefix(path, "zerops.")
	if !ok {
		return path
	}
	return rest
}

// factsAttestingDirectives returns the set of directive-group keys
// covered by a `field_rationale` fact on this codebase scope. The
// fact's FieldPath is suffix-matched against each canonical group;
// e.g. `FieldPath="run.envVariables.NODE_ENV"` attests `run.envVariables`.
//
// "Bypass" facts (Why prefix `"intentionally skipped:"`) are excluded
// here so they don't double-count; factsBypassingDirectives covers
// them separately.
func factsAttestingDirectives(facts []FactRecord, hostname string) map[string]bool {
	out := map[string]bool{}
	for _, f := range facts {
		if f.Kind != FactKindFieldRationale || f.FieldPath == "" {
			continue
		}
		if !factScopeMatchesCodebase(f, hostname) {
			continue
		}
		if isBypassRationale(f) {
			continue
		}
		for _, group := range directiveGroupsForGate {
			if f.FieldPath == group ||
				strings.HasSuffix(f.FieldPath, "."+group) ||
				strings.HasPrefix(f.FieldPath, group+".") ||
				f.FieldPath == group {
				out[group] = true
			}
		}
	}
	return out
}

// factsBypassingDirectives returns the set of directive groups whose
// attesting fact carries the bypass marker (`Why` starts with
// `"intentionally skipped:"`). Bypass is the agent's way to declare
// "no rationale needed for this directive" while still having a fact
// trail. The codebase-content sub-agent reads bypass facts and
// authors no comment for them.
func factsBypassingDirectives(facts []FactRecord, hostname string) map[string]bool {
	out := map[string]bool{}
	for _, f := range facts {
		if f.Kind != FactKindFieldRationale || f.FieldPath == "" {
			continue
		}
		if !factScopeMatchesCodebase(f, hostname) {
			continue
		}
		if !isBypassRationale(f) {
			continue
		}
		for _, group := range directiveGroupsForGate {
			if f.FieldPath == group ||
				strings.HasSuffix(f.FieldPath, "."+group) ||
				strings.HasPrefix(f.FieldPath, group+".") {
				out[group] = true
			}
		}
	}
	return out
}

// isBypassRationale reports whether a field_rationale fact carries
// the bypass marker prefix. The marker is plain English so the agent
// can author it inline without extra schema fields. Run-20 C5.
func isBypassRationale(f FactRecord) bool {
	return strings.HasPrefix(strings.TrimSpace(f.Why), "intentionally skipped:")
}

// factScopeMatchesCodebase reports whether a fact's Scope names the
// codebase. Mirrors the scoping convention used by gateFactsRecorded
// (`<host>` or `<host>/...`).
func factScopeMatchesCodebase(f FactRecord, hostname string) bool {
	if hostname == "" {
		return false
	}
	if f.Scope == hostname {
		return true
	}
	return strings.HasPrefix(f.Scope, hostname+"/")
}
