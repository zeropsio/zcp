package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type whereBlockedSSH struct {
	started chan struct{}
	version chan struct{}
	once    sync.Once
}

// witness: internal/tools/where.go | env = env.WithParts(nil) → env = env.WithParts(serverStates)
func TestWhere_WGPart_UsesTheStatusPartName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, setup, server, want string
	}{
		{"preview", "preview", "web", "preview"},
		{"app preview", "app-preview", "web", "app-preview"},
		{"no pair", "", "web", "web"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var services []workflow.ServiceSnapshot
			if tc.setup != "" {
				services = []workflow.ServiceSnapshot{{Hostname: "pool", DevIsPool: true, Stages: []topology.StageRef{{Hostname: "stage", SetupName: tc.setup}}}}
			}
			leases := []workflow.LeaseSnapshot{{Alias: "pool-1", Pool: "pool", State: "held", Branch: "feature"}}
			env := workflow.StateEnvelope{Services: services, Leases: &leases}
			servers := map[string]ops.DevServerRead{"pool-1": {Servers: []topology.DevServerState{{Name: tc.server, Status: "running"}}}}
			facts := whereFacts(env, nil, nil, servers, nil, nil, runtime.Info{}, "", time.Time{}, nil, nil)
			if len(facts.Copies) != 1 || facts.Copies[0].Part != tc.want || len(facts.Copies[0].DevServers) != 1 || facts.Copies[0].DevServers[0].Name != tc.server {
				t.Fatalf("copies = %+v, want part %q and server %q", facts.Copies, tc.want, tc.server)
			}
			if tc.setup != "" && (len(facts.Stages) != 1 || facts.Stages[0].Part != tc.want) {
				t.Errorf("stages = %+v, want part %q", facts.Stages, tc.want)
			}
		})
	}
}

