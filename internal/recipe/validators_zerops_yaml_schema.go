package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/schema"
)

// gateZeropsYamlSchema validates every codebase's zerops.yaml in two parts:
//
//  1. STRUCTURE — against the embedded structure-only schema
//     (`schema.ValidateZeropsYAMLStructure`): `additionalProperties: false`
//     on run/build/cache/start blocks, required fields, the stable enums, and
//     the build.base string-or-array type contract. This is the load-bearing
//     check (Run-21-prep §RC1: it caught a `run.verticalAutoscaling:` block —
//     a valid import.yaml service-level field but NOT a zerops.yaml run-level
//     field — that no other gate noticed).
//  2. BASE EXISTENCE — build.base / run.base values against the LIVE Zerops
//     schema enums (`schema.CheckZeropsBasesLive` over the session's
//     short-TTL `Schemas`, falling back to the embedded floor when absent).
//     Validating the bases live (rather than baking them into the structural
//     enum) means a brand-new platform base is NOT false-rejected during
//     authoring, while a hallucinated base still fails.
//
// The validator returns Blocking severity — a schema-invalid yaml fails
// import; recipe ships broken.
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

		// Layer A (run-21 race fix): prefer the in-memory whole-yaml
		// fragment when one is recorded for this codebase. Validating the
		// in-memory body eliminates the disk-read race against
		// WriteCodebaseYAMLWithComments — agents in run-21 saw 0-byte
		// reads catching the writer's truncate-then-write window even
		// though the eventual on-disk content was a 6-8 KB valid yaml.
		// Disk fallback below handles the SSH-edit-only path
		// (system.md:384-394) where no fragment has been re-recorded.
		fragID := fragmentIDCodebaseZeropsYAML(cb.Hostname)
		if ctx.Plan.Fragments != nil {
			if body, ok := ctx.Plan.Fragments[fragID]; ok && strings.TrimSpace(body) != "" {
				errs := validateRecipeZerops(ctx, body)
				for _, ve := range errs {
					out = append(out, Violation{
						Code:     "zerops-yaml-schema-violation",
						Path:     fragID,
						Severity: SeverityBlocking,
						Message:  ve.Error(),
					})
				}
				continue
			}
		}

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
		errs := validateRecipeZerops(ctx, string(raw))
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

// validateRecipeZerops runs the gate's two-part check: structure (embedded
// structure-only schema) + base existence (live enums via ctx.Schemas, or the
// embedded floor when no live snapshot is attached — e.g. the sim/tests).
func validateRecipeZerops(ctx GateContext, body string) []schema.ValidationError {
	errs := schema.ValidateZeropsYAMLStructure(body, "")
	schemas := ctx.Schemas
	if schemas == nil {
		schemas = schema.Embedded()
	}
	errs = append(errs, schema.CheckZeropsBasesLive(body, schemas)...)
	return errs
}
