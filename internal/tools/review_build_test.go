package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
	"gopkg.in/yaml.v3"
)

func reviewBuildStage(ctx context.Context, d devGitDeps, source ops.DeploySource, stage topology.StageRef, log func(string)) hq.ReviewBuildPart {
	return reviewBuildStageWithLock(ctx, d, source, "app/code", "abcdef", stage, log, false)
}

// witness: internal/tools/review_build.go | c.State == hq.ChangeAdding → c.State != hq.ChangeAdding
func TestReviewNext_RBAdding_OnlyAdding_Takes(t *testing.T) {
	t.Parallel()
	head := "abcdef"
	for _, tc := range []struct {
		name, state, repo string
		running           bool
		want              reviewAction
	}{
		{"adding", hq.ChangeAdding, "app", false, reviewTake},
		{"described", hq.ChangeOpen, "app", false, reviewNothing},
		{"merged", hq.ChangeMerged, "app", false, reviewNothing},
		{"already running", hq.ChangeAdding, "app", true, reviewNothing},
		{"recipe", hq.ChangeAdding, hq.RecipeRepo, false, reviewNothing},
		{"merging recovery", hq.ChangeMerging, "app", false, reviewRun},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := reviewNext(hq.MateChange{Repo: tc.repo, State: tc.state, Head: &head}, tc.running, time.Time{})
			if got != tc.want {
				t.Fatalf("action = %v, want %v", got, tc.want)
			}
		})
	}
}

func reviewTestProcess(name, status string) platform.Process {
	return platform.Process{ID: "process", ActionName: "stack.build", Status: status,
		ServiceStacks: []platform.ServiceStackRef{{ID: "stage"}}, AppVersion: &platform.ProcessAppVersion{ID: "version", Name: name}}
}

// witness: internal/tools/review_build.go | if ops.IsProcessLive(p.Status) && p.ID != "" { → if !ops.IsProcessLive(p.Status) && p.ID != "" {
func TestReviewVersion_RBFlight_Restart_JoinsHead(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"PENDING", "RUNNING"} {
		got := reviewVersionOf("abcdef", "stage", "", nil, []platform.Process{reviewTestProcess("zcp abcdef", status)})
		if got.action != reviewJoin || got.id != "version" || got.process != "process" {
			t.Fatalf("%s: got %+v", status, got)
		}
	}
}

// witness: internal/tools/review_build.go | v.ID == active → v.ID != active
func TestReviewVersion_RBActive_ActiveHead_Probes(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"FINISHED", "FAILED"} {
		got := reviewVersionOf("abcdef", "stage", "version", []platform.AppVersionEvent{{ID: "version", Name: "zcp abcdef", Status: "ACTIVE"}}, []platform.Process{reviewTestProcess("zcp abcdef", status)})
		if got.action != reviewProbe || got.id != "version" {
			t.Fatalf("%s: got %+v", status, got)
		}
	}
}

// witness: internal/tools/review_build.go | if failed { → if !failed {
func TestReviewVersion_RBFailed_EndedHead_ReportsFailure(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"BUILD_FAILED", "DEPLOY_FAILED", "PREPARING_RUNTIME_FAILED", "BUILDING"} {
		got := reviewVersionOf("abcdef", "stage", "", []platform.AppVersionEvent{{ID: "version", Name: "zcp abcdef", Status: status}}, []platform.Process{reviewTestProcess("zcp abcdef", "FAILED")})
		if got.action != reviewFailed || got.id != "version" || got.status != status {
			t.Fatalf("%s: got %+v", status, got)
		}
	}
}

// witness: internal/tools/review_build.go | return reviewVersion{action: reviewBuild} → return reviewVersion{action: reviewProbe}
func TestReviewVersion_RBNeeded_InactiveVersion_BuildsAgain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		versions []platform.AppVersionEvent
		want     reviewVersionAction
	}{
		{nil, reviewBuild},
		{[]platform.AppVersionEvent{{ID: "other", Name: "zcp another", Status: "BACKUP"}}, reviewBuild},
		{[]platform.AppVersionEvent{{ID: "version", Name: "zcp abcdef", Status: "BACKUP"}}, reviewBuild},
	} {
		got := reviewVersionOf("abcdef", "stage", "", tc.versions, nil)
		if got.action != tc.want {
			t.Fatalf("versions %+v: got %+v, want %v", tc.versions, got, tc.want)
		}
	}
	other := reviewVersionOf("abcdef", "stage", "", nil, []platform.Process{reviewTestProcess("zcp another", "FINISHED")})
	if other.action != reviewBuild {
		t.Fatalf("another head's build: got %+v", other)
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "stage", Name: "apistage", ProjectID: "project"}}).
		WithProcess(&platform.Process{ID: "build-av-stage-zcp abcdef", Status: "FINISHED"})
	d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
	part := reviewBuildStage(context.Background(), d, reviewTestSource{}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
	if part.AppVersionID != "av-stage-zcp abcdef" || part.Outcome != "unavailable" || !strings.Contains(part.Reason, "was not returned") {
		t.Fatalf("absent version: %+v", part)
	}
}