// WG-STANDING — Where lists only open changes without a working copy, while their standing leaves existing tool JSON unchanged.
// witness: internal/tools/hq_change_status.go | line.Labels, line.Merged = nil, true → line.Labels, line.Merged, line.Open = nil, true, true
func TestWhere_WGStanding_ListsOnlyOpenChanges(t *testing.T) {
	t.Parallel()
	app := "app-1"
	change := workflow.HQChange{AppID: app, Repo: "app", Number: 7, Branch: "feature", Told: &workflow.HQChangeTold{State: hq.ChangeOpen}}
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	header := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\nchange #7 feature: no working copy"
	for _, tc := range []struct {
		name, state, wire, want string
		open                    bool
	}{
		{"open", hq.ChangeOpen, `{"number":7,"branch":"feature","labels":null,"leases":null,"merged":false}`, header, true},
		{"closed", hq.ChangeClosed, `{"number":7,"branch":"feature","labels":["closed without merging"],"leases":null,"merged":false}`, "", false},
		{"merged", hq.ChangeMerged, `{"number":7,"branch":"feature","labels":null,"leases":null,"merged":true}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := &hq.MateState{AppID: &app, Changes: []hq.MateChange{{Repo: "app", Number: 7, Branch: "feature", State: tc.state}}}
			line, _ := changeLineNow(change, state, at)
			wire, err := json.Marshal(line)
			if err != nil || line.Open != tc.open || string(wire) != tc.wire {
				t.Fatalf("open = %v; wire = %s, err = %v; want %v and %s", line.Open, wire, err, tc.open, tc.wire)
			}
			got := renderWhereAnswer(workflow.StateEnvelope{}, nil, nil, nil, nil, []workflow.HQRepoChanges{{Repo: "app", Changes: []workflow.HQChangeLine{line}}}, runtime.Info{}, "", at, nil, nil)
			if got != tc.want {
				t.Fatalf("where = %q; want %q", got, tc.want)
			}
		})
	}
}

func (s *whereBlockedSSH) ExecSSH(ctx context.Context, _, _ string) ([]byte, error) {
	<-s.version
	s.once.Do(func() { close(s.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (s *whereBlockedSSH) ExecSSHStdin(ctx context.Context, host, cmd string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, cmd)
}

func (s *whereBlockedSSH) ExecSSHBackground(ctx context.Context, host, cmd string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, cmd)
}

// WG-BOUND — A read still unanswered when the bound passes leaves its fact not read and the answer still comes with every fact that was read.
// witness: internal/tools/where.go | return g.render(), nil → return "", ctx.Err()
func TestWhere_WGBound_KeepsIndependentFacts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLeaseRecordFile(t, dir, "devpool", "svc-pool", "c-abc", 3, "feat-x")
	meta := &workflow.ServiceMeta{Hostname: "devpool", Mode: topology.PlanModeStandard,
		BootstrappedAt: "2026-11-25", DevPool: &workflow.DevPoolRef{ServiceID: "svc-pool"},
		Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "app"}}}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	pool := swarmPoolService("svc-pool", "devpool", "ACTIVE")
	stage := platform.ServiceStack{ID: "svc-stage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}
	client := platform.NewMock().WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{pool, stage}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "test", Sensitive: true}})
	orchestrator := retireSwarm{containers: []platform.SwarmContainer{{ID: "c-abc", Number: 3, Hostname: "node-id-3.runtime.devpool.zerops", Status: "ACTIVE", LockState: "reserved"}}}
	ssh := &whereBlockedSSH{started: make(chan struct{}), version: make(chan struct{})}
	deps := ops.LeaseDeps{Client: client, ProjectID: "proj-1", StateDir: dir, SSH: ssh,
		Swarm: func(string, string) ops.SwarmAPI { return orchestrator }}
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type answer struct {
			text string
			err  error
		}
		result := make(chan answer, 1)
		go func() {
			text, err := WhereAnswer(ctx, deps, runtime.Info{}, "devpool-3", at, func(context.Context, string, string) (string, error) {
				close(ssh.version)
				return "main 7e2d4c1", nil
			})
			result <- answer{text, err}
		}()
		<-ssh.started
		synctest.Wait() // The independently answered stage has reached the coordinator.
		cancel()
		got := <-result
		want := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\n" +
			"devpool-3 (this conversation's): part app, branch feat-x, working copy not read (no answer in 3 s); dev servers not read (no answer in 3 s)\n" +
			"stage appstage (part app) runs main 7e2d4c1"
		if got.err != nil || got.text != want {
			t.Fatalf("answer = %q, error = %v; want %q", got.text, got.err, want)
		}
	})
}

// WG-WIRE — The early envelope snapshot exposes stages before lease reconciliation without changing the envelope tools return.
// witness: internal/workflow/compute_envelope.go | if cfg.preview != nil { → if cfg.preview != nil && cfg.leases == nil {
func TestWhere_WGWire_PreservesExistingEnvelope(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeLeaseRecordFile(t, dir, "devpool", "svc-pool", "c-abc", 3, "feat-x")
	meta := &workflow.ServiceMeta{Hostname: "devpool", Mode: topology.PlanModeStandard,
		BootstrappedAt: "2026-11-25", DevPool: &workflow.DevPoolRef{ServiceID: "svc-pool"},
		Stages: []topology.StageRef{{Hostname: "appstage", SetupName: "app"}}}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	pool := swarmPoolService("svc-pool", "devpool", "ACTIVE")
	stage := platform.ServiceStack{ID: "svc-stage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}
	client := platform.NewMock().WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).WithServices([]platform.ServiceStack{pool, stage})
	previewed, beforeReconcile := false, false
	preview := workflow.WithEnvelopePreview(func(env workflow.StateEnvelope, _ []platform.ServiceStack) {
		previewed = len(env.LeaseList()) == 1 && env.LeaseList()[0].Unverified
		for _, svc := range env.Services {
			if svc.Hostname == "devpool" && len(svc.PairStageHostnames()) != 1 {
				previewed = false
			}
		}
	})
	reader := workflow.WithLeaseReader(func(context.Context) ([]workflow.LeaseSnapshot, error) {
		beforeReconcile = previewed
		return nil, errors.New("orchestrator unavailable")
	})
	env, err := workflow.ComputeEnvelope(context.Background(), client, dir, "proj-1", runtime.Info{},
		time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC), reader, workflow.WithLeasesAlwaysRead(), preview)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(env.WithParts(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"phase":"idle","environment":"local","idleScenario":"bootstrapped","project":{"id":"proj-1","name":"proj"},"services":[{"hostname":"appstage","typeVersion":"nodejs@22","runtimeClass":"dynamic","status":"ACTIVE","bootstrapped":true,"mode":"stage","closeDeployMode":"unset","gitPushState":"unconfigured","buildIntegration":"none","setupName":"app"},{"hostname":"devpool","typeVersion":"swarm@1","runtimeClass":"pool","status":"ACTIVE","bootstrapped":true,"closeDeployMode":"unset","gitPushState":"unconfigured","buildIntegration":"none","stages":[{"hostname":"appstage","setup":"app","part":"app"}]}],"leases":[{"alias":"devpool-3","pool":"devpool","branch":"feat-x","containerHostname":"node-id-3.runtime.devpool.zerops","state":"held","unverified":true,"acquiredAt":"2026-10-23T18:00:00Z","part":"app","stage":"appstage"}],"generated":"2026-11-25T12:00:00Z"}`
	if !beforeReconcile || string(got) != want {
		t.Fatalf("preview before reconciliation = %v; envelope = %s; want %s", beforeReconcile, got, want)
	}
}

// witness: internal/tools/where.go | fresh := answered && answer.err == nil → fresh := answered || answer.err == nil
func TestWhere_WGFresh_LastHeardCannotOpenChange(t *testing.T) {
	t.Parallel()
	facts := workflow.WhereFacts{Copies: []workflow.WhereCopy{{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", HasHQ: true}}}
	branches := map[string]whereEvent{"pool-1": {value: "feature", head: "head"}}
	refs := map[string]whereEvent{"pool-1": {ref: &topology.BranchRef{Head: "head", Published: true}}}
	planWhereEdits(&facts, map[string]string{"pool-1": "app"}, branches, refs, nil, time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	if got := facts.Copies[0].Step; got == nil || got.Action != workflow.EditUnknown || got.Reason != "HQ was not read" || facts.Copies[0].HasHQ {
		t.Errorf("last-heard absence was accepted: %+v", facts.Copies[0])
	}
}

// WG-HEAD — A detached checkout and an unborn branch say which branch or commit is absent instead of saying the checkout did not answer.
// witness: internal/tools/where.go | case branch.head == "": → case false:
func TestWhere_WGHead_NamesAbsentBranchOrCommit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		branch whereEvent
		want   string
	}{
		{name: "detached", branch: whereEvent{head: "head"}, want: "checkout is not on a branch"},
		{name: "unborn", branch: whereEvent{value: "feature"}, want: "checkout has no commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			facts := workflow.WhereFacts{Copies: []workflow.WhereCopy{{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", HasHQ: true}}}
			planWhereEdits(&facts, map[string]string{"pool-1": "app"}, map[string]whereEvent{"pool-1": tc.branch}, nil, nil, time.Time{})
			if got := facts.Copies[0].Step; got == nil || got.Action != workflow.EditUnknown || got.Reason != tc.want {
				t.Errorf("step = %+v, want unknown: %s", got, tc.want)
			}
		})
	}
}

