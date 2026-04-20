package workflow

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// InferRecipeShape inspects a recipe's project-import YAML and returns the
// bootstrap mode it implies plus the runtime service count. Runtime services
// are identified by the presence of a `zeropsSetup` field — recipes set it on
// every runtime and never on managed services.
//
// Modes:
//   - "standard" — two runtimes, one `zeropsSetup: dev` + one `zeropsSetup: prod`
//   - "simple"   — single runtime with `zeropsSetup: prod`
//   - "dev"      — single runtime with `zeropsSetup: dev`
//   - ""         — managed-only, unknown pattern, or invalid YAML
func InferRecipeShape(importYAML string) (mode string, runtimeCount int) {
	var doc struct {
		Services []struct {
			Hostname    string `yaml:"hostname"`
			ZeropsSetup string `yaml:"zeropsSetup"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		return "", 0
	}
	var setups []string
	for _, svc := range doc.Services {
		if svc.ZeropsSetup != "" {
			setups = append(setups, svc.ZeropsSetup)
		}
	}
	switch len(setups) {
	case 0:
		return "", 0
	case 1:
		if setups[0] == RecipeSetupProd {
			return PlanModeSimple, 1
		}
		if setups[0] == RecipeSetupDev {
			return PlanModeDev, 1
		}
		return "", 1
	case 2:
		hasDev := setups[0] == RecipeSetupDev || setups[1] == RecipeSetupDev
		hasProd := setups[0] == RecipeSetupProd || setups[1] == RecipeSetupProd
		if hasDev && hasProd {
			return PlanModeStandard, 2
		}
		return "", 2
	default:
		return "", len(setups)
	}
}

// ValidateBootstrapRecipeMode rejects plans whose bootstrap mode deviates from
// the recipe the route matched. Recipes ship with a fixed shape (standard,
// simple, or dev) baked into their import YAML; deviation strips the agent of
// the recipe-specific rules it needs (e.g. startWithoutCode on simple).
//
// A nil match or empty Mode (unrecognised shape) disables the check — recipe
// atoms may ship without a structured shape, and we shouldn't block on that.
func ValidateBootstrapRecipeMode(match *RecipeMatch, targets []BootstrapTarget) error {
	if match == nil || match.Mode == "" {
		return nil
	}
	for _, t := range targets {
		if t.Runtime.EffectiveMode() != match.Mode {
			return fmt.Errorf("recipe %q is %s mode but target %q uses %s — deviating from the recipe strips mode-specific rules from provisioning; either follow the recipe or restart bootstrap with a different intent",
				match.Slug, match.Mode, t.Runtime.DevHostname, t.Runtime.EffectiveMode())
		}
	}
	return nil
}
