package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// SV-COMMIT-FLIGHT — Only the requested commit's build may be joined.
// witness: internal/ops/commit_deploy.go | p.AppVersion.Name != name → p.AppVersion.Name == name
func TestCommitFlight_SVCommitFlight_RequestedHead_OrdersBuilds(t *testing.T) {
	t.Parallel()
	build := func(id, service, name, status string) platform.Process {
		return platform.Process{ID: id, ActionName: "stack.build", Status: status,
			ServiceStacks: []platform.ServiceStackRef{{ID: service}},
			AppVersion:    &platform.ProcessAppVersion{ID: "v-" + id, Name: name}}
	}
	for _, tc := range []struct {
		name      string
		processes []platform.Process
		settled   map[string]bool
		want      flightDecision
		process   string
	}{
		{"none", nil, nil, flightNone, ""},
		{"same commit", []platform.Process{build("same", "stage", "zcp abc123", "RUNNING")}, nil, flightJoin, "same"},
		{"another commit", []platform.Process{build("old", "stage", "zcp def456", "PENDING")}, nil, flightWait, "old"},
		{"unnamed", []platform.Process{build("other", "stage", "", "RUNNING")}, nil, flightWait, "other"},
		{"missing process id", []platform.Process{build("", "stage", "zcp abc123", "RUNNING")}, nil, flightWait, ""},
		{"version deploy", []platform.Process{{ID: "redeploy", ActionName: "stack.deploy", Status: "RUNNING", ServiceStacks: []platform.ServiceStackRef{{ID: "stage"}}}}, nil, flightWait, "redeploy"},
		{"missing version", []platform.Process{{ID: "other", ActionName: "stack.build", Status: "RUNNING", ServiceStacks: []platform.ServiceStackRef{{ID: "stage"}}}}, nil, flightWait, "other"},
		{"other stage", []platform.Process{build("other", "other-stage", "zcp def456", "RUNNING")}, nil, flightNone, ""},
		{"finished", []platform.Process{build("old", "stage", "zcp def456", "FINISHED")}, nil, flightNone, ""},
		{"cancelling", []platform.Process{build("old", "stage", "zcp def456", "CANCELING")}, nil, flightWait, "old"},
		{"other before join", []platform.Process{build("same", "stage", "zcp abc123", "PENDING"), build("old", "stage", "zcp def456", "RUNNING")}, nil, flightWait, "old"},
		{"settled other", []platform.Process{build("same", "stage", "zcp abc123", "PENDING"), build("old", "stage", "zcp def456", "RUNNING")}, map[string]bool{"old": true}, flightJoin, "same"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, decision := commitFlight(tc.processes, "stage", "zcp abc123", tc.settled)
			if decision != tc.want || got.ProcessID != tc.process {
				t.Fatalf("decision = %v, process = %q; want %v, %q", decision, got.ProcessID, tc.want, tc.process)
			}
		})
	}
}

