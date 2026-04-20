package workflow

import "fmt"

// buildClosePostCompletion returns the post-completion summary and next-steps
// for the close step. Both entries are STRICTLY user-gated — they are
// reference commands the agent may relay when asked, never unprompted
// actions. v8.98 Fix B originally framed export as autonomous; v8.103
// reverts that: recipe creation ends at close complete, nothing runs
// after unless the user explicitly asks (export for a local archive,
// publish for a PR on zeropsio/recipes).
//
// The server-side close gate on `zcp sync recipe export` remains — it
// refuses an early export with a diagnostic — but the gate existing
// does NOT mean the agent should trigger export. The trigger belongs
// to the user (or to an orchestrator that the user set up explicitly).
func buildClosePostCompletion(plan *RecipePlan, outputDir string) (string, []string) {
	slug := "<slug>"
	if plan != nil && plan.Slug != "" {
		slug = plan.Slug
	}
	dir := "<recipe-dir>"
	if outputDir != "" {
		dir = outputDir
	}
	summary := "Recipe verified (code-review + close-browser-walk complete). The workflow is done. Do NOT run export or publish unless the user explicitly asks — they are local CLI commands, not workflow steps."
	nextSteps := []string{
		fmt.Sprintf("ON REQUEST ONLY — if the user asks for a local archive: `zcp sync recipe export %s`. Do NOT run unprompted.", dir),
		fmt.Sprintf("ON REQUEST ONLY — if the user asks to publish to zeropsio/recipes: `zcp sync recipe publish %s %s`. This opens a PR; do NOT run unprompted.", slug, dir),
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