// witness: internal/tools/review_build.go | v.Name != ops.AppVersionName(head) || v.Status == platform.AppVersionStatusUploading → v.Name != ops.AppVersionName(head)
func TestReviewStage_RBCut_InterruptedUpload_CleansBeforeBuild(t *testing.T) {
	t.Parallel()
	cut := platform.AppVersionEvent{ID: "cut", ProjectID: "project", ServiceStackID: "stage", Name: "zcp abcdef", Status: "UPLOADING"}
	for _, tc := range []struct {
		name, reason, version string
		deleteErr             error
	}{
		{"cleanup refused", "delete refused", "", errors.New("delete refused")},
		{"cleanup succeeds", "was not returned", "av-stage-zcp abcdef", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().
				WithServices([]platform.ServiceStack{{ID: "stage", Name: "apistage", ProjectID: "project"}}).
				WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: cut.ID, Status: cut.Status}}).
				WithAppVersionEvents([]platform.AppVersionEvent{cut}).
				WithProcess(&platform.Process{ID: "build-av-stage-zcp abcdef", Status: "FINISHED"})
			if tc.deleteErr != nil {
				client.WithError("DeleteAppVersion", tc.deleteErr)
			}
			d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
			part := reviewBuildStage(context.Background(), d, reviewTestSource{}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
			if part.Outcome != "unavailable" || part.AppVersionID != tc.version || !strings.Contains(part.Reason, tc.reason) {
				t.Fatalf("cut upload: %+v", part)
			}
		})
	}
	for _, tc := range []struct {
		status string
		want   reviewVersionAction
	}{
		{"BUILDING", reviewUnknown},
		{"DEPLOYING", reviewUnknown},
	} {
		versions := []platform.AppVersionEvent{cut, {ID: "built", Name: "zcp abcdef", Status: tc.status}}
		if got := reviewVersionOf("abcdef", "stage", "", versions, nil); got.action != tc.want || got.id != "built" {
			t.Fatalf("cut beside %s: got %+v, want %v for built", tc.status, got, tc.want)
		}
	}
}

// witness: internal/tools/review_build.go | meta.HQ.Repo != repo → meta.HQ.Repo == repo
func TestReviewStages_RBStages_Repository_CoversParts(t *testing.T) {
	t.Parallel()
	metas := []*workflow.ServiceMeta{
		{HQ: &workflow.HQRepoRef{AppID: "app", Repo: "code"}, Stages: []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}},
		{HQ: &workflow.HQRepoRef{AppID: "app", Repo: "other"}, Stages: []topology.StageRef{{Hostname: "otherstage"}}},
	}
	for _, tc := range []struct {
		repo string
		want []topology.StageRef
	}{
		{"code", []topology.StageRef{{Hostname: "webstage", SetupName: "web"}, {Hostname: "apistage", SetupName: "api"}}},
		{"none", nil},
	} {
		if got := reviewStages(metas, "app", tc.repo); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: got %+v, want %+v", tc.repo, got, tc.want)
		}
	}
}

// witness: internal/tools/review_build.go | if pair.RepoName != repo { → if pair.RepoName == repo {
func TestReviewSetups_RBSetup_Tier_ResolvesOnlyTheRepository(t *testing.T) {
	t.Parallel()
	tier := workflow.MateTier{Pairs: []workflow.MateTierPair{
		{RepoName: "other", Stages: []workflow.MateTierRuntime{{Hostname: "apistage", Setup: "wrong"}}},
		{RepoName: "app", Stages: []workflow.MateTierRuntime{{Hostname: "apistage", Setup: "api"}}},
	}}
	for _, tc := range []struct {
		repo, host, want string
		wantErr          bool
	}{
		{"app", "apistage", "api", false},
		{"app", "missing", "", true},
		{"none", "apistage", "", true},
	} {
		stages := []topology.StageRef{{Hostname: tc.host}}
		err := reviewStageSetups(stages, tier, tc.repo)
		if (err != nil) != tc.wantErr || stages[0].SetupName != tc.want {
			t.Fatalf("%s/%s: got %+v, %v", tc.repo, tc.host, stages, err)
		}
	}
}

// witness: internal/tools/review_build.go | return 5 * time.Minute → return 6 * time.Minute
func TestReviewRetry_RBRetry_Unavailable_IsBounded(t *testing.T) {
	t.Parallel()
	for i, want := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 0, 0} {
		if got := reviewRetryDelay(i); got != want {
			t.Fatalf("retry %d = %v, want %v", i, got, want)
		}
	}
}

// witness: internal/tools/review_build_watch.go | a.head != *change.Head || a.main != main → a.head != *change.Head || a.main == main
func TestReviewAttempt_RBBase_MainMoves_StartsNewAttempt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		head := "feature"
		key := reviewKey{repo: "app", number: 7}
		for _, tc := range []struct {
			main    string
			restart bool
		}{{"base-one", false}, {"base-two", true}} {
			old := &reviewAttempt{head: head, main: "base-one", completed: true}
			o := &reviewObserver{attempts: map[reviewKey]*reviewAttempt{key: old}, deps: func(context.Context) (devGitDeps, bool) { return devGitDeps{}, false }}
			o.consider(context.Background(), key, hq.MateChange{Repo: "app", State: hq.ChangeAdding, Head: &head}, "app-id", tc.main)
			if got := o.attempts[key]; (got != old) != tc.restart || got.main != tc.main {
				t.Fatalf("main %s: attempt %+v", tc.main, got)
			}
		}
	})
}

