package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// CommitDeployRequest names one commit deploy: the service it builds into,
// the setup in the commit's zerops.yaml it builds by, and the commit HQ holds.
type CommitDeployRequest struct {
	ServiceID  string
	Hostname   string
	Setup      string
	Repository string
	// Commit is the full commit id; the app version is named by it.
	Commit string
}

// CommitDeployResult is a submitted commit deploy: the app version named by
// the commit and the build-and-deploy process behind it. The build itself is
// read from the app version, never waited on here.
type CommitDeployResult struct {
	AppVersionID string
	ProcessID    string
}

// CommitDeploy builds a commit HQ holds into a service with no container's
// checkout: it creates an app version named by the commit, streams the
// commit's deploy source from source into it and starts build-and-deploy.
// Every build into a pool or a stage goes through it.
//
// It assumes it is the one run for the service (the stand-up runs once per
// project):
//   - a build of this commit already in flight on the service (its
//     build-and-deploy process pending or running, read from the project's
//     direct process list) answers as it is: never a second build (F8);
//   - a version a cut upload left UPLOADING (a zcp restart mid-upload; the
//     platform keeps it so for 14 min and more) is deleted before each try,
//     and any left beside a submitted build after it (F7);
//   - a Zerops answer of class unreachable (5xx, 429, no answer) is tried
//     three times, then the last answer is returned (F11). A
//     build-and-deploy that got no answer is never repeated in the run.
//
// Every error carries its class for platform.ClassOf: a Zerops answer as
// the platform's error, a failure of its own as a deployError.
func CommitDeploy(ctx context.Context, client platform.Client, source DeploySource, req CommitDeployRequest) (CommitDeployResult, error) {
	if req.ServiceID == "" || req.Setup == "" || req.Repository == "" || req.Commit == "" {
		return CommitDeployResult{}, &deployError{class: platform.ClassRefused, step: "commit deploy",
			words: fmt.Sprintf("service %q, setup %q, repository %q and commit %q are all needed", req.ServiceID, req.Setup, req.Repository, req.Commit)}
	}
	name := AppVersionName(req.Commit)
	service, err := zeropsTries(ctx, func() (*platform.ServiceStack, error) {
		return client.GetService(ctx, req.ServiceID)
	})
	if err != nil {
		return CommitDeployResult{}, err
	}
	run := commitRun{client: client, projectID: service.ProjectID, serviceID: req.ServiceID}
	if found, ok, err := run.inFlight(ctx, name); err != nil || ok {
		return found, err
	}
	for try := 0; ; try++ {
		if err := run.removeCutUploads(ctx, ""); err != nil {
			return CommitDeployResult{}, err
		}
		res, final, err := run.submit(ctx, source, req, name)
		if err == nil {
			_ = run.removeCutUploads(ctx, res.AppVersionID) // the next run removes what this one could not
			return res, nil
		}
		if final || platform.ClassOf(err) != platform.ClassUnreachable || try == len(zeropsTryPauses) {
			return res, err
		}
		if !pause(ctx, zeropsTryPauses[try]) {
			return res, err
		}
	}
}

// AppVersionName is the name of the app version a commit deploy creates:
// the commit in it, so a version shows which head it builds (F13) and a
// later run finds it.
func AppVersionName(commit string) string {
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return commitVersionPrefix + commit
}

// commitVersionPrefix starts the name of every version a commit deploy
// creates, and only theirs.
const commitVersionPrefix = "zcp "

// commitRun is one commit deploy's reads and writes on one service.
type commitRun struct {
	client    platform.Client
	projectID string
	serviceID string
}

// liveProcess is a process still pending or running: the only ones whose
// build is in flight. A failed, cancelled or finished one is not, whatever
// its version's status says.
func liveProcess(p platform.Process) bool {
	return p.Status == platform.ProcessStatusPending || p.Status == platform.ProcessStatusRunning
}

// inFlight is the service's live build-and-deploy of the version named
// name, when it has one.
func (r commitRun) inFlight(ctx context.Context, name string) (CommitDeployResult, bool, error) {
	processes, err := zeropsTries(ctx, func() ([]platform.Process, error) {
		return r.client.GetProjectProcessesDirect(ctx, r.projectID)
	})
	if err != nil {
		return CommitDeployResult{}, false, err
	}
	for _, p := range processes {
		if !liveProcess(p) || p.AppVersion == nil || p.AppVersion.Name != name || !touches(p, r.serviceID) {
			continue
		}
		return CommitDeployResult{AppVersionID: p.AppVersion.ID, ProcessID: p.ID}, true, nil
	}
	return CommitDeployResult{}, false, nil
}

