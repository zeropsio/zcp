package recipe

import "context"

// Surface is one of seven named content surfaces a recipe produces.
// Every fact lives on exactly one surface; cross-surface references
// are allowed, cross-surface duplication is not. See
// docs/spec-content-surfaces.md for contract details.
type Surface string

const (
	SurfaceRootREADME             Surface = "ROOT_README"
	SurfaceEnvREADME              Surface = "ENV_README"
	SurfaceEnvImportComments      Surface = "ENV_IMPORT_COMMENTS"
	SurfaceCodebaseIG             Surface = "CODEBASE_IG"
	SurfaceCodebaseKB             Surface = "CODEBASE_KB"
	SurfaceCodebaseCLAUDE         Surface = "CODEBASE_CLAUDE"
	SurfaceCodebaseZeropsComments Surface = "CODEBASE_ZEROPS_COMMENTS"
)

// Author identifies who owns a surface. Writer surfaces are authored by
// the content-writer sub-agent; engine surfaces are emitted deterministically
// by internal/recipe from typed plan + tier data.
type Author string

const (
	AuthorWriter Author = "writer"
	AuthorEngine Author = "engine"
)

// SurfaceInputs is the typed bundle a surface's InputFn assembles. Phase 2
// InputFn implementations read a Plan, the filtered FactsLog, and an
// optional ParentRecipe (chain) to compose this struct; the writer brief
// receives it per owned surface.
type SurfaceInputs struct {
	Plan     *Plan
	Facts    []FactRecord
	Parent   *ParentRecipe
	TierDiff *TierDiff
}

// Violation is one finding from a ValidateFn. Empty return means pass.
type Violation struct {
	Code    string `json:"code"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// InputFn assembles the typed inputs a surface needs. Phase 2 implements.
type InputFn func(ctx context.Context, p *Plan, facts []FactRecord, parent *ParentRecipe) (SurfaceInputs, error)

// ValidateFn checks one file's content against a surface contract. Phase 2
// implements surface-specific checks; empty Violations means pass.
type ValidateFn func(ctx context.Context, path string, content []byte, inputs SurfaceInputs) ([]Violation, error)

// SurfaceContract is the typed record for one surface. FormatSpec anchors
// into docs/spec-content-surfaces.md so the writer brief excerpts by
// reference, not by inline copy. AdjacentSurfaces names the surfaces a
// fact might collide with — used by the cross-surface-duplication gate.
type SurfaceContract struct {
	Name             Surface
	Author           Author
	FormatSpec       string
	Owns             []string
	FactHint         string
	AdjacentSurfaces []Surface
}

var surfaceContracts = map[Surface]SurfaceContract{
	SurfaceRootREADME: {
		Name: SurfaceRootREADME, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-1--root-readme",
		Owns:             []string{"README.md"},
		FactHint:         "root-overview",
		AdjacentSurfaces: []Surface{SurfaceEnvREADME},
	},
	SurfaceEnvREADME: {
		Name: SurfaceEnvREADME, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-2--environment-readme",
		Owns:             []string{"*/README.md"},
		FactHint:         "tier-promotion",
		AdjacentSurfaces: []Surface{SurfaceRootREADME, SurfaceEnvImportComments},
	},
	SurfaceEnvImportComments: {
		Name: SurfaceEnvImportComments, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-3--environment-importyaml-comments",
		Owns:             []string{"*/import.yaml"},
		FactHint:         "tier-decision",
		AdjacentSurfaces: []Surface{SurfaceEnvREADME, SurfaceCodebaseZeropsComments},
	},
	SurfaceCodebaseIG: {
		Name: SurfaceCodebaseIG, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-4--per-codebase-readme-integration-guide-fragment",
		Owns:             []string{"codebases/*/README.md#integration-guide"},
		FactHint:         "porter-change",
		AdjacentSurfaces: []Surface{SurfaceCodebaseKB, SurfaceCodebaseZeropsComments},
	},
	SurfaceCodebaseKB: {
		Name: SurfaceCodebaseKB, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-5--per-codebase-readme-knowledge-base--gotchas-fragment",
		Owns:             []string{"codebases/*/README.md#gotchas"},
		FactHint:         "platform-trap",
		AdjacentSurfaces: []Surface{SurfaceCodebaseIG},
	},
	SurfaceCodebaseCLAUDE: {
		Name: SurfaceCodebaseCLAUDE, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-6--per-codebase-claudemd",
		Owns:             []string{"codebases/*/CLAUDE.md"},
		FactHint:         "operational",
		AdjacentSurfaces: []Surface{SurfaceCodebaseIG},
	},
	SurfaceCodebaseZeropsComments: {
		Name: SurfaceCodebaseZeropsComments, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-7--per-codebase-zeropsyaml-comments",
		Owns:             []string{"codebases/*/zerops.yaml"},
		FactHint:         "scaffold-decision",
		AdjacentSurfaces: []Surface{SurfaceEnvImportComments, SurfaceCodebaseIG},
	},
}

// Surfaces returns all surface names in a deterministic order.
func Surfaces() []Surface {
	return []Surface{
		SurfaceRootREADME,
		SurfaceEnvREADME,
		SurfaceEnvImportComments,
		SurfaceCodebaseIG,
		SurfaceCodebaseKB,
		SurfaceCodebaseCLAUDE,
		SurfaceCodebaseZeropsComments,
	}
}

// ContractFor returns the SurfaceContract for a surface, or false if unknown.
func ContractFor(s Surface) (SurfaceContract, bool) {
	c, ok := surfaceContracts[s]
	return c, ok
}

// Phase 2 populates these via package-init registration. Phase 1 leaves
// them empty; any consumer of InputFnFor / ValidatorFor must handle the
// nil-function case.
var (
	surfaceInputFns   = map[Surface]InputFn{}
	surfaceValidators = map[Surface]ValidateFn{}
)

// RegisterInputFn registers a surface's InputFn. Called from init() in the
// implementing file. Last registration wins (supports test-time overrides
// within the same test binary).
func RegisterInputFn(s Surface, fn InputFn) {
	surfaceInputFns[s] = fn
}

// RegisterValidator registers a surface's ValidateFn. Called from init().
func RegisterValidator(s Surface, v ValidateFn) {
	surfaceValidators[s] = v
}

// InputFnFor returns a registered InputFn or nil.
func InputFnFor(s Surface) InputFn { return surfaceInputFns[s] }

// ValidatorFor returns a registered ValidateFn or nil.
func ValidatorFor(s Surface) ValidateFn { return surfaceValidators[s] }
