// Tests for: tools/hq_delivery.go — a deploy of a wired pair's stage half
// delivers the dev half's work as a change in HQ, run against a fake HQ that
// serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// startSession opens a work session with intent, as the agent's task.
func startSession(t *testing.T, stateDir, intent string) {
	t.Helper()
	ws := workflow.NewWorkSession(labMate, string(workflow.EnvContainer), intent, []string{"appdev"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
}

// TestAStageDeployOfAWiredPairDeliversItself is the owner's run of
// 2026-09-17: "build a todo app" has to end with a change without the person
// saying how code travels. The stage half's deploy commits the dev half's
// tree with the task's words, opens the change and pushes its branch; the dev
// half's own deploys deliver nothing.
func TestAStageDeployOfAWiredPairDeliversItself(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	startSession(t, lab.stateDir, "Build a todo app\nwith a list")
	lab.write(map[string]string{"index.js": "the app\n", ".gitignore": "node_modules/\n"})

	if d := deliverHQPair(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, "appdev"); d != nil {
		t.Fatalf("the dev half delivers nothing, got %+v", d)
	}
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want a delivery through a change, got %+v", delivery)
	}
	change := lab.hq.change(1)
	if change == nil || change.Title != "Build a todo app" {
		t.Fatalf("change #1 in HQ = %+v, want it titled with the task", change)
	}
	head := lab.remoteHead("mate/p-mate/1")
	if head == "" || head != lab.git("rev-parse", "HEAD") {
		t.Fatalf("the change's branch is at %q, want the checkout's HEAD", head)
	}
	if !lab.descends(labApp, "main", "mate/p-mate/1") {
		t.Errorf("the change's branch must descend from main")
	}
	if got := lab.git("log", "-1", "--format=%s"); got != "Build a todo app" {
		t.Errorf("the commit reads %q, want the task's words", got)
	}
	wantURL := lab.hq.srv.URL + "/changes/" + labApp + "/appdev/1"
	if delivery.Change.URL != wantURL || delivery.Change.Number != 1 || delivery.Change.Branch != "mate/p-mate/1" || !delivery.Change.Created {
		t.Errorf("change = %+v, want #1 at %s", delivery.Change, wantURL)
	}
	for _, want := range []string{wantURL, "change #1", `zerops_workflow action="describe-change" service="appdev"`, "Tell the person that link", "may merge it at any moment", "next stage deploy folds the merge in"} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	for _, never := range []string{"git-push", "build-integration", "pull request"} {
		if strings.Contains(delivery.Line, never) {
			t.Errorf("the line must not say %q:\n%s", never, delivery.Line)
		}
	}
	if lab.meta().HQ.Change != 1 {
		t.Errorf("the change's number is not on the pair: %+v", lab.meta().HQ)
	}
}

// TestAStageDeployAbsorbsTheMatesOwnMergedChange is the squash landing at the
// delivery: HQ merges a change by squashing it, and the squash shares no
// history with the branch that became it. The delivery reads the Mate's own
// state fresh — no pass has to have run since the merge — folds the squash in
// as a real merge, and opens the next change for the new work, never a
// conflict against history the squash discarded.
func TestAStageDeployAbsorbsTheMatesOwnMergedChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	if d := lab.deliver(); d == nil || d.Change == nil {
		t.Fatalf("first delivery: %+v", d)
	}
	squash := lab.hq.merge()

	lab.write(map[string]string{"footer.js": "the footer\n"})
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil || delivery.Change.Number != 2 {
		t.Fatalf("want the work on change #2, got %+v", delivery)
	}
	if !strings.Contains(delivery.Line, "Change #1 is merged") {
		t.Errorf("the delivery must say what it learned of change #1:\n%s", delivery.Line)
	}
	if old, next := strings.Index(delivery.Line, "Change #1 is merged"), strings.Index(delivery.Line, "Delivered:"); old > next {
		t.Errorf("the previous merge must be reported before the next delivery:\n%s", delivery.Line)
	}
	if strings.Contains(delivery.Line, "; its next change opens a new one") {
		t.Errorf("the delivery already opened change #2, so it must not promise another:\n%s", delivery.Line)
	}
	if !lab.descends(labApp, squash, "mate/p-mate/2") {
		t.Errorf("change #2 must take the squash in as a real merge")
	}
	files := lab.git("ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{"index.js", "footer.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q: %s", want, files)
		}
	}
	if hqRecord := lab.meta().HQ; hqRecord.Landed != nil || hqRecord.Change != 2 {
		t.Errorf("the pair's record = %+v, want change #2 and the landing forgotten", hqRecord)
	}
}

