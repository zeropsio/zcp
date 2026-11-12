// Tests for: tools/hq_change_close.go — a Mate closes its own abandoned open
// change, by branch, on an HQ that lets it; on any other HQ a person closes
// it — run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
)

// closeChange is zerops_workflow action="close-change" in the lab.
func (l *hqLab) closeChange(service, branch string) (changeCloseResult, string, bool) {
	l.t.Helper()
	res, typed, err := handleCloseChange(context.Background(), l.hq.srv.Client(), l.rt, l.stateDir, WorkflowInput{Service: service, Branch: branch})
	if err != nil {
		l.t.Fatalf("handleCloseChange: %v", err)
	}
	if typed != nil {
		l.t.Error("a handler's typed output must stay nil — machine state rides in the text")
	}
	text := resultText(l.t, res)
	var answer changeCloseResult
	if !res.IsError {
		if err := json.Unmarshal([]byte(text), &answer); err != nil {
			l.t.Fatalf("answer is not JSON: %v\n%s", err, text)
		}
	}
	return answer, text, res.IsError
}

// TestCloseChange_OwnByBranch: on an HQ that lets a Mate close its own open
// change, close-change by branch — or by a lease of it — closes that change
// alone, forgets it, and says its branch and leases stay; a branch with no
// open change of this Mate's — another Mate's — closes nothing.
func TestCloseChange_OwnByBranch(t *testing.T) {
	lab := describedChanges(t)
	lab.hq.letMateClose()
	lab.hq.openForAnotherMate("appdev", "shared")
	alias := lab.leaseOf("hqpool", 4, "fix-login")

	answer, text, isError := lab.closeChange("", "add-signup")

	if isError || !answer.Closed || answer.PullRequest != 2 || answer.Branch != "add-signup" {
		t.Fatalf("want change #2 on add-signup closed: %s", text)
	}
	if !strings.Contains(answer.Message, "branch stays") {
		t.Errorf("the answer does not say the branch stays: %s", answer.Message)
	}
	if c := lab.hq.change(2); c.State != hq.ChangeClosed {
		t.Errorf("change #2 in HQ = %+v, want closed", c)
	}
	if c := lab.hq.change(1); c.State != hq.ChangeOpen {
		t.Errorf("closing #2 closed #1: %+v", c)
	}
	// The ledger forgets the close once the Mate hears HQ's state after it.
	lab.passReport()
	if got := branchesOf(lab.ledger()); got != "#1@fix-login" {
		t.Errorf("ledger = %s, want #1@fix-login", got)
	}

	answer, text, isError = lab.closeChange(alias, "")
	if isError || !answer.Closed || answer.PullRequest != 1 || !strings.Contains(answer.Message, alias) {
		t.Errorf("closing by the lease = %s, want #1 closed with its lease named", text)
	}

	answer, text, isError = lab.closeChange("", "shared")
	if isError || answer.Closed || !strings.Contains(answer.Message, "No change of this Mate is open") {
		t.Errorf("closing another Mate's change = %s, want nothing closed", text)
	}
	closes := 0
	for _, n := range []int{1, 2} {
		closes += lab.hq.callCount(fmt.Sprintf("POST /api/mate/changes/appdev/%d/close", n))
	}
	if all := closeCalls(lab.hq); all != closes {
		t.Errorf("HQ was asked to close %d change(s) besides this Mate's #1 and #2", all-closes)
	}
}