// witness: internal/tools/review_build_run.go | part.AppVersionID = found.id → part.AppVersionID = ""
func TestReviewStage_RBCurrent_ActiveVersion_ProbesWithoutRebuilding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, status, stage string
		public, probe       bool
	}{
		{"private active", "ACTIVE", "apistage", false, false},
		{"public active", "ACTIVE", "apistage-example.test", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{ID: "stage", Name: "apistage", SubdomainAccess: tc.public, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
			if tc.status == "ACTIVE" {
				svc.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "version"}
			}
			client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
				WithProject(&platform.Project{ID: "project", SubdomainHost: "example.test"}).
				WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "other", Status: "BUILDING"}, {ID: "version", Status: tc.status}}).
				WithAppVersionEvents([]platform.AppVersionEvent{{ID: "version", ServiceStackID: "stage", Name: "zcp abcdef"}}).
				WithError("CreateAppVersion", errors.New("must not build"))
			d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"},
				httpClient: checkLogDoer(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
				})}
			part := reviewBuildStage(context.Background(), d, reviewTestSource{yaml: "zerops:\n  - setup: api\n    run:\n      base: static\n"}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
			if part.Outcome != "ok" || part.AppVersionID != "version" || part.Stage != tc.stage || (part.Probe != nil) != tc.probe {
				t.Fatalf("%s: result %+v", tc.name, part)
			}
		})
	}
}

// witness: internal/tools/review_build_run.go | return nil, reviewVersion{}, fmt.Errorf("stage version %s is not yet in the name search", versions[i].ID) → continue
func TestReviewStage_RBName_SearchLag_DoesNotBuildAgain(t *testing.T) {
	t.Parallel()
	svc := platform.ServiceStack{ID: "stage", Name: "apistage"}
	client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
		WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "BACKUP"}})
	d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
	part := reviewBuildStage(context.Background(), d, reviewTestSource{err: errors.New("a duplicate build was started")}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
	if part.Outcome != "unavailable" || !strings.Contains(part.Reason, "version is not yet in the name search") {
		t.Fatalf("missing name: %+v", part)
	}
}

// witness: internal/ops/stage_lock.go | release, err := lockKey(ctx, path, "stage", onWait) → release, err := lockKey(ctx, path, "stage", nil)
func TestReviewStage_RBWait_HeldStage_WaitsForItsDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		release, err := ops.LockStage(context.Background(), dir, "stage")
		if err != nil {
			t.Fatal(err)
		}
		svc := platform.ServiceStack{ID: "stage", Name: "apistage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "version"}}
		client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
			WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "ACTIVE"}}).
			WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "FINISHED")})
		d := devGitDeps{client: client, stateDir: dir, rt: runtime.Info{ProjectID: "project"}}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		waiting := make(chan string, 1)
		result := make(chan hq.ReviewBuildPart, 1)
		go func() {
			result <- reviewBuildStage(ctx, d, nil, topology.StageRef{Hostname: "apistage", SetupName: "api"}, func(line string) { waiting <- line })
		}()
		synctest.Wait()
		select {
		case line := <-waiting:
			if line != "Review build app/code: waiting for the stage apistage" {
				t.Fatalf("waiting message = %q", line)
			}
		default:
			t.Fatal("no waiting message")
		}
		time.Sleep(6 * time.Minute)
		synctest.Wait()
		select {
		case part := <-result:
			t.Fatalf("returned while stage held: %+v", part)
		default:
		}
		release()
		part := <-result
		if part.Outcome != "ok" || part.AppVersionID != "version" {
			t.Fatalf("released stage: %+v", part)
		}
	})
}

// witness: internal/tools/review_build_run.go | if found.action == reviewJoin { → if found.action == reviewProbe {
func TestReviewStage_RBResume_HeadInFlight_WaitsForVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		version, outcome, phase string
	}{
		{"ACTIVE", "ok", ""},
		{"BUILD_FAILED", "failed", "build"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{ID: "stage", Name: "apistage"}
			if tc.version == "ACTIVE" {
				svc.ActiveAppVersion = &platform.ActiveAppVersionDigest{ID: "version"}
			}
			client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
				WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: tc.version}}).
				WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "RUNNING")}).
				WithProcess(&platform.Process{ID: "process", Status: "FINISHED"})
			d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
			part := reviewBuildStage(context.Background(), d, nil, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
			if part.Outcome != tc.outcome || part.Phase != tc.phase || part.AppVersionID != "version" {
				t.Fatalf("joined private stage: %+v", part)
			}
		})
	}
}

