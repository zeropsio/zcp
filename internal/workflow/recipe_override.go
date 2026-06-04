package workflow

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// RewriteRecipeImportYAMLFromShape rewrites a recipe's canonical import YAML
// from the recipe's OWN identities — keyed by each service's original hostname
// — rather than re-matching an agent-authored plan back onto the YAML. The
// recipe import YAML is the owner of the service shape (R3); the only changes a
// caller can make are carried in overrides:
//
//   - RuntimeHostnameByOriginal — rename a runtime service, keyed by the recipe's
//     ORIGINAL hostname. A runtime not in the map keeps its hostname.
//   - ManagedResolutionByHost — when a managed hostname maps to EXISTS, its
//     service entry is dropped (the service already exists; the import must not
//     recreate it). Managed services are never renamed here: the recipe's app
//     repo holds ${hostname_*} env refs a rename would break, so a managed
//     rename is rejected upstream when an explicit plan is reconciled into
//     overrides (reconcileRecipeOverrides) — it is not expressible in this shape.
//
// Everything else (buildFromGit, type, envSecrets, autoscaling, priority, …) is
// preserved verbatim. Empty overrides therefore yield a faithful re-marshal of
// the recipe — EVERY runtime (workers, second-repo pairs, cross-type stages) is
// kept, which the deleted slot-matcher could not guarantee.
func RewriteRecipeImportYAMLFromShape(recipe string, overrides RecipeShapeOverrides) (string, error) {
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

	// Managed services resolved to EXISTS are dropped. Collect forward, apply
	// in reverse so earlier indices stay valid.
	var dropIndices []int
	for i, svc := range servicesNode.Content {
		if svc.Kind != yaml.MappingNode {
			continue
		}
		hostname := mappingScalar(svc, "hostname")
		if mappingScalar(svc, "zeropsSetup") != "" {
			// Runtime service — apply a hostname override if one is present.
			if nh, ok := overrides.RuntimeHostnameByOriginal[hostname]; ok && nh != "" && nh != hostname {
				setMappingScalar(svc, "hostname", nh)
			}
			continue
		}
		// Managed service — drop it when resolved to EXISTS.
		if overrides.ManagedResolutionByHost[hostname] == ResolutionExists {
			dropIndices = append(dropIndices, i)
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
// the key is absent or its value is non-scalar — only existing `hostname:`
// scalars are rewritten, never adding fields.
func setMappingScalar(mapNode *yaml.Node, key, value string) {
	v := mappingValue(mapNode, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return
	}
	v.Value = value
	// Preserve scalar style: if the original was quoted, the library keeps
	// its Style field; we only update Value. Platform parses either way.
}