// witness: internal/ops/stage_lock.go | process.ActionName == actionStackBuild → process.ActionName != actionStackBuild
func TestStageBuilds_SLPending_LiveBuilds_IdentifyTargets(t *testing.T) {
	t.Parallel()
	process := func(id, action, status, service string) platform.Process {
		return platform.Process{ID: id, ActionName: action, Status: status, ServiceStacks: []platform.ServiceStackRef{{ID: service}}}
	}
	for _, tc := range []struct {
		name      string
		processes []platform.Process
		want      []string
		wantErr   bool
	}{
		{"none", nil, nil, false},
		{"live versions", []platform.Process{process("a", "stack.build", "RUNNING", "stage"), process("b", "stack.deploy", "PENDING", "stage"), process("c", "stack.deploy.backup", "RUNNING", "stage")}, []string{"a", "b", "c"}, false},
		{"other service", []platform.Process{process("a", "stack.build", "RUNNING", "other")}, nil, false},
		{"finished", []platform.Process{process("a", "stack.build", "FINISHED", "stage")}, nil, false},
		{"other operation", []platform.Process{process("a", "stack.restart", "RUNNING", "stage")}, nil, false},
		{"missing id", []platform.Process{process("", "stack.build", "RUNNING", "stage")}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := stageVersionProcessIDs(tc.processes, "stage")
			if (err != nil) != tc.wantErr || !slices.Equal(got, tc.want) {
				t.Errorf("ids = %v, error = %v; want %v, error %t", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// SV-COMMIT-NAME — The whole commit names its build.
// witness: internal/ops/commit_deploy.go | return topology.AppVersionPrefix + commit → return topology.AppVersionPrefix + commit[:12]
func TestCommitName_SVCommitName_WholeCommit_DistinguishesHeads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ commit, want string }{
		{"abcdef1234560000000000000000000000000000", "zcp abcdef1234560000000000000000000000000000"},
		{"abcdef1234561111111111111111111111111111", "zcp abcdef1234561111111111111111111111111111"},
		{"abcdef1234560000000000000000000000000000000000000000000000000000", "zcp abcdef1234560000000000000000000000000000000000000000000000000000"},
	} {
		if got := AppVersionName(tc.commit); got != tc.want {
			t.Errorf("AppVersionName(%q) = %q, want %q", tc.commit, got, tc.want)
		}
	}
}

// SL-ORDER — Stage locks share one key only for the same service.
// witness: internal/ops/stage_lock.go | key := sha256.Sum256([]byte(serviceID)) → key := sha256.Sum256([]byte("stage"))
func TestStageLock_SLOrder_Services_SerializeIndependently(t *testing.T) {
	// non-parallel: the parent must hold the lock until both subtests finish.
	dir := t.TempDir()
	release, err := LockStage(context.Background(), dir, "stage-a")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, tc := range []struct {
		service string
		blocked bool
	}{{"stage-a", true}, {"stage-b", false}} {
		t.Run(tc.service, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			unlock, err := LockStage(ctx, dir, tc.service)
			if unlock != nil {
				unlock()
			}
			if tc.blocked && !errors.Is(err, context.Canceled) || !tc.blocked && err != nil {
				t.Fatalf("lock error = %v, blocked = %t", err, tc.blocked)
			}
		})
	}
}

// SL-HEALTH — A 4xx is a failed Review build health check.
// witness: internal/ops/verify_checks.go | if status >= 200 && status < 400 { → if status >= 200 && status < 500 {
func TestStageProbe_SLHealth_HTTPStatus_RequiresSuccess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status int
		want   string
	}{{0, "failed"}, {199, "failed"}, {200, "ok"}, {399, "ok"}, {400, "failed"}, {404, "failed"}, {500, "failed"}} {
		if got := stageProbeVerdict("v1", "v1", "v1", tc.status, false); got != tc.want {
			t.Errorf("status %d: got %q, want %q", tc.status, got, tc.want)
		}
	}
}

// SL-VERSION — A response observed around a version move proves no verdict.
// witness: internal/ops/verify_checks.go | if unreadable || expected == "" || before != expected || after != expected { → if unreadable || expected == "" || before != expected && after != expected {
func TestStageProbe_SLVersion_ActiveVersion_BindsObservation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		expected, before, after string
		unreadable              bool
		want                    string
	}{
		{"stable", "v1", "v1", "v1", false, "ok"},
		{"moved before", "v1", "v2", "v1", false, "unavailable"},
		{"moved after", "v1", "v1", "v2", false, "unavailable"},
		{"unreadable", "v1", "v1", "v1", true, "unavailable"},
		{"no expected version", "", "", "", false, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := stageProbeVerdict(tc.expected, tc.before, tc.after, 200, tc.unreadable); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// SL-PRIVATE — Private stages have no public probe.
// witness: internal/ops/verify_checks.go | return StageProbe{Outcome: "ok"} → return StageProbe{Outcome: "unavailable"}
func TestStageProbe_SLPrivate_PrivatePart_BuildIsEnough(t *testing.T) {
	t.Parallel()
	for _, url := range []string{"", "https://stage.example"} {
		got := ProbeStage(context.Background(), nil, nil, "stage", "v1", url, "/health", false)
		if got.Outcome != "ok" || got.URL != "" || !got.At.IsZero() {
			t.Errorf("private part with address %q: got %+v", url, got)
		}
	}
}