// witness: internal/tools/review_build_run.go | d.rt.ProjectID, serviceHostname, version.id, 200) → d.rt.ProjectID, part.Stage, version.id, 200)
func TestReviewStage_RBFaillog_PublicFailure_ReportsLastBuildLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, processStatus string
	}{
		{"already failed", "FINISHED"},
		{"joined in flight", "RUNNING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := platform.ServiceStack{ID: "stage", Name: "apistage", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
			client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
				WithProject(&platform.Project{ID: "project", SubdomainHost: "1df2.prg1.zerops.app"}).
				WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "BUILD_FAILED"}}).
				WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", tc.processStatus)}).
				WithProcess(&platform.Process{ID: "process", Status: "FINISHED"}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example"})
			fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{
				{Timestamp: "2026-11-27T12:00:00Z", Facility: "local0", Tag: "zbuilder@version", Message: "compiling"},
				{Timestamp: "2026-11-27T12:00:01Z", Facility: "local0", Tag: "zbuilder@version", Message: "missing module"},
			})
			d := devGitDeps{client: client, logFetcher: fetcher, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
			part := reviewBuildStage(context.Background(), d, nil, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
			if part.Outcome != "failed" || part.Phase != "build" || part.AppVersionID != "version" || part.Stage != "apistage-1df2.prg1.zerops.app" || part.Reason != "build: missing module" {
				t.Fatalf("public stage failure: %+v", part)
			}
		})
	}
}

// witness: internal/tools/review_build_run.go | if line := reviewFailureLine(lines); line != "" { → if line := lines[len(lines)-1]; line != "" {
func TestReviewStage_RBError_ReportsTheDiagnostic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, log, want string
	}{
		{"recorded syntax failure", `/build/source/server.js:40
const = ;
^

SyntaxError: Unexpected token '='
at checkSyntax (node:internal/main/check_syntax:84:5)

Node.js v26.13.0
npm notice
npm notice New major version of npm available! 11.20.0 -> 12.2.3
npm notice Changelog: https://github.com/npm/cli/releases/tag/v12.2.3
npm notice To update run: npm install -g npm@12.2.3
npm notice

━━━━  ❌ npm run check  ━━━━

[BUILD ERROR] running build commands failed`, "build: SyntaxError: Unexpected token '='"},
		{"last diagnostic", "Error: first\nfatal: second\n[BUILD ERROR] running build commands failed", "build: fatal: second"},
		{"no failed tests", "Error: real cause\nTests: 0 failed, 12 passed\n[BUILD ERROR] running build commands failed", "build: Error: real cause"},
		{"error in filename", "Error: real cause\npage reload src/lib/onError.ts\n[BUILD ERROR] running build commands failed", "build: Error: real cause"},
		{"unmarked diagnostic", "compiling\nmissing module\n[BUILD ERROR] running build commands failed", "build: missing module"},
		{"wrapper only", "[BUILD ERROR] running build commands failed", "build: [BUILD ERROR] running build commands failed"},
		{"bounded", "Error: " + strings.Repeat("x", 400) + "\n[BUILD ERROR] running build commands failed", "build: Error: " + strings.Repeat("x", 286)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc", Name: "stage"}}).
				WithServiceAppVersions("svc", []platform.AppVersionEvent{{ID: "v1"}}).
				WithLogAccess(&platform.LogAccess{URL: "https://logs.example"})
			fetcher := platform.NewMockLogFetcher().WithEntries([]platform.LogEntry{{Facility: "local0", Tag: "zbuilder@v1", Message: tc.log}})
			d := devGitDeps{client: client, logFetcher: fetcher, rt: runtime.Info{ProjectID: "project"}}
			part := reviewFailedPart(context.Background(), d, "stage", hq.ReviewBuildPart{}, reviewVersion{id: "v1", status: "BUILD_FAILED", reason: "FAILED"})
			if part.Reason != tc.want {
				t.Errorf("reason = %q, want %q", part.Reason, tc.want)
			}
		})
	}
}

// witness: internal/tools/review_build_run.go | part.Outcome, part.Reason = reviewUnavailable, reviewReason(step, err.Error()) → part.Outcome, part.Reason = reviewOutcomeFailed, reviewReason(step, err.Error())
func TestReviewUnavailable_RBUnavailable_WaitOrLock_ReportsNoVerdict(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"lock stage", "wait for build"} {
		part := reviewUnavailablePart(hq.ReviewBuildPart{Setup: "api", Stage: "apistage"}, step, errors.New("first line\nlast error"))
		if part.Outcome != "unavailable" || part.Phase != "" || part.Reason != step+": last error" {
			t.Fatalf("%s: got %+v", step, part)
		}
	}
	if n := len([]rune(reviewReason("build", strings.Repeat("x", 400)))); n != 300 {
		t.Fatalf("reason length = %d, want 300", n)
	}
	for _, tc := range []struct {
		step     string
		busyLock bool
	}{{"lock stage", true}, {"wait for build", false}} {
		t.Run(tc.step, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				dir := t.TempDir()
				if tc.busyLock {
					release, err := ops.LockStage(context.Background(), dir, "stage")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(release)
				}
				svc := platform.ServiceStack{ID: "stage", Name: "apistage", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
				client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
					WithProject(&platform.Project{ID: "project", SubdomainHost: "example.test"}).
					WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "BUILDING"}}).
					WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "RUNNING")}).
					WithProcess(&platform.Process{ID: "process", Status: "RUNNING"})
				d := devGitDeps{client: client, stateDir: dir, rt: runtime.Info{ProjectID: "project"}}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				part := reviewBuildStage(ctx, d, nil, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
				if part.Outcome != "unavailable" || part.Phase != "" || part.Stage != "apistage-example.test" || !strings.HasPrefix(part.Reason, tc.step+": ") || tc.busyLock && !strings.Contains(part.Reason, "wait for stage lock") {
					t.Fatalf("%s: got %+v", tc.step, part)
				}
			})
		})
	}
}

