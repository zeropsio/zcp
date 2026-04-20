package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// specificityMarkers are tokens that signal a comment is earning its
// keep — it explains WHY, names a concrete platform behavior, or points
// at a failure mode the reader would otherwise trip on. A comment
// containing any of these is "specific"; comments without any marker are
// boilerplate ("npm ci for reproducible builds" is the archetype — true
// of every recipe, load-bearing on none).
//
// The list is intentionally coarse: we want to accept real comments
// without stylistic hand-wringing, not grade them for prose quality.
// Every v12 API zerops.yaml comment already clears this bar.
var specificityMarkers = []string{
	// causal / reasoning
	"because", "so that", "otherwise", "prevents", "required", "ensures",
	"needed", "must", "without",
	// failure mode
	"fails", "breaks", "crashes", "silent", "race", "502", "401", "cold start",
	"blocked", "drops", "empty",
	// platform constraints
	"zerops", "execonce", "l7", "balancer", "httpsupport", "advisory lock",
	"0.0.0.0", "subdomain", "reverse proxy", "terminates ssl", "trust proxy",
	"vxlan", "${",
	// framework×platform intersection signals
	"build time", "build-time", "runtime", "os-level", "horizontal container",
	"multi-container", "fresh container", "stateless", "bundle",
}

// minSpecificComments is the absolute floor — even a very short
// zerops.yaml must have at least this many non-boilerplate comments to
// pass the specificity check.
const minSpecificComments = 3

// specificCommentRatio is the proportional floor — even a very long
// zerops.yaml with many comments must have at least this fraction that
// clear the specificity bar. Tuned low enough that v12 API's comments
// easily pass; tight enough that copy-paste boilerplate fails.
const specificCommentRatio = 0.25

// commentSpecificityRatio measures how many comment lines in a zerops.yaml
// block clear the specificity floor. A comment is specific when it
// contains at least one specificityMarker (after lowercasing). The check
// that consumes this ratio requires both an absolute floor (at least
// minSpecificComments specific comments) and a proportional floor (at
// least specificCommentRatio of all comments are specific).
func commentSpecificityRatio(yamlContent string) (specific, total int, ratio float64) {
	for line := range strings.SplitSeq(yamlContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		total++
		lower := strings.ToLower(trimmed)
		for _, marker := range specificityMarkers {
			if strings.Contains(lower, marker) {
				specific++
				break
			}
		}
	}
	if total == 0 {
		return 0, 0, 0
	}
	return specific, total, float64(specific) / float64(total)
}

// CheckCommentSpecificity is the companion to the comment-ratio check.
// commentRatio measures how many comments are PRESENT; specificity
// measures how many are LOAD-BEARING. The v12 session had comments like
// "npm ci for reproducible builds" and "cache node_modules between
// builds" that pass the 30%-present ratio but read as generic
// boilerplate that could appear in any recipe. A reader learning Zerops
// from the integration guide needs to see comments that explain
// Zerops-specific constraints — execOnce on multi-container deploys,
// L7 balancer behavior, the $-interpolated credential injection, the
// tilde deployFiles suffix.
//
// Scoped to showcase tier. Returns nil (no check emitted) when the
// input block has zero comments — the check is about QUALITY of
// comments that exist, not VOLUME.
func CheckCommentSpecificity(_ context.Context, yamlBlock string, isShowcase bool) []workflow.StepCheck {
	if !isShowcase {
		return nil
	}
	specific, total, ratio := commentSpecificityRatio(yamlBlock)
	if total == 0 {
		return nil
	}
	if specific >= minSpecificComments && ratio >= specificCommentRatio {
		return []workflow.StepCheck{{
			Name:   "comment_specificity",
			Status: StatusPass,
			Detail: fmt.Sprintf("%d of %d comments are specific (%.0f%%)", specific, total, ratio*100),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "comment_specificity",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"comment specificity too low: %d of %d are specific (%.0f%%, need >= %d and >= %.0f%%). Specific means the comment explains WHY (because/so that/prevents/required/fails/breaks), or names a Zerops platform term (execOnce, L7 balancer, ${env_var}, httpSupport, 0.0.0.0, subdomain, advisory lock, trust proxy, cold start, build time, horizontal container). Generic lines like \"npm ci for reproducible builds\" or \"cache node_modules between builds\" pass the ratio check but teach the reader nothing Zerops-specific. Rewrite boilerplate comments to explain what would break without them.",
			specific, total, ratio*100, minSpecificComments, specificCommentRatio*100,
		),
	}}
}
