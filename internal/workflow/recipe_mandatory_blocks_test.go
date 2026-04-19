package workflow

import (
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// mandatoryBlockRe matches a single MANDATORY sentinel block (lazy — smallest
// region between <<<MANDATORY ... >>> and <<<END MANDATORY>>>).
var mandatoryBlockRe = regexp.MustCompile(`(?s)<<<MANDATORY[^>]*>>>(.*?)<<<END MANDATORY>>>`)

// mandatoryOpenRe matches any opening sentinel.
var mandatoryOpenRe = regexp.MustCompile(`<<<MANDATORY[^>]*>>>`)

// mandatoryCloseRe matches the closing sentinel.
var mandatoryCloseRe = regexp.MustCompile(`<<<END MANDATORY>>>`)

// TestRecipeMd_AllMandatoryBlocksClosed — v8.97 Fix 3 Part A.
// Every opening <<<MANDATORY...>>> sentinel is paired with a closing
// <<<END MANDATORY>>> sentinel and the two appear in matching order
// (no overlap, no unclosed blocks). Regex walks recipe.md verifying
// the open-count matches close-count AND every open is closed before
// the next open begins.
func TestRecipeMd_AllMandatoryBlocksClosed(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	opens := mandatoryOpenRe.FindAllStringIndex(md, -1)
	closes := mandatoryCloseRe.FindAllStringIndex(md, -1)
	if len(opens) != len(closes) {
		t.Fatalf("MANDATORY sentinel imbalance: %d opens, %d closes", len(opens), len(closes))
	}
	if len(opens) == 0 {
		t.Fatal("expected at least one MANDATORY block in recipe.md (Fix 3 Part A)")
	}

	// Every close at index i must sit between open[i] and open[i+1] (or
	// after the last open). No open should start inside another block.
	for i := range opens {
		if closes[i][0] <= opens[i][1] {
			t.Errorf("block %d: close at %d precedes its open's end at %d", i, closes[i][0], opens[i][1])
		}
		if i+1 < len(opens) && opens[i+1][0] <= closes[i][1] {
			t.Errorf("block %d: next open at %d starts before this block's close at %d (overlapping MANDATORY blocks)", i, opens[i+1][0], closes[i][1])
		}
	}
}

// TestRecipeMd_MandatoryBlocksCoverFourSubagentBriefs — v8.97 Fix 3 Part A.
// The recipe has four subagent dispatch points (scaffold, feature,
// readmes-writer, code-review). Each brief section should contain at least
// one MANDATORY block carrying the three load-bearing rules (file-op
// sequencing, tool-use policy, SSH-only executables).
func TestRecipeMd_MandatoryBlocksCoverFourSubagentBriefs(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	// At least 4 blocks — one per subagent brief.
	blocks := mandatoryBlockRe.FindAllStringSubmatch(md, -1)
	if len(blocks) < 4 {
		t.Fatalf("expected >= 4 MANDATORY blocks (scaffold + feature + readmes-writer + code-review + principles), got %d", len(blocks))
	}

	// Count how many blocks carry each of the three load-bearing rules.
	const (
		ruleFileOp  = "File-op sequencing"
		ruleToolUse = "Tool-use policy"
		ruleSSH     = "SSH-only executables"
	)
	ruleCount := map[string]int{}
	for _, m := range blocks {
		body := m[1]
		for _, r := range []string{ruleFileOp, ruleToolUse, ruleSSH} {
			if strings.Contains(body, r) {
				ruleCount[r]++
			}
		}
	}

	// Each of the three rules must appear in at least 4 blocks (one per
	// subagent brief). This lets a fifth "principles" block exist without
	// over-constraining the structure.
	for _, r := range []string{ruleFileOp, ruleToolUse, ruleSSH} {
		if ruleCount[r] < 4 {
			t.Errorf("rule %q appears in only %d MANDATORY block(s); need >= 4 (one per subagent brief)", r, ruleCount[r])
		}
	}
}

// TestRecipeMd_DispatchConstructionRulePresent — v8.97 Fix 3 Part B.
// The main-agent-facing dispatch-construction rule must appear in recipe.md
// so the main agent knows to transmit MANDATORY blocks byte-identically
// when constructing an Agent() dispatch prompt.
func TestRecipeMd_DispatchConstructionRulePresent(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	if !strings.Contains(md, "Constructing the Agent() dispatch prompt") {
		t.Error("missing dispatch-construction rule heading (Fix 3 Part B)")
	}
	if !strings.Contains(md, "BYTE-IDENTICALLY") {
		t.Error("dispatch-construction rule must instruct BYTE-IDENTICALLY transmission (Fix 3 Part B)")
	}
}

// TestScaffoldBrief_PrinciplesSectionPresent — v8.97 Fix 5.
// The scaffold pre-flight platform principles block is present inside a
// MANDATORY sentinel pair in the scaffold brief.
func TestScaffoldBrief_PrinciplesSectionPresent(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	if !strings.Contains(md, "Scaffold pre-flight — platform principles") {
		t.Error("missing \"Scaffold pre-flight — platform principles\" heading (Fix 5)")
	}

	// Locate the principles block — it should sit inside a MANDATORY
	// sentinel pair.
	principlesBlock := extractPrinciplesBlock(t, md)
	if principlesBlock == "" {
		t.Fatal("principles block not located inside a MANDATORY sentinel pair")
	}
}

// TestScaffoldBrief_AllSixPrinciplesPresent — v8.97 Fix 5.
// Each of the six principles appears in the principles block.
func TestScaffoldBrief_AllSixPrinciplesPresent(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	block := extractPrinciplesBlock(t, md)
	if block == "" {
		t.Fatal("principles block not found")
	}
	for i := 1; i <= 6; i++ {
		marker := "Principle " + string(rune('0'+i)) + " —"
		if !strings.Contains(block, marker) {
			t.Errorf("principles block missing marker %q", marker)
		}
	}
}

// TestScaffoldBrief_EachPrincipleHasShape — v8.97 Fix 5.
// Every principle's body contains the three required labels. Principle
// bodies are delimited by consecutive "Principle N —" markers.
func TestScaffoldBrief_EachPrincipleHasShape(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	block := extractPrinciplesBlock(t, md)
	if block == "" {
		t.Fatal("principles block not found")
	}
	requiredLabels := []string{"Platform constraint", "Symptom of violation", "Obligation"}

	for i := 1; i <= 6; i++ {
		body := principleBody(block, i)
		if body == "" {
			t.Errorf("Principle %d: body not found", i)
			continue
		}
		for _, label := range requiredLabels {
			if !strings.Contains(body, label) {
				t.Errorf("Principle %d: missing label %q", i, label)
			}
		}
	}
}

// TestScaffoldBrief_PrincipleBodiesAreBacktickFree — v8.97 Fix 5.
// Structural invariant replacing the earlier deny-list of framework idiom
// substrings. Principle prose describes what the code must DO, not how a
// specific framework does it. Code-as-code (backtick-quoted inline) is an
// idiom leakage signal — any backtick between "Principle N —" and the next
// "Principle N+1 —" (or the closing sentinel) is a regression.
//
// The intro paragraph before Principle 1 may use backticks (it references
// scaffolder names like "nest new"); the final prose after Principle 6 may
// also use backticks (it references the close-step code review). Only the
// per-principle bodies are required to be backtick-free.
func TestScaffoldBrief_PrincipleBodiesAreBacktickFree(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	block := extractPrinciplesBlock(t, md)
	if block == "" {
		t.Fatal("principles block not found")
	}

	for i := 1; i <= 6; i++ {
		body := principleBody(block, i)
		if strings.Contains(body, "`") {
			t.Errorf("Principle %d: body contains backtick (code-as-code) — principle prose must be backtick-free. Body excerpt:\n%s", i, truncate(body, 300))
		}
	}
}

// TestScaffoldBrief_PrincipleRecordFactInstruction — v8.97 Fix 5.
// The principles block instructs the subagent to record a fact naming both
// the principle number AND the idiom chosen.
func TestScaffoldBrief_PrincipleRecordFactInstruction(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	block := extractPrinciplesBlock(t, md)
	if block == "" {
		t.Fatal("principles block not found")
	}

	// Accept variants of "scope=both" (may or may not be quoted).
	if !strings.Contains(block, "scope=both") {
		t.Error("principles block must instruct the subagent to record a fact with scope=both")
	}
	// Must mention recording both the principle AND the idiom.
	if !strings.Contains(block, "principle number") {
		t.Error("principles block must name \"principle number\" in the fact-recording instruction")
	}
	if !strings.Contains(block, "idiom used") {
		t.Error("principles block must name \"idiom used\" in the fact-recording instruction")
	}
}

// TestFeatureBrief_HasPrincipleSustainBlock — v8.98 Fix A.
// The feature-subagent-brief region carries the "Feature pre-ship —
// sustain the platform principles" MANDATORY block so principles apply
// at feature-authoring time, not just scaffold time.
func TestFeatureBrief_HasPrincipleSustainBlock(t *testing.T) {
	t.Parallel()

	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if !strings.Contains(md, "Feature pre-ship — sustain the platform principles") {
		t.Error("missing \"Feature pre-ship — sustain the platform principles\" heading in feature-subagent-brief (Fix A)")
	}
}

// TestFeatureBrief_SustainBlockReferencesAllPrinciplesByNumber — v8.98 Fix A.
// The sustain block references Principles 1-5 by number (Principle 6 is
// static-deploy-only and doesn't apply to feature authoring).
func TestFeatureBrief_SustainBlockReferencesAllPrinciplesByNumber(t *testing.T) {
	t.Parallel()

	block := extractFeatureSustainBlock(t)
	if block == "" {
		t.Fatal("feature-sustain block not found")
	}
	for i := 1; i <= 5; i++ {
		marker := "Principle " + string(rune('0'+i))
		if !strings.Contains(block, marker) {
			t.Errorf("feature-sustain block missing %q", marker)
		}
	}
}

// TestFeatureBrief_SustainBlockIsMandatoryWrapped — v8.98 Fix A.
// The sustain block sits between <<<MANDATORY... and <<<END MANDATORY>>>
// sentinels so the dispatch-construction rule transmits it verbatim.
// extractFeatureSustainBlock only returns a block wrapped in sentinels;
// a non-empty return proves the invariant.
func TestFeatureBrief_SustainBlockIsMandatoryWrapped(t *testing.T) {
	t.Parallel()

	block := extractFeatureSustainBlock(t)
	if block == "" {
		t.Fatal("feature-sustain block not found inside a MANDATORY sentinel pair (Fix A)")
	}
}

// TestFeatureBrief_SustainBlockPointsAtScaffoldBrief — v8.98 Fix A.
// Enforces single-source-of-truth: principle text lives in the scaffold
// brief; the feature brief references it rather than duplicating.
func TestFeatureBrief_SustainBlockPointsAtScaffoldBrief(t *testing.T) {
	t.Parallel()

	block := extractFeatureSustainBlock(t)
	if block == "" {
		t.Fatal("feature-sustain block not found")
	}
	if !strings.Contains(block, "authoritative source") {
		t.Error("sustain block must reference the scaffold brief as the authoritative source (Fix A)")
	}
}

// extractFeatureSustainBlock returns the body of the MANDATORY block that
// contains the "Feature pre-ship — sustain the platform principles"
// heading, or empty if no such block exists.
func extractFeatureSustainBlock(t *testing.T) string {
	t.Helper()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	blocks := mandatoryBlockRe.FindAllStringSubmatch(md, -1)
	for _, m := range blocks {
		body := m[1]
		if strings.Contains(body, "Feature pre-ship — sustain the platform principles") {
			return body
		}
	}
	return ""
}

// extractPrinciplesBlock returns the body of the MANDATORY block that
// contains the "Scaffold pre-flight — platform principles" heading, or
// empty if no such block exists.
func extractPrinciplesBlock(t *testing.T, md string) string {
	t.Helper()
	blocks := mandatoryBlockRe.FindAllStringSubmatch(md, -1)
	for _, m := range blocks {
		body := m[1]
		if strings.Contains(body, "Scaffold pre-flight — platform principles") {
			return body
		}
	}
	return ""
}

// principleBody returns the text between "Principle N —" and the next
// principle marker (or the end of the block). Returns "" if N is not found.
func principleBody(block string, n int) string {
	startMarker := "Principle " + string(rune('0'+n)) + " —"
	start := strings.Index(block, startMarker)
	if start < 0 {
		return ""
	}
	start += len(startMarker)
	nextMarker := "Principle " + string(rune('0'+n+1)) + " —"
	end := strings.Index(block[start:], nextMarker)
	if end < 0 {
		return block[start:]
	}
	return block[start : start+end]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
