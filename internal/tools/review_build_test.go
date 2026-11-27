package tools

import (
	"context"
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
)

// witness: internal/tools/review_build.go | c.Ready == nil || !*c.Ready || running → c.Ready == nil || *c.Ready || running
func TestReviewNext_RBTrigger_Change_SelectsWork(t *testing.T) {
	t.Parallel()
	head, ready, draft := "abcdef", true, false
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		change  hq.MateChange
		running bool
		want    reviewAction
	}{
		{"new", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready}, false, reviewTake},
		{"taken", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "awaiting", Until: "2026-11-27T12:30:00Z"}}, false, reviewRun},
		{"expired", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "awaiting", Until: "2026-11-27T11:30:00Z"}}, false, reviewRetry},
		{"unavailable", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "unavailable"}}, false, reviewRetry},
		{"passing held", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "passing"}}, false, reviewNothing},
		{"failing held", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: head, State: "failing"}}, false, reviewNothing},
		{"old head", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: "older", State: "passing"}}, false, reviewTake},
		{"draft", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &draft}, false, reviewNothing},
		{"closed", hq.MateChange{State: "closed", Repo: "app", Head: &head, Ready: &ready}, false, reviewNothing},
		{"recipe", hq.MateChange{State: "open", Repo: hq.RecipeRepo, Head: &head, Ready: &ready}, false, reviewNothing},
		{"running", hq.MateChange{State: "open", Repo: "app", Head: &head, Ready: &ready}, true, reviewNothing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reviewNext(tc.change, tc.running, now); got != tc.want {
				t.Fatalf("action = %v, want %v", got, tc.want)
			}
		})
	}
}

// witness: internal/tools/review_build.go | return action == reviewTake && !resultStored → return action == reviewTake && resultStored
func TestReviewTake_RBTake_Retry_ProtectsStoredResult(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		action       reviewAction
		stored, want bool
	}{{reviewTake, false, true}, {reviewTake, true, false}, {reviewRun, false, false}, {reviewRetry, true, false}} {
		if got := reviewNeedsTake(tc.action, tc.stored); got != tc.want {
			t.Fatalf("action %v stored %t: got %t, want %t", tc.action, tc.stored, got, tc.want)
		}
	}
}

// witness: internal/tools/review_build_run.go | until.Before(deadline) → until.After(deadline)
func TestReviewDeadline_RBDeadline_Resume_PreservesTake(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		build *hq.ReviewBuild
		take  bool
		want  time.Time
	}{
		{nil, true, time.Date(2026, 11, 27, 12, 29, 30, 0, time.UTC)},
		{&hq.ReviewBuild{State: "awaiting", Until: "2026-11-27T12:05:00Z"}, false, time.Date(2026, 11, 27, 12, 4, 30, 0, time.UTC)},
		{&hq.ReviewBuild{State: "awaiting", Until: "2026-11-27T11:59:00Z"}, false, time.Date(2026, 11, 27, 12, 29, 30, 0, time.UTC)},
		{&hq.ReviewBuild{State: "unavailable", Until: "2026-11-27T12:05:00Z"}, false, time.Date(2026, 11, 27, 12, 29, 30, 0, time.UTC)},
	} {
		if got := reviewWorkDeadline(now, tc.build, tc.take); !got.Equal(tc.want) {
			t.Fatalf("build %+v take %t: got %v, want %v", tc.build, tc.take, got, tc.want)
		}
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

// witness: internal/tools/review_build.go | if activeVersion.id != "" { → if activeVersion.id == "" {
func TestReviewVersion_RBActive_ActiveHead_Probes(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"FINISHED", "FAILED"} {
		got := reviewVersionOf("abcdef", "stage", "version", nil, []platform.Process{reviewTestProcess("zcp abcdef", status)})
		if got.action != reviewProbe || got.id != "version" {
			t.Fatalf("%s: got %+v", status, got)
		}
	}
}

