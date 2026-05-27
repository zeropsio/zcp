package workflow

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// recipeSetupNamesForTarget returns the canonical (PrimarySetupName,
// StageSetupName) values a recipe-bootstrapped target's ServiceMeta should
// carry, based on its mode shape.
//
// Conventions emitted by the recipe corpus (see
// `internal/knowledge/recipes/*.md` + `recipe_templates_import.go`):
//
//   - Standard pair: dev half runs setup "dev", stage half runs setup "prod"
//   - Dev (standalone): single half runs setup "dev"
//   - Simple (standalone): single half runs setup "prod" (corpus convention,
//     not "simple" — see `recipe_corpus_store_test.go`)
//   - LocalStage (Theme-1 dev-stripped recipe): meta collapses
//     Hostname==StageHostname == stage hostname, so SetupNameFor reads
//     StageSetupName (the StageHostname branch fires first). Write the
//     stage half value there ("prod") and leave PrimarySetupName empty.
//   - LocalOnly: no Zerops-side runtime; both empty.
//
// Drift between this helper and recipe templates is caught by the
// belt-and-suspenders parse in `verifySetupNameConvention` — when a
// recipe template emits a non-conventional zeropsSetup, the convention
// asserted here triggers a stderr log so the discrepancy surfaces in
// bootstrap output rather than silently writing a wrong value.
func recipeSetupNamesForTarget(mode topology.Mode) (primary, stage string) {
	switch mode {
	case topology.PlanModeStandard:
		return "dev", "prod"
	case topology.PlanModeDev:
		return "dev", ""
	case topology.PlanModeSimple:
		return "prod", ""
	case topology.PlanModeLocalStage:
		// Hostname==StageHostname collapse in writeBootstrapOutputs:
		// SetupNameFor reads StageSetupName for the stage hostname.
		return "", "prod"
	case topology.PlanModeLocalOnly:
		return "", ""
	default:
		return "", ""
	}
}

// extractZeropsSetupMapFromImportYAML parses a recipe import yaml body and
// returns the hostname→zeropsSetup mapping for every service declaring a
// zeropsSetup field. Services without zeropsSetup (managed deps) are
// omitted. Returns nil + nil on parse error or absent `services:` block —
// belt-and-suspenders is best-effort and must not fail bootstrap.
func extractZeropsSetupMapFromImportYAML(body string) map[string]string {
	if body == "" {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(body), &root); err != nil {
		return nil
	}
	doc := documentRoot(&root)
	if doc == nil {
		return nil
	}
	services := mappingValue(doc, "services")
	if services == nil || services.Kind != yaml.SequenceNode {
		return nil
	}
	out := make(map[string]string, len(services.Content))
	for _, svc := range services.Content {
		if svc.Kind != yaml.MappingNode {
			continue
		}
		host := mappingScalar(svc, "hostname")
		setup := mappingScalar(svc, "zeropsSetup")
		if host == "" || setup == "" {
			continue
		}
		out[host] = setup
	}
	return out
}

// verifySetupNameConvention parses the in-hand recipe yaml + asserts the
// emitted zeropsSetup for the dev / stage hostnames matches what
// recipeSetupNamesForTarget would compute for this mode. Mismatch is
// logged to stderr (best-effort) so a recipe-corpus drift surfaces
// during bootstrap; the convention value is still authoritative for the
// meta write per the plan's value-source ordering.
//
// Skips entries the recipe doesn't carry (empty hostname or absent
// zeropsSetup) — those are legitimately not-a-runtime cases (managed
// deps, LocalStage's dropped dev hostname).
func verifySetupNameConvention(yamlBody, devHostname, stageHostname, expectedPrimary, expectedStage string) {
	if yamlBody == "" {
		return
	}
	setups := extractZeropsSetupMapFromImportYAML(yamlBody)
	if len(setups) == 0 {
		return
	}
	if devHostname != "" && expectedPrimary != "" {
		if got, ok := setups[devHostname]; ok && got != expectedPrimary {
			fmt.Fprintf(os.Stderr,
				"zcp: setup-name drift: hostname=%s recipe zeropsSetup=%q, convention=%q\n",
				devHostname, got, expectedPrimary)
		}
	}
	if stageHostname != "" && expectedStage != "" {
		if got, ok := setups[stageHostname]; ok && got != expectedStage {
			fmt.Fprintf(os.Stderr,
				"zcp: setup-name drift: hostname=%s recipe zeropsSetup=%q, convention=%q\n",
				stageHostname, got, expectedStage)
		}
	}
}