// witness: internal/tools/where.go | repos[other.Name] != repos[workcopy.Name] → repos[other.Name] == repos[workcopy.Name]
func TestWhere_WGGroup_UnreadOtherCopyPreventsHandover(t *testing.T) {
	t.Parallel()
	ready := true
	head := "head"
	facts := workflow.WhereFacts{Copies: []workflow.WhereCopy{
		{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", HasHQ: true},
		{Name: "pool-2", Part: "api", Branch: "feature", Lease: true, State: "held", HasHQ: true},
	}}
	branches := map[string]whereEvent{"pool-1": {value: "feature", head: head}, "pool-2": {err: errors.New("offline")}}
	refs := map[string]whereEvent{"pool-1": {ref: &topology.BranchRef{Head: head, Published: true}}}
	hqReads := map[string]whereEvent{"pool-1": {change: &hq.Change{Number: 7, Head: &head, Ready: &ready}}}
	planWhereEdits(&facts, map[string]string{"pool-1": "app", "pool-2": "app"}, branches, refs, hqReads, time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC))
	if got := facts.Copies[0].Step; got == nil || got.Action != "read" || got.Copy != "pool-2" {
		t.Errorf("readable copy falsely handed over: %+v", got)
	}
	if got := facts.Copies[1].Step; got == nil || got.Action != workflow.EditUnknown {
		t.Errorf("unread copy = %+v, want unknown", got)
	}
}

