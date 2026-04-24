package workflow

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Role markers used when matching plan runtime slots to recipe services.
// These are orthogonal to envelope Mode constants — they describe which
// half of a standard-mode plan target (dev vs stage) a recipe service
// consumes, not the envelope phase.
const (
	recipeRoleDev   = "dev"
	recipeRoleStage = "stage"
)

// RewriteRecipeImportYAML rewrites a recipe's canonical import YAML using
// hostnames derived from the agent's plan. Runtime service hostnames
// (services declaring zeropsSetup) are replaced with the matching plan
// target's DevHostname (zeropsSetup=dev) or ExplicitStage (any other
// zeropsSetup value). Managed service hostnames stay verbatim: the recipe's
// app repo holds `${hostname_*}` env-var references that cannot be resolved
// through the rewrite, so renaming a managed dep is rejected.
//
// When a dependency's Resolution is EXISTS, the corresponding managed
// service entry is dropped from the output — the service already exists
// and the import must not attempt to create it.
//
// Returns the recipe verbatim when plan is nil or has no targets (the
// discover step calls this before plan submission, so there is nothing to
// apply yet). Errors name the exact failure so the plan can be rejected
// with a specific, actionable diagnostic:
//   - parse: recipe YAML malformed
//   - services: recipe missing `services:` sequence
//   - managed service: plan declares a dep hostname different from recipe's
//   - no recipe service matches: plan target has a type not present in recipe
func RewriteRecipeImportYAML(recipe string, plan *ServicePlan) (string, error) {
	if plan == nil || len(plan.Targets) == 0 {
		return recipe, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(recipe), &doc); err != nil {
		return "", fmt.Errorf("recipe YAML parse: %w", err)
	}

	root := documentRoot(&doc)
	if root == nil {
		return "", errors.New("recipe YAML parse: empty document")
	}

	servicesNode := mappingValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.SequenceNode {
		return "", errors.New("recipe YAML missing services: sequence")
	}

	slots := buildRuntimeSlots(plan)
	managedDeps := collectManagedDeps(plan)

	// Indices of service entries to drop (managed deps with Resolution=EXISTS).
	// Collected forward; applied in reverse so intermediate indices stay stable.
	var dropIndices []int

	for i, svc := range servicesNode.Content {
		if svc.Kind != yaml.MappingNode {
			continue
		}
		svcType := mappingScalar(svc, "type")
		svcHostname := mappingScalar(svc, "hostname")
		zSetup := mappingScalar(svc, "zeropsSetup")

		if zSetup != "" {
			role := recipeRuntimeRole(zSetup)
			slotIdx := findRuntimeSlot(slots, svcType, role)
			if slotIdx == -1 {
				continue // no matching plan slot — keep recipe verbatim
			}
			slot := &slots[slotIdx]
			newHostname := slot.hostname()
			if newHostname != "" && newHostname != svcHostname {
				setMappingScalar(svc, "hostname", newHostname)
			}
			slot.used = true
			continue
		}

		// Managed service (no zeropsSetup). Match by type against plan deps.
		dep := findDepByType(managedDeps, svcType)
		if dep == nil {
			continue // recipe managed service not claimed in plan — keep verbatim
		}
		if dep.Hostname != svcHostname {
			return "", fmt.Errorf(
				"managed service hostname cannot be renamed: recipe declares %q, plan declares %q (rename would break ${%s_*} env-var references in the recipe's app repo zerops.yaml — either keep the recipe's hostname or use classic route and write your own zerops.yaml)",
				svcHostname, dep.Hostname, svcHostname,
			)
		}
		if dep.Resolution == ResolutionExists {
			dropIndices = append(dropIndices, i)
		}
	}

	// Unused runtime slots mean plan declares a target type the recipe
	// doesn't have — that is a plan/recipe shape mismatch. Loud error.
	for _, s := range slots {
		if !s.used {
			return "", fmt.Errorf("no recipe service matches plan target type %q (role=%s)", s.target.Type, s.role)
		}
	}

	for i := len(dropIndices) - 1; i >= 0; i-- {
		idx := dropIndices[i]
		servicesNode.Content = append(servicesNode.Content[:idx], servicesNode.Content[idx+1:]...)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("rewritten YAML marshal: %w", err)
	}
	return string(out), nil
}