// TestAStageDeployWithNothingBeyondMainOpensNoChange: a change is opened only
// for work `main` does not already have.
func TestAStageDeployWithNothingBeyondMainOpensNoChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.deliver()
	lab.hq.merge()
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch -q origin && git reset -q --hard origin/main"); err != nil {
		t.Fatalf("reset the checkout onto main: %v\n%s", err, out)
	}

	delivery := lab.deliver()
	if delivery == nil || delivery.Change != nil {
		t.Fatalf("want a delivery with no change, got %+v", delivery)
	}
	if !strings.Contains(delivery.Line, "nothing to deliver: main already has this") {
		t.Errorf("line:\n%s", delivery.Line)
	}
	if lab.hq.change(2) != nil {
		t.Errorf("no change may be opened for a checkout main already has")
	}
}

// TestADeliveryHQCannotReachFailsFast: HQ not answering at all fails the
// delivery within the call — taking main in tried three times, 1 s and 3 s
// apart, each try's preflight finding HQ not answering, so git never runs
// against it — saying the step, HQ's address, the last error and the way on.
// The work is committed in the checkout first, nothing asks HQ again once the
// call has returned, and the next delivery once HQ answers sends it
// (spec-mate §10.10).
func TestADeliveryHQCannotReachFailsFast(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	startSession(t, lab.stateDir, "Add a footer")
	lab.write(map[string]string{"footer.js": "the footer\n"})

	lab.hq.setDown(true)
	const (
		preflight = "HEAD /"
		fetch     = "GET /git/" + labApp + "/appdev.git/info/refs"
	)
	preflights, fetches := lab.hq.callCount(preflight), lab.hq.callCount(fetch)
	delivery := lab.deliver()
	if delivery == nil || delivery.Change != nil {
		t.Fatalf("want a failed delivery, got %+v", delivery)
	}
	for _, want := range []string{
		"appstage runs, but its code has not reached HQ",
		"HQ at " + lab.hq.srv.URL + ` is not answering: taking "main" in failed after 3 tries (the last: no answer (hq /: `,
		"the work stays committed in appdev's checkout, and deploying appstage again delivers it",
		"tell the person HQ is not answering",
	} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	if got := lab.hq.callCount(preflight) - preflights; got != 3 {
		t.Errorf("HQ was preflighted %d times, want 3", got)
	}
	if got := lab.hq.callCount(fetch) - fetches; got != 0 {
		t.Errorf("git asked HQ for main %d times, want none past a failed preflight", got)
	}
	if want := []time.Duration{time.Second, 3 * time.Second}; !slices.Equal(lab.waits, want) {
		t.Errorf("waits = %v, want %v", lab.waits, want)
	}
	if got := lab.git("log", "-1", "--format=%s"); got != "Add a footer" {
		t.Errorf("the work is committed in the checkout even while HQ is away, HEAD reads %q", got)
	}

	// Nothing asks HQ again once the call has returned.
	asked := lab.hq.callTotal()
	time.Sleep(200 * time.Millisecond)
	if got := lab.hq.callTotal(); got != asked {
		t.Errorf("HQ was asked %d more times after the delivery returned", got-asked)
	}

	lab.hq.setDown(false)
	again := lab.deliver()
	if again == nil || again.Change == nil || again.Change.Number != 1 {
		t.Fatalf("the next delivery = %+v, want change #1", again)
	}
	if change := lab.hq.change(1); change == nil || change.Title != "Add a footer" {
		t.Errorf("change #1 = %+v, want it titled with the task", change)
	}
	if head := lab.remoteHead("mate/p-mate/1"); head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #1's branch is at %q, want the checkout's HEAD", head)
	}
}

