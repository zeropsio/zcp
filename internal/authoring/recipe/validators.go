package recipe

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Run-8-readiness Workstream D — spec validators walk the stitched
// output and enforce the per-surface contracts in
// docs/spec-content-surfaces.md. Violations surface through
// FinalizeGates() so `complete-phase` blocks on any missed rule.
//
// Each validator lives as a package-level function registered at init
// against a Surface. The harness (RunSurfaceValidators) resolves the
// surface's owned files from disk, calls the registered ValidateFn per
// file, and aggregates violations.

// causalWords is the allowlist of tokens that satisfy the
// causal-phrase requirement in yaml and import comments. Kept generous
// so idiomatic causal phrasings don't trip a false positive — run-8
// risk note §6.
var causalWords = []string{
	"because", "so that", "so the ", "so we ", "so it ",
	"so a ", "so an ", "so each ", "so any ", "so every ",
	"otherwise", "required for", "required so", "required because",
	"trade-off", "trade off",
	"avoids", "avoid ", "prevents", "prevent ",
	" — ", // em-dash separated rationale is common
	"instead of", "rather than",
	"to allow", "to keep", "to let",
	"mandatory", "must ",
	"without this", "without it",
	"enables", "allows the",
}

// metaVoiceWords are words that leak agent-voice into a porter-facing
// surface. Run-8-readiness §2.D env-README rule.
var metaVoiceWords = []string{
	" agent ", "agent-", "the agent ", "sub-agent", "subagent",
	"zerops_knowledge", "zerops_recipe", "zerops_workflow",
	"the scaffolder", "our scaffold",
}

// boldBulletRE matches a markdown bullet that opens with `**...**`
// (the bold symptom the KB contract requires on every bullet).
var boldBulletRE = regexp.MustCompile(`(?m)^\s*-\s+\*\*[^*]+\*\*`)