// runtimeSlot represents one hostname slot in the plan that needs to be
// matched against a recipe service. Each standard-mode plan target emits
// two slots (dev + stage); dev/simple mode emits one (dev).
type runtimeSlot struct {
	target *RuntimeTarget
	role   string // recipeRoleDev or recipeRoleStage
	used   bool
}

// hostname returns the plan-declared hostname for this slot.
func (s runtimeSlot) hostname() string {
	if s.target == nil {
		return ""
	}
	if s.role == recipeRoleDev {
		return s.target.DevHostname
	}
	return s.target.ExplicitStage
}

// buildRuntimeSlots derives the flat list of (target, role) slots from a
// plan. Empty slot values (e.g. ExplicitStage for a dev-mode plan) are
// skipped so the slot set accurately reflects what the agent declared.
func buildRuntimeSlots(plan *ServicePlan) []runtimeSlot {
	var slots []runtimeSlot
	for i := range plan.Targets {
		t := &plan.Targets[i].Runtime
		if t.DevHostname != "" {
			slots = append(slots, runtimeSlot{target: t, role: recipeRoleDev})
		}
		if t.ExplicitStage != "" {
			slots = append(slots, runtimeSlot{target: t, role: recipeRoleStage})
		}
	}
	return slots
}

// findRuntimeSlot returns the index of the first unused slot matching the
// given type + role. Returns -1 if no match. "First-unused" semantics let
// multi-target plans (rare in recipes today) map deterministically.
func findRuntimeSlot(slots []runtimeSlot, svcType, role string) int {
	for i, s := range slots {
		if s.used || s.target == nil || s.role != role || s.target.Type != svcType {
			continue
		}
		return i
	}
	return -1
}

// recipeRuntimeRole maps zeropsSetup into the matcher role. recipeRoleDev is the
// only first-class dev marker recognized today; everything else (prod,
// staging, etc.) matches the stage half of a standard-mode target.
func recipeRuntimeRole(zeropsSetup string) string {
	if zeropsSetup == recipeRoleDev {
		return recipeRoleDev
	}
	return recipeRoleStage
}

// collectManagedDeps flattens Dependencies across all plan targets.
// Preserves order for deterministic type-based matching.
func collectManagedDeps(plan *ServicePlan) []Dependency {
	total := 0
	for _, t := range plan.Targets {
		total += len(t.Dependencies)
	}
	out := make([]Dependency, 0, total)
	for _, t := range plan.Targets {
		out = append(out, t.Dependencies...)
	}
	return out
}

// findDepByType returns the first dep with matching type, or nil. Recipes
// typically have one managed dep per type — disambiguating beyond that is
// out of scope for F6 and would need explicit plan-side hostname matching.
func findDepByType(deps []Dependency, svcType string) *Dependency {
	for i := range deps {
		if deps[i].Type == svcType {
			return &deps[i]
		}
	}
	return nil
}

// documentRoot returns the first non-empty content node of a parsed YAML
// document, unwrapping the DocumentNode container. Returns nil for empty
// documents so callers can emit a precise error.
func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

// mappingValue returns the value node for a given key in a MappingNode, or
// nil when absent / when mapNode is not a mapping.
func mappingValue(mapNode *yaml.Node, key string) *yaml.Node {
	if mapNode == nil || mapNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapNode.Content); i += 2 {
		if mapNode.Content[i].Value == key {
			return mapNode.Content[i+1]
		}
	}
	return nil
}

// mappingScalar returns the scalar string value for a key in a MappingNode,
// or "" when the key is missing or its value is not a scalar.
func mappingScalar(mapNode *yaml.Node, key string) string {
	v := mappingValue(mapNode, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// setMappingScalar updates an existing scalar value for a key. No-op when
// the key is absent or its value is non-scalar — F6 only rewrites existing
// `hostname:` scalars, never adds fields.
func setMappingScalar(mapNode *yaml.Node, key, value string) {
	v := mappingValue(mapNode, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return
	}
	v.Value = value
	// Preserve scalar style: if the original was quoted, the library keeps
	// its Style field; we only update Value. Platform parses either way.
}
