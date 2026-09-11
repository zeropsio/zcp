// Package console: tests for labels.go's display-vocabulary layer (§8.8
// FM-56) and GET /terms.
package console

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval/farm"
	"github.com/zeropsio/zcp/internal/eval/farm/observer"
)

// TestLabels_EveryEnumValueHasALabel pins §8.8 FM-56: every value of every
// vocabulary this file knows about carries a non-empty label — a raw enum
// value must never reach a person unexplained. Independent oracle: the
// exact enum value sets named in the spec text (verdict "incl. not
// started, stalled", the six owner values, the three severities, the
// three outcomes, the five observer states) — not merely "whatever
// labels.go happens to define."
func TestLabels_EveryEnumValueHasALabel(t *testing.T) {
	t.Run("verdict", func(t *testing.T) {
		for _, v := range []string{farm.VerdictPassed, farm.VerdictFailed, farm.VerdictBlocked, farm.VerdictNotRun, verdictRunning, verdictStalled} {
			if got := verdictLabel(v); got == "" {
				t.Errorf("verdictLabel(%q) is empty", v)
			}
			if got := verdictTooltip(v); got == "" {
				t.Errorf("verdictTooltip(%q) is empty", v)
			}
		}
	})

	t.Run("severity", func(t *testing.T) {
		for _, v := range []string{observer.SeverityHigh, observer.SeverityMedium, observer.SeverityLow} {
			if got := severityLabel(v); got == "" {
				t.Errorf("severityLabel(%q) is empty", v)
			}
			if got := severityTooltip(v); got == "" {
				t.Errorf("severityTooltip(%q) is empty", v)
			}
		}
	})

	t.Run("cause", func(t *testing.T) {
		owners := []string{"zcp-guidance", "zcp-tool", "platform", "agent", "scenario", "evaluator"}
		wantLabel := map[string]string{
			"zcp-guidance": "ZCP guidance",
			"zcp-tool":     "ZCP tool",
			"platform":     "Zerops platform",
			"agent":        "Agent mistake",
			"scenario":     "Test scenario",
			"evaluator":    "Test check",
		}
		wantClass := map[string]string{
			"zcp-guidance": "ZCP", "zcp-tool": "ZCP",
			"platform": "Platform", "agent": "Agent",
			"scenario": "Test", "evaluator": "Test",
		}
		for _, owner := range owners {
			if got := causeLabel(owner); got != wantLabel[owner] {
				t.Errorf("causeLabel(%q) = %q, want %q", owner, got, wantLabel[owner])
			}
			if got := causeClass(owner); got != wantClass[owner] {
				t.Errorf("causeClass(%q) = %q, want %q", owner, got, wantClass[owner])
			}
		}
		// causeOrder must be exactly this set (ZCP-first) — every owner
		// value appears, and no other value does.
		if len(causeOrder) != len(owners) {
			t.Fatalf("causeOrder has %d entries, want %d", len(causeOrder), len(owners))
		}
		for _, owner := range owners {
			found := false
			for _, o := range causeOrder {
				if o == owner {
					found = true
				}
			}
			if !found {
				t.Errorf("causeOrder missing owner %q", owner)
			}
		}
	})

	t.Run("assessment outcome", func(t *testing.T) {
		// §8.8 (2026-09 update): outcome gains "none" — no current ok
		// observation to summarize — and (FIX3 item 6) "failed" — one
		// specific observation's own assessment attempt errored.
		for _, v := range []string{observer.OutcomeOK, observer.OutcomeProblem, observer.OutcomeInconclusive, outcomeNone, outcomeFailed} {
			if got := assessmentOutcomeLabel(v); got == "" {
				t.Errorf("assessmentOutcomeLabel(%q) is empty", v)
			}
			if got := assessmentOutcomeClass(v); got != "outcome outcome-"+v {
				t.Errorf("assessmentOutcomeClass(%q) = %q, want %q", v, got, "outcome outcome-"+v)
			}
		}
	})

	t.Run("assessment state", func(t *testing.T) {
		for _, v := range []string{observerStateObserved, observerStateObserving, observerStateNotObserved, observerStateOff, observerStateDisabled} {
			if got := assessmentStateLabel(v); got == "" {
				t.Errorf("assessmentStateLabel(%q) is empty", v)
			}
		}
	})

	// §8.6/§8.8 (2026-09 update): problem status gains "first seen" — the
	// set is now new/first-seen/recurring/gone/unconfirmed, and "live" =
	// new, first seen or recurring (previously new/recurring/gone/
	// unconfirmed, live = new or recurring).
	t.Run("problem status", func(t *testing.T) {
		// Item 11 (FIX2): status "new" displays as "regressed" — the word
		// "Problem" (not "new") stays reserved for the cross-run cluster
		// concept itself.
		wantLabel := map[string]string{
			"new": "regressed", "first-seen": "first seen", "recurring": "recurring",
			"gone": "gone", "unconfirmed": "unconfirmed",
		}
		for value, label := range wantLabel {
			if got := problemStatusLabel(value); got != label {
				t.Errorf("problemStatusLabel(%q) = %q, want %q", value, got, label)
			}
			if got := problemStatusTooltip(value); got == "" {
				t.Errorf("problemStatusTooltip(%q) is empty", value)
			}
		}
		if len(problemStatusVocab) != len(wantLabel) {
			t.Fatalf("problemStatusVocab has %d entries, want %d", len(problemStatusVocab), len(wantLabel))
		}
		for _, v := range []string{"new", "first-seen", "recurring"} {
			if !isLiveStatus(v) {
				t.Errorf("isLiveStatus(%q) = false, want true", v)
			}
		}
		for _, v := range []string{"gone", "unconfirmed"} {
			if isLiveStatus(v) {
				t.Errorf("isLiveStatus(%q) = true, want false", v)
			}
		}
	})
}