// witness: internal/tools/hq_dev_git.go | case _, open := <-stateChanged: → case _, open := <-ctx.Done():
func TestMateWatch_RBDevwatch_Ended_ContinuesReading(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "reopened", true: "reopen failed"}[fail], func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				first, next := make(chan struct{}), make(chan struct{}, 1)
				initial := true
				watch := func(context.Context) (<-chan struct{}, error) {
					if initial {
						initial = false
						return first, nil
					}
					if fail {
						return nil, errors.New("watch refused")
					}
					return next, nil
				}
				var said string
				changed := watchMateState(ctx, watch, func(err error) { said = err.Error() })
				ticks := make(chan time.Time, 1)
				reads := make(chan string, 4)
				state := "first"
				deps := func(context.Context) (devGitDeps, bool) {
					reads <- state
					return devGitDeps{rt: runtime.Info{ProjectID: state}}, false
				}
				go keepMateDevGit(ctx, deps, time.Time{}, devGitRetryClock{ticks, func(bool) {}}, changed, func(string) {})
				synctest.Wait()
				if got := <-reads; got != "first" {
					t.Fatalf("initial state = %q", got)
				}
				state = "after"
				close(first)
				synctest.Wait()
				if fail {
					if said != "watch refused" {
						t.Fatalf("reopen error = %q", said)
					}
					ticks <- time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
					synctest.Wait()
				}
				select {
				case got := <-reads:
					if got != "after" {
						t.Fatalf("read state %q, want after", got)
					}
				default:
					t.Fatal("the observer did not read after the watch ended")
				}
				if !fail {
					state = "next"
					next <- struct{}{}
					synctest.Wait()
					select {
					case got := <-reads:
						if got != "next" {
							t.Fatalf("read state %q, want next", got)
						}
					default:
						t.Fatal("the reopened watch did not deliver the next state")
					}
				}
				cancel()
				synctest.Wait()
			})
		})
	}
}

// witness: internal/tools/review_build_watch.go | case _, open := <-changed: → case _, open := <-o.done:
func TestReviewWatch_RBWatch_Ended_ContinuesReading(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "reopened", true: "reopen failed"}[fail], func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				first, next := make(chan struct{}), make(chan struct{}, 1)
				initial := true
				watch := func(context.Context) (<-chan struct{}, error) {
					if initial {
						initial = false
						return first, nil
					}
					if fail {
						return nil, errors.New("watch refused")
					}
					return next, nil
				}
				var said string
				changed := watchMateState(ctx, watch, func(err error) { said = err.Error() })
				standup := make(chan struct{}, 1)
				head, app, ready := "first", "app-id", true
				key := reviewKey{repo: "app", number: 7}
				o := &reviewObserver{read: func() (hq.MateStateDocument, error) {
					return hq.MateStateDocument{Link: hq.MateLinkLinked, Mate: &hq.MateState{AppID: &app, Repos: []hq.MateRepo{{Name: "app"}}, Changes: []hq.MateChange{{Repo: "app", Number: 7, State: "adding", Head: &head, Ready: &ready}}}}, nil
				}, deps: func(context.Context) (devGitDeps, bool) { return devGitDeps{}, false }, attempts: make(map[reviewKey]*reviewAttempt), done: make(chan reviewDone)}
				go o.run(ctx, changed, standup)
				synctest.Wait()
				if o.attempts[key] == nil || o.attempts[key].head != "first" {
					t.Fatal("initial state was not read")
				}
				head = "after"
				close(first)
				synctest.Wait()
				if fail {
					if said != "watch refused" {
						t.Fatalf("reopen error = %q", said)
					}
					standup <- struct{}{}
					synctest.Wait()
				}
				if o.attempts[key] == nil || o.attempts[key].head != "after" {
					t.Fatal("the observer did not read after the watch ended")
				}
				if !fail {
					head = "next"
					next <- struct{}{}
					synctest.Wait()
					if o.attempts[key] == nil || o.attempts[key].head != "next" {
						t.Fatal("the reopened watch did not deliver the next state")
					}
				}
				cancel()
				synctest.Wait()
			})
		})
	}
}

// witness: internal/tools/review_build_run.go | path, _ := get["path"].(string) → path, _ := get["other"].(string)
func TestReviewHealth_RBHealth_Setup_UsesDeclaredPath(t *testing.T) {
	t.Parallel()
	body := `zerops:
  - setup: base
    deploy:
      readinessCheck:
        httpGet:
          path: /ready
  - setup: api
    extends: base
  - setup: web
    run:
      start: npm start
`
	for _, tc := range []struct{ setup, want string }{{"api", "/ready"}, {"web", ""}} {
		got, err := reviewHealthPath(body, tc.setup)
		if err != nil || got != tc.want {
			t.Fatalf("%s: path %q, error %v; want %q", tc.setup, got, err, tc.want)
		}
	}
}

