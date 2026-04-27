package recipe

import (
	"context"
	"strings"
)

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

// Severity classifies a Violation's effect on phase completion. The
// zero value (SeverityBlocking) keeps `complete-phase` blocked — the
// historical behavior every gate had before the 2026-04-25
// architectural reframe. SeverityNotice is opt-in for findings that
// belong on the DISCOVER side of the TEACH/DISCOVER line (system.md
// §4): the agent sees the lesson at gate-eval time but publication
// is not held back. Any validator that returns a Violation with no
// severity set is treated as blocking, by design — defaulting to
// notice would silently relax existing gates.
type Severity int

const (
	SeverityBlocking Severity = iota
	SeverityNotice
)

// Violation is one finding from a ValidateFn. Empty return means pass.
type Violation struct {
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
	Severity Severity `json:"-"`
}

// PartitionBySeverity splits a violation slice into blocking + notice
// halves. Used by complete-phase to decide whether the phase advances
// (blocking only) and what surfaces in `Notices` for the agent's eyes.
func PartitionBySeverity(vs []Violation) (blocking, notices []Violation) {
	for _, v := range vs {
		if v.Severity == SeverityNotice {
			notices = append(notices, v)
		} else {
			blocking = append(blocking, v)
		}
	}
	return
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
//
// Reader / Test / Length caps mirror docs/spec-content-surfaces.md
// §"Per-surface line-budget table" + each §"Surface N" section. The
// engine returns this struct on every record-fragment response so the
// agent reads the relevant test verbatim at authoring decision time
// (run-15 F.2). Caps are zero when not applicable to that surface
// (e.g. ItemCap=0 on the root README which counts lines, not items).
type SurfaceContract struct {
	Name             Surface   `json:"name"`
	Author           Author    `json:"author"`
	FormatSpec       string    `json:"formatSpec"`
	Owns             []string  `json:"owns,omitempty"`
	FactHint         string    `json:"factHint,omitempty"`
	AdjacentSurfaces []Surface `json:"adjacentSurfaces,omitempty"`
	// Reader is the one-sentence audience description per spec §"Surface
	// N → Reader". Threaded into the record-fragment response so the
	// agent re-reads the audience at the moment of authoring.
	Reader string `json:"reader,omitempty"`
	// Test is the single-question self-review test the spec attaches to
	// each surface (spec §"Per-surface test cheatsheet"). Returned on
	// record-fragment so authors apply it before terminating.
	Test string `json:"test,omitempty"`
	// LineCap is the spec's hard line cap for the whole surface body.
	// Zero when the surface uses ItemCap or IntroExtractCharCap instead.
	LineCap int `json:"lineCap,omitempty"`
	// ItemCap is the spec's hard item / bullet cap (IG items, KB bullets,
	// tier service-block comment lines). Zero when not applicable.
	ItemCap int `json:"itemCap,omitempty"`
	// IntroExtractCharCap is the hard cap for content between
	// `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers (Surface 2: 350
	// chars per spec). Zero on surfaces without an extract marker.
	IntroExtractCharCap int `json:"introExtractCharCap,omitempty"`
}

var surfaceContracts = map[Surface]SurfaceContract{
	SurfaceRootREADME: {
		Name: SurfaceRootREADME, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-1--root-readme",
		Owns:             []string{"README.md"},
		FactHint:         "root-overview",
		AdjacentSurfaces: []Surface{SurfaceEnvREADME},
		Reader:           "A developer browsing zerops.io/recipes deciding whether to click deploy.",
		Test:             "Can a reader decide in 30 seconds whether this recipe deploys what they need, and pick the right tier?",
		LineCap:          35,
	},
	SurfaceEnvREADME: {
		Name: SurfaceEnvREADME, Author: AuthorWriter,
		FormatSpec:          "docs/spec-content-surfaces.md#surface-2--environment-readme",
		Owns:                []string{"*/README.md"},
		FactHint:            "tier-promotion",
		AdjacentSurfaces:    []Surface{SurfaceRootREADME, SurfaceEnvImportComments},
		Reader:              "Someone hovering over a tier card on zerops.io/recipes — the recipe-page UI renders the content between the EXTRACT markers as the tier-card description.",
		Test:                "Does this 1-2 sentence card description tell a porter which tier to click?",
		LineCap:             10,
		IntroExtractCharCap: 350,
	},
	SurfaceEnvImportComments: {
		Name: SurfaceEnvImportComments, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-3--environment-importyaml-comments",
		Owns:             []string{"*/import.yaml"},
		FactHint:         "tier-decision",
		AdjacentSurfaces: []Surface{SurfaceEnvREADME, SurfaceCodebaseZeropsComments},
		Reader:           "Someone who deployed this tier and is reading the manifest in the Zerops dashboard to understand what they're running.",
		Test:             "Does each service-block comment explain a decision (scale, mode, why this service exists at this tier), not just narrate what the field does?",
		LineCap:          40,
		ItemCap:          5, // 3-5 lines per service block, max 8
	},
	SurfaceCodebaseIG: {
		Name: SurfaceCodebaseIG, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-4--per-codebase-readme-integration-guide-fragment",
		Owns:             []string{"codebases/*/README.md#integration-guide"},
		FactHint:         "porter-change",
		AdjacentSurfaces: []Surface{SurfaceCodebaseKB, SurfaceCodebaseZeropsComments},
		Reader:           "A porter bringing their own existing application — they extract the Zerops-specific steps to adapt their own code.",
		Test:             "Does a porter who isn't using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?",
		ItemCap:          5,
	},
	SurfaceCodebaseKB: {
		Name: SurfaceCodebaseKB, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-5--per-codebase-readme-knowledge-base--gotchas-fragment",
		Owns:             []string{"codebases/*/README.md#gotchas"},
		FactHint:         "platform-trap",
		AdjacentSurfaces: []Surface{SurfaceCodebaseIG},
		Reader:           "A developer hitting a confusing failure on Zerops and searching for what's wrong.",
		Test:             "Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?",
		ItemCap:          8,
	},
	SurfaceCodebaseCLAUDE: {
		Name: SurfaceCodebaseCLAUDE, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-6--per-codebase-claudemd",
		Owns:             []string{"codebases/*/CLAUDE.md"},
		FactHint:         "operational",
		AdjacentSurfaces: []Surface{SurfaceCodebaseIG},
		Reader:           "Someone (human or AI agent) with this repo checked out locally, working on the codebase.",
		Test:             "Is this useful for operating THIS repo specifically — not for deploying it to Zerops, not for porting it to other code?",
		LineCap:          50,
	},
	SurfaceCodebaseZeropsComments: {
		Name: SurfaceCodebaseZeropsComments, Author: AuthorWriter,
		FormatSpec:       "docs/spec-content-surfaces.md#surface-7--per-codebase-zeropsyaml-comments",
		Owns:             []string{"codebases/*/zerops.yaml"},
		FactHint:         "scaffold-decision",
		AdjacentSurfaces: []Surface{SurfaceEnvImportComments, SurfaceCodebaseIG},
		Reader:           "Someone reading the deploy config to understand or modify it.",
		Test:             "Does each comment explain a trade-off or consequence the reader couldn't infer from the field name?",
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

// SurfaceFromFragmentID maps a record-fragment id to the Surface that
// owns its content body. Run-15 F.2 — the engine returns the resolved
// surface's contract on every record-fragment response so the agent
// reads the per-surface reader / test / caps verbatim at authoring
// decision time, not just at brief-preface time.
//
// Fragment-id schema (mirrors handlers.RecipeInput.FragmentID):
//
//   - root/intro                                       → SurfaceRootREADME
//   - env/<N>/intro                                    → SurfaceEnvREADME
//   - env/<N>/import-comments/<host|project>           → SurfaceEnvImportComments
//   - codebase/<host>/intro                            → SurfaceCodebaseIG (intro lives in the codebase README alongside IG)
//   - codebase/<host>/integration-guide                → SurfaceCodebaseIG
//   - codebase/<host>/knowledge-base                   → SurfaceCodebaseKB
//   - codebase/<host>/claude-md/{service-facts,notes}  → SurfaceCodebaseCLAUDE
//
// Returns ("", false) for unknown ids — caller treats as no contract
// to attach.
func SurfaceFromFragmentID(fragmentID string) (Surface, bool) {
	if fragmentID == "" {
		return "", false
	}
	if fragmentID == fragmentIDRoot {
		return SurfaceRootREADME, true
	}
	if rest, ok := strings.CutPrefix(fragmentID, "env/"); ok {
		// rest = "<N>/<tail>"
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return "", false
		}
		tail := rest[slash+1:]
		switch {
		case tail == fragmentTailIntro:
			return SurfaceEnvREADME, true
		case tail == "import-comments/project",
			strings.HasPrefix(tail, "import-comments/"):
			return SurfaceEnvImportComments, true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(fragmentID, "codebase/"); ok {
		slash := strings.IndexByte(rest, '/')
		if slash <= 0 {
			return "", false
		}
		tail := rest[slash+1:]
		switch tail {
		case fragmentTailIntro, "integration-guide":
			return SurfaceCodebaseIG, true
		case "knowledge-base":
			return SurfaceCodebaseKB, true
		case "claude-md/service-facts", "claude-md/notes":
			return SurfaceCodebaseCLAUDE, true
		}
		return "", false
	}
	return "", false
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
