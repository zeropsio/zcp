// emit-refinement subcommand — composes the refinement sub-agent
// dispatch prompt against a stitched simulation. Run after
// codebase-content + env-content sub-agents have authored fragments
// AND `stitch` has assembled the full corpus.
//
// Spec: docs/zcprecipator3/plans/run-20-prep.md §S5.
//
// Aligned with production multi-file shape (run-31 Fix #1 closure):
// production refinement routes through buildRefinementBriefMultiFile
// WithFraming via buildSubagentDispatchForPhase (handlers.go:477) and
// writes index.md + N part-*.md to <outputRoot>/.briefs/refinement-
// phase/. The sim mirrors that exact shape via the public wrapper
// recipe.BuildRefinementBriefMultiFile, with sim-flavored header
// (recipe context) and footer (closing notes minus the MCP
// `complete-phase` step that has no replay equivalent) composed
// here. The legacy single-file briefs/refinement-prompt.md is now a
// thin pointer carrying the replay-adapter + a path to index.md, so
// the brief-edits-land-identically-in-sim-and-prod contract documented
// at internal/authoring/recipe/briefs_subagent_prompt.go:32-38 holds for
// refinement. Pinned by TestRunEmitRefinement_MatchesProductionMulti
// FileShape.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/authoring/recipe"
)

func runEmitRefinement(args []string) error {
	fs := flag.NewFlagSet("emit-refinement", flag.ContinueOnError)
	dir := fs.String("dir", "", "simulation directory previously populated by `emit` + sub-agent dispatch + `stitch`")
	mountRoot := fs.String("mount-root", "", "recipes mount root for parent chain resolution (mirrors emit -mount-root)")
	parentOverride := fs.String("parent", "", "parent recipe slug override (mirrors emit -parent); requires -mount-root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("emit-refinement: -dir is required")
	}
	if *parentOverride != "" && *mountRoot == "" {
		return errors.New("emit-refinement: -parent requires -mount-root")
	}

	envDir := filepath.Join(*dir, "environments")
	plan, err := recipe.ReadPlan(envDir)
	if err != nil {
		return fmt.Errorf("read plan from %s: %w", envDir, err)
	}
	facts, err := loadFactsJSONL(filepath.Join(envDir, "facts.jsonl"))
	if err != nil {
		return fmt.Errorf("load facts: %w", err)
	}

	parent, err := loadEmitParent(plan.Slug, *parentOverride, *mountRoot)
	if err != nil {
		return err
	}

	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return fmt.Errorf("abs(%s): %w", *dir, err)
	}

	briefsDir := filepath.Join(absDir, "briefs")
	if err := os.MkdirAll(briefsDir, 0o755); err != nil {
		return err
	}
	fragsRoot := filepath.Join(absDir, "fragments-new")
	refineDir := filepath.Join(fragsRoot, "refinement")
	if err := os.MkdirAll(refineDir, 0o755); err != nil {
		return err
	}

	// Build the multi-file brief shape under <absDir>/.briefs/refinement-
	// phase/ — index.md + N part-*.md — exactly as production does. The
	// sim-flavored header carries recipe-level context and the footer
	// substitutes the MCP `complete-phase` step (no replay equivalent)
	// with a plain "terminate when done" instruction.
	header := refinementMultiFileHeader(plan.Slug, absDir)
	footer := refinementMultiFileFooter()
	brief, err := recipe.BuildRefinementBriefMultiFile(plan, parent, absDir, facts, absDir, header, footer)
	if err != nil {
		return fmt.Errorf("BuildRefinementBriefMultiFile: %w", err)
	}
	if brief.IndexPath == "" {
		return errors.New("BuildRefinementBriefMultiFile returned empty IndexPath")
	}

	// Legacy briefs/refinement-prompt.md becomes a thin dispatch pointer:
	// the replay-adapter (file-write redirect for record-fragment) plus a
	// path to the multi-file index. Sub-agents read the pointer first,
	// then walk the index's "Read order" section through every part file.
	full := refinementDispatchPointer(plan.Slug, absDir, brief.IndexPath, refineDir)
	promptPath := filepath.Join(briefsDir, "refinement-prompt.md")
	if err := os.WriteFile(promptPath, []byte(full), 0o600); err != nil {
		return fmt.Errorf("write refinement prompt: %w", err)
	}
	fmt.Printf("refinement: multi-file brief → %s (+ %d part files)\n", brief.IndexPath, len(brief.PartPaths))
	fmt.Printf("refinement: dispatch pointer (%d bytes) → %s\n", len(full), promptPath)
	fmt.Printf("\nready: dispatch the refinement Agent against %s,\nthen re-run `zcp-recipe-sim stitch -dir %s` to fold replace-fragment recordings into the stitched corpus.\n",
		promptPath, absDir)
	return nil
}

