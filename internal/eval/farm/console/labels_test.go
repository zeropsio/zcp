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
		for _, v := range []string{observer.OutcomeOK, observer.OutcomeProblem, observer.OutcomeInconclusive} {
			if got := assessmentOutcomeLabel(v); got == "" {
				t.Errorf("assessmentOutcomeLabel(%q) is empty", v)
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
		"OK", "Problem", "Inconclusive", // outcome
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