// TestADeliveryToAnHQThatNeverAcceptsEndsWithinItsBound: an HQ that never
// accepts a connection costs a delivery step three tries of the connect
// bound and their waits — never curl's 300 s, never git's cap: the preflight
// before git, and an API call's own connect bound, end each try. git's step
// meets it taking main in; an API call meets it opening the change.
//
// Non-parallel: it narrows the delivery's bounds.
func TestADeliveryToAnHQThatNeverAcceptsEndsWithinItsBound(t *testing.T) {
	prevBounds := deliveryBounds
	deliveryBounds.connect, deliveryBounds.answer = 200*time.Millisecond, time.Second
	t.Cleanup(func() { deliveryBounds = prevBounds })
	tests := []struct {
		name    string
		deliver func(lab *hqLab) string
		want    string
	}{
		{"taking main in", func(lab *hqLab) string {
			d := lab.deliver()
			if d == nil || d.Change != nil {
				t.Fatalf("delivery = %+v, want it failed", d)
			}
			return d.Line
		}, ` is not answering: taking "main" in failed after 3 tries (the last: no connection within 200ms)`},
		{"opening the change", func(lab *hqLab) string {
			hqc, err := hq.Open(lab.hq.srv.Client(), hq.EnrollmentPath())
			if err != nil {
				t.Fatal(err)
			}
			return shipChange(t.Context(), lab.ssh, lab.stateDir, deliveryClient(hqc), lab.meta(), "Add a footer").line
		}, " is not answering: opening its change failed after 3 tries (the last: no connection within 200ms)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"footer.js": "the footer\n"})
			address := lab.hq.srv.URL
			lab.hq.neverAccept()

			start := time.Now()
			line := tt.deliver(lab)
			if took := time.Since(start); took > 5*time.Second {
				t.Errorf("the step took %s, want it ended by its bound", took)
			}
			if want := "HQ at " + address + tt.want; !strings.Contains(line, want) {
				t.Errorf("the line misses %q:\n%s", want, line)
			}
			if want := []time.Duration{time.Second, 3 * time.Second}; !slices.Equal(lab.waits, want) {
				t.Errorf("waits = %v, want %v", lab.waits, want)
			}
		})
	}
}

// TestADeliveryTriesHQOnlyWhileItCannotServe: opening the change, or
// pushing its branch, is tried again only while HQ cannot serve it — a
// standby's 503, or the balancer's 502 or 504 for an HQ that is down — three
// times at most, 1 s and 3 s apart, and said as HQ not serving; a refusal
// fails at once, and an HQ that answers on a later try delivers.
func TestADeliveryTriesHQOnlyWhileItCannotServe(t *testing.T) {
	const (
		open = "POST /api/mate/changes"
		push = "POST /git/" + labApp + "/appdev.git/git-receive-pack"
	)
	tests := []struct {
		name      string
		call      string // the request HQ fails, "METHOD /path"
		status    int
		code      string
		failing   int
		wantCalls int
		wantWaits []time.Duration
		wantLine  string // after HQ's address, when it starts " is not"
		delivered bool
	}{
		{"a standby throughout", open, http.StatusServiceUnavailable, "not_active", 3, 3,
			[]time.Duration{time.Second, 3 * time.Second}, " is not answering: opening its change failed after 3 tries (the last: HQ answered 503 (not serving))", false},
		{"the balancer's 502 throughout", open, http.StatusBadGateway, "", 3, 3,
			[]time.Duration{time.Second, 3 * time.Second}, " is not answering: opening its change failed after 3 tries (the last: HQ answered 502 (not serving))", false},
		{"the balancer's 504 throughout", open, http.StatusGatewayTimeout, "", 3, 3,
			[]time.Duration{time.Second, 3 * time.Second}, " is not answering: opening its change failed after 3 tries (the last: HQ answered 504 (not serving))", false},
		{"a 502 to the push throughout", push, http.StatusBadGateway, "", 3, 3,
			[]time.Duration{time.Second, 3 * time.Second}, " is not answering: pushing mate/p-mate/1 failed after 3 tries (the last: HQ answered 502 (not serving))", false},
		{"a refusal", open, http.StatusForbidden, "forbidden", 1, 1,
			nil, `HQ refused to open a change on "appdev"`, false},
		{"answered on the second try", open, http.StatusServiceUnavailable, "not_active", 1, 2,
			[]time.Duration{time.Second}, "Delivered", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.hq.answerWith(tt.status, tt.code, tt.failing, func(r *http.Request) bool {
				return r.Method+" "+r.URL.Path == tt.call
			})
			before := lab.hq.callCount(tt.call)

			delivery := lab.deliver()
			if delivery == nil || (delivery.Change != nil) != tt.delivered {
				t.Fatalf("delivery = %+v, want delivered=%v", delivery, tt.delivered)
			}
			if got := lab.hq.callCount(tt.call) - before; got != tt.wantCalls {
				t.Errorf("HQ was asked %s %d times, want %d", tt.call, got, tt.wantCalls)
			}
			if !slices.Equal(lab.waits, tt.wantWaits) {
				t.Errorf("waits = %v, want %v", lab.waits, tt.wantWaits)
			}
			want := tt.wantLine
			if strings.HasPrefix(want, " is not") {
				want = "HQ at " + lab.hq.srv.URL + want
			}
			if strings.Contains(delivery.Line, "hq refused: 5") {
				t.Errorf("an HQ that does not serve never reads as refusing:\n%s", delivery.Line)
			}
			if !strings.Contains(delivery.Line, want) {
				t.Errorf("the line misses %q:\n%s", want, delivery.Line)
			}
			if tt.wantLine != "Delivered" && tt.status >= 500 && !strings.Contains(delivery.Line, "tell the person HQ is not answering") {
				t.Errorf("an HQ that could not serve the delivery must be named to the person:\n%s", delivery.Line)
			}
			if tt.status < 500 && strings.Contains(delivery.Line, "not answering") {
				t.Errorf("a refusal is HQ answering, never HQ not answering:\n%s", delivery.Line)
			}
		})
	}
}