// refinementMultiFileHeader is the sim-side analog of the recipe-level
// header production composes via composePromptHeaderAndFooter — recipe
// context (slug, simulation root, tool-call shape note) without the
// "Engine brief — refinement" inline divider, because the multi-file
// shape points at part files rather than inlining a brief body. Lands
// at the top of <absDir>/.briefs/refinement-phase/index.md, ABOVE the
// "Read order" section.
func refinementMultiFileHeader(slug, simDir string) string {
	return fmt.Sprintf(`You are the refinement sub-agent for the %s recipe.
Read the engine brief below verbatim and follow it; the recipe-level
context above and the closing notes below are wrapper notes from the
engine.

**Tool-call shape**: `+"`zerops_recipe`"+` is an **MCP tool** invoked as a
JSON tool call (e.g. `+"`{\"action\": \"record-fragment\", ...}`"+`).
It is NOT a shell command. The brief uses backtick shorthand
`+"`zerops_recipe action=X slug=Y`"+` to refer to an MCP invocation; do
not run it via Bash.

## Recipe-level context

- Slug: `+"`%s`"+`
- Simulation root: `+"`%s`"+`
`, slug, slug, simDir)
}

// refinementMultiFileFooter is the sim-side analog of the
// writePromptCloseFooter BriefRefinement branch. Lives at the BOTTOM
// of index.md (production puts it under the "Read order" listing).
// Substitutes the MCP `complete-phase` step (no replay equivalent)
// with a plain terminate-when-done instruction; otherwise carries the
// snapshot/restore + edit-cap teaching verbatim.
func refinementMultiFileFooter() string {
	return `## Closing notes from the engine

When you've refined every fragment that meets the 100%-sure
threshold, terminate (this is an offline replay; no
` + "`complete-phase`" + ` MCP tool to call). Each fragment file
you Write at this phase replaces the original verbatim — there
is no engine-side snapshot/restore primitive in replay, so your
refinement is final. Per-fragment edit cap is 1 attempt; do NOT
loop.
`
}

// refinementDispatchPointer is the legacy briefs/refinement-prompt.md
// content under the multi-file shape: replay-adapter (file-write
// redirect for record-fragment + on-disk knowledge corpus mapping)
// plus a pointer to the multi-file index. Production has no analog
// (the dispatch envelope itself returns BriefPath pointing at
// index.md); the sim keeps a single entry-point file so existing
// dispatch ergonomics survive.
func refinementDispatchPointer(slug, simDir, indexPath, refineDir string) string {
	return fmt.Sprintf(`You are the refinement sub-agent for the %s recipe.

%s

## Multi-file refinement brief

Read this index FIRST, then walk every part file listed in its "Read
order" section in order BEFORE authoring any refinement:

    %s

Each part is bounded by the per-part token cap; skipping a part means
missing teaching that landed there for a reason.

## Recipe-level context

- Slug: `+"`%s`"+`
- Simulation root: `+"`%s`"+`
`, slug, replayAdapter(refineDir), indexPath, slug, simDir)
}