// TestCloseChange_OldHQ_SaysPersonCloses: an HQ that keeps one open change per
// repository is not asked to close anything — a person closes the change in
// its review; an HQ that keeps one per branch but has no close for a Mate
// answers the same way. Nothing is closed and the change stays kept.
func TestCloseChange_OldHQ_SaysPersonCloses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		perBranch bool
	}{
		{name: "an HQ that keeps one open change per repository"},
		{name: "an HQ that keeps one per branch, with no close for a Mate", perBranch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			if tc.perBranch {
				lab.hq.keepOpenPerBranch()
			}
			lab.wire()
			lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
			if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
				t.Fatalf("open: %s", text)
			}

			answer, text, isError := lab.closeChange("", "fix-login")

			if isError || answer.Closed || answer.PullRequest != 1 {
				t.Fatalf("want change #1 left to a person: %s", text)
			}
			for _, want := range []string{"a person closes", "/changes/app-1/appdev/1"} {
				if !strings.Contains(answer.Message, want) {
					t.Errorf("the answer lacks %q: %s", want, answer.Message)
				}
			}
			if c := lab.hq.change(1); c.State != hq.ChangeOpen {
				t.Errorf("change #1 = %+v, want open", c)
			}
			if lab.kept(1) == nil {
				t.Error("the ledger forgot an open change it did not close")
			}
			if !tc.perBranch && lab.hq.callCount("POST /api/mate/changes/appdev/1/close") != 0 {
				t.Error("an HQ that keeps one open change per repository was asked to close one")
			}
		})
	}
}

// TestCloseChange_NamesItsBranch: close-change never infers which change —
// not even the only open one: it names a branch, or a lease of it.
func TestCloseChange_NamesItsBranch(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.hq.letMateClose()
	lab.wire()
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open: %s", text)
	}

	_, text, isError := lab.closeChange("", "")

	if !isError || !strings.Contains(text, "branch=") {
		t.Errorf("want a refusal asking for branch=, got: %s", text)
	}
	if c := lab.hq.change(1); c.State != hq.ChangeOpen {
		t.Errorf("change #1 = %+v, want open", c)
	}
}

// closeCalls is how many closes of any change HQ was asked for.
func closeCalls(f *fakeHQ) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "POST /api/mate/changes/") && strings.HasSuffix(c, "/close") {
			n++
		}
	}
	return n
}

// TestCloseChange_RefusedKey_SaysSoAndNoRetry: HQ answering this Mate's
// close 401 key_refused is a known answer, not HQ silent — close-change
// names the refused key, closes nothing and advises no retry, since asking
// again closes nothing until the key is renewed. Run in-process: the Mate's
// state names its change open on the branch, and HQ's answer is the 401
// {"code":"key_refused"} it gives every call of a revoked key.
// witness: internal/tools/hq_change_close.go | if refusal := accessRefusal(err); refusal != "" { → if refusal := ""; refusal != "" {
func TestCloseChange_RefusedKey_SaysSoAndNoRetry(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeWiredPair(t, stateDir, "appdev", "appdev")
	statePath := filepath.Join(t.TempDir(), "state.json")
	mate := `{"rev":1,"projectId":"` + labMate + `","appId":"` + labApp + `","repos":[],"chief":null,"openPerBranch":true,` +
		`"changes":[{"repo":"appdev","number":1,"title":"Fix the login","state":"open","branch":"fix-login","head":null,"mergedSha":null,"landedHead":null}]}`
	if err := os.WriteFile(statePath, []byte(`{"link":"linked","mate":`+mate+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := runtime.Info{OrgID: labOrg, HQ: &runtime.HQLink{Origin: "https://hq.example.invalid", ProjectID: labMate, Key: labKey, StatePath: statePath}}
	refusing := refusingDoer{status: http.StatusUnauthorized, body: `{"code":"key_refused"}`}

	res, _, err := handleCloseChange(context.Background(), refusing, rt, stateDir, WorkflowInput{Branch: "fix-login"})
	if err != nil {
		t.Fatalf("handleCloseChange: %v", err)
	}
	text := resultText(t, res)
	var answer changeCloseResult
	if res.IsError || json.Unmarshal([]byte(text), &answer) != nil {
		t.Fatalf("want an answer naming the refused key, got: %s", text)
	}
	if answer.Closed || !strings.Contains(answer.Message, "HQ refused this Mate's key (401 key_refused)") || !strings.Contains(answer.Message, "will not help until") {
		t.Errorf("want nothing closed, the refused key named and no retry: %s", answer.Message)
	}
	if strings.Contains(answer.Message, "once HQ answers") {
		t.Errorf("the answer advises a retry: %s", answer.Message)
	}
}

// refusingDoer answers every request in-process with status and body.
type refusingDoer struct {
	status int
	body   string
}

func (d refusingDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: d.status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(d.body)), Request: req}, nil
}