// TestADeliveryWaitsOutAStandby: through a deploy of HQ two instances run
// side by side, and the standby answers git 503 with no Retry-After git can
// show. The push is tried again, never read as a failure.
func TestADeliveryWaitsOutAStandby(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.hq.standby(2, func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-receive-pack")
	})

	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want the delivery through once the standby is gone, got %+v", delivery)
	}
	if head := lab.remoteHead("mate/p-mate/1"); head == "" {
		t.Errorf("change #1's branch was never pushed")
	}
}

// TestADeliveryFollowsTheMateToAnotherApplication: HQ now holds the Mate in
// another application, so the delivery wires the pair to the repository of
// the same name there — made if new, its origin pointed at it, the Mate's
// branch joined onto its main — and delivers there. The change in the old
// application stays where it is.
func TestADeliveryFollowsTheMateToAnotherApplication(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.deliver()

	lab.hq.moveMate("app-2")
	lab.write(map[string]string{"footer.js": "the footer\n"})
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil || delivery.Change.Number != 1 {
		t.Fatalf("want change #1 in the new application, got %+v", delivery)
	}
	if !strings.Contains(delivery.Change.URL, "/changes/app-2/appdev/1") {
		t.Errorf("the change's address = %q, want it in app-2", delivery.Change.URL)
	}
	if !strings.Contains(delivery.Line, "change #1 stays in the one it was opened in") {
		t.Errorf("the line must say the old change stays:\n%s", delivery.Line)
	}
	meta := lab.meta()
	if meta.HQ.AppID != "app-2" || meta.HQ.Change != 1 || !strings.Contains(lab.git("remote", "get-url", "origin"), "/git/app-2/appdev.git") {
		t.Errorf("the pair is not wired to app-2: %+v, origin %s", meta.HQ, lab.git("remote", "get-url", "origin"))
	}
	if !lab.descends("app-2", "main", "mate/p-mate/1") {
		t.Errorf("the change in app-2 must descend from its main")
	}
	if old := lab.hq.change(1); old == nil || old.State != "open" {
		t.Errorf("the change in the old application = %+v, want it left open there", old)
	}
}

// TestADeliveryBringsTheCredentialToTheCurrentOne: a re-enrollment replaces
// the Mate credential, and the copy the push source holds goes stale with
// it. The delivery rewrites the copy and proves it before anything pushes.
func TestADeliveryBringsTheCredentialToTheCurrentOne(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.mock.WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "ud-git-token", Key: "GIT_TOKEN", Content: "a-credential-hq-revoked", Sensitive: true}})
	lab.write(map[string]string{"index.js": "the app\n"})

	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want the delivery through with the current credential, got %+v", delivery)
	}
	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labCredential {
		t.Errorf("the push source holds %q, want the current credential", held)
	}
}