// witness: internal/tools/hq_change_description.go | ReviewBuild: current.ReviewBuild → ReviewBuild: nil
func TestWhere_WGReviewStep_FreshFailureReachesPlanner(t *testing.T) {
	t.Parallel()
	ready, head := true, "head"
	facts := workflow.WhereFacts{Copies: []workflow.WhereCopy{{Name: "appdev", Branch: "feature", HasHQ: true}}}
	branches := map[string]whereEvent{"appdev": {value: "feature", head: head}}
	refs := map[string]whereEvent{"appdev": {ref: &topology.BranchRef{Head: head, Published: true}}}
	reads := map[string]whereEvent{"appdev": {change: &hq.Change{Number: 7, Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "failing", Parts: []hq.ReviewBuildPart{{Setup: "prod", Outcome: "failed"}}}}}}
	planWhereEdits(&facts, map[string]string{"appdev": "app"}, branches, refs, reads, time.Time{})
	step := facts.Copies[0].Step
	if step == nil || step.Words() != `read failure (zerops_check_log repo="app" change=7 setup="prod"; omit setup to list and read other failed parts), fix, commit, push, describe-change in appdev` {
		t.Fatalf("step = %+v; want the failed setup's repair step", step)
	}
}

// witness: internal/tools/where.go | classicDevs[service.HQRepo] = service.Hostname → classicDevs[service.HQRepo] = ""
func TestWhere_WGNoCopy_SelectsTheRepositorysDevHalf(t *testing.T) {
	t.Parallel()
	env := workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{
		{Hostname: "appdev", StageHostname: "appstage", HQRepo: "classic"},
		{Hostname: "webpool", StageHostname: "webstage", HQRepo: "pool", DevIsPool: true},
	}}
	changes := []workflow.HQRepoChanges{
		{Repo: "classic", Changes: []workflow.HQChangeLine{{Number: 7, Branch: "feature", Open: true}}},
		{Repo: "pool", Changes: []workflow.HQChangeLine{{Number: 8, Branch: "other", Open: true}}},
	}
	answer := renderWhereAnswer(env, nil, map[string]whereEvent{"appdev": {value: "main", head: "head"}}, nil, nil, changes, runtime.Info{MateEnabled: true}, "", time.Time{}, nil, nil)
	for _, want := range []string{"change #7 feature: no working copy; step: checkout feature in appdev", "change #8 other: no working copy; step: acquire"} {
		if !strings.Contains(answer, want) {
			t.Errorf("where = %q; missing %q", answer, want)
		}
	}
}

// witness: internal/tools/where.go | workingCopy.WorkUnread != "" && service.HQRepo != "" → workingCopy.WorkUnread == "" && service.HQRepo != ""
func TestWhere_WGNoCopyRead_UnreadClassicBranchIsUnknown(t *testing.T) {
	t.Parallel()
	env := workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "appdev", StageHostname: "appstage", HQRepo: "app"}}}
	changes := []workflow.HQRepoChanges{{Repo: "app", Changes: []workflow.HQChangeLine{{Number: 7, Branch: "feature", Open: true}}}}
	answer := renderWhereAnswer(env, nil, map[string]whereEvent{"appdev": {err: errors.New("unreachable")}}, nil, nil, changes, runtime.Info{MateEnabled: true}, "", time.Time{}, nil, nil)
	want := "change #7 feature: working copy not read (checkout appdev branch was not read); step: unknown: checkout appdev branch was not read"
	if !strings.Contains(answer, want) || strings.Contains(answer, "no working copy; step: checkout") {
		t.Fatalf("where = %q; want %q", answer, want)
	}
}

