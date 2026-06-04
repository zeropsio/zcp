package workflow

import (
	"strings"
	"testing"
)

func TestComposeUnderBudget(t *testing.T) {
	t.Parallel()

	corpus := []KnowledgeAtom{
		{ID: "framing", Priority: 0, Title: "Framing"},
		{ID: "essential", Priority: 1, Title: "Essential"},
		{ID: "mid", Priority: 5, Title: "Mid"},
		{ID: "late", Priority: 9, Title: "Late"},
		{ID: "ref", Priority: 5, Title: "Ref", Reference: true},
	}
	bigBody := func(prefix string) string {
		return prefix + " lever sentence.\n\n" + strings.Repeat("x", 4000)
	}
	matches := []MatchedRender{
		{AtomID: "framing", Body: bigBody("framing")},
		{AtomID: "essential", Body: bigBody("essential")},
		{AtomID: "mid", Body: bigBody("mid")},
		{AtomID: "late", Body: bigBody("late")},
		{AtomID: "ref", Body: "**Ref** — pull on demand: ..."},
	}

	t.Run("under_budget_unchanged", func(t *testing.T) {
		t.Parallel()
		out := ComposeUnderBudget(matches, corpus, 1<<20) // 1 MB — everything fits
		for i := range matches {
			if out[i].Body != matches[i].Body {
				t.Errorf("atom %q changed under budget", out[i].AtomID)
			}
		}
	})

	t.Run("over_budget_demotes_least_important_first", func(t *testing.T) {
		t.Parallel()
		// Budget forces demotion of the heaviest non-protected atoms. With 5
		// bodies ~4 KB each, a 9 KB budget must demote "late" (pri 9) and "mid"
		// (pri 5) but keep "framing"(0)/"essential"(1)/"ref" full.
		out := ComposeUnderBudget(matches, corpus, 9*1024)
		byID := map[string]string{}
		total := 0
		for _, m := range out {
			byID[m.AtomID] = m.Body
			total += len(m.Body)
		}
		if total > 9*1024 {
			t.Errorf("composed total %d exceeds budget %d", total, 9*1024)
		}
		// late demotes before mid (higher priority number first).
		if !strings.HasPrefix(byID["late"], "**Late** —") {
			t.Errorf("late should be demoted to a head, got %q", trunc(byID["late"]))
		}
		// framing + essential are protected (priority ≤ 1) — never demoted.
		if !strings.Contains(byID["framing"], strings.Repeat("x", 4000)) {
			t.Error("framing (priority 0) must never be demoted")
		}
		if !strings.Contains(byID["essential"], strings.Repeat("x", 4000)) {
			t.Error("essential (priority 1) must never be demoted")
		}
		// reference stub left alone.
		if byID["ref"] != "**Ref** — pull on demand: ..." {
			t.Error("reference atom stub must not be touched")
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		a := ComposeUnderBudget(matches, corpus, 9*1024)
		b := ComposeUnderBudget(matches, corpus, 9*1024)
		for i := range a {
			if a[i].Body != b[i].Body {
				t.Fatalf("non-deterministic composition at %q", a[i].AtomID)
			}
		}
	})

	t.Run("head_skips_leading_heading", func(t *testing.T) {
		t.Parallel()
		head := composedHead("T", "## A Heading\n\nThe real lever.\n\nmore")
		if !strings.Contains(head, "The real lever.") || strings.Contains(head, "Heading") {
			t.Errorf("head should carry the prose lever, not the heading: %q", head)
		}
	})
}

func trunc(s string) string {
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
