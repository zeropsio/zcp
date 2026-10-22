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

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestReconcileHQRepositories_GivesThePairItsRepositoryOnMain: the pass asks
// HQ for the repository named after the dev half, points the checkout's
// origin at it with the Mate credential where git-push-setup keeps it — a
// sensitive service env on the push source, never a file zcp writes — and
// puts the checkout on `main`, descending from HQ's main.
func TestReconcileHQRepositories_GivesThePairItsRepositoryOnMain(t *testing.T) {
	lab := newHQLab(t)

	report := lab.wire()

	if len(report) != 1 || !strings.Contains(report[0], `repository "appdev" wired in HQ`) || !strings.Contains(report[0], `branches cut from "main"`) {
		t.Fatalf("report = %q", report)
	}
	meta := lab.meta()
	if meta.HQ == nil || meta.HQ.AppID != labApp || meta.HQ.Repo != "appdev" {
		t.Fatalf("the pair's HQ record = %+v", meta.HQ)
	}
	if meta.GitPushState != topology.GitPushConfigured || meta.RemoteURL != lab.hq.srv.URL+"/git/"+labApp+"/appdev.git" {
		t.Errorf("git-push = %s to %q", meta.GitPushState, meta.RemoteURL)
	}
	if got := lab.git("rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("the checkout is on %q, want main", got)
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
		m.HQ = &workflow.HQRepoRef{AppID: labApp, Repo: "medusa"}
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
	if len(report) != 1 || !strings.Contains(report[0], "ZCP_BASE_NOT_SEED") || !strings.Contains(report[0], "git reset --keep FETCH_HEAD") {
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
// and forgets the number: a later change is another one.
func TestReconcileHQRepositories_TellsTheMateWhatBecameOfItsChange(t *testing.T) {
	for _, tt := range []struct {
		name   string
		settle func(*fakeHQ)
		want   []string
	}{
		// The checkout is brought up to date where git can ask zcp, whether its
		// origin is HQ or GitHub.
		{name: "merged", settle: func(f *fakeHQ) { f.merge() }, want: []string{"change #1 is merged", "in your own shell on the mounted checkout, never over ssh"}},
		{name: "closed without merging", settle: func(f *fakeHQ) { f.close(1) }, want: []string{"change #1 was closed without merging"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			recordChange(lab.stateDir, lab.meta(), lab.openChange().Number, "")
			elapseHQBackoff(t, lab.stateDir)
			if report := lab.wire(); len(report) != 0 {
				t.Fatalf("an open change says nothing, got %q", report)
			}
			tt.settle(lab.hq)
			elapseHQBackoff(t, lab.stateDir)
			report := lab.wire()
			if len(report) != 1 {
				t.Fatalf("report = %q, want one line", report)
			}
			for _, want := range tt.want {
				if !strings.Contains(report[0], want) {
					t.Errorf("report = %q, want %q", report[0], want)
				}
			}
			if meta := lab.meta(); meta.HQ.Change != 0 {
				t.Errorf("the pair's record = %+v", meta.HQ)
			}
		})
	}
}

// TestClearChange_SkipsAStaleNumber: a pass that read change #3's outcome
// never writes over a pair a concurrent describe already moved on to #4.
func TestClearChange_SkipsAStaleNumber(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", BootstrapSession: "test", BootstrappedAt: "2026-10-02",
		HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 4},
	}); err != nil {
		t.Fatal(err)
	}
	clearChange(stateDir, &workflow.ServiceMeta{Hostname: "appdev", HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", Change: 3}}, 3)
	if fresh, _ := workflow.FindServiceMeta(stateDir, "appdev"); fresh.HQ.Change != 4 {
		t.Errorf("a stale read wrote over the newer change: %+v", fresh.HQ)
	}
}

// A wired pair hands release to the person even when its checkout is ahead
// of main. Non-parallel: stubs the package-level push-proof reader.
func TestHandleRelease_AWiredPairHandsOff(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()

	var askedRef string
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, ref string) (LaunchPushProofResult, error) {
		askedRef = ref
		return LaunchPushProofResult{LocalHead: "branchhead", RemoteHead: "mainhead"}, nil
	}
	t.Cleanup(func() { launchPushProofReader = prev })

	ssh := &containerSSHStub{}
	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "appdev", ReleaseVersion: "v1.0.0"}, lab.stateDir, runtime.Info{InContainer: true})
	if askedRef != "" || len(ssh.commands) != 0 {
		t.Errorf("HQ release ran legacy preflight: ref=%q SSH=%v", askedRef, ssh.commands)
	}
	if result == nil || result.IsError || !strings.Contains(extractText(result), "release-person-required") {
		t.Fatalf("expected person handoff, got %+v", result)
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

// A pass learns a merge without changing even a clean checkout: taking main
// in is the agent's git.
func TestHQPass_LearnsAMergeWithoutChangingTheCheckout(t *testing.T) {
	for _, tt := range []struct {
		name  string
		upset func(*hqLab)
	}{
		{name: "clean checkout", upset: func(*hqLab) {}},
		{name: "work not committed", upset: func(l *hqLab) { l.write(map[string]string{"index.js": "an edit\n"}) }},
		{name: "another branch checked out", upset: func(l *hqLab) { l.git("checkout", "-q", "-b", "elsewhere") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			recordChange(lab.stateDir, lab.meta(), lab.openChange().Number, "")
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
				Hostname: tt.hostname, Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: tt.hostname + "stage"}},
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

// TestReconcileHQRepositories_RenewsTheDevServicesCredentialAfterTheMateEnrollsAgain:
// a redeployed zcp enrolls the Mate again and HQ revokes the credential the
// dev service holds. The next pass gives the dev service the new one at once,
// backoff or not, so git in its sessions keeps reaching HQ — and leaves the
// checkout's branch and origin as the agent left them.
func TestReconcileHQRepositories_RenewsTheDevServicesCredentialAfterTheMateEnrollsAgain(t *testing.T) {
	const renewed = "the-renewed-credential"
	lab := newHQLab(t)
	lab.wire()
	lab.git("checkout", "-q", "-b", "fix-login")
	origin := lab.git("remote", "get-url", "origin")
	lab.reenroll(renewed)
	if _, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch -q origin"); err == nil {
		t.Fatal("the revoked credential still reaches HQ; the lab does not revoke it")
	}

	lab.wire()

	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != renewed {
		t.Errorf("the dev service holds %q, want the renewed credential", held)
	}
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch -q origin"); err != nil {
		t.Errorf("git in a dev service session cannot reach HQ: %v\n%s", err, out)
	}
	if got := lab.git("branch", "--show-current"); got != "fix-login" {
		t.Errorf("the checkout is on %q, want the agent's branch kept", got)
	}
	if got := lab.git("remote", "get-url", "origin"); got != origin {
		t.Errorf("origin = %q, want %q kept", got, origin)
	}
	assertNoSecretOnDisk(t, lab.stateDir, renewed)
}

// TestDevelopStart_RenewsTheDevServicesCredential: work starts at develop,
// so its start runs the repository pass that renews a revoked credential.
func TestDevelopStart_RenewsTheDevServicesCredential(t *testing.T) {
	const renewed = "the-renewed-credential"
	lab := newHQLab(t)
	lab.wire()
	lab.reenroll(renewed)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, lab.mock, lab.hq.srv.Client(), labMate, nil,
		workflow.NewEngine(lab.stateDir, workflow.EnvContainer, nil), nil, lab.stateDir, "", nil, lab.ssh, lab.rt, "")

	callTool(t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "develop", "intent": "fix the login", "scope": []string{"appdev"},
	})

	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != renewed {
		t.Errorf("after develop's start the dev service holds %q, want the renewed credential", held)
	}
}

