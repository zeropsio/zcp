package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// consumes_services.go — Run-21 R2-3.
//
// At scaffold completion the engine reads each codebase's bare
// zerops.yaml, walks `run.envVariables`, extracts `${X}` and `${X_*}`
// references, and records the subset of X tokens that match a managed
// service hostname in the plan. The result lands on
// Codebase.ConsumesServices.
//
// Downstream consumers:
//   - briefs_subagent_prompt.go recipe-context Services block —
//     filtered to the codebase's consumed subset (was: dump every
//     managed service in every codebase's brief).
//   - briefs_content_phase.go atom selection — uses the precision to
//     drop `cross-service-urls.md` / `nats-shapes.md` for codebases
//     that don't consume the relevant service families.

// envVarReferencePattern matches `${X}` and `${X_*}` where X is the
// service hostname. The greedy `[A-Za-z0-9_-]+` captures the head;
// downstream code splits on `_` to extract the leading hostname token.
var envVarReferencePattern = regexp.MustCompile(`\$\{([A-Za-z0-9_-]+)\}`)

// parseConsumedServicesFromYaml extracts managed-service hostnames the
// yaml's run.envVariables references. Returns sorted, de-duplicated
// hostname list. Empty slice when no matches OR yaml unparseable.
//
// Heuristic for "service hostname" inside a `${X_y}` reference: split
// on `_`, take the leading token, look it up in plan.Services by
// hostname. Only managed-kind services count (runtime codebases reach
// each other via `${<host>_zeropsSubdomain}` etc., handled separately
// by sister-codebase teaching).
func parseConsumedServicesFromYaml(yamlBody string, plan *Plan) []string {
	if plan == nil || len(plan.Services) == 0 {
		return nil
	}
	managedHosts := map[string]bool{}
	for _, svc := range plan.Services {
		// Both managed + storage + utility map to managed for filter
		// precision — the SPA-shouldn't-see-db case covers all three.
		managedHosts[svc.Hostname] = true
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlBody), &doc); err != nil {
		return nil
	}
	envVarsNodes := collectRunEnvVariables(&doc)
	if len(envVarsNodes) == 0 {
		return nil
	}

	hits := map[string]bool{}
	for _, envVarsNode := range envVarsNodes {
		// envVarsNode is a mapping of var-name → var-value scalars.
		for i := 0; i+1 < len(envVarsNode.Content); i += 2 {
			valNode := envVarsNode.Content[i+1]
			if valNode == nil || valNode.Kind != yaml.ScalarNode {
				continue
			}
			matches := envVarReferencePattern.FindAllStringSubmatch(valNode.Value, -1)
			for _, m := range matches {
				if len(m) < 2 {
					continue
				}
				head := leadingHostnameToken(m[1])
				if head == "" {
					continue
				}
				if managedHosts[head] {
					hits[head] = true
				}
			}
		}
	}

	if len(hits) == 0 {
		return nil
	}
	out := make([]string, 0, len(hits))
	for h := range hits {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// filterConsumedServices narrows a Service slice down to the
// hostnames the codebase consumes. Nil consumed → return all (back-
// compat fallback for codebases the engine couldn't analyze). Empty
// non-nil slice → return empty (codebase consumes none). Otherwise →
// return services whose hostname appears in consumed.
func filterConsumedServices(all []Service, consumed []string) []Service {
	if consumed == nil {
		return all
	}
	if len(consumed) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(consumed))
	for _, h := range consumed {
		wanted[h] = true
	}
	out := make([]Service, 0, len(consumed))
	for _, svc := range all {
		if wanted[svc.Hostname] {
			out = append(out, svc)
		}
	}
	return out
}

// leadingHostnameToken splits on `_` and returns the first segment.
// `db_hostname` → `db`, `cache` → `cache`, `nats_jetstream_url` → `nats`.
func leadingHostnameToken(s string) string {
	for i, r := range s {
		if r == '_' {
			return s[:i]
		}
	}
	return s
}

// collectRunEnvVariables walks a parsed yaml document collecting every
// `run.envVariables` mapping it can find. Handles three shapes:
//
//   - codebase zerops.yaml (`zerops: [ {setup, run, ...} ]`) — array
//     under `zerops:` key, each item carries one `run` block.
//   - workspace import.yaml (`services: [ ... ]`) — used by the
//     emit-yaml workspace shape.
//   - flat `run:` at root — defensive fallback.
//
// Returns one node per `run.envVariables` mapping found; bare scaffold
// yaml usually has one, but workspace yaml can have many.
func collectRunEnvVariables(doc *yaml.Node) []*yaml.Node {
	if doc == nil || len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}
	var out []*yaml.Node
	collectFromRunBlock := func(runNode *yaml.Node) {
		if runNode == nil {
			return
		}
		if envNode := mappingChild(runNode, "envVariables"); envNode != nil &&
			envNode.Kind == yaml.MappingNode {
			out = append(out, envNode)
		}
	}
	collectFromSequence := func(seqNode *yaml.Node) {
		if seqNode == nil || seqNode.Kind != yaml.SequenceNode {
			return
		}
		for _, entry := range seqNode.Content {
			collectFromRunBlock(mappingChild(entry, "run"))
		}
	}
	collectFromRunBlock(mappingChild(root, "run"))
	collectFromSequence(mappingChild(root, "zerops"))
	collectFromSequence(mappingChild(root, "services"))
	return out
}

// populateConsumesServicesFromYaml reads each codebase's on-disk
// zerops.yaml at <SourceRoot>/zerops.yaml, parses, and writes the
// per-codebase ConsumesServices slice. Idempotent — safe to call
// multiple times. Codebases without a SourceRoot or with an
// unreadable yaml get an empty slice (zero managed-service teaching
// in their downstream brief — the safe default for an unanalyzable
// codebase is "tell them nothing extra" not "tell them everything").
func populateConsumesServicesFromYaml(sess *Session) error {
	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	planSnap := *sess.Plan
	outputRoot := sess.OutputRoot
	codebases := make([]Codebase, len(sess.Plan.Codebases))
	copy(codebases, sess.Plan.Codebases)
	sess.mu.Unlock()

	type result struct {
		consumed []string
		analyzed bool
	}
	results := make([]result, len(codebases))
	for i, cb := range codebases {
		if cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		body, err := os.ReadFile(yamlPath)
		if err != nil {
			// Yaml not yet on disk (sim path that skips scaffold) —
			// leave ConsumesServices nil; downstream filters fall back
			// to the proxy heuristic.
			continue
		}
		parsed := parseConsumedServicesFromYaml(string(body), &planSnap)
		// Mark as analyzed regardless of hit count so downstream
		// distinguishes "read, found none" (omit Managed services
		// block) from "couldn't read" (fall back to all-services).
		results[i] = result{consumed: parsed, analyzed: true}
	}

	sess.mu.Lock()
	if sess.Plan == nil {
		sess.mu.Unlock()
		return nil
	}
	for i, res := range results {
		if i >= len(sess.Plan.Codebases) {
			break
		}
		if !res.analyzed {
			continue
		}
		if res.consumed == nil {
			// Analyzed but no managed-service hits — record explicit
			// empty (non-nil) to signal "filter to none" downstream.
			sess.Plan.Codebases[i].ConsumesServices = []string{}
		} else {
			sess.Plan.Codebases[i].ConsumesServices = res.consumed
		}
	}
	snapshot := *sess.Plan
	sess.mu.Unlock()

	if err := WritePlan(outputRoot, &snapshot); err != nil {
		return fmt.Errorf("persist plan after consumes-services populate: %w", err)
	}
	return nil
}
