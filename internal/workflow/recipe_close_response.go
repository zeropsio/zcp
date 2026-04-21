package workflow

import "fmt"

// buildClosePostCompletion returns the post-completion summary for the
// close step. Per C-11 (principles.md P4 §Replaces + data-flow-showcase
// §6b) the close response carries `NextSteps = []` unconditionally —
// export and publish are user-request-only local CLI commands, never
// workflow steps the agent triggers unprompted. v8.98 Fix B originally
// framed export as autonomous; v8.103 content-only reverted that;
// C-11 makes the empty-default structural instead of content-only.
//
// The server-side close gate on `zcp sync recipe export` remains — it
// refuses an early export with a diagnostic. That gate exists to catch
// premature user invocations, not to signal the agent should trigger
// export. plan + outputDir are retained on the signature for symmetry
// with buildRecipeTransition (still emits the user-facing publish
// walkthrough) and for future use when the close response grows a
// real summary surface.
func buildClosePostCompletion(plan *RecipePlan, outputDir string) (string, []string) {
	_ = plan
	_ = outputDir
	summary := "Recipe verified (editorial-review + code-review + close-browser-walk complete). The workflow is done. Export and publish are local CLI commands the user runs on demand — do NOT trigger them autonomously."
	return summary, []string{}
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
