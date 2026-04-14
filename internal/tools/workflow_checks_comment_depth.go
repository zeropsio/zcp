// Tests for: internal/tools/workflow_checks_comment_depth.go — env
// import.yaml comment depth rubric. This check grades comments on
// whether they explain WHY a decision was made, not WHAT the field
// does. The v7 gold-standard import.yaml comments carried production
// wisdom ("JWT_SECRET project-scoped because session-cookie validation
// fails if containers disagree on the signing key", "minContainers:2
// load-balances NATS queue-group members so a single restart doesn't
// pause processing"); v16 regressed to field narration ("Small prod
// — minContainers: 2 enables rolling deploys"). Same field, lost the
// insight behind the decision.
package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minReasoningCommentRatio is the minimum fraction of non-trivial
// comments that must contain a reasoning marker (a word or phrase
// that signals the comment is explaining WHY rather than WHAT).
// 35% is calibrated against the v7 set: roughly one in three v7
// comments carries an explicit reasoning marker, and the other
// two provide context that's harder to grade automatically.
const minReasoningCommentRatio = 0.35

// minReasoningComments is the hard floor regardless of ratio. On a
// short env file (Local or AI Agent), 35% of 4 comments is 2; the
// floor is also 2 so the check doesn't falsely pass a short file
// with a single reasoning comment + one narration comment.
const minReasoningComments = 2

// reasoningMarkers are substrings whose presence in a comment
// signals that the comment is explaining WHY, WHAT-IF, or WHAT-ELSE
// rather than narrating what the adjacent field does. Each category
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
// mention "restart" without explaining anything) are fine because
// a comment using these words at all is almost always reasoning
// about something. False negatives (real insights without any of
// these markers) are less fine but manageable since we only need
// 35% of comments to match.
var reasoningMarkers = []string{
	// Consequence
	"because",
	"otherwise",
	"without",
	"so that",
	"means that",
	"prevents",
	"causes",
	"leads to",
	"results in",
	"avoids",
	"which is why",
	"that way",
	"for this reason",

	// Trade-off
	"instead of",
	"rather than",
	"in favor of",
	"over ",
	"as opposed to",
	"trade-off",
	"tradeoff",

	// Constraint
	"must",
	"required",
	"cannot",
	"forced",
	"mandatory",
	"never",
	"always",
	"guaranteed",

	// Operational consequence
	"rotation",
	"rotate",
	"redeploy",
	"restart",
	"scale",
	"scaling",
	"downtime",
	"zero-downtime",
	"rolling",
	"fan-out",
	"fan out",
	"concurrent",
	"race",
	"lock",
	"drain",

	// Framework × platform intersection signals
	"build time",
	"build-time",
	"runtime",
	"cross-service",
	"at startup",
	"at runtime",
	"at import time",
	"at deploy time",

	// Decision framing
	"we chose",
	"picked",
	"default here",
	"this tier",
	"this env",
	"matches prod",
	"mirrors prod",
}

// checkCommentDepth walks the import.yaml content, groups contiguous
// `#` lines into logical comment blocks (a multi-line comment is ONE
// piece of reasoning, not N), and computes the fraction of blocks
// whose combined text contains at least one reasoning marker. Fails
// if below minReasoningCommentRatio AND below the minReasoningComments
// hard floor.
//
// Grouping matters: the v7 gold-standard comments are 3-5 lines each
// and carry the "because" on line 1, the consequence on line 3, and
// the rotation note on line 5. Counting per-line would grade 2/5 on
// a block that's actually one complete insight.
func checkCommentDepth(content, prefix string) []workflow.StepCheck {
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
		// Non-comment line ends the current block.
		flush()
	}
	// Trailing comment block at EOF.
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
			Name: prefix + "_comment_depth", Status: statusPass,
		}}
	}
	ratio := float64(reasoning) / float64(total)
	if reasoning >= minReasoningComments && ratio >= minReasoningCommentRatio {
		return []workflow.StepCheck{{
			Name:   prefix + "_comment_depth",
			Status: statusPass,
			Detail: fmt.Sprintf("%d of %d comments (%.0f%%) explain WHY", reasoning, total, ratio*100),
		}}
	}
	return []workflow.StepCheck{{
		Name:   prefix + "_comment_depth",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"only %d of %d substantive comments (%.0f%%, need >= %.0f%% or >= %d) explain WHY a decision was made. The v7 gold-standard import.yaml comments teach — they explain what goes wrong if the decision flips ('session-cookie validation fails if any container in the L7 pool disagrees on the signing key'), the trade-off chosen ('single broker is sufficient for the small-prod tier; the queue-group already gives consumer-side redundancy'), and the operational consequence ('service-level envSecrets would force every container to be redeployed when the key rotates'). v16 regressed to describing field values without the reasoning behind them. Rewrite comments so they answer one of: WHY this value specifically (vs the obvious alternative), WHAT BREAKS if we flip the decision, or HOW THIS AFFECTS operations (rotation, scaling, rolling deploys, concurrent containers, failure modes). Narration like 'minContainers: 2 enables rolling deploys' fails; 'JWT_SECRET project-scoped because cookie validation fails if two containers in the L7 pool disagree on the signing key' passes.",
			reasoning, total, ratio*100, minReasoningCommentRatio*100, minReasoningComments,
		),
	}}
}

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