// developStart starts develop on appdev through zerops_workflow, as the agent
// does, and answers every text block of its result.
func (l *hqLab) developStart() string {
	l.t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, l.mock, l.hq.srv.Client(), labMate, nil,
		workflow.NewEngine(l.stateDir, workflow.EnvContainer, nil), nil, l.stateDir, "", nil, l.ssh, l.rt, "")
	result := callTool(l.t, srv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "develop", "intent": "fix the login", "scope": []string{"appdev"},
	})
	var text strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text.WriteString(block.Text + "\n")
		}
	}
	return text.String()
}

// TestDevelopStart_AMainThatIsOnlyZcpsSeedTakesHQsMain: a checkout wired
// before branches of the agent's own kept zcp's empty seed as `main`, the
// work on the Mate's branch, so `git merge --ff-only origin/main` failed on
// it. Develop's start puts that `main` on HQ's — within the backoff of the
// pass before it too — and the agent's branch and its commit stay as they are.
func TestDevelopStart_AMainThatIsOnlyZcpsSeedTakesHQsMain(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.hq.landOnMain("appdev", map[string]string{"app.js": "landed\n"})
	lab.git("checkout", "-q", "-b", "mate/"+labMate)
	lab.write(map[string]string{"work.txt": "the agent's work\n"})
	lab.commit("the agent's work")
	work := lab.git("rev-parse", "HEAD")
	lab.git("update-ref", "refs/heads/main",
		lab.git("-c", "user.name=zcp", "-c", "user.email=zcp@example.invalid", "commit-tree", lab.git("mktree"), "-m", "zcp init"))

	text := lab.developStart()

	if got, want := lab.git("rev-parse", "main"), lab.remoteHead("main"); got != want {
		t.Errorf("main is %s, want HQ's main %s", got, want)
	}
	if got := lab.git("branch", "--show-current"); got != "mate/"+labMate {
		t.Errorf("the checkout is on %q, want the agent's branch kept", got)
	}
	if got := lab.git("rev-parse", "HEAD"); got != work {
		t.Errorf("the agent's branch moved: %s → %s", work, got)
	}
	if !strings.Contains(text, "zcp's empty seed") {
		t.Errorf("develop's start does not say main was put on HQ's:\n%s", text)
	}
}

