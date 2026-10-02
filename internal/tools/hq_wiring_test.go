// Tests for: tools/hq_wiring.go — every bootstrapped pair gets its
// repository in HQ and the Mate's own branch descending from its main, run
// against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestReconcileHQRepositories_GivesThePairItsRepositoryAndBranch: the pass
// asks HQ for the repository named after the dev half, points the checkout's
// origin at it with the Mate credential where git-push-setup keeps it — a
// sensitive service env on the push source, never a file zcp writes — and
// puts the checkout on the Mate's branch, cut from HQ's main.
func TestReconcileHQRepositories_GivesThePairItsRepositoryAndBranch(t *testing.T) {
	lab := newHQLab(t)

	report := lab.wire()

	if len(report) != 1 || !strings.Contains(report[0], `repository "appdev" wired in HQ`) || !strings.Contains(report[0], `"mate/p-mate"`) {
		t.Fatalf("report = %q", report)
	}
	meta := lab.meta()
	if meta.HQ == nil || meta.HQ.AppID != labApp || meta.HQ.Repo != "appdev" || meta.HQ.Branch != "mate/p-mate" {
		t.Fatalf("the pair's HQ record = %+v", meta.HQ)
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.RemoteURL != lab.hq.srv.URL+"/git/"+labApp+"/appdev.git" {
		t.Errorf("git-push = %s to %q", meta.GitPushState, meta.RemoteURL)
	}
	if got := lab.git("rev-parse", "--abbrev-ref", "HEAD"); got != "mate/p-mate" {
		t.Errorf("the checkout is on %q, want the Mate's branch", got)
	}
	if got := lab.git("remote", "get-url", "origin"); got != meta.RemoteURL {
		t.Errorf("origin = %q, want the repository in HQ", got)
	}
	if lab.git("rev-parse", "HEAD") != lab.remoteHead("main") {
		t.Errorf("a pair fresh from zcp init is cut AT HQ's main")
	}
	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labCredential {
		t.Errorf("the push source holds %q, want the Mate credential", held)
	}
	assertNoSecretOnDisk(t, lab.stateDir, labCredential)

	// One repository per pair: the next pass asks HQ for none.
	elapseHQBackoff(t, lab.stateDir)
	before := len(lab.hq.calls)
	lab.wire()
	for _, call := range lab.hq.calls[before:] {
		if strings.Contains(call, "/api/mate/repos") {
			t.Errorf("a wired pair asked HQ for its repository again: %v", lab.hq.calls[before:])
		}
	}
}

// elapseHQBackoff makes appdev's next pass due.
func elapseHQBackoff(t *testing.T, stateDir string) {
	t.Helper()
	writeHQPairState(stateDir, "appdev", hqPairState{})
}

// TestReconcileHQRepositories_RewiresAPairToTheRepositoryItsRecordNames: a
// pair whose push an earlier refusal marked is wired again — to the
// repository its record names, which a stand-up took from the group recipe,
// never to one named after its hostname.
func TestReconcileHQRepositories_RewiresAPairToTheRepositoryItsRecordNames(t *testing.T) {
	lab := newHQLab(t)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.GitPushState = topology.GitPushBroken
		m.HQ = &workflow.HQRepoRef{AppID: labApp, Repo: "medusa", Branch: "mate/" + labMate}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	lab.wire()

	meta := lab.meta()
	if meta.HQ == nil || meta.HQ.Repo != "medusa" || !strings.HasSuffix(meta.RemoteURL, "/git/"+labApp+"/medusa.git") {
		t.Fatalf("the pair is wired to %+v at %q, want the repository its record names", meta.HQ, meta.RemoteURL)
	}
	for _, call := range lab.hq.calls {
		if strings.Contains(call, "/git/"+labApp+"/appdev.git") {
			t.Errorf("the pass reached a repository named after the hostname: %s", call)
		}
	}
}

func TestHQAttemptDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }
	tests := []struct {
		name  string
		state hqPairState
		want  bool
	}{
		{name: "never tried", want: true},
		{name: "corrupt timestamp runs", state: hqPairState{Attempts: 3, LastAttemptAt: "not a time"}, want: true},
		{name: "just tried", state: hqPairState{Attempts: 1, LastAttemptAt: at(5 * time.Second)}},
		{name: "first backoff elapsed", state: hqPairState{Attempts: 1, LastAttemptAt: at(time.Minute)}, want: true},
		{name: "second backoff not elapsed", state: hqPairState{Attempts: 2, LastAttemptAt: at(45 * time.Second)}},
		{name: "capped, elapsed", state: hqPairState{Attempts: 40, LastAttemptAt: at(11 * time.Minute)}, want: true},
		{name: "capped, not elapsed", state: hqPairState{Attempts: 40, LastAttemptAt: at(9 * time.Minute)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := hqAttemptDue(tt.state, now); got != tt.want {
				t.Errorf("hqAttemptDue(%+v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

// TestReconcileHQRepositories_AsksNothingItNeedNot: outside a container
// there is no Mate, and a pair the user pointed at a remote of their own is
// theirs — HQ is asked nothing, since it makes the repository it is asked for.
func TestReconcileHQRepositories_AsksNothingItNeedNot(t *testing.T) {
	t.Run("local mode", func(t *testing.T) {
		lab := newHQLab(t)
		lab.rt = runtime.Info{}
		if report := lab.wire(); report != nil || len(lab.hq.calls) != 0 {
			t.Errorf("local mode must do nothing, got report=%v calls=%v", report, lab.hq.calls)
		}
	})
	t.Run("a remote of the user's own", func(t *testing.T) {
		lab := newHQLab(t)
		if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
			m.GitPushState, m.RemoteURL = topology.GitPushConfigured, "https://github.com/acme/app.git"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if report := lab.wire(); report != nil || len(lab.hq.calls) != 0 {
			t.Errorf("the user's own remote must be left alone, got report=%v calls=%v", report, lab.hq.calls)
		}
	})
}

// TestReconcileHQRepositories_BacksOff: a refusal is said once, and a pass
// inside the backoff window asks HQ nothing.
func TestReconcileHQRepositories_BacksOff(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.moveMate("")

	first := lab.wire()
	if len(first) != 1 || !strings.Contains(first[0], "HQ refuses this Mate a repository") || !strings.Contains(first[0], "not_in_app") {
		t.Fatalf("first pass = %q, want HQ's refusal said once", first)
	}
	asked := len(lab.hq.calls)
	if second := lab.wire(); len(second) != 0 || len(lab.hq.calls) != asked {
		t.Errorf("a pass inside the backoff window must stay silent and ask nothing, got %v and %d more calls", second, len(lab.hq.calls)-asked)
	}
}

// TestReconcileHQRepositories_ARefusalNamesItsRemedy: a branch step refused
// by name — a history of its own against a main that holds code — names the
// one thing to do, and the next attempt after it wires the pair.
func TestReconcileHQRepositories_ARefusalNamesItsRemedy(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.mu.Lock()
	lab.hq.ensureRepo(t.Context(), labApp, "appdev")
	lab.hq.mu.Unlock()
	lab.hq.landOnMain("appdev", map[string]string{"app.js": "the first Mate's work\n"})
	lab.write(map[string]string{"app.js": "this pair's work\n"})
	lab.git("add", "-A")
	lab.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", "own history")

	report := lab.wire()
	if len(report) != 1 || !strings.Contains(report[0], "ZCP_BASE_NOT_SEED") || !strings.Contains(report[0], "git merge --allow-unrelated-histories") {
		t.Fatalf("report = %q, want the refusal and its remedy", report)
	}
	if lab.meta().HQ != nil {
		t.Errorf("a refused pair must not be recorded as wired")
	}
}

// TestReconcileHQRepositories_AWiredPairsTrackedRefStaysMain: the wiring
// leaves the pair tracking main — what a release compares against — while a
// push goes to its change.
func TestReconcileHQRepositories_AWiredPairsTrackedRefStaysMain(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	if got := lab.meta().TrackedRef; got != "main" {
		t.Errorf("tracked ref = %q, want main", got)
	}
}

// TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange: nothing
// pushes a merge at the Mate — a person merges in the app — so a pass asks
// the Mate's own state what became of the change on record, says so once,
// and forgets the number so the next delivery opens the next change.
func TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange(t *testing.T) {
	for _, tt := range []struct {
		name       string
		settle     func(*fakeHQ)
		want       string
		wantLanded bool
	}{
		{name: "merged", settle: func(f *fakeHQ) { f.merge() }, want: "change #1 is merged", wantLanded: true},
		{name: "closed without merging", settle: func(f *fakeHQ) { f.close(1) }, want: "change #1 was closed without merging"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.deliver()
			elapseHQBackoff(t, lab.stateDir)
			if report := lab.wire(); len(report) != 0 {
				t.Fatalf("an open change says nothing, got %q", report)
			}
			tt.settle(lab.hq)
			elapseHQBackoff(t, lab.stateDir)
			report := lab.wire()
			if len(report) != 1 || !strings.Contains(report[0], tt.want) {
				t.Fatalf("report = %q, want %q", report, tt.want)
			}
			meta := lab.meta()
			if meta.HQ.Change != 0 || (meta.HQ.Landed != nil) != tt.wantLanded {
				t.Errorf("the pair's record = %+v", meta.HQ)
			}
			// The pass folds a squash into the clean checkout right away.
			if tt.wantLanded && !strings.Contains(lab.git("log", "--format=%H"), *lab.hq.change(1).MergedSha) {
				t.Errorf("the pass must fold the squash into the checkout")
			}
		})
	}
}

// TestRecordLandingAndClearChange_SkipAStaleNumber: a pass that read change
// #3's outcome never writes over a pair a concurrent delivery already moved
// on to #4.
func TestRecordLandingAndClearChange_SkipAStaleNumber(t *testing.T) {
	t.Parallel()
	for name, write := range map[string]func(string, *workflow.ServiceMeta){
		"record a landing": func(stateDir string, m *workflow.ServiceMeta) { recordLanding(stateDir, m, 3, "s", "h") },
		"clear the change": func(stateDir string, m *workflow.ServiceMeta) { clearChange(stateDir, m, 3) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
				Hostname: "appdev", BootstrapSession: "test", BootstrappedAt: "2026-10-02",
				HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Branch: "mate/p1", Change: 4},
			}); err != nil {
				t.Fatal(err)
			}
			stale := &workflow.ServiceMeta{Hostname: "appdev", HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 3}}
			write(stateDir, stale)
			fresh, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if fresh.HQ.Change != 4 || fresh.HQ.Landed != nil {
				t.Errorf("a stale read wrote over the newer change: %+v", fresh.HQ)
			}
		})
	}
}

