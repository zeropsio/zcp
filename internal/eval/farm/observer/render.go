package observer

import (
	"fmt"
	"strings"
)

// unparsedRawDisplayCap is how much of an unparsed answer's raw text a
// rendering shows — Render's own display cap, distinct from (and much
// smaller than) observe.go's unparsedRawStoreCap, which governs how much
// of that text the stored observation document keeps in the first place
// (§7.5).
const unparsedRawDisplayCap = 2000

// Render renders an observation as stable plain text (§7.5), reused by the
// console's markdown API. Every surface that shows an observation shows its
// quote-found count (§7.5 FM-46; §8.8: "quote found — the quoted words
// occur in the cited step; it does not prove the finding right"), which is
// why the header line always carries it, regardless of status — worded as
// "quotes found N/M" (N verified, M total), never "N unverified", matching
// the run page and the agent API.
func Render(obs Observation) string {
	unverified, total := countQuotes(obs.Findings)
	found := total - unverified
	var b strings.Builder
	fmt.Fprintf(&b, "Observer · %s · %s · quotes found %d/%d\n",
		obs.Model, obs.CreatedAt.UTC().Format("2006-01-02T15:04Z"), found, total)

	switch obs.Status {
	case statusUnparsed:
		b.WriteString("Observer answer could not be parsed\n")
		b.WriteString(firstN(obs.Raw, unparsedRawDisplayCap))
		b.WriteString("\n")
		return b.String()
	case statusError:
		fmt.Fprintf(&b, "Observer failed: %s\n", obs.Error)
		return b.String()
	}

	if obs.Story != nil {
		renderFormat2(&b, obs)
	} else {
		renderFormat1(&b, obs)
	}
	return b.String()
}

// renderFormat1 renders the "ok" body exactly as it always has, for a
// format-1 observation (Story nil, no surface/anchor/span/causedVerdict/
// judged/outcome/warnings).
func renderFormat1(b *strings.Builder, obs Observation) {
	b.WriteString(obs.Headline + "\n")
	fmt.Fprintf(b, "Goal: %s — %s\n", obs.Goal.Reached, obs.Goal.Why)
	renderChecksLine(b, obs.Checks)
	renderFindings(b, obs.Findings)
	b.WriteString("Self-review: " + obs.SelfReview.Accurate)
	if obs.SelfReview.Note != "" {
		b.WriteString(" — " + obs.SelfReview.Note)
	}
	b.WriteString("\n")
}

// renderFormat2 renders the "ok" body for a format-2 observation (§7.5 item
// 4): outcome first, headline, story, judged checks, findings with
// surface/anchor/span/causedVerdict, self-review, then warnings.
func renderFormat2(b *strings.Builder, obs Observation) {
	fmt.Fprintf(b, "Outcome: %s\n", obs.Outcome)
	b.WriteString(obs.Headline + "\n")
	renderStory(b, obs.Story)
	fmt.Fprintf(b, "Goal: %s — %s\n", obs.Goal.Reached, obs.Goal.Why)
	renderChecksLine(b, obs.Checks)
	renderJudgedChecks(b, obs.Checks.Judged)
	renderFindingsV2(b, obs.Findings)
	b.WriteString("Self-review: " + obs.SelfReview.Accurate)
	if obs.SelfReview.Note != "" {
		b.WriteString(" — " + obs.SelfReview.Note)
	}
	b.WriteString("\n")
	renderWarnings(b, obs.Warnings)
}

func renderStory(b *strings.Builder, s *Story) {
	fmt.Fprintf(b, "Task: %s\n", s.Task)
	fmt.Fprintf(b, "Expected: %s\n", s.Expected)
	fmt.Fprintf(b, "Did: %s\n", s.Did)
	if s.Stuck != nil {
		fmt.Fprintf(b, "Stuck: steps #%d–#%d — %s\n", s.Stuck.From, s.Stuck.To, s.Stuck.What)
	}
	fmt.Fprintf(b, "Ending: %s\n", s.Ending)
}