// TestDevelopStart_AMainWithCommitsOfItsOwnIsLeftWithItsStepNamed: a `main`
// carrying commits HQ's does not is never moved — main moves only by a
// merge, and nothing of the agent's is discarded — and develop's start names
// the one step that keeps them and puts main on HQ's.
func TestDevelopStart_AMainWithCommitsOfItsOwnIsLeftWithItsStepNamed(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"own.txt": "committed on main\n"})
	lab.commit("on main")
	own := lab.git("rev-parse", "main")

	text := lab.developStart()

	if got := lab.git("rev-parse", "main"); got != own {
		t.Errorf("main moved: %s → %s", own, got)
	}
	for _, step := range []string{"git branch <name> main", "git reset --keep origin/main"} {
		if !strings.Contains(text, step) {
			t.Errorf("develop's start does not name %q:\n%s", step, text)
		}
	}
}

// TestDevelopStart_SaysHowEachRepositorysCodeIsKept: develop's start reads
// from HQ how each wired pair's repository's code is kept and records it, so
// status names it (origin.mode). A repository kept on GitHub is worked on
// through its checkout's origin, which is GitHub — set there, never a second
// remote — and its change is opened after the push: HQ opens its pull request
// and the person merges it in Mate. A repository HQ alone keeps is worked on
// as before.
func TestDevelopStart_SaysHowEachRepositorysCodeIsKept(t *testing.T) {
	const github = "https://github.com/acme/app"
	for _, tc := range []struct {
		name   string
		origin *hq.RepoOrigin
		record workflow.HQOrigin
		want   []string
		never  []string
	}{
		{
			name: "kept on GitHub", origin: &hq.RepoOrigin{URL: github, Mode: hq.OriginOnGitHub},
			record: workflow.HQOrigin{Mode: hq.OriginOnGitHub, URL: github},
			want: []string{
				"origin.mode=github", "origin=" + github, "its checkout's origin is GitHub",
				"git push origin <branch>", `action="open-change"`, "HQ opens its pull request", "merges it in Mate",
			},
			never: []string{"remote add", "set-url origin", "--fill"},
		},
		{
			name: "followed", origin: &hq.RepoOrigin{URL: github, Mode: hq.OriginFollowed},
			record: workflow.HQOrigin{Mode: hq.OriginFollowed, URL: github},
			want:   []string{"origin.mode=follow", "origin=" + github},
			never:  []string{"set-url origin"},
		},
		{
			name:   "in HQ alone",
			record: workflow.HQOrigin{Mode: workflow.HQOriginNone},
			want:   []string{"origin.mode=none"},
			never:  []string{"set-url origin", "origin=https"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.mock.WithServices([]platform.ServiceStack{
				{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			})
			if tc.origin != nil {
				lab.hq.keepOn(*tc.origin)
			}

			text := lab.developStart()

			if got := lab.meta().HQ.Origin; got == nil || *got != tc.record {
				t.Errorf("the pair's record of how its code is kept = %+v, want %+v", got, tc.record)
			}
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Errorf("develop's start lacks %q:\n%s", want, text)
				}
			}
			for _, never := range tc.never {
				if strings.Contains(text, never) {
					t.Errorf("develop's start says %q:\n%s", never, text)
				}
			}
		})
	}
}