type reviewTestSource struct {
	yaml string
	err  error
}

func (s reviewTestSource) Open(_ context.Context, repository, commit string) (ops.CommitSource, error) {
	if s.err != nil {
		return ops.CommitSource{}, s.err
	}
	if repository != "app/code" || commit != "abcdef" {
		return ops.CommitSource{}, errors.New("wrong repository or commit")
	}
	if s.yaml == "" {
		s.yaml = "zerops:\n  - setup: api\n    run:\n      base: static\n"
	}
	return ops.CommitSource{ZeropsYaml: s.yaml, Archive: io.NopCloser(strings.NewReader(""))}, nil
}

// witness: internal/tools/review_build_run.go | source.Open(ctx, repository, head) → source.Open(ctx, repository, "")
func TestReviewStage_RBSource_PublicVersion_ProbesBuiltHead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, yaml, wantPath, outcome string
		err                           error
	}{
		{"no check", "# repo config\nzerops:\n  - setup: pool\n    run:\n      base: nodejs@20\n  - setup: prod\n    run:\n      base: static\n", "/", "ok", nil},
		{"inherited check", "zerops:\n  - setup: base\n    run:\n      healthCheck:\n        httpGet:\n          path: /health\n  - setup: prod\n    extends: base\n", "/health", "ok", nil},
		{"unreadable source", "", "", "unavailable", errors.New("source unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				svc := platform.ServiceStack{ID: "stage", Name: "apistage", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}},
					ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "version"}}
				client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
					WithProject(&platform.Project{ID: "project", SubdomainHost: "example.test"}).
					WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "ACTIVE"}}).
					WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "FINISHED")})
				d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"},
					httpClient: checkLogDoer(func(req *http.Request) (*http.Response, error) {
						if req.Method != http.MethodGet || req.URL.String() != "https://apistage-example.test"+tc.wantPath {
							t.Errorf("probe request: %s %s", req.Method, req.URL)
						}
						return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
					})}
				part := reviewBuildStage(context.Background(), d, reviewTestSource{yaml: tc.yaml, err: tc.err}, topology.StageRef{Hostname: "apistage", SetupName: "prod"}, nil)
				if part.Outcome != tc.outcome || part.AppVersionID != "version" {
					t.Fatalf("got %+v, want outcome %s for version", part, tc.outcome)
				}
				if tc.err != nil {
					if part.Probe != nil || !strings.Contains(part.Reason, "source unavailable") {
						t.Fatalf("unreadable source: %+v", part)
					}
					return
				}
				if part.Probe == nil || part.Probe.URL != "https://apistage-example.test"+tc.wantPath || part.Probe.Status == nil || *part.Probe.Status != http.StatusOK || part.Probe.At != "2000-01-01T00:00:00Z" {
					t.Fatalf("probe: %+v", part.Probe)
				}
			})
		})
	}
}

// witness: internal/tools/hq_change_description.go | ExpectedHead: head → ExpectedHead: ""
func TestDescriptionEdit_RBDescribeHead_BindsWordsToCommit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, head, body, title string
	}{
		{"first commit", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "first description", "First"},
		{"different commit", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "second description", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := descriptionEdit(tc.body, tc.title, tc.head)
			if got.ExpectedHead != tc.head || got.Body == nil || *got.Body != tc.body || (tc.title == "" && got.Title != nil) || (tc.title != "" && (got.Title == nil || *got.Title != tc.title)) {
				t.Fatalf("description edit = %+v, want body, title and expected head for %s", got, tc.head)
			}
		})
	}
}

// witness: internal/tools/review_build.go | !repo.Adding && repo.MainHead != nil → repo.Adding && repo.MainHead != nil
// witness: internal/tools/review_build_watch.go | attempt.completed = false → attempt.completed = true
func TestReviewMain_RBMain_NoAdd_FollowsLaggingMain(t *testing.T) {
	t.Parallel()
	main := "main-sha"
	for _, tc := range []struct {
		name            string
		adding, running bool
		head            *string
		want            bool
	}{
		{"idle", false, false, &main, true}, {"adding", true, false, &main, false},
		{"already running", false, true, &main, false}, {"unborn", false, false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reviewMainNext(hq.MateRepo{Adding: tc.adding, MainHead: tc.head}, tc.running); got != tc.want {
				t.Fatalf("follow main = %t, want %t", got, tc.want)
			}
		})
	}
	found := reviewVersionOf(main, "stage", "older", []platform.AppVersionEvent{{ID: "older", Name: ops.AppVersionName("old"), Status: "ACTIVE"}}, nil)
	if found.action != reviewBuild {
		t.Fatalf("lagging stage: %+v", found)
	}
	key := reviewKey{repo: "code"}
	attempt := &reviewAttempt{head: main, main: main, completed: true}
	calls := 0
	observer := reviewObserver{
		attempts: map[reviewKey]*reviewAttempt{key: attempt},
		deps: func(context.Context) (devGitDeps, bool) {
			calls++
			return devGitDeps{}, false
		},
	}
	repo := hq.MateRepo{Name: "code", MainHead: &main, Adding: true}
	observer.considerMain(t.Context(), key, repo, "app")
	if attempt.completed || calls != 0 {
		t.Fatalf("adding: completed=%t, builds=%d", attempt.completed, calls)
	}
	repo.Adding = false
	observer.considerMain(t.Context(), key, repo, "app")
	if calls != 1 {
		t.Fatalf("after declined candidate: main checked %d times, want 1", calls)
	}
}

