package workflow

import (
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// LocalizeRecipeImportYAML transforms a container-shape recipe import
// YAML into local-mode shape. ONE operation:
//
//   - Drop services with `zeropsSetup: dev` — local mode replaces the
//     SSH-in dev runtime with the user's CWD; provisioning a Zerops
//     dev service would be redundant and break bootstrap checks.
//
// `buildFromGit` is preserved on remaining runtimes — Zerops API
// rejects a runtime that declares `zeropsSetup` without
// `buildFromGit` (`pipelineConfig` requires the source URL). This
// constraint surfaced in flow-eval-local recipe-nodejs-hello-world
// suite 20260507-125401, where the agent submitted a stripped YAML
// and got an API rejection. Stage auto-seeds from the upstream
// recipe repo on import; first local deploy lands as a normal
// iteration after the user edits the cloned tree, not as a mandatory
// bootstrap-completion step.
//
// Managed services (no zeropsSetup) pass through unchanged.
//
// Recipes already shaped for local mode (single runtime, no
// `zeropsSetup: dev` block — e.g. `nextjs-ssr-hello-world`) become
// no-ops: the dev-drop finds nothing.
//
// Uses yaml.Node round-trip so comments and field ordering survive.
// Returns the input verbatim on parse failure with a wrapped error so
// callers can name the recipe in their diagnostics.
func LocalizeRecipeImportYAML(recipe string) (string, error) {
	if recipe == "" {
		return "", nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(recipe), &doc); err != nil {
		return "", fmt.Errorf("local-mode transform: recipe YAML parse: %w", err)
	}

	root := documentRoot(&doc)
	if root == nil {
		return "", errors.New("local-mode transform: empty document")
	}

	servicesNode := mappingValue(root, "services")
	if servicesNode == nil || servicesNode.Kind != yaml.SequenceNode {
		// No services to transform — return verbatim.
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return "", fmt.Errorf("local-mode transform: marshal: %w", err)
		}
		return string(out), nil
	}

	// Collect indices of services with zeropsSetup: dev for removal.
	// Reverse-apply so intermediate indices stay stable.
	var dropIndices []int
	for i, svc := range servicesNode.Content {
		if svc.Kind != yaml.MappingNode {
			continue
		}
		if mappingScalar(svc, "zeropsSetup") == RecipeSetupDev {
			dropIndices = append(dropIndices, i)
		}
	}
	for i := len(dropIndices) - 1; i >= 0; i-- {
		idx := dropIndices[i]
		servicesNode.Content = append(servicesNode.Content[:idx], servicesNode.Content[idx+1:]...)
	}

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return "", fmt.Errorf("local-mode transform: marshal: %w", err)
	}
	return string(out), nil
}