// witness: internal/tools/review_build.go | if failed.id != "" { → if failed.id == "" {
func TestReviewVersion_RBFailed_EndedHead_ReportsFailure(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"BUILD_FAILED", "DEPLOY_FAILED", "PREPARING_RUNTIME_FAILED", "BUILDING"} {
		got := reviewVersionOf("abcdef", "stage", "", []platform.AppVersionEvent{{ID: "version", Status: status}}, []platform.Process{reviewTestProcess("zcp abcdef", "FAILED")})
		if got.action != reviewFailed || got.id != "version" || got.status != status {
			t.Fatalf("%s: got %+v", status, got)
		}
	}
}

// witness: internal/tools/review_build.go | return reviewVersion{action: reviewBuild} → return reviewVersion{action: reviewProbe}
func TestReviewVersion_RBAgain_UnknownIdentity_Builds(t *testing.T) {
	t.Parallel()
	for _, processes := range [][]platform.Process{
		nil,
		{reviewTestProcess("", "FINISHED")},
		{reviewTestProcess("zcp another", "FINISHED")},
	} {
		got := reviewVersionOf("abcdef", "stage", "version", []platform.AppVersionEvent{{ID: "version", Status: "ACTIVE"}}, processes)
		if got.action != reviewBuild {
			t.Fatalf("unknown identity: got %+v", got)
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

// witness: internal/tools/review_build_watch.go | a.stopped && reviewUpdated(change.UpdatedAt, a.updatedAt) → a.stopped && false
func TestReviewRetry_RBMoved_Change_RestartsSpentClock(t *testing.T) {
	t.Parallel()
	head, ready := "abcdef", true
	key := reviewKey{repo: "app", number: 7}
	for _, tc := range []struct {
		name, updatedAt string
		restart         bool
	}{
		{"older frame", "2026-11-27T12:00:00Z", false},
		{"own result", "2026-11-27T12:01:00Z", false},
		{"change moved", "2026-11-27T12:02:00Z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				a := &reviewAttempt{head: head, tries: 3, running: true, updatedAt: "2026-11-27T12:00:00Z"}
				o := &reviewObserver{attempts: map[reviewKey]*reviewAttempt{key: a}}
				o.finish(reviewDone{key: key, head: head, updatedAt: "2026-11-27T12:01:00Z", unavailable: true})
				change := hq.MateChange{Repo: "app", Number: 7, State: "open", Head: &head, Ready: &ready,
					UpdatedAt: tc.updatedAt, ReviewBuild: &hq.ReviewBuild{Head: head, State: "unavailable"}}
				o.consider(context.Background(), key, change, "app-id")
				got := o.attempts[key]
				if restarted := got != a; restarted != tc.restart || (restarted && (got.stopped || got.next.IsZero())) {
					t.Fatalf("restarted %t, attempt %+v; want restart %t", restarted, got, tc.restart)
				}
			})
		})
	}
}

// witness: internal/tools/review_build_run.go | Parts: parts}, unavailable && !failed → Parts: parts[:1]}, unavailable && !failed
func TestReviewResult_RBResult_Parts_PreservedWithFailurePrecedence(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		parts []hq.ReviewBuildPart
		retry bool
	}{
		{[]hq.ReviewBuildPart{{Setup: "web", Stage: "webstage", Outcome: "ok"}, {Setup: "api", Stage: "apistage", Outcome: "ok"}}, false},
		{[]hq.ReviewBuildPart{{Setup: "web", Stage: "webstage", Outcome: "unavailable"}, {Setup: "api", Stage: "apistage", Outcome: "ok"}}, true},
		{[]hq.ReviewBuildPart{{Setup: "web", Stage: "webstage", Outcome: "unavailable"}, {Setup: "api", Stage: "apistage", Outcome: "failed"}}, false},
	} {
		got, retry := reviewResult("head", at, tc.parts)
		if got.Head != "head" || got.Step != "result" || got.At != "2026-11-27T12:00:00Z" || !reflect.DeepEqual(got.Parts, tc.parts) || retry != tc.retry {
			t.Fatalf("parts %+v: got %+v retry %t, want retry %t", tc.parts, got, retry, tc.retry)
		}
	}
}