// touches reports whether process p acts on the service.
func touches(p platform.Process, serviceID string) bool {
	for _, s := range p.ServiceStacks {
		if s.ID == serviceID {
			return true
		}
	}
	return false
}

// removeCutUploads deletes the service's versions a commit deploy created
// and left UPLOADING, except keep (the version just built, which the
// search may still show UPLOADING).
func (r commitRun) removeCutUploads(ctx context.Context, keep string) error {
	uploading, err := zeropsTries(ctx, func() ([]platform.AppVersionEvent, error) {
		return r.client.ListUploadingAppVersions(ctx, r.projectID, r.serviceID)
	})
	if err != nil {
		return err
	}
	for _, v := range uploading {
		if v.ID == keep || !strings.HasPrefix(v.Name, commitVersionPrefix) {
			continue
		}
		if _, err := zeropsTries(ctx, func() (struct{}, error) {
			return struct{}{}, r.client.DeleteAppVersion(ctx, v.ID)
		}); err != nil {
			return err
		}
	}
	return nil
}

// submit is one create, upload and build-and-deploy. A version it created
// and did not hand to the build is deleted before it returns. final is
// true once build-and-deploy was asked: whatever its answer, the run asks
// it no second time.
func (r commitRun) submit(ctx context.Context, source DeploySource, req CommitDeployRequest, name string) (CommitDeployResult, bool, error) {
	opened, err := source.Open(ctx, req.Repository, req.Commit)
	if err != nil {
		return CommitDeployResult{}, false, err
	}
	defer opened.Archive.Close()
	versionID, err := r.client.CreateAppVersion(ctx, req.ServiceID, name)
	if err != nil {
		return CommitDeployResult{}, false, err
	}
	if err := r.client.UploadAppVersion(ctx, versionID, opened.Archive); err != nil {
		removeVersion(ctx, r.client, versionID)
		return CommitDeployResult{}, false, err
	}
	process, err := r.client.BuildAndDeployAppVersion(ctx, versionID, opened.ZeropsYaml, req.Setup)
	switch {
	case err == nil:
		return CommitDeployResult{AppVersionID: versionID, ProcessID: process.ID}, true, nil
	case platform.ClassOf(err) == platform.ClassUnreachable:
		// No answer: the build may have started. The version stays; a live
		// process for it is the build, else the next run finds the version
		// UPLOADING and removes it.
		if found, ok, readErr := r.inFlight(ctx, name); readErr == nil && ok && found.AppVersionID == versionID {
			return found, true, nil
		}
		return CommitDeployResult{AppVersionID: versionID}, true, err
	default:
		removeVersion(ctx, r.client, versionID)
		return CommitDeployResult{}, true, err
	}
}

// removeVersionBound bounds the deletion of a version a try left behind;
// it runs past the caller's cancellation, which is what usually cut it.
const removeVersionBound = 30 * time.Second

// removeVersion deletes a version a try created and could not build. A
// deletion that fails leaves it UPLOADING, which the next run deletes.
func removeVersion(ctx context.Context, client platform.Client, versionID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), removeVersionBound)
	defer cancel()
	_ = client.DeleteAppVersion(ctx, versionID)
}

// zeropsTryPauses are the pauses between the three tries of a call whose
// failure is of class unreachable.
var zeropsTryPauses = []time.Duration{5 * time.Second, 15 * time.Second}

// zeropsTries is call tried while it fails with class unreachable, three
// times at most; the last answer is returned.
func zeropsTries[T any](ctx context.Context, call func() (T, error)) (T, error) {
	for i := 0; ; i++ {
		out, err := call()
		if err == nil || platform.ClassOf(err) != platform.ClassUnreachable || i == len(zeropsTryPauses) {
			return out, err
		}
		if !pause(ctx, zeropsTryPauses[i]) {
			return out, err
		}
	}
}

// pause waits d, false when the caller stopped first.
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// deployError is a commit deploy's own failure, not a Zerops answer: the
// step, the source's words verbatim and its class (platform.Classed).
type deployError struct {
	class platform.ErrorClass
	step  string
	words string
	err   error
}

func (e *deployError) Error() string {
	switch {
	case e.words != "":
		return e.step + ": " + e.words
	case e.err != nil:
		return e.step + ": " + e.err.Error()
	}
	return e.step
}

func (e *deployError) Unwrap() error { return e.err }

// Class is the failure's class.
func (e *deployError) Class() platform.ErrorClass { return e.class }
