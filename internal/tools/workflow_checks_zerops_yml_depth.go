package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// checkZeropsYmlCommentDepth validates that a codebase's zerops.yaml
// carries reasoning-anchored comments at floor parity with the env
// import.yaml comment-depth check (35% of substantive comment blocks
// contain a reasoning marker; ≥2 reasoning comments absolute minimum).
//
// Why this check exists:
//   - zerops.yaml is the source IG #1 copies verbatim into each README's
//     integration-guide fragment. Shallow comments at the source become
//     shallow IG #1 by direct inheritance; there is no later surface
//     where the "why" gets retroactively added.
//   - v22 and earlier runs had no rubric and no check on zerops.yaml
//     comments — the deepest content surface (published to users) was
//     inheriting the weakest teaching surface.
//
// The reasoning-marker corpus is identical to the env import.yaml
// comment-depth check (because / otherwise / without / must / rather
// than / instead of / so that / prevents / leads to / mandatory /
// required / at build time / at runtime / rolling / drain). Same
// taxonomy, same floor, different file.
//
// hostname is used to scope the check name so multi-codebase recipes
// surface failures per-codebase. Empty content returns no checks — the
// existence check upstream handles missing zerops.yaml.
func checkZeropsYmlCommentDepth(zeropsYml, hostname string) []workflow.StepCheck {
	if zeropsYml == "" {
		return nil
	}
	// Reuse the env-comment-depth engine via a hostname-scoped prefix.
	// checkCommentDepth already implements the 35% + hard-floor-of-2
	// rubric with the same reasoning-marker taxonomy; the only thing
	// that differs is the failure-message framing.
	prefix := hostname + "_zerops_yml"
	checks := checkCommentDepth(zeropsYml, prefix)
	// checkCommentDepth's failure detail is written for env import.yaml
	// ("the v7 gold-standard import.yaml comments teach…"). Rewrite the
	// detail on failure so the agent sees a zerops.yaml-shaped message
	// (IG #1 inheritance, field-attached reasoning, etc.) instead of
	// the env-comments language that doesn't quite fit here.
	for i := range checks {
		if checks[i].Status != statusFail {
			continue
		}
		checks[i].Detail = fmt.Sprintf(
			"%s zerops.yaml comments describe WHAT fields do but not WHY the values were chosen. IG #1 of this codebase's README copies zerops.yaml verbatim — shallow comments here become shallow integration-guide teaching. Rewrite comments attached to each field so they answer one of: WHY this value (vs the obvious alternative: 'nodejs@22 rather than nodejs@20 because the TypeORM driver needs structuredClone'), WHAT BREAKS if flipped ('binding 127.0.0.1 makes the L7 balancer return 502'), or HOW THIS AFFECTS operations ('prepareCommands at build time so rolling deploys don't block on npm ci'). Bare narration like 'install dependencies' or 'start the application' fails; a comment ending in `because X` / `otherwise Y` / `rather than Z` / `at build time so…` / `without this…` passes. Floor: %s",
			hostname, stripPrefix(checks[i].Detail, "only "),
		)
		// Name is already prefixed with hostname_zerops_yml by the
		// env-depth engine ("{prefix}_comment_depth" → "apidev_zerops
		// _yml_comment_depth"). Keep that naming.
	}
	return checks
}

// stripPrefix returns s with the leading needle removed (returns s
// unchanged if the prefix isn't present). Small helper kept local so
// the failure-message rewrite stays self-contained.
func stripPrefix(s, needle string) string {
	return strings.TrimPrefix(s, needle)
}