// TestADeliveryProvesItsCredentialFailingFast: proving the pair's push
// credential is a delivery step like any other. (a) HQ not serving the proof
// is tried three times and said as HQ not answering, and marks nothing — the
// credential was never refused. (b) HQ refusing it (401/403) marks the pair
// refused, at once. (c) A credential zcp renewed — in this call, or in one
// within the window the record keeps — is waited for until a fresh session
// holds it, asked of the container and never of HQ, bounded from the
// renewal; HQ is asked for the proof once it does, and a refusal then says
// it came after that wait.
func TestADeliveryProvesItsCredentialFailingFast(t *testing.T) {
	const (
		proof   = "GET /git/" + labApp + "/appdev.git/info/refs"
		revoked = "a-credential-hq-revoked"
	)
	tests := []struct {
		name        string
		stale       bool          // the push source holds a credential HQ revoked
		marked      bool          // an earlier refusal marked the pair
		renewedAgo  time.Duration // an earlier call renewed the credential this long ago; 0: none
		lateReads   int           // session reads that still see the revoked credential; -1: always
		status      int           // what HQ answers the proof, 0 for served
		wantProofs  int           // -1: not counted (a delivery's own fetch reads the same refs)
		wantReads   int           // session reads of the credential's digest; -1: not counted
		wantState   topology.GitPushState
		wantRenewal bool // the record still carries a renewal not yet proved
		wantLine    []string
		neverLine   string
	}{
		{"(a) HQ not serving the proof of a renewed credential", true, false, 0, 0, http.StatusBadGateway, 3, 1, topology.GitPushConfigured, true,
			[]string{" is not answering: proving appdev's credential failed after 3 tries (the last: HQ answered 502 (not serving))", "deploying appstage again delivers it", "tell the person HQ is not answering"},
			"refus"},
		{"(a) HQ not serving the proof of a marked pair", false, true, 0, 0, http.StatusServiceUnavailable, 3, 0, topology.GitPushBroken, false,
			[]string{" is not answering: proving appdev's credential failed after 3 tries (the last: HQ answered 503 (not serving))"},
			"refuses this Mate"},
		{"(b) HQ refusing the credential", false, true, 0, 0, http.StatusForbidden, 1, 0, topology.GitPushBroken, false,
			[]string{"HQ refuses this Mate's current credential for appdev", "marked as refused"},
			"just-renewed"},
		{"(b) HQ taking the credential again", false, true, 0, 0, 0, -1, 0, topology.GitPushConfigured, false,
			[]string{"Delivered"},
			"refus"},
		{"(b) a renewal older than the window waits for nothing", false, false, time.Minute, -1, 0, -1, 0, topology.GitPushConfigured, false,
			[]string{"Delivered"},
			"refus"},
		{"(c) a renewed credential reaching sessions late", true, false, 0, 2, 0, -1, 3, topology.GitPushConfigured, false,
			[]string{"Delivered"},
			"refus"},
		{"(c) a credential an earlier call renewed, reaching sessions late", false, false, 50 * time.Millisecond, 2, 0, -1, 3, topology.GitPushConfigured, false,
			[]string{"Delivered"},
			"refus"},
		{"(c) HQ refusing a just-renewed credential", true, false, 0, 0, http.StatusForbidden, 1, 1, topology.GitPushBroken, false,
			[]string{"HQ refuses this Mate's credential for appdev (after waiting for a just-renewed credential to take effect: appdev's sessions hold it)", "marked as refused"},
			"not answering"},
		{"(c) a renewed credential never reaching sessions", true, false, 0, -1, 0, 0, -1, topology.GitPushConfigured, true,
			[]string{"the credential renewed onto appdev has not reached its sessions within 300ms of its renewal", "deploy appstage again"},
			"refus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			if tt.stale {
				lab.mock.WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "ud-git-token", Key: "GIT_TOKEN", Content: "a-credential-hq-revoked", Sensitive: true}})
			}
			if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				if tt.marked {
					m.GitPushState = topology.GitPushBroken
				}
				if tt.renewedAgo != 0 {
					m.HQ.CredentialRenewedAt = time.Now().Add(-tt.renewedAgo).UTC().Format(time.RFC3339Nano)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if tt.status != 0 {
				lab.hq.answerWith(tt.status, "", 99, func(r *http.Request) bool { return r.Method+" "+r.URL.Path == proof })
			}
			if tt.lateReads != 0 {
				late := tt.lateReads
				lab.ssh.sessionToken = func(command, current string) string {
					if !strings.Contains(command, "hash-object --stdin") || late == 0 {
						return current
					}
					late--
					return revoked
				}
			}
			lab.write(map[string]string{"index.js": "the app\n"})
			before := lab.hq.callCount(proof)

			delivery := lab.deliver()
			if delivery == nil {
				t.Fatal("want a delivery")
			}
			for _, want := range tt.wantLine {
				if strings.HasPrefix(want, " is not") {
					want = "HQ at " + lab.hq.srv.URL + want
				}
				if !strings.Contains(delivery.Line, want) {
					t.Errorf("the line misses %q:\n%s", want, delivery.Line)
				}
			}
			if strings.Contains(delivery.Line, tt.neverLine) {
				t.Errorf("the line must not say %q:\n%s", tt.neverLine, delivery.Line)
			}
			if tt.wantProofs >= 0 {
				if got := lab.hq.callCount(proof) - before; got != tt.wantProofs {
					t.Errorf("HQ was asked for the proof %d times, want %d", got, tt.wantProofs)
				}
			}
			if tt.wantReads >= 0 {
				reads := 0
				for _, command := range lab.ssh.commands {
					if strings.Contains(command, "hash-object --stdin") {
						reads++
					}
				}
				if reads != tt.wantReads {
					t.Errorf("sessions were read for the credential %d times, want %d", reads, tt.wantReads)
				}
			}
			meta := lab.meta()
			if meta.GitPushState != tt.wantState {
				t.Errorf("the pair's git-push state = %q, want %q", meta.GitPushState, tt.wantState)
			}
			if renewal := meta.HQ.CredentialRenewedAt; (renewal != "") != tt.wantRenewal {
				t.Errorf("the record's renewal = %q, want one kept: %v", renewal, tt.wantRenewal)
			}
		})
	}
}