// witness: internal/tools/review_build_run.go | part.AppVersionID = found.id → part.AppVersionID = ""
func TestReviewStage_RBObserve_ActivePrivate_ReturnsVersion(t *testing.T) {
	t.Parallel()
	svc := platform.ServiceStack{ID: "stage", Name: "apistage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "version"}}
	client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
		WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "ACTIVE"}}).
		WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "FINISHED")})
	d := devGitDeps{client: client, stateDir: t.TempDir(), rt: runtime.Info{ProjectID: "project"}}
	part := reviewBuildStage(context.Background(), d, nil, "app/code", "abcdef", topology.StageRef{Hostname: "apistage", SetupName: "api"})
	if part.Outcome != "ok" || part.Setup != "api" || part.Stage != "apistage" || part.AppVersionID != "version" {
		t.Fatalf("active private stage: %+v", part)
	}
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
			part := reviewBuildStage(context.Background(), d, nil, "app/code", "abcdef", topology.StageRef{Hostname: "apistage", SetupName: "api"})
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
			part := reviewBuildStage(context.Background(), d, nil, "app/code", "abcdef", topology.StageRef{Hostname: "apistage", SetupName: "api"})
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
				svc := platform.ServiceStack{ID: "stage", Name: "apistage"}
				client := platform.NewMock().WithServices([]platform.ServiceStack{svc}).
					WithServiceAppVersions("stage", []platform.AppVersionEvent{{ID: "version", Status: "BUILDING"}}).
					WithProjectProcesses([]platform.Process{reviewTestProcess("zcp abcdef", "RUNNING")}).
					WithProcess(&platform.Process{ID: "process", Status: "RUNNING"})
				d := devGitDeps{client: client, stateDir: dir, rt: runtime.Info{ProjectID: "project"}}
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
				defer cancel()
				part := reviewBuildStage(ctx, d, nil, "app/code", "abcdef", topology.StageRef{Hostname: "apistage", SetupName: "api"})
				if part.Outcome != "unavailable" || part.Phase != "" || !strings.HasPrefix(part.Reason, tc.step+": ") {
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
					return hq.MateStateDocument{Link: hq.MateLinkLinked, Mate: &hq.MateState{AppID: &app, Changes: []hq.MateChange{{Repo: "app", Number: 7, State: "open", Head: &head, Ready: &ready}}}}, nil
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
				part := reviewBuildStage(context.Background(), d, reviewTestSource{yaml: tc.yaml, err: tc.err}, "app/code", "abcdef", topology.StageRef{Hostname: "apistage", SetupName: "prod"})
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
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, head, body, title string
	}{
		{"first commit", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "first description", "First"},
		{"different commit", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "second description", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := descriptionEdit(tc.body, tc.title, tc.head, false, at)
			if got.ExpectedHead != tc.head || got.Body == nil || *got.Body != tc.body || (tc.title == "" && got.Title != nil) || (tc.title != "" && (got.Title == nil || *got.Title != tc.title)) {
				t.Fatalf("description edit = %+v, want body, title and expected head for %s", got, tc.head)
			}
		})
	}
}

// witness: internal/tools/hq_change_description.go | if stageBacked { → if !stageBacked {
func TestDescriptionEdit_RBDescribeTake_StageControlsTake(t *testing.T) {
	t.Parallel()
	head := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	at := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		stage bool
		want  *hq.ReviewBuildWrite
	}{
		{"stage", true, &hq.ReviewBuildWrite{Head: head, Step: "taken", At: "2026-11-27T12:00:00Z", Until: "2026-11-27T12:30:00Z"}},
		{"no stage", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := descriptionEdit("body", "title", head, tc.stage, at)
			if !reflect.DeepEqual(got.ReviewBuild, tc.want) {
				t.Fatalf("take = %+v, want %+v", got.ReviewBuild, tc.want)
			}
		})
	}
}