// witness: internal/tools/review_build_take.go | base + "\x00" + head → head
func TestReviewCandidate_RBCandidate_MainAndHead_IdentifyTheSource(t *testing.T) {
	t.Parallel()
	one, two := "main-one", "main-two"
	a, again, moved := reviewCandidateName(&one, "feature"), reviewCandidateName(&one, "feature"), reviewCandidateName(&two, "feature")
	if a == "feature" || a != again || a == moved {
		t.Fatalf("candidate identities: %q, %q, %q", a, again, moved)
	}
}

// witness: internal/ops/review_readiness.go | if !public { → if public {
func TestReviewSetup_RBWorker_PreservesDeclaredReadiness(t *testing.T) {
	t.Parallel()
	body := "zerops:\n  - setup: worker\n    run:\n      start: worker\n    deploy:\n      temporaryShutdown: true\n      readinessCheck:\n        exec:\n          command: check-worker\n"
	got, path, err := ops.ReviewStageSetup(body, "worker", 0, false)
	if err != nil || path != "" {
		t.Fatalf("worker setup: path %q, error %v", path, err)
	}
	var doc struct {
		Zerops []struct {
			Deploy struct {
				TemporaryShutdown bool           `yaml:"temporaryShutdown"`
				ReadinessCheck    map[string]any `yaml:"readinessCheck"`
			} `yaml:"deploy"`
		} `yaml:"zerops"`
	}
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	check := doc.Zerops[0].Deploy.ReadinessCheck
	exec, _ := check["exec"].(map[string]any)
	if doc.Zerops[0].Deploy.TemporaryShutdown || exec["command"] != "check-worker" || check["httpGet"] != nil {
		t.Fatalf("worker deploy: %+v", doc.Zerops[0].Deploy)
	}
}

// witness: internal/ops/review_readiness.go | check["httpGet"] = map[string]any{"path": path, "port": port} → check["httpGet"] = map[string]any{"path": "/", "port": port}
func TestReviewSetup_RBReadiness_PublicStage_UsesProbePath(t *testing.T) {
	t.Parallel()
	body := "zerops:\n  - setup: base\n    run:\n      healthCheck:\n        httpGet:\n          path: /ready\n  - setup: web\n    extends: base\n    deploy:\n      temporaryShutdown: true\n"
	got, path, err := ops.ReviewStageSetup(body, "web", 3000, true)
	if err != nil || path != "/ready" {
		t.Fatalf("public setup: path %q, error %v", path, err)
	}
	var doc struct {
		Zerops []struct {
			Deploy struct {
				TemporaryShutdown bool `yaml:"temporaryShutdown"`
				ReadinessCheck    struct {
					HTTPGet struct {
						Path string `yaml:"path"`
						Port int    `yaml:"port"`
					} `yaml:"httpGet"`
				} `yaml:"readinessCheck"`
			} `yaml:"deploy"`
		} `yaml:"zerops"`
	}
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	check := doc.Zerops[1].Deploy.ReadinessCheck.HTTPGet
	if doc.Zerops[1].Deploy.TemporaryShutdown || check.Path != path || check.Port != 3000 {
		t.Fatalf("public deploy: %+v", doc.Zerops[1].Deploy)
	}
}

// witness: internal/tools/review_build_run.go | errors.As(deployErr, &setupErr) → false
// witness: internal/tools/review_build_run.go | errors.As(err, &missing) → !errors.As(err, &missing)
func TestReviewStage_RBSetupFail_InvalidSetup_ReportsFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body, reason string
		sourceErr          error
	}{
		{"invalid yaml", "zerops: [", "read stage setup", nil},
		{"missing setup", "zerops:\n  - setup: other\n", "setup api", nil},
		{"missing config", "", "has no zerops.yaml", &ops.MissingDeployConfigError{Tree: "candidate"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "stage", Name: "apistage", ProjectID: "project"}})
			d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
			part := reviewBuildStage(t.Context(), d, reviewTestSource{yaml: tc.body, err: tc.sourceErr}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
			if part.Outcome != "failed" || part.Phase != "build" || !strings.Contains(part.Reason, tc.reason) {
				t.Fatalf("invalid setup result: %+v", part)
			}
		})
	}
}