// TestADeliveryRefusedByItsGitSaysWhatToDo: a delivery its git stops by name
// leaves the checkout whole, pushes nothing, opens nothing, and says the one
// thing to do — never a force.
func TestADeliveryRefusedByItsGitSaysWhatToDo(t *testing.T) {
	tests := []struct {
		name  string
		setup func(lab *hqLab)
		files map[string]string
		want  []string
	}{
		{
			name:  "main moved on and changes the same lines",
			setup: func(lab *hqLab) { lab.hq.landOnMain("appdev", map[string]string{"index.js": "somebody else's line\n"}) },
			files: map[string]string{"index.js": "my line\n"},
			want:  []string{"has moved on", "index.js", "git fetch origin && git merge origin/main"},
		},
		{
			name:  "an unignored dependency directory",
			files: map[string]string{"index.js": "the app\n", "node_modules/express/index.js": "a dependency\n"},
			want:  []string{"node_modules", ".gitignore", "deploy appstage again"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			if tt.setup != nil {
				tt.setup(lab)
			}
			lab.write(tt.files)
			before := lab.git("rev-parse", "HEAD")

			delivery := lab.deliver()
			if delivery == nil || delivery.Change != nil {
				t.Fatalf("want a refused delivery, got %+v", delivery)
			}
			for _, want := range tt.want {
				if !strings.Contains(delivery.Line, want) {
					t.Errorf("the line misses %q:\n%s", want, delivery.Line)
				}
			}
			if strings.Contains(delivery.Line, "--force") || strings.Contains(delivery.Line, "rebase") {
				t.Errorf("a refusal never offers a force or a rebase:\n%s", delivery.Line)
			}
			if lab.hq.change(1) != nil || lab.remoteHead("mate/p-mate/1") != "" {
				t.Errorf("nothing may be opened or pushed")
			}
			if state := lab.git("status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
				t.Errorf("the checkout must be left whole, not half-merged: %q", state)
			}
			if tt.setup == nil && lab.git("rev-parse", "HEAD") != before {
				t.Errorf("nothing may be committed before the refusal")
			}
		})
	}
}