// renderJudgedChecks matches the run page's "Verdict right" wording: one
// line per judged check, "✓ correct <id>" / "✗ incorrect <id>", its why
// appended when non-empty (the run page's own §8.3 item 5 rendering, not
// in this package's write-set, uses the identical mark/verdict/id shape).
func renderJudgedChecks(b *strings.Builder, judged []JudgedCheck) {
	if len(judged) == 0 {
		return
	}
	b.WriteString("Verdict right:\n")
	for _, j := range judged {
		mark, verdict := "✓", "correct"
		if !j.Correct {
			mark, verdict = "✗", "incorrect"
		}
		if j.Why != "" {
			fmt.Fprintf(b, "  %s %s %s — %s\n", mark, verdict, j.ID, j.Why)
			continue
		}
		fmt.Fprintf(b, "  %s %s %s\n", mark, verdict, j.ID)
	}
}

func renderFindingsV2(b *strings.Builder, findings []Finding) {
	if len(findings) == 0 {
		b.WriteString("Findings: none\n")
		return
	}
	b.WriteString("Findings:\n")
	for i, f := range findings {
		header := fmt.Sprintf("%d. [%s · %s", i+1, f.Severity, f.Owner)
		if f.Surface != "" {
			header += " · " + f.Surface
		}
		fmt.Fprintf(b, "%s] %s\n", header, f.Title)
		fmt.Fprintf(b, "   %s\n", f.What)
		if f.Anchor != "" {
			fmt.Fprintf(b, "   Anchor: %q\n", f.Anchor)
		}
		fmt.Fprintf(b, "   Evidence: %s\n", renderEvidence(f.Evidence))
		if f.Span != nil {
			fmt.Fprintf(b, "   Span: steps #%d–#%d\n", f.Span.From, f.Span.To)
		}
		if f.CausedVerdict {
			b.WriteString("   Caused verdict: yes\n")
		}
		fmt.Fprintf(b, "   Look at: %s\n", f.LookAt)
		if f.Fix != "" {
			fmt.Fprintf(b, "   Fix: %s\n", f.Fix)
		}
	}
}

func renderWarnings(b *strings.Builder, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	b.WriteString("Warnings:\n")
	for _, w := range warnings {
		fmt.Fprintf(b, " - %s\n", w)
	}
}

func renderChecksLine(b *strings.Builder, c Checks) {
	agree := "agree"
	if !c.Agree {
		agree = "disagree"
	}
	if c.Why != "" {
		fmt.Fprintf(b, "Checks: %s with verdict %s — %s\n", agree, c.Verdict, c.Why)
		return
	}
	fmt.Fprintf(b, "Checks: %s with verdict %s\n", agree, c.Verdict)
}

func renderFindings(b *strings.Builder, findings []Finding) {
	if len(findings) == 0 {
		b.WriteString("Findings: none\n")
		return
	}
	b.WriteString("Findings:\n")
	for i, f := range findings {
		fmt.Fprintf(b, "%d. [%s · %s] %s\n", i+1, f.Severity, f.Owner, f.Title)
		fmt.Fprintf(b, "   %s\n", f.What)
		fmt.Fprintf(b, "   Evidence: %s\n", renderEvidence(f.Evidence))
		fmt.Fprintf(b, "   Look at: %s\n", f.LookAt)
		if f.Fix != "" {
			fmt.Fprintf(b, "   Fix: %s\n", f.Fix)
		}
	}
}

func renderEvidence(evidence []Evidence) string {
	parts := make([]string, len(evidence))
	for i, e := range evidence {
		mark, suffix := "✓", ""
		if !e.Verified {
			mark, suffix = "✗", " unverified"
		}
		// Display only: a quote copied verbatim from a multi-line step can
		// carry a real newline, which would otherwise break this line's
		// layout. The stored Evidence.Quote is untouched.
		parts[i] = fmt.Sprintf("#%d \"%s\" %s%s", e.Step, collapseWhitespace(e.Quote), mark, suffix)
	}
	return strings.Join(parts, " · ")
}

// countQuotes returns the unverified and total evidence-entry counts across
// every finding (§7.5 FM-46).
func countQuotes(findings []Finding) (unverified, total int) {
	for _, f := range findings {
		for _, e := range f.Evidence {
			total++
			if !e.Verified {
				unverified++
			}
		}
	}
	return unverified, total
}
