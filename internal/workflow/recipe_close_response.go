package workflow

import "fmt"

// buildClosePostCompletion returns the post-completion summary and next-steps
// for the close step. Two entries: export at [0] (autonomous — the agent
// runs it without asking the user) and publish at [1] (user-gated — relay
// to the user only when they explicitly asked to ship). v8.98 Fix B gives
// export a named trigger so it no longer relies on an out-of-scope
// orchestrator prompt to fire.
func buildClosePostCompletion(plan *RecipePlan, outputDir string) (string, []string) {
	slug := "<slug>"
	if plan != nil && plan.Slug != "" {
		slug = plan.Slug
	}
	dir := "<recipe-dir>"
	if outputDir != "" {
		dir = outputDir
	}
	summary := "Recipe verified (code-review + close-browser-walk complete). Next: run export autonomously against the output directory; relay the publish command to the user only if they explicitly asked to ship."
	nextSteps := []string{
		fmt.Sprintf("Export the archive now (autonomous, not user-gated): run `zcp sync recipe export %s`. The server-side close gate is satisfied; export will succeed. Include `--include-timeline` if TIMELINE.md is not yet present.", dir),
		fmt.Sprintf("To publish to zeropsio/recipes: run `zcp sync recipe publish %s %s`. This opens a PR on the recipes repo; relay to the user only when they explicitly asked to ship.", slug, dir),
	}
	return summary, nextSteps
}

// buildRecipeTransition returns the post-completion transition message with
// publish commands, test instructions, and eval launch info.
func buildRecipeTransition(plan *RecipePlan) string {
	return fmt.Sprintf(`

## Recipe Complete: %s

### Publish
1. Push to GitHub:
   `+"`"+`zcp sync push recipes %s`+"`"+`
2. After merge, clear Strapi cache:
   `+"`"+`zcp sync cache-clear %s`+"`"+`
3. Pull merged version:
   `+"`"+`zcp sync pull recipes %s`+"`"+`

### Test
Run through eval to verify quality:
`+"`"+`zcp eval run --recipe %s`+"`"+`
`, plan.Slug, plan.Slug, plan.Slug, plan.Slug, plan.Slug)
}