// TestPages_TermsListsEveryTerm pins §8.3 FM-51's /terms page: it renders
// the full glossary plus every vocabulary value's label, each carrying its
// definition as a title attribute (§8.8: "each badge carries its
// definition as a title").
func TestPages_TermsListsEveryTerm(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := doGET(t, h, "/terms")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /terms: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	// html/template escapes text content (', <, >, & and friends) — decode
	// those entities back before comparing against the vocabulary's own
	// raw, unescaped strings.
	body := html.UnescapeString(rr.Body.String())

	for _, term := range glossaryTerms {
		if !strings.Contains(body, term.Term) {
			t.Errorf("/terms missing glossary term %q:\n%s", term.Term, body)
		}
		if !strings.Contains(body, term.Definition) {
			t.Errorf("/terms missing glossary definition for %q:\n%s", term.Term, body)
		}
	}

	for _, want := range []string{
		"not started", "stalled", // verdict
		"High", "Medium", "Low", // severity
		"ZCP guidance", "ZCP tool", "Zerops platform", "Agent mistake", "Test scenario", "Test check", // cause
		"OK", "Needs attention", "Inconclusive", "none", // outcome
		"regressed", "first seen", "recurring", "gone", "unconfirmed", // problem status
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/terms missing vocabulary label %q:\n%s", want, body)
		}
	}

	// Every tooltip text set on a value with a non-empty Tooltip appears as
	// a title="..." attribute somewhere in the page.
	if !strings.Contains(body, `title="every check held"`) {
		t.Errorf("/terms missing the passed verdict's title attribute:\n%s", body)
	}
}

// TestPages_TermsReflectsSeptember2026VocabularyUpdate pins the §8.8
// update landed on feat/zcp-farm: the "Evaluation batch"/"Empty batch"
// terms, the sha256-identified ZCP build wording, and the new "Disputed"
// definition (checks.agree: false, counted under the disputed verdict,
// passed included) — the generic glossary loop above already proves every
// glossaryTerms row renders; this test proves the row TEXT itself changed,
// not merely that some old text still renders.
func TestPages_TermsReflectsSeptember2026VocabularyUpdate(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	body := html.UnescapeString(doGET(t, h, "/terms").Body.String())

	for _, want := range []string{
		"Evaluation batch", "at least one run finished",
		"Empty batch", "no run finished",
		"identified by its sha256",
		"checks.agree: false",
		"counted under the verdict it disputes, passed included",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/terms missing updated vocabulary text %q:\n%s", want, body)
		}
	}
}

// TestPages_TermsRequiresAuth pins that /terms is a normal authenticated
// HTML route like every other page (FM-50).
func TestPages_TermsRequiresAuth(t *testing.T) {
	srv, _, _ := testServer(t)
	h := srv.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/terms", nil))
	if rr.Code != http.StatusSeeOther {
		t.Errorf("GET /terms unauthenticated: got %d, want 303", rr.Code)
	}
}

// TestFormatCheckValue_RewritesMonotonicClockStamp pins item 19 (round-1
// follow-up): a check's expected/observed text from an older bundle can
// carry Go's monotonic-clock suffix ("... +0000 UTC m=+0.112197840") — one
// display helper rewrites that to RFC3339 wherever a page prints
// expected/observed (Why-this-verdict strip, checks tables, batch
// first-failed line). Independent oracle: the exact literal example quoted
// in the brief, plus a plain string with no such stamp (left untouched) and
// a stamp with a negative monotonic offset (m=-0...).
func TestFormatCheckValue_RewritesMonotonicClockStamp(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"brief's own example",
			"2026-09-11 18:33:47.123456789 +0000 UTC m=+0.112197840",
			"2026-09-11T18:33:47Z",
		},
		{
			"negative monotonic offset",
			"2026-09-11 18:33:47 +0000 UTC m=-0.000000001",
			"2026-09-11T18:33:47Z",
		},
		{
			"embedded in a larger sentence",
			"service web is degraded since 2026-09-11 18:33:47.5 +0000 UTC m=+12.5",
			"service web is degraded since 2026-09-11T18:33:47Z",
		},
		{"plain text, no stamp", "expected running, got degraded", "expected running, got degraded"},
		{"empty string", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatCheckValue(c.in); got != c.want {
				t.Errorf("formatCheckValue(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