// containsAny reports whether any of needles appears in haystack (case
// insensitive).
func containsAny(haystack string, needles []string) bool {
	hay := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(hay, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// containsAnyCausal specializes containsAny with the causalWords list.
func containsAnyCausal(s string) bool { return containsAny(s, causalWords) }

// extractBetweenMarkers returns the body between the start/end markers
// for a named fragment. Empty string when no marker or no body.
func extractBetweenMarkers(body, name string) string {
	start := "<!-- #ZEROPS_EXTRACT_START:" + name + "# -->"
	end := "<!-- #ZEROPS_EXTRACT_END:" + name + "# -->"
	_, after, ok := strings.Cut(body, start)
	if !ok {
		return ""
	}
	inside, _, ok := strings.Cut(after, end)
	if !ok {
		return ""
	}
	return strings.TrimSpace(inside)
}

// RunSurfaceValidators walks every registered surface validator,
// resolves its owned files from the recipe's output tree, and returns
// the aggregate violations. Used by finalize `complete-phase` after
// stitch-content lands the surfaces on disk.
func RunSurfaceValidators(ctx context.Context, outputRoot string, plan *Plan, facts []FactRecord, parent *ParentRecipe) []Violation {
	inputs := SurfaceInputs{Plan: plan, Facts: facts, Parent: parent}
	var violations []Violation
	for _, s := range Surfaces() {
		fn := ValidatorFor(s)
		if fn == nil {
			continue
		}
		paths := resolveSurfacePaths(outputRoot, s, plan)
		for _, p := range paths {
			content, err := os.ReadFile(p)
			if err != nil {
				if os.IsNotExist(err) {
					continue // missing-file is the stitch gate's job
				}
				violations = append(violations, Violation{
					Code: "validator-read-failed", Path: p, Message: err.Error(),
				})
				continue
			}
			vs, err := fn(ctx, p, content, inputs)
			if err != nil {
				violations = append(violations, Violation{
					Code: "validator-error", Path: p, Message: err.Error(),
				})
				continue
			}
			violations = append(violations, vs...)
		}
	}
	// Cross-surface uniqueness runs against the full stitched surface
	// set — not per-file — so it lives outside the per-surface loop.
	surfaces := map[string]string{}
	for _, s := range Surfaces() {
		for _, p := range resolveSurfacePaths(outputRoot, s, plan) {
			if body, err := os.ReadFile(p); err == nil {
				surfaces[filepath.Base(p)] = string(body)
			}
		}
	}
	violations = append(violations, validateCrossSurfaceUniqueness(surfaces, facts)...)
	return violations
}

// resolveSurfacePaths returns the list of disk paths a surface's
// Owns-glob resolves to under outputRoot.
func resolveSurfacePaths(outputRoot string, s Surface, plan *Plan) []string {
	switch s {
	case SurfaceRootREADME:
		return []string{filepath.Join(outputRoot, "README.md")}
	case SurfaceEnvREADME:
		var out []string
		for _, t := range Tiers() {
			out = append(out, filepath.Join(outputRoot, t.Folder, "README.md"))
		}
		return out
	case SurfaceEnvImportComments:
		var out []string
		for _, t := range Tiers() {
			out = append(out, filepath.Join(outputRoot, t.Folder, "import.yaml"))
		}
		return out
	case SurfaceCodebaseIG, SurfaceCodebaseKB:
		return codebasePaths(outputRoot, plan, "README.md")
	case SurfaceCodebaseCLAUDE:
		return codebasePaths(outputRoot, plan, "CLAUDE.md")
	case SurfaceCodebaseZeropsComments:
		return codebasePaths(outputRoot, plan, "zerops.yaml")
	}
	return nil
}

// codebasePaths resolves per-codebase surface files to <cb.SourceRoot>/<leaf>,
// matching the apps-repo shape at the reference (`laravel-showcase-app/`)
// and the stitch write target. Codebases without a SourceRoot are skipped —
// stitch would already have refused to produce their files. Run-10-readiness §L.
func codebasePaths(_ string, plan *Plan, leaf string) []string {
	if plan == nil {
		return nil
	}
	out := make([]string, 0, len(plan.Codebases))
	for _, cb := range plan.Codebases {
		if cb.SourceRoot == "" {
			continue
		}
		out = append(out, filepath.Join(cb.SourceRoot, leaf))
	}
	return out
}

// violation is a small ctor helper so validator bodies stay concise.
// Default severity is blocking — defaulting to notice would silently
// relax existing gates. Validators on the DISCOVER side of the
// TEACH/DISCOVER line (system.md §4) opt in via `notice()`.
func violation(code, path, msg string) Violation {
	return Violation{Code: code, Path: path, Message: msg}
}

// notice is the ctor for SeverityNotice findings — the agent sees the
// finding at gate-eval time but `complete-phase` does not block. Used
// by validators wired on the DISCOVER side of the TEACH/DISCOVER line.
func notice(code, path, msg string) Violation {
	return Violation{Code: code, Path: path, Message: msg, Severity: SeverityNotice}
}

// registerValidators wires every validator at init. Kept in one place
// so the surface → function mapping is single-source.
func init() {
	RegisterValidator(SurfaceRootREADME, validateRootREADME)
	RegisterValidator(SurfaceEnvREADME, validateEnvREADME)
	RegisterValidator(SurfaceEnvImportComments, validateEnvImportComments)
	RegisterValidator(SurfaceCodebaseIG, validateCodebaseIG)
	RegisterValidator(SurfaceCodebaseKB, validateCodebaseKB)
	RegisterValidator(SurfaceCodebaseCLAUDE, validateCodebaseCLAUDE)
	RegisterValidator(SurfaceCodebaseZeropsComments, validateCodebaseYAML)
}

// Individual surface validators live in sibling files:
//   validators_root_env.go — root README + env README + env import comments
//   validators_codebase.go — per-codebase IG + KB + CLAUDE.md + zerops.yaml
//   plus cross-surface uniqueness.