// TestHandleRelease_AWiredPairIsRefused: a wired pair's work lands on main
// only through its change, so a release — which tags what main holds — is
// compared against main and refused while the checkout is ahead of it; no
// tag is pushed. Not parallel: it stubs the package-level push-proof reader.
func TestHandleRelease_AWiredPairIsRefused(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()

	var askedRef string
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, ref string) (LaunchPushProofResult, error) {
		askedRef = ref
		return LaunchPushProofResult{LocalHead: "branchhead", RemoteHead: "mainhead"}, nil
	}
	t.Cleanup(func() { launchPushProofReader = prev })

	ssh := &containerSSHStub{dispatch: func(string) ([]byte, error) { return []byte("ok"), nil }}
	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "appdev", ReleaseVersion: "v1.0.0"}, lab.stateDir, runtime.Info{InContainer: true})
	if askedRef != "main" {
		t.Errorf("freshness compared against %q, want main", askedRef)
	}
	if result == nil || !result.IsError {
		t.Fatalf("release of a wired pair ahead of main must be refused, got %+v", result)
	}
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "git tag") {
			t.Errorf("no tag may be pushed, but ran:\n%s", cmd)
		}
	}
}

// TestBootstrapClose_WiresTheRepository pins WHERE the wiring runs. Provision
// writes PARTIAL metas (no BootstrappedAt), so a pass there sees no pair at
// all; the metas become complete at the bootstrap's terminal step, which is
// where the pair gets its repository in HQ.
func TestBootstrapClose_WiresTheRepository(t *testing.T) {
	lab := newHQLab(t)
	stateDir := t.TempDir()
	lab.stateDir = stateDir
	lab.ssh.dirs["appstage"] = t.TempDir()
	client := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE"},
		{ID: "svc-appstage", Name: "appstage", Status: "READY_TO_DEPLOY"},
	})
	lab.ssh.mock = client
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, client, lab.hq.srv.Client(), labMate, nil, engine, nil, stateDir, "zcp",
		nil, lab.ssh, lab.rt, "")

	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "a Mate's first pair",
	})
	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{{"runtime": map[string]any{
			"devHostname": "appdev", "stageHostname": "appstage", "type": "nodejs@22", "bootstrapMode": "standard",
		}}},
	})
	callTool(t, srv, "zerops_workflow", map[string]any{"action": "complete", "step": "provision", "attestation": "The pair is up."})
	if lab.hq.repos[labApp+"/appdev"] {
		t.Fatal("provision's partial metas must not be wired yet")
	}
	closed := getTextContent(t, callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "close", "attestation": "Bootstrap closed.",
	}))
	if !strings.Contains(closed, `repository \"appdev\" wired in HQ`) {
		t.Errorf("close must report the repository it wired:\n%s", closed)
	}
	if meta := lab.meta(); meta.HQ == nil || meta.HQ.Repo != "appdev" {
		t.Fatalf("the pair must carry its repository once bootstrap is closed, got %+v", meta.HQ)
	}
	assertNoSecretOnDisk(t, stateDir, labCredential)
}

