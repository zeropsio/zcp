package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Run-17 §11 — deployFiles narrowness validator. Closes R-17-C8: every
// prod-setup deployFiles entry must be reachable from runtime config
// (run.start, run.initCommands, run.envVariables) or be backed by a
// field_rationale fact justifying its presence. Unreferenced entries
// are dead weight (never read at runtime) or path mismatches
// (referenced from a different relative path than ships).
//
// Run-16 evidence: apidev shipped `src/scripts` while initCommands
// invoked `node dist/scripts/migrate.js` — either dead weight (the
// porter ships TypeScript source they never invoke) or implicit
// ts-node fallback (the porter expects ts-node to pick up `src/`,
// which Zerops' bare-runtime image doesn't carry).

// gateDeployFilesNarrowness walks every codebase's zerops.yaml on
// disk, parses each prod setup's deployFiles list, and emits a notice
// for any entry not referenced by run.start / run.initCommands /
// run.envVariables values OR backed by a field_rationale fact.
//
// Notice severity (not blocking) — there are legitimate reasons to
// ship an unreferenced path (e.g. a static-asset directory served by
// nginx via a config the validator can't introspect). Surfacing it as
// a notice forces the agent to record a field_rationale fact (which
// then routes to a zerops.yaml comment) when the entry is intentional.
func gateDeployFilesNarrowness(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var facts []FactRecord
	if ctx.FactsLog != nil {
		if all, err := ctx.FactsLog.Read(); err == nil {
			facts = all
		}
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		body, err := os.ReadFile(yamlPath)
		if err != nil {
			// Codebase without an authored zerops.yaml — provision phase
			// hasn't reached it yet, or it's chain-parent-shared. Skip.
			continue
		}
		out = append(out, validateDeployFilesNarrowness(context.Background(), yamlPath, body, SurfaceInputs{Plan: ctx.Plan, Facts: facts})...)
	}
	return out
}

// validateDeployFilesNarrowness — Run-17 §11. Pure function exposed
// for direct unit testing without the full GateContext stack.
func validateDeployFilesNarrowness(_ context.Context, path string, body []byte, inputs SurfaceInputs) []Violation {
	var root yaml.Node
	if err := yaml.Unmarshal(body, &root); err != nil {
		return nil
	}
	zeropsArr := findZeropsArray(&root)
	if zeropsArr == nil {
		return nil
	}
	var out []Violation
	for _, setupNode := range zeropsArr.Content {
		setup := mappingValue(setupNode, "setup")
		if setup == "" || isDevSetup(setup) {
			// Dev setups ship `.` by construction; narrowness only
			// applies to prod-class setups.
			continue
		}
		buildNode := mappingChild(setupNode, "build")
		runNode := mappingChild(setupNode, "run")
		if buildNode == nil || runNode == nil {
			continue
		}
		entries := stringSeqOrSingle(mappingChild(buildNode, "deployFiles"))
		if len(entries) == 0 {
			continue
		}
		// Reference text — anything in run.start / run.initCommands
		// / run.envVariables values that an entry name might appear
		// in. Substring match is sufficient — deployFiles entries are
		// project-relative and don't normalize across forms.
		var refBuf strings.Builder
		refBuf.WriteString(stringValue(mappingChild(runNode, "start")))
		refBuf.WriteByte('\n')
		for _, line := range stringSeqOrSingle(mappingChild(runNode, "initCommands")) {
			refBuf.WriteString(line)
			refBuf.WriteByte('\n')
		}
		if envNode := mappingChild(runNode, "envVariables"); envNode != nil && envNode.Kind == yaml.MappingNode {
			for i := 1; i < len(envNode.Content); i += 2 {
				refBuf.WriteString(envNode.Content[i].Value)
				refBuf.WriteByte('\n')
			}
		}
		ref := refBuf.String()
		for _, entry := range entries {
			if entry == "" || entry == "." {
				continue
			}
			if isCanonicalRuntimeDep(entry) {
				// node_modules / package.json / vendor / composer.json
				// etc. are loaded implicitly by language runtimes; the
				// porter doesn't need to name them in run.start.
				continue
			}
			if strings.Contains(ref, entry) {
				continue
			}
			if hasFieldRationaleForEntry(inputs.Facts, entry) {
				continue
			}
			out = append(out, Violation{
				Code:     "deploy-files-unreferenced",
				Path:     path,
				Severity: SeverityNotice,
				Message: fmt.Sprintf(
					"setup %q deployFiles entry %q is not referenced by run.start / run.initCommands / run.envVariables and has no field_rationale fact justifying its presence; either drop the entry or record a field_rationale explaining why it ships",
					setup, entry),
			})
		}
	}
	return out
}

