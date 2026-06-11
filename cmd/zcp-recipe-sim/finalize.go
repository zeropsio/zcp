// emit-finalize subcommand — composes the finalize sub-agent dispatch
// prompt against a stitched simulation. Run AFTER the codebase-content
// + env-content sub-agents have authored fragments and `stitch` has
// assembled the corpus, BEFORE `emit-refinement`.
//
// In sim the env-content phase already authors `root/intro` + per-tier
// `env/<N>/import-comments/project` (Surface 1 + 3), so the finalize
// sub-agent's only orphan in practice is `root/intro` (defensive: the
// brief still lists the full fragment shape catalog). Production reuses
// the same brief — the engine's BriefFinalize composer is shared.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zeropsio/zcp/internal/authoring/recipe"
)

func runEmitFinalize(args []string) error {
	fs := flag.NewFlagSet("emit-finalize", flag.ContinueOnError)
	dir := fs.String("dir", "", "simulation directory previously populated by `emit` + sub-agent dispatch + `stitch`")
	mountRoot := fs.String("mount-root", "", "recipes mount root for parent chain resolution (mirrors emit -mount-root)")
	parentOverride := fs.String("parent", "", "parent recipe slug override (mirrors emit -parent); requires -mount-root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("emit-finalize: -dir is required")
	}
	if *parentOverride != "" && *mountRoot == "" {
		return errors.New("emit-finalize: -parent requires -mount-root")
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
	finalizeDir := filepath.Join(fragsRoot, "finalize")
	if err := os.MkdirAll(finalizeDir, 0o755); err != nil {
		return err
	}

	// BuildSubagentPromptForReplay handles BriefFinalize end-to-end:
	// recipe-level context header + BuildFinalizeBrief body + finalize
	// closing footer. Same path the production handler composes.
	input := recipe.RecipeInput{
		BriefKind: string(recipe.BriefFinalize),
		Slug:      plan.Slug,
	}
	canonical, err := recipe.BuildSubagentPromptForReplay(plan, parent, input, recipe.PhaseFinalize, *mountRoot, facts)
	if err != nil {
		return fmt.Errorf("BuildSubagentPromptForReplay finalize: %w", err)
	}
	full := replayAdapter(finalizeDir) + canonical
	promptPath := filepath.Join(briefsDir, "finalize-prompt.md")
	if err := os.WriteFile(promptPath, []byte(full), 0o600); err != nil {
		return fmt.Errorf("write finalize prompt: %w", err)
	}
	fmt.Printf("finalize: %d bytes → %s\n", len(full), promptPath)
	fmt.Printf("\nready: dispatch the finalize Agent against %s,\nthen re-run `zcp-recipe-sim stitch -dir %s` to fold finalize fragments before emit-refinement.\n",
		promptPath, absDir)
	return nil
}