// TestAbsorbLandedChangeOnCheckout_OnlyOnACleanCheckoutOfTheMatesBranch: the
// pass that learns of a merge folds it into the checkout only when that is
// safe — a clean tree on the Mate's own branch — and otherwise leaves the
// checkout exactly as it was; the next delivery is the one that reports.
func TestAbsorbLandedChangeOnCheckout_OnlyOnACleanCheckoutOfTheMatesBranch(t *testing.T) {
	for _, tt := range []struct {
		name  string
		upset func(*hqLab)
	}{
		{name: "work not committed", upset: func(l *hqLab) { l.write(map[string]string{"index.js": "an edit\n"}) }},
		{name: "another branch checked out", upset: func(l *hqLab) { l.git("checkout", "-q", "-b", "elsewhere") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.deliver()
			squash := lab.hq.merge()
			tt.upset(lab)
			head := lab.git("rev-parse", "HEAD")

			elapseHQBackoff(t, lab.stateDir)
			if report := lab.wire(); len(report) != 1 || !strings.Contains(report[0], "change #1 is merged") {
				t.Fatalf("report = %q", report)
			}
			if lab.git("rev-parse", "HEAD") != head {
				t.Errorf("the checkout must be left exactly as it was, not take %s in", squash)
			}
			if lab.meta().HQ.Landed == nil {
				t.Errorf("the landing must stay recorded for the next delivery")
			}
		})
	}
}

