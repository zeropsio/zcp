package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// CheckIGPerItemCode enforces that every H3 heading inside the
// integration-guide fragment carries at least one fenced code block in
// its section. Catches the v18 appdev regression where IG step 3 was
// prose-only while v7/v14/v15 had a code diff for every step.
//
// Recipe content already tells the agent "### 2. Step Title (for each
// code adjustment you actually made) … with the code diff". The
// complementary CheckIGCodeAdjustment enforces ≥1 non-YAML block in the
// whole fragment — it passes even when half the items are prose-only.
// This per-item floor enforces what the template says: one item per
// real code adjustment, each with a code block.
//
// Single-H3 IG sections are allowed (minimal shape). The check fires
// when there are ≥2 H3 headings AND any heading after the first has no
// fenced code block in its section.
//
// Scoped to showcase tier (minimal recipes have simpler IG shapes that
// don't always need per-item code blocks).
func CheckIGPerItemCode(_ context.Context, content string, isShowcase bool) []workflow.StepCheck {
	if !isShowcase {
		return nil
	}
	igContent := extractFragmentContent(content, "integration-guide")
	if igContent == "" {
		return nil
	}
	sections := splitByH3(igContent)
	if len(sections) < 2 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_per_item_code",
			Status: StatusPass,
		}}
	}

	var missing []string
	for _, sec := range sections {
		if sectionHasFencedBlock(sec.Body) {
			continue
		}
		missing = append(missing, sec.Heading)
	}
	if len(missing) == 0 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_per_item_code",
			Status: StatusPass,
			Detail: fmt.Sprintf("%d IG items, all with fenced code blocks", len(sections)),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "integration_guide_per_item_code",
		Status: StatusFail,
		Detail: fmt.Sprintf(
			"integration-guide H3 section(s) with no fenced code block: %s. Every IG step must carry a code diff — the content contract says 'one IG item per real code adjustment, with the code diff'. If a step describes a config placement (envVariables section, where to put a var), show the before/after as a fenced block. If a step is prose-only, either delete it or fold its content into a neighbouring step that has code.",
			strings.Join(missing, " | "),
		),
	}}
}

// igSection is one H3 section inside the integration-guide fragment.
type igSection struct {
	Heading string // the H3 heading text (without the ### prefix)
	Body    string // the content between this H3 and the next one (or end-of-fragment)
}

// splitByH3 splits markdown content into sections keyed by H3 headings.
// Content before the first H3 is dropped (usually a fragment start marker).
func splitByH3(content string) []igSection {
	lines := strings.Split(content, "\n")
	var sections []igSection
	var current *igSection
	var body strings.Builder
	flush := func() {
		if current == nil {
			return
		}
		current.Body = body.String()
		sections = append(sections, *current)
		current = nil
		body.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			current = &igSection{Heading: heading}
			continue
		}
		if current != nil {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return sections
}

// sectionHasFencedBlock returns true when the body contains at least
// one complete fenced code block (``` ... ```), any language. Each
// opener/closer emits a regex match, so a complete block produces ≥2
// matches.
func sectionHasFencedBlock(body string) bool {
	matches := codeBlockFenceRe.FindAllStringIndex(body, -1)
	return len(matches) >= 2
}
