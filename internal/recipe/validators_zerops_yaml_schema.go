package recipe

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/schema"
)

// gateZeropsYamlSchema validates every codebase's on-disk zerops.yaml
// against the embedded zerops-yml-json-schema. The schema closes
// `run.properties` with `additionalProperties: false` (and similarly
// for build/cache/start blocks), so any unknown field — including
// fields that ARE valid in import.yaml at service-level (like
// `verticalAutoscaling`) but NOT valid in zerops.yaml at runtime-block
// level — produces a violation.
//
// Run-21-prep §RC1. The codex audit on sim 21-input-1 caught a
// `run.verticalAutoscaling:` block in apidev + workerdev zerops.yaml
// that no existing gate noticed. Schema-conformance is a mechanical
// check, not a heuristic, so it lives at the gate layer (not as a
// content reviewer). The validator returns Blocking severity — a
// schema-invalid yaml fails import; recipe ships broken.
//
// Codebases without a SourceRoot (chain-parent, pre-scaffold) are
// silently skipped. Read errors surface as their own violation code
// so a stitch-corruption regression doesn't masquerade as a schema
// violation.
func gateZeropsYamlSchema(ctx GateContext) []Violation {
	if ctx.Plan == nil {
		return nil
	}
	var out []Violation
	for _, cb := range ctx.Plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		yamlPath := filepath.Join(cb.SourceRoot, "zerops.yaml")
		raw, err := os.ReadFile(yamlPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			out = append(out, Violation{
				Code:     "zerops-yaml-read-failed",
				Path:     yamlPath,
				Severity: SeverityBlocking,
				Message:  fmt.Sprintf("read zerops.yaml: %v", err),
			})
			continue
		}
		errs := schema.ValidateZeropsYAML(string(raw), "")
		for _, ve := range errs {
			out = append(out, Violation{
				Code:     "zerops-yaml-schema-violation",
				Path:     yamlPath,
				Severity: SeverityBlocking,
				Message:  ve.Error(),
			})
		}
	}
	return out
}