// TestWireHQPair_TheRecipeRepositoryIsNoServicesRepository: HQ answers a
// repository named `group` with the application's recipe repository, so zcp
// never asks HQ for it on a pair's behalf — not for a dev half named so, nor
// for a recipe naming it — and says the one thing to do.
func TestWireHQPair_TheRecipeRepositoryIsNoServicesRepository(t *testing.T) {
	tests := []struct {
		name       string
		hostname   string
		repoName   string
		wantRemedy string
	}{
		{name: "a dev half named group", hostname: "group", repoName: "group", wantRemedy: `Rename the service "group"`},
		{name: "a recipe naming group", hostname: "appdev", repoName: "group", wantRemedy: "Fix the recipe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{
				Hostname: tt.hostname, Mode: topology.PlanModeStandard, StageHostname: tt.hostname + "stage",
				BootstrapSession: "test", BootstrappedAt: "2026-10-02",
			}); err != nil {
				t.Fatal(err)
			}
			m, _ := workflow.FindServiceMeta(lab.stateDir, tt.hostname)
			hqc, _ := openHQ(lab.hq.srv.Client())

			outcome := rewireHQPair(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, hqc, m, tt.repoName)

			if outcome.wired || !strings.Contains(outcome.remedy, tt.wantRemedy) || !strings.Contains(outcome.line, "recipe repository") {
				t.Errorf("outcome = %+v, want a refusal whose remedy says %q", outcome, tt.wantRemedy)
			}
			if lab.hq.repos[labApp+"/group"] {
				t.Error("HQ was asked for the recipe repository on a pair's behalf")
			}
			if meta, _ := workflow.FindServiceMeta(lab.stateDir, tt.hostname); meta.HQ != nil {
				t.Errorf("the pair was recorded wired: %+v", meta.HQ)
			}
		})
	}
}
