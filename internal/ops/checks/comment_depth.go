package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minReasoningCommentRatio is the minimum fraction of non-trivial
// comments that must contain a reasoning marker (a word or phrase
// that signals the comment is explaining WHY rather than WHAT). 35%
// is calibrated against the v7 set: roughly one in three v7 comments
// carries an explicit reasoning marker, and the other two provide
// context that's harder to grade automatically.
const minReasoningCommentRatio = 0.35

// minReasoningComments is the hard floor regardless of ratio. On a
// short env file (Local or AI Agent), 35% of 4 comments is 2; the
// floor is also 2 so the check doesn't falsely pass a short file
// with a single reasoning comment + one narration comment.
const minReasoningComments = 2

// reasoningMarkers are substrings whose presence in a comment signals
// that the comment is explaining WHY, WHAT-IF, or WHAT-ELSE rather
// than narrating what the adjacent field does. Each category
// corresponds to a class of insight the v7 gold-standard comments
// carried:
//
//   - Consequence markers (because, otherwise, without, so that,
//     means that, prevents, causes) — explain what happens if the
//     decision goes the other way.
//   - Trade-off markers (instead of, rather than, in favor of) —
//     explain why the chosen option beats the alternative.
//   - Constraint markers (must, required, cannot, forced, mandatory) —
//     explain a hard constraint the reader would otherwise violate.
//   - Operational consequence markers (rotation, redeploy, restart,
//     scaling, downtime, fan-out, concurrent) — explain lifecycle
//     implications that matter during operations.
//
// The list is intentionally broad — false positives (comments that
// mention "restart" without explaining anything) are fine because a
// comment using these words at all is almost always reasoning about
// something. False negatives (real insights without any of these
// markers) are less fine but manageable since we only need 35% of
// comments to match.
var reasoningMarkers = []string{
	// Consequence
	"because", "otherwise", "without", "so that", "means that",
	"prevents", "causes", "leads to", "results in", "avoids",
	"which is why", "that way", "for this reason",
	// Trade-off
	"instead of", "rather than", "in favor of", "over ", "as opposed to",
	"trade-off", "tradeoff",
	// Constraint
	"must", "required", "cannot", "forced", "mandatory",
	"never", "always", "guaranteed",
	// Operational consequence
	"rotation", "rotate", "redeploy", "restart",
	"scale", "scaling", "downtime", "zero-downtime", "rolling",
	"fan-out", "fan out", "concurrent", "race", "lock", "drain",
	// Framework × platform intersection signals
	"build time", "build-time", "runtime", "cross-service",
	"at startup", "at runtime", "at import time", "at deploy time",
	// Decision framing
	"we chose", "picked", "default here", "this tier", "this env",
	"matches prod", "mirrors prod",
}

// containsAny returns true when s contains any of the given needles.
func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// CheckCommentDepth walks the import.yaml content, groups contiguous
// `#` lines into logical comment blocks (a multi-line comment is ONE
// piece of reasoning, not N), and computes the fraction of blocks
// whose combined text contains at least one reasoning marker. Fails
// if below minReasoningCommentRatio OR below the minReasoningComments
// hard floor.
//
// Grouping matters: the v7 gold-standard comments are 3-5 lines each
// and carry the "because" on line 1, the consequence on line 3, and
// the rotation note on line 5. Counting per-line would grade 2/5 on a
// block that's actually one complete insight.
func CheckCommentDepth(_ context.Context, content, prefix string) []workflow.StepCheck {
	var blocks []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		text := strings.TrimSpace(current.String())
		if len(text) >= 20 && !strings.HasPrefix(text, "zeropsPreprocessor") {
			blocks = append(blocks, text)
		}
		current.Reset()
	}
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			body := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			if current.Len() > 0 {
				current.WriteByte(' ')
			}
			current.WriteString(body)
			continue
		}
		flush()
	}
	flush()

	total := len(blocks)
	reasoning := 0
	for _, block := range blocks {
		lower := strings.ToLower(block)
		if containsAny(lower, reasoningMarkers) {
			reasoning++
		}
	}
	// Not enough substantive comments to grade — the comment-ratio
	// check handles the "too few comments" case separately; this
	// check is about QUALITY, not VOLUME. Pass silently.
	if total < 3 {
		return []workflow.StepCheck{{
			Name:   prefix + "_comment_depth",
			Status: StatusPass,
		}}
	}
	ratio := float64(reasoning) / float64(total)
	if reasoning >= minReasoningComments && ratio >= minReasoningCommentRatio {
		return []workflow.StepCheck{{
			Name:   prefix + "_comment_depth",
			Status: StatusPass,
			Detail: fmt.Sprintf("%d of %d comments (%.0f%%) explain WHY", reasoning, total, ratio*100),
		}}
	}
	envFolder := strings.TrimSuffix(prefix, "_import")
	readSurface := fmt.Sprintf("%s/import.yaml WHY-marker lines (multi-line `#` blocks counted as one)", envFolder)
	return []workflow.StepCheck{{
		Name:        prefix + "_comment_depth",
		Status:      StatusFail,
		ReadSurface: readSurface,
		Required:    fmt.Sprintf("≥%.0f%% of substantive comment blocks (≥%d) carry a reasoning marker", minReasoningCommentRatio*100, minReasoningComments),
		Actual:      fmt.Sprintf("%d of %d (%.0f%%)", reasoning, total, ratio*100),
		HowToFix: fmt.Sprintf(
			"Rewrite comment blocks in %s/import.yaml so each answers one of: WHY this value vs the obvious alternative; WHAT BREAKS if the decision flips; HOW THIS AFFECTS operations (rotation, scaling, rolling deploys, concurrent containers). Add the WHY clause inline (use words like `because`, `otherwise`, `without`, `prevents`, `rolling`, `rotation`, `redeploy`). Narration like 'minContainers: 2 enables rolling deploys' fails; 'minContainers: 2 because a single container would drop in-flight requests during rolling deploys' passes.",
			envFolder,
		),
		Detail: fmt.Sprintf(
			"only %d of %d substantive comments (%.0f%%, need >= %.0f%% or >= %d) explain WHY a decision was made.",
			reasoning, total, ratio*100, minReasoningCommentRatio*100, minReasoningComments,
		),
	}}
}
