package tools

import (
	"context"
	"encoding/json"
	"errors"
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
