// emit-refinement subcommand — composes the refinement sub-agent
// dispatch prompt against a stitched simulation. Run after
// codebase-content + env-content sub-agents have authored fragments
// AND `stitch` has assembled the full corpus.
//
// Spec: docs/zcprecipator3/plans/run-20-prep.md §S5.
//
// The refinement brief composer (briefs_refinement.go) builds its
// "## Stitched output to refine" pointer block when runDir is non-
// empty; the sim path supplies the simulation directory verbatim.
// Production wires runDir via Session.OutputRoot at the handler
// boundary; the static composition path BuildSubagentPromptForReplay
// passes "" instead, so the sim driver drops below the public entry
// and calls BuildRefinementBrief directly. This is the only refinement-
// specific divergence from the canonical engine prompt.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/recipe"
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

	brief, err := recipe.BuildRefinementBrief(plan, parent, absDir, facts)
	if err != nil {
		return fmt.Errorf("BuildRefinementBrief: %w", err)
	}

	full := refinementPromptHeader(plan.Slug, absDir) +
		replayAdapter(refineDir) +
		brief.Body +
		refinementPromptFooter()
	promptPath := filepath.Join(briefsDir, "refinement-prompt.md")
	if err := os.WriteFile(promptPath, []byte(full), 0o600); err != nil {
		return fmt.Errorf("write refinement prompt: %w", err)
	}
	fmt.Printf("refinement: %d bytes → %s\n", len(full), promptPath)
	fmt.Printf("\nready: dispatch the refinement Agent against %s,\nthen re-run `zcp-recipe-sim stitch -dir %s` to fold replace-fragment recordings into the stitched corpus.\n",
		promptPath, absDir)
	return nil
}

// refinementPromptHeader is the sim-side analog of writePromptHeader
// for the refinement kind. Kept here (not in emit.go) because the
// production composer's writePromptHeader is package-private; this
// matches its shape without reaching across the package boundary.
func refinementPromptHeader(slug, simDir string) string {
	return fmt.Sprintf(`You are the refinement sub-agent for the %s recipe.
Read the engine brief below verbatim and follow it; the recipe-level
context above and the closing notes below the brief are wrapper notes
from the engine.

**Tool-call shape**: `+"`zerops_recipe`"+` is an **MCP tool** invoked as a
JSON tool call (e.g. `+"`{\"action\": \"record-fragment\", ...}`"+`).
It is NOT a shell command. The brief uses backtick shorthand
`+"`zerops_recipe action=X slug=Y`"+` to refer to an MCP invocation; do
not run it via Bash.

## Recipe-level context

- Slug: `+"`%s`"+`
- Simulation root: `+"`%s`"+`

---

# Engine brief — refinement

`, slug, slug, simDir)
}

// refinementPromptFooter mirrors the production refinement footer
// (writePromptCloseFooter / BriefRefinement branch) so the replayed
// agent sees the snapshot/restore + edit-cap teaching.
func refinementPromptFooter() string {
	return `

---

## Closing notes from the engine

When you've refined every fragment that meets the 100%-sure
threshold, terminate (this is an offline replay; no
` + "`complete-phase`" + ` MCP tool to call). Each fragment file
you Write at this phase replaces the original verbatim — there
is no engine-side snapshot/restore primitive in replay, so your
refinement is final. Per-fragment edit cap is 1 attempt; do NOT
loop.
`
}
