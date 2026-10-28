// Tests for: tools/hq_change_open.go — an open whose answer never arrived is
// checked against the Mate's state before it is answered, run against a fake
// HQ that serves real git (hq_lab_test.go).
package tools

import (
	"net/http"
	"strings"
	"testing"
)

// isOpenCall matches open-change's POST to HQ.
func isOpenCall(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/mate/changes"
}

// TestOpenChange_AnswerLost_AnswersTheChangeItOpened is Filip on 2026-10-26:
// open-change met "context deadline exceeded" waiting for HQ's answer and
// said "Nothing was opened", yet HQ had opened change #2, which the retry
// found. An open whose answer never arrived — HQ may have acted on it —
// reads the Mate's state before it answers: the change open on the branch
// is the answer, recorded as an answered open is, and a call again answers
// that same change rather than opening another.
func TestOpenChange_AnswerLost_AnswersTheChangeItOpened(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		perBranch bool
		// status is what the open is answered with once HQ served it; 0
		// drops the connection.
		status int
	}{
		{name: "the connection dropped, an HQ with a change per branch", perBranch: true},
		{name: "a balancer's 504, an HQ with a change per branch", perBranch: true, status: http.StatusGatewayTimeout},
		{name: "a balancer's 502, an HQ with a change per branch", perBranch: true, status: http.StatusBadGateway},
		{name: "the connection dropped, an HQ with one change per repository"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			if tt.perBranch {
				lab.hq.keepOpenPerBranch()
			}
			lab.hq.loseAnswers(1, tt.status, isOpenCall)

			answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login")

			if isError || answer.PullRequest != 1 || answer.Branch != "fix-login" || !answer.Created {
				t.Fatalf("want change #1 answered as opened on fix-login, got isError=%v: %s", isError, text)
			}
			if strings.Contains(text, "Nothing was opened") || !strings.Contains(answer.Message, "Change #1 is open") {
				t.Errorf("the answer does not say change #1 is open: %s", text)
			}
			if kept := lab.kept(1); kept == nil || kept.Branch != "fix-login" {
				t.Errorf("the ledger keeps %+v, want change #1 on fix-login", kept)
			}
			if n := lab.openChanges(); n != 1 {
				t.Errorf("HQ holds %d open changes, want 1", n)
			}

			again, text, isError := lab.openChangeFrom("fix-login", "Fix the login")
			if isError || again.Created || again.PullRequest != 1 {
				t.Errorf("a call again answers %s, want change #1 answered open", text)
			}
			if n := lab.openChanges(); n != 1 {
				t.Errorf("a call again left HQ holding %d open changes, want 1", n)
			}
		})
	}
}

// TestOpenChange_AnswerAndStateLost_SaysUnknownNeverNothing: when the open's
// answer and the read of the Mate's state after it are both lost, whether
// the change was opened is unknown — never "Nothing was opened" — and the
// answer says a call again answers the change if it was opened rather than
// opening a second; the call again does exactly that.
func TestOpenChange_AnswerAndStateLost_SaysUnknownNeverNothing(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.hq.keepOpenPerBranch()
	opened := false
	// Called with HQ's lock held, as every request is served.
	lab.hq.loseAnswers(2, 0, func(r *http.Request) bool {
		if isOpenCall(r) {
			opened = true
			return true
		}
		return opened && r.Method == http.MethodGet && r.URL.Path == "/api/mate/self"
	})

	_, text, isError := lab.openChangeFrom("fix-login", "Fix the login")

	if !isError || strings.Contains(text, "Nothing was opened") {
		t.Fatalf("want an error that does not say nothing was opened, got isError=%v: %s", isError, text)
	}
	for _, want := range []string{"unknown", `action="open-change"`, "never a second"} {
		if !strings.Contains(text, want) {
			t.Errorf("the answer does not say %q: %s", want, text)
		}
	}
	if kept := lab.ledger(); len(kept) != 0 {
		t.Errorf("an open no answer confirmed recorded %+v", kept)
	}

	again, text, isError := lab.openChangeFrom("fix-login", "Fix the login")
	if isError || again.Created || again.PullRequest != 1 || again.Branch != "fix-login" {
		t.Errorf("a call again answers %s, want change #1 on fix-login answered open", text)
	}
	if n := lab.openChanges(); n != 1 {
		t.Errorf("HQ holds %d open changes, want 1", n)
	}
}

// openChanges is how many open changes the lab's Mate holds in appdev.
func (l *hqLab) openChanges() int {
	l.hq.mu.Lock()
	defer l.hq.mu.Unlock()
	return l.hq.openCount("appdev")
}
