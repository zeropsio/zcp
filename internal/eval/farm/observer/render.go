package observer

import (
	"fmt"
	"strings"
)

// unparsedRawCap is how much of an unparsed answer's raw text render shows
// (§7.5: "the first 2,000 chars of raw").
const unparsedRawCap = 2000

// Render renders an observation as stable plain text (§7.5), reused by the
// console's markdown API. Every surface that shows an observation shows its
// unverified-quote count (§7.5 FM-46), which is why the header line always
// carries it, regardless of status.
func Render(obs Observation) string {
	unverified, total := countQuotes(obs.Findings)
	var b strings.Builder
	fmt.Fprintf(&b, "Observer · %s · %s · %d of %d quotes unverified\n",
		obs.Model, obs.CreatedAt.UTC().Format("2006-01-02T15:04Z"), unverified, total)

	switch obs.Status {
	case "unparsed":
		b.WriteString("Observer answer could not be parsed\n")
		b.WriteString(firstN(obs.Raw, unparsedRawCap))
		b.WriteString("\n")
		return b.String()
	case "error":
		fmt.Fprintf(&b, "Observer failed: %s\n", obs.Error)
		return b.String()
	}

	b.WriteString(obs.Headline + "\n")
	fmt.Fprintf(&b, "Goal: %s — %s\n", obs.Goal.Reached, obs.Goal.Why)
	renderChecksLine(&b, obs.Checks)
	renderFindings(&b, obs.Findings)
	b.WriteString("Self-review: " + obs.SelfReview.Accurate)
	if obs.SelfReview.Note != "" {
		b.WriteString(" — " + obs.SelfReview.Note)
	}
	b.WriteString("\n")
	return b.String()
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
		parts[i] = fmt.Sprintf("#%d \"%s\" %s%s", e.Step, e.Quote, mark, suffix)
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