// TestAWiredPairDeploysDirectlyAndIsNeverSentToPush: nothing builds a Mate's
// services from its change, so a wired pair's direct deploy proceeds and is
// never told to push instead — its stage deploy delivers by itself.
func TestAWiredPairDeploysDirectlyAndIsNeverSentToPush(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
		BootstrapSession: "test", BootstrappedAt: "2026-10-02", FirstDeployedAt: "2026-10-02T09:00:00Z",
		GitPushState: topology.GitPushConfigured, RemoteURL: "https://hq.example/git/a1/appdev.git",
		BuildIntegration: topology.BuildIntegrationActions,
		HQ:               &workflow.HQRepoRef{AppID: "a1", Repo: "appdev", Branch: "mate/p1"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"appdev", "appstage"} {
		if r := repoDeliveryRedirect(stateDir, target, "", false); r != nil {
			t.Errorf("%s: a wired pair's direct deploy must proceed", target)
		}
		if w := repoDeliveryDivergenceWarning(stateDir, target); w != "" {
			t.Errorf("%s: no push-by-hand warning on a wired pair, got %q", target, w)
		}
	}
}

// refusingPushSSH answers a change's push the way git reports HQ refusing
// it — a ref HQ refused carries HQ's own reason — and everything else as a
// healthy container would. pushes counts the pushes it was sent.
type refusingPushSSH struct {
	reason string
	pushes int
}

func (s *refusingPushSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	if !strings.Contains(command, "push -u origin") {
		return []byte("ok"), nil
	}
	s.pushes++
	out := " ! [remote rejected] HEAD -> mate/p-mate/1 (" + s.reason + ")\nerror: failed to push some refs\n"
	return []byte(out), &platform.SSHExecError{Hostname: host, Output: out, Err: errors.New("exit status 1")}
}

func (s *refusingPushSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// TestShipChange_AChangeSettledBeforeItsPushFailsAtOnce: a change merged or
// closed between its opening and its push is refused per ref with HQ's
// reason — a refusal, so the push is not tried again; the number is
// forgotten, so the next delivery opens the next change.
func TestShipChange_AChangeSettledBeforeItsPushFailsAtOnce(t *testing.T) {
	for _, reason := range []string{"change_closed", "unknown_change"} {
		t.Run(reason, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			hqc, err := hq.Open(lab.hq.srv.Client(), hq.EnrollmentPath())
			if err != nil {
				t.Fatal(err)
			}
			ssh := &refusingPushSSH{reason: reason}
			shipped := shipChange(t.Context(), ssh, lab.stateDir, hqc, lab.meta(), "Add a footer")
			if shipped.unreachable || shipped.ref != nil || !strings.Contains(shipped.line, reason) || !strings.Contains(shipped.line, "the next delivery opens the next change") {
				t.Fatalf("shipped = %+v, want a refusal that names the next delivery", shipped)
			}
			if ssh.pushes != 1 {
				t.Errorf("the refused push ran %d times, want once", ssh.pushes)
			}
			if record := lab.meta().HQ; record.Change != 0 {
				t.Errorf("the pair's record = %+v, want no change", record)
			}
		})
	}
}

func TestAStageDeployAfterASquashWithNoNewWorkReportsNothingToDeliver(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "app\n"})
	lab.deliver()
	main := lab.hq.merge()
	d := lab.deliver()
	if d == nil || d.Change != nil || !strings.Contains(d.Line, "nothing to deliver: main already has this") {
		t.Fatalf("delivery = %+v", d)
	}
	if lab.hq.change(2) != nil {
		t.Fatal("opened an empty change")
	}
	if got := lab.git("rev-parse", "HEAD"); got != main {
		t.Fatalf("HEAD = %s, want main %s", got, main)
	}
	if !strings.Contains(d.Line, "starts from main") {
		t.Fatalf("fresh base wasn't reported: %s", d.Line)
	}
}

// A legacy delivery could have cleared the local landing while retaining its
// pre-squash history. HQ's latest landing remains the authoritative base.
func TestAStageDeliveryStartsFromHQsLandingWhenTheLocalRecordWasAlreadyCleared(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "app\n"})
	lab.deliver()
	main := lab.hq.merge()
	if err := workflow.UpsertServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.HQ.Change = 0
		m.HQ.Landed = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.write(map[string]string{"footer.js": "footer\n"})
	d := lab.deliver()
	if d == nil || d.Change == nil || d.Change.Number != 2 {
		t.Fatalf("delivery = %+v", d)
	}
	if parent := lab.git("rev-parse", "HEAD^1"); parent != main {
		t.Fatalf("next change's parent = %s, want main %s", parent, main)
	}
	if !strings.Contains(d.Line, "starts from main") {
		t.Fatalf("fresh start wasn't reported: %s", d.Line)
	}
}