// TestRepositoryPass_TheCheckoutsOriginIsWhereItsCodeIsKept: the wiring is
// the one writer of a wired pair's `origin`. Kept on GitHub, origin is the
// GitHub repository, git asking only zcp for it; kept in HQ or followed, it
// is HQ's repository — and it follows the person's pick when it changes.
// There is never a second remote, and HQ is still reached at its own address.
func TestRepositoryPass_TheCheckoutsOriginIsWhereItsCodeIsKept(t *testing.T) {
	const github = "https://github.com/acme/app"
	lab := newHQLab(t)
	lab.wire()
	hqOrigin := lab.git("remote", "get-url", "origin")
	lab.hq.keepOn(hq.RepoOrigin{URL: github, Mode: hq.OriginOnGitHub})
	lab.hq.landOnMain("appdev", map[string]string{"app.js": "landed\n"})
	lab.git("checkout", "-q", "-b", "fix-login")
	lab.git("update-ref", "refs/heads/main",
		lab.git("-c", "user.name=zcp", "-c", "user.email=zcp@example.invalid", "commit-tree", lab.git("mktree"), "-m", "zcp init"))

	lab.developStart()

	if got := lab.git("remote", "get-url", "origin"); got != github {
		t.Errorf("kept on GitHub, origin = %q, want %q", got, github)
	}
	if got := lab.git("config", "--get-all", "credential.https://github.com.helper"); !strings.Contains(got, "!zcp hq git-credential") {
		t.Errorf("git does not ask zcp for GitHub: %q", got)
	}
	if got := lab.git("remote"); got != "origin" {
		t.Errorf("remotes = %q, want origin alone", got)
	}
	if got, want := lab.git("rev-parse", "main"), lab.remoteHead("main"); got != want {
		t.Errorf("HQ was not reached at its own address: main is %s, want HQ's %s", got, want)
	}

	lab.hq.keepInHQ()
	lab.developStart()

	if got := lab.git("remote", "get-url", "origin"); got != hqOrigin {
		t.Errorf("kept in HQ again, origin = %q, want HQ's %q", got, hqOrigin)
	}
	if got := lab.git("branch", "--show-current"); got != "fix-login" {
		t.Errorf("the checkout is on %q, want the agent's branch kept", got)
	}
}

// TestStatus_AWiredPairNotReadYetAsksHQHowItsCodeIsKept: status names how a
// wired pair's code is kept from its record; a pair with none yet — wired
// before zcp recorded it — is asked of HQ once, bounded, and recorded. When
// HQ does not answer, status says the way is unknown.
func TestStatus_AWiredPairNotReadYetAsksHQHowItsCodeIsKept(t *testing.T) {
	const github = "https://github.com/acme/app"
	for _, tc := range []struct {
		name string
		down bool
		want string
	}{
		{name: "HQ answers", want: "origin.mode=github"},
		{name: "HQ does not answer", down: true, want: "origin.mode=unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.mock.WithServices([]platform.ServiceStack{
				{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			})
			if err := workflow.UpsertServiceMeta(lab.stateDir, "appdev", func(meta *workflow.ServiceMeta, _ bool) error {
				meta.HQ.Origin = nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			lab.hq.keepOn(hq.RepoOrigin{URL: github, Mode: hq.OriginOnGitHub})
			lab.hq.setDown(tc.down)
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterWorkflow(srv, lab.mock, lab.hq.srv.Client(), labMate, nil,
				workflow.NewEngine(lab.stateDir, workflow.EnvContainer, nil), nil, lab.stateDir, "", nil, lab.ssh, lab.rt, "")

			text := getTextContent(t, callTool(t, srv, "zerops_workflow", map[string]any{"action": "status"}))

			if !strings.Contains(text, tc.want) {
				t.Errorf("status lacks %q:\n%s", tc.want, text)
			}
			if got := lab.meta().HQ.Origin; !tc.down && (got == nil || got.Mode != hq.OriginOnGitHub) {
				t.Errorf("the pair's record = %+v, want github recorded", got)
			}
		})
	}
}