// witness: internal/tools/hq_change_target.go | changeBranchOf(workcopy.branch) → changeBranchOf(workcopy.name)
func TestOpenChange_WGOpenBranch_UsesClassicCheckout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, branch string
		wantErr      bool
	}{
		{"feature", "feature", false},
		{"main", "main", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeWiredPair(t, dir, "appdev", "app")
			ssh := &stubSSH{output: []byte("checkout-head:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ncheckout-branch:" + tc.branch + "\ncheckout-clean\n")}
			target, workcopy, err := resolveOpenChangeTarget(context.Background(), ssh, dir, targetHQ, changeAsk{Service: "appdev", Verb: changeVerbOpen})
			if tc.wantErr {
				if err == nil {
					t.Fatal("open from main was accepted")
				}
				return
			}
			if err != nil || target.Branch != tc.branch || workcopy == nil || workcopy.branch != tc.branch || workcopy.name != "appdev" {
				t.Fatalf("target = %+v, copy = %+v, error = %v", target, workcopy, err)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | !workcopy.lease && target.Branch != "" && branch != target.Branch → !workcopy.lease && target.Branch != "" && branch == target.Branch
func TestDescription_WGNeedCopy_NamesLeaseOrClassicCheckout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, service, want string
		pair                *workflow.ServiceMeta
	}{
		{"pool without lease", "apppool", `zerops_lease action=acquire branch="feature" pool="apppool"`, &workflow.ServiceMeta{Hostname: "apppool", DevPool: &workflow.DevPoolRef{}}},
		{"classic on another branch", "appdev", "checkout feature in appdev", &workflow.ServiceMeta{Hostname: "appdev"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ssh := &stubSSH{output: []byte("checkout-head:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\ncheckout-branch:other\ncheckout-clean\n")}
			_, err := readDescriptionCopy(context.Background(), ssh, dir, targetHQ, changeTarget{AppID: "app", Repo: "app", Branch: "feature", Pair: tc.pair}, tc.service)
			var refusal *platform.PlatformError
			if !errors.As(err, &refusal) || refusal.Code != platform.ErrPrerequisiteMissing || !strings.Contains(refusal.Suggestion, tc.want) {
				t.Fatalf("refusal = %v; want next step %q", err, tc.want)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | step.Head == head → step.Head != head
func TestDescribed_WGDescribedHead_OnlySameCommitHandedOver(t *testing.T) {
	t.Parallel()
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		step workflow.EditStep
		want bool
	}{
		{"same change and head", workflow.EditStep{Action: "handed over", Head: head, Number: 7}, true},
		{"other head", workflow.EditStep{Action: "handed over", Head: other, Number: 7}, false},
		{"other change", workflow.EditStep{Action: "handed over", Head: head, Number: 8}, false},
		{"other copy dirty", workflow.EditStep{Action: "commit", Copy: "pool-2"}, false},
		{"unknown", workflow.EditStep{Action: workflow.EditUnknown, Reason: "HQ was not read"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := describedAtHandedOverHead(7, head, tc.step); got != tc.want {
				t.Fatalf("handed over = %v, want %v", got, tc.want)
			}
			answer := describedHandover(7, head, tc.step, nil, now)
			if tc.step.Copy != "" && !strings.Contains(answer, tc.step.Copy) {
				t.Fatalf("answer omitted the copy holding work: %s", answer)
			}
			t.Log(answer)
		})
	}
}

// witness: internal/tools/hq_change_description.go | current.ReviewBuild.Head == head → current.ReviewBuild.Head != head
func TestDescribed_WGReviewPromise_OnlyObservedHeadAwaits(t *testing.T) {
	t.Parallel()
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	other := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	step := workflow.EditStep{Action: "handed over", Head: head, Number: 7}
	for _, tc := range []struct {
		name    string
		current *hq.Change
		wait    bool
	}{
		{name: "no observation"},
		{name: "other head", current: &hq.Change{ReviewBuild: &hq.ReviewBuild{Head: other, State: "awaiting", Until: "2026-11-27T12:30:00Z"}}},
		{name: "current head", current: &hq.Change{ReviewBuild: &hq.ReviewBuild{Head: head, State: "awaiting", Until: "2026-11-27T12:30:00Z"}}, wait: true},
		{name: "deadline passed", current: &hq.Change{ReviewBuild: &hq.ReviewBuild{Head: head, State: "awaiting", Until: "2026-11-27T11:30:00Z"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			answer := describedHandover(7, head, step, tc.current, now)
			if got := strings.Contains(answer, "awaits its Review build"); got != tc.wait {
				t.Fatalf("awaiting promise = %v, want %v: %s", got, tc.wait, answer)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | c.Description = kept → if !existed { c.Description = kept }
func TestDescription_WGKeptHead_ExplicitDescribeBindsWordsToNewHead(t *testing.T) {
	t.Parallel()
	const words = "The same reviewed description."
	const newHead = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, tc := range []struct {
		name, previousHead string
	}{
		{"previous commit", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"empty previous head", ""},
		{"same commit", newHead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ref := changeRef{AppID: "app", Repo: "repo", Number: 7}
			pick := changePick{Branch: "feature"}
			if err := keepChangeDescription(dir, ref, pick, words, "title", tc.previousHead); err != nil {
				t.Fatal(err)
			}
			before, found, err := workflow.ReadHQChange(dir, ref.AppID, ref.Repo, ref.Number)
			if err != nil || !found || before.Description == nil || before.Description.Text != words || before.Description.Head != tc.previousHead {
				t.Fatalf("before new call: %+v, found %v, err %v", before.Description, found, err)
			}
			if err := keepChangeDescription(dir, ref, pick, words, "title", newHead); err != nil {
				t.Fatal(err)
			}
			after, found, err := workflow.ReadHQChange(dir, ref.AppID, ref.Repo, ref.Number)
			if err != nil || !found || after.Description == nil || after.Description.Text != words || after.Description.Head != newHead {
				t.Fatalf("after new call: %+v, found %v, err %v; want words at %s", after.Description, found, err, newHead)
			}
			edit := descriptionEdit(words, "title", newHead, false, time.Time{})
			if edit.Body == nil || *edit.Body != words || edit.ExpectedHead != newHead {
				t.Fatalf("description edit = %+v, want same words at %s", edit, newHead)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | readStep().Words() → readStep().Action
func TestDescription_WGMovedStep_RefusalKeepsWordsAndNamesStep(t *testing.T) {
	t.Parallel()
	const head = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name string
		step workflow.EditStep
		want []string
	}{
		{"ahead", workflow.EditStep{Action: "push", Copy: "pool-1"}, []string{"push", "pool-1"}},
		{"behind", workflow.EditStep{Action: "pull", Copy: "pool-1"}, []string{"pull", "pool-1"}},
		{"other copy", workflow.EditStep{Action: "commit", Copy: "pool-2"}, []string{"commit", "pool-2"}},
		{"unread", workflow.EditStep{Action: workflow.EditUnknown, Reason: "HQ was not read"}, []string{"unknown", "HQ was not read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := descriptionWriteFailure("", changeRef{Number: 7}, changeDescriptionResult{Kept: true}, "words", head, "", 1, describeServed,
				&hq.RefusedError{Status: 409, Code: "conflict", Reason: "head_moved"}, nil, func() workflow.EditStep { return tc.step })
			var got changeDescriptionResult
			if err := json.Unmarshal([]byte(resultText(t, result)), &got); err != nil {
				t.Fatal(err)
			}
			if !got.Kept || !strings.Contains(got.Message, head) {
				t.Fatalf("refusal reply = %+v, want words kept at %s", got, head)
			}
			for _, want := range tc.want {
				if !strings.Contains(got.Message, want) {
					t.Fatalf("refusal reply = %+v, want %q", got, want)
				}
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | || c.Description.Head != head →
func TestDescription_WGAckHead_OldAcknowledgementKeepsNewWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, keptHead, ackHead string
		wantKept                bool
	}{
		{"same head", "head-a", "head-a", false},
		{"new head", "head-b", "head-a", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			ref := changeRef{AppID: "app", Repo: "repo", Number: 7}
			if err := keepChangeDescription(dir, ref, changePick{Branch: "feature"}, "words", "title", tc.keptHead); err != nil {
				t.Fatal(err)
			}
			forgetChangeDescription(dir, ref, "words", tc.ackHead)
			change, found, err := workflow.ReadHQChange(dir, "app", "repo", 7)
			if err != nil || !found || (change.Description != nil) != tc.wantKept {
				t.Fatalf("stored description = %+v, found %v, err %v; want kept %v", change.Description, found, err, tc.wantKept)
			}
			if tc.wantKept && change.Description.Head != tc.keptHead {
				t.Fatalf("stored head = %q, want %q", change.Description.Head, tc.keptHead)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | len(candidates) > 1 → len(candidates) > 2
func TestDescription_WGDescribeCopy_OnlyOneAddressedCheckout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		candidates []descriptionCopy
		want       string
		wantError  bool
	}{
		{name: "none"},
		{name: "one", candidates: []descriptionCopy{{name: "pool-1"}}, want: "pool-1"},
		{name: "several", candidates: []descriptionCopy{{name: "pool-1"}, {name: "pool-2"}}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := uniqueDescriptionCopy(tc.candidates)
			if got.name != tc.want || (err != nil) != tc.wantError {
				t.Fatalf("copy = %q, error = %v; want %q, error %v", got.name, err, tc.want, tc.wantError)
			}
		})
	}
}

// witness: internal/tools/hq_change_open.go | Landed string `json:"landed,omitempty"` → Landed string `json:"-"`
func TestOpenChange_WGOpenReply_KeepsOpenAndDescription(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, description string
		created, failed   bool
	}{
		{"created and described", `{"described":true,"message":"handed over"}`, true, false},
		{"existing and kept", `{"described":false,"kept":true}`, false, false},
		{"description refused", `{"error":"description refused"}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opened := changeOpenResult{Created: tc.created, Landed: "Change #2 merged.", Message: "HQ's open answer was lost."}
			described := textResult(tc.description)
			described.IsError = tc.failed
			answer := openDescriptionResult(opened, described)
			var got struct {
				Created     bool            `json:"created"`
				Landed      string          `json:"landed"`
				Message     string          `json:"message"`
				Description json.RawMessage `json:"description"`
			}
			if err := json.Unmarshal([]byte(resultText(t, answer)), &got); err != nil {
				t.Fatal(err)
			}
			if got.Created != tc.created || got.Landed != opened.Landed || got.Message != opened.Message || string(got.Description) != tc.description || answer.IsError != tc.failed {
				t.Fatalf("open and description = %+v, isError %v; want open %+v, description %s, isError %v", got, answer.IsError, opened, tc.description, tc.failed)
			}
		})
	}
}

// witness: internal/tools/hq_change_open.go | if describing { → if !describing {
func TestOpenChange_WGOpenStep_AsksOnlyForMissingDescription(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                      string
		created, describing, want bool
	}{
		{"new with description", true, true, false},
		{"new without description", true, false, true},
		{"existing with description", false, true, false},
		{"existing without description", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opened := hq.OpenedChange{Created: tc.created, Change: hq.Change{Number: 7, Branch: "feature"}}
			words := openChangeWords(hq.Client{}, changeTarget{Repo: "app"}, "feature", "", repoRoute{}, nil, opened, changeOpenResult{Branch: "feature"}, "", hq.MateState{}, tc.describing)
			if got := strings.Contains(words, `action="describe-change"`); got != tc.want {
				t.Fatalf("open reply asks for a description = %v; want %v: %s", got, tc.want, words)
			}
		})
	}
}