// hasFieldRationaleForEntry returns true when any field_rationale fact
// in the log mentions the deployFiles entry by FieldPath substring or
// in its Why prose.
func hasFieldRationaleForEntry(facts []FactRecord, entry string) bool {
	for _, f := range facts {
		if f.Kind != FactKindFieldRationale {
			continue
		}
		if strings.Contains(f.FieldPath, entry) || strings.Contains(f.Why, entry) {
			return true
		}
	}
	return false
}

// canonicalRuntimeDeps — deployFiles entries that are language-
// ecosystem standard runtime dependencies. These are loaded implicitly
// by node / php / python runtimes from a fixed location relative to
// the entry point; the porter doesn't name them in run.start /
// run.initCommands. Skip the narrowness check for these entries.
var canonicalRuntimeDeps = map[string]struct{}{
	// Node
	"node_modules":      {},
	"package.json":      {},
	"package-lock.json": {},
	"yarn.lock":         {},
	"pnpm-lock.yaml":    {},
	"tsconfig.json":     {},
	// PHP
	"vendor":        {},
	"composer.json": {},
	"composer.lock": {},
	// Python
	"requirements.txt": {},
	"poetry.lock":      {},
	"pyproject.toml":   {},
	"setup.py":         {},
	// Ruby
	"Gemfile":       {},
	"Gemfile.lock":  {},
	"vendor/bundle": {},
	// Generic
	".env.example": {},
	"public":       {}, // canonical web root for many frameworks
}

func isCanonicalRuntimeDep(entry string) bool {
	_, ok := canonicalRuntimeDeps[entry]
	return ok
}

// isDevSetup reports whether a setup name maps to a dev-class
// container — dev setups ship the working tree and aren't subject to
// the narrowness contract.
func isDevSetup(setup string) bool {
	s := strings.ToLower(setup)
	return s == "dev" || strings.HasPrefix(s, "dev-") || strings.HasSuffix(s, "-dev") || strings.Contains(s, "-dev-")
}

// findZeropsArray walks the parsed zerops.yaml document looking for
// the top-level `zerops:` sequence. Returns nil when the document is
// shaped unexpectedly (the syntax validator handles those cases).
func findZeropsArray(root *yaml.Node) *yaml.Node {
	if root == nil || len(root.Content) == 0 {
		return nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "zerops" && doc.Content[i+1].Kind == yaml.SequenceNode {
			return doc.Content[i+1]
		}
	}
	return nil
}

// mappingChild returns the value node for the named key on a mapping
// node, or nil when absent.
func mappingChild(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// mappingValue returns the scalar string value at the named key, or
// "" when the key is absent or non-scalar.
func mappingValue(node *yaml.Node, key string) string {
	v := mappingChild(node, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// stringValue returns the scalar string value of a node, or "" when
// nil/non-scalar.
func stringValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

// stringSeqOrSingle reads a node that may be a sequence of scalars or
// a single scalar — returns the scalar slice. Empty slice for nil /
// empty / non-matching node shapes.
func stringSeqOrSingle(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	//exhaustive:ignore — Document/Mapping/Alias don't appear in this schema position.
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return nil
		}
		return []string{node.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			if child.Kind == yaml.ScalarNode && child.Value != "" {
				out = append(out, child.Value)
			}
		}
		return out
	}
	return nil
}