// witness: internal/tools/review_build_run.go | if errors.As(deployErr, &conflict) { → if !errors.As(deployErr, &conflict) {
func TestReviewStage_RBConflict_MergeConflictError_ReportsFiles(t *testing.T) {
	t.Parallel()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "stage", Name: "apistage", ProjectID: "project"}})
	d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
	part := reviewBuildStage(context.Background(), d, reviewTestSource{err: &ops.MergeConflictError{Paths: []string{"src/order.go", "src/app.go"}}}, topology.StageRef{Hostname: "apistage", SetupName: "api"}, nil)
	if part.Outcome != "failed" || part.Phase != "build" || !strings.Contains(part.Reason, "src/order.go") || !strings.Contains(part.Reason, "src/app.go") {
		t.Fatalf("conflict result: %+v", part)
	}
	var reported []hq.ReviewBuildPart
	doer := checkLogDoer(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/api/mate/changes/code/7/review-build/take-id" {
			t.Errorf("result destination: %s %s", req.Method, req.URL.Path)
		}
		var body struct {
			Parts []hq.ReviewBuildPart `json:"parts"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			return nil, err
		}
		reported = body.Parts
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"take":"take-id","decision":"failed"}`)), Header: make(http.Header)}, nil
	})
	hqc, err := hq.Open(doer, hq.Identity{Origin: "https://hq.example", Key: "test", ProjectID: "project"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := hqc.FinishReviewBuild(context.Background(), "code", 7, "take-id", []hq.ReviewBuildPart{part})
	if err != nil || result.Decision != "failed" || len(reported) != 1 || reported[0].Reason != part.Reason || reported[0].Part != "apistage" {
		t.Fatalf("reported result: %+v, %+v, %v", result, reported, err)
	}
}

// witness: internal/tools/review_build_take.go | !uncertain || refused.Code == reviewChangeNotOpen → !uncertain || refused.Code != reviewChangeNotOpen
func TestReviewResult_RBResultwait_UncertainAnswer_HoldsThroughRefusal(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		doer := checkLogDoer(func(*http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return nil, errors.New("answer lost")
			case 2:
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"code":"not_authorized"}`)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"take":"take-id","decision":"merging"}`)), Header: make(http.Header)}, nil
			}
		})
		hqc, err := hq.Open(doer, hq.Identity{Origin: "https://hq.example", Key: "test", ProjectID: "project"})
		if err != nil {
			t.Fatal(err)
		}
		changed := make(chan struct{}, 1)
		done := make(chan error, 1)
		go func() {
			decision, err := reviewFinishResult(t.Context(), hqc, "code", 7, "take-id", nil, changed)
			if err == nil && decision.Decision != "merging" {
				err = errors.New("wrong decision")
			}
			done <- err
		}()
		synctest.Wait()
		changed <- struct{}{}
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("later refusal released uncertain take: %v", err)
		default:
		}
		changed <- struct{}{}
		if err := <-done; err != nil || calls != 3 {
			t.Fatalf("settled take: calls=%d, error=%v", calls, err)
		}
	})
}

// witness: internal/tools/review_build_take.go | change.State != hq.ChangeMerging && (alreadyMerging → change.State == hq.ChangeMerging && (alreadyMerging
func TestReviewDecision_RBSettle_Merging_HoldsUntilSettled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		changed := make(chan struct{}, 1)
		head := "feature"
		state := &hq.MateState{Rev: 10, Repos: []hq.MateRepo{{Name: "app"}}, Changes: []hq.MateChange{{Repo: "app", Number: 7, State: hq.ChangeAdding, Head: &head}}}
		var readErr error
		read := func() (hq.MateStateDocument, error) {
			return hq.MateStateDocument{Link: hq.MateLinkLinked, Mate: state}, readErr
		}
		done := make(chan error, 1)
		go func() { done <- reviewAwaitDecision(ctx, changed, read, "app", 7, "take-id", 10, false) }()
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("stale frame released lock: %v", err)
		default:
		}
		readErr = errors.New("state temporarily unavailable")
		changed <- struct{}{}
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("unreadable state released lock: %v", err)
		default:
		}
		readErr = nil
		state.Rev, state.Changes[0].State = 11, hq.ChangeMerging
		changed <- struct{}{}
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("merging released lock: %v", err)
		default:
		}
		state.Rev, state.Changes[0].State = 12, hq.ChangeAdding
		state.Changes[0].ReviewBuild = &hq.ReviewBuild{Take: "take-id", State: "passing"}
		changed <- struct{}{}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

// witness: internal/hq/self.go | json:"checksGivenUp,omitempty" → json:"otherHandoff,omitempty"
// witness: internal/tools/hq_change.go | case hq.ChangeOpen, hq.ChangeAdding, hq.ChangeMerging: → case hq.ChangeOpen:
func TestReviewState_RBWire_HandoffAndAdding_DecodeAsOpen(t *testing.T) {
	t.Parallel()
	var state hq.MateState
	body := `{"rev":8,"appId":"app","changes":[{"repo":"code","number":7,"state":"adding","checksGivenUp":{"reason":"tries"}}]}`
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatal(err)
	}
	if state.Changes[0].ChecksGivenUp == nil || state.Changes[0].ChecksGivenUp.Reason != "tries" {
		t.Fatalf("handoff: %+v", state.Changes[0].ChecksGivenUp)
	}
	outcome, _ := outcomeOf(state, workflow.HQChange{Repo: "code", Number: 7})
	if outcome != changeStillOpen {
		t.Fatalf("adding outcome: %v", outcome)
	}
}
