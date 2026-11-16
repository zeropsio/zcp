package ops

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
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
	// ProcessID is "" where CommitDeploy found the commit's version already
	// building or running and submitted nothing.
	ProcessID string
}

// CommitDeploy builds a commit HQ holds into a service with no container's
// checkout: it creates an app version named by the commit, streams the
// commit's deploy source from source into it and starts build-and-deploy.
// Every build into a pool or a stage goes through it.
//
// It assumes it is the one run for the service (the stand-up runs once per
// project), and reads the service's versions of this commit first:
//   - one left UPLOADING was cut (a zcp restart mid-upload; the platform
//     keeps it UPLOADING for 14 min and more) and is deleted, then the
//     deploy is submitted anew (F7);
//   - one building, deploying or running answers as it is: never a second
//     build of the same commit (F8);
//   - a failed or superseded one is no reason not to build again.
//
// A Zerops answer of class unreachable (5xx, 429, no answer) is tried three
// times, then the last answer is returned (F11). Every error carries its
// class for platform.ClassOf: a Zerops answer as the platform's error, a
// failure of its own as a deployError.
func CommitDeploy(ctx context.Context, client platform.Client, source DeploySource, req CommitDeployRequest) (CommitDeployResult, error) {
	if req.ServiceID == "" || req.Setup == "" || req.Repository == "" || req.Commit == "" {
		return CommitDeployResult{}, &deployError{class: platform.ClassRefused, step: "commit deploy",
			words: fmt.Sprintf("service %q, setup %q, repository %q and commit %q are all needed", req.ServiceID, req.Setup, req.Repository, req.Commit)}
	}
	name := AppVersionName(req.Commit)
	versions, err := zeropsTries(ctx, func() ([]platform.AppVersionEvent, error) {
		return client.ListServiceAppVersions(ctx, req.ServiceID)
	})
	if err != nil {
		return CommitDeployResult{}, err
	}
	for _, v := range versions {
		if v.Name != name {
			continue
		}
		switch v.Status {
		case platform.AppVersionStatusUploading:
			if _, err := zeropsTries(ctx, func() (struct{}, error) {
				return struct{}{}, client.DeleteAppVersion(ctx, v.ID)
			}); err != nil {
				return CommitDeployResult{}, err
			}
		case platform.BuildStatusBuildFailed, platform.BuildStatusDeployFailed,
			platform.BuildStatusPreparingRuntimeFail, platform.BuildStatusBackup:
		default:
			return CommitDeployResult{AppVersionID: v.ID}, nil
		}
	}
	return zeropsTries(ctx, func() (CommitDeployResult, error) {
		return submitCommit(ctx, client, source, req, name)
	})
}

// AppVersionName is the name of the app version a commit deploy creates:
// the commit in it, so a version shows which head it builds (F13) and a
// later run finds it.
func AppVersionName(commit string) string {
	if len(commit) > 12 {
		commit = commit[:12]
	}
	return "zcp " + commit
}

// submitCommit is one create, upload and build-and-deploy. A version it
// created and could not hand to the build is deleted before it returns, so
// no try leaves a version UPLOADING.
func submitCommit(ctx context.Context, client platform.Client, source DeploySource, req CommitDeployRequest, name string) (CommitDeployResult, error) {
	archive, err := source.Open(ctx, req.Repository, req.Commit)
	if err != nil {
		return CommitDeployResult{}, err
	}
	defer archive.Close()
	versionID, err := client.CreateAppVersion(ctx, req.ServiceID, name)
	if err != nil {
		return CommitDeployResult{}, err
	}
	built := false
	defer func() {
		if !built {
			removeVersion(ctx, client, versionID)
		}
	}()
	body, tap := tapZeropsYaml(archive)
	err = client.UploadAppVersion(ctx, versionID, body)
	yaml := tap.finish(err)
	if err != nil {
		return CommitDeployResult{}, err
	}
	if yaml.text == "" {
		return CommitDeployResult{}, &deployError{class: platform.ClassRefused, step: "commit deploy",
			words: fmt.Sprintf("commit %s has no zerops.yaml or zerops.yml at its root%s", req.Commit, yaml.why())}
	}
	process, err := client.BuildAndDeployAppVersion(ctx, versionID, yaml.text, req.Setup)
	if err != nil {
		if platform.ClassOf(err) == platform.ClassUnreachable && buildAccepted(ctx, client, req.ServiceID, versionID) {
			built = true
			return CommitDeployResult{AppVersionID: versionID}, nil
		}
		return CommitDeployResult{}, err
	}
	built = true
	return CommitDeployResult{AppVersionID: versionID, ProcessID: process.ID}, nil
}

// buildAccepted reports whether a build-and-deploy that got no answer was
// taken all the same: the version left UPLOADING.
func buildAccepted(ctx context.Context, client platform.Client, serviceID, versionID string) bool {
	versions, err := client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		return false
	}
	for _, v := range versions {
		if v.ID == versionID {
			return v.Status != platform.AppVersionStatusUploading
		}
	}
	return false
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
		pause := time.NewTimer(zeropsTryPauses[i])
		select {
		case <-ctx.Done():
			pause.Stop()
			return out, err
		case <-pause.C:
		}
	}
}

// zeropsYamlLimit bounds the zerops.yaml kept from the archive.
const zeropsYamlLimit = 1 << 20

// yamlTap reads the archive as it streams to the upload, keeping the root
// zerops.yaml (zerops.yml when there is none): build-and-deploy sends the
// file of the very archive uploaded, so the build is of the commit read.
type yamlTap struct {
	pipe *io.PipeWriter
	done chan struct{}
	seen zeropsYamlSeen
}

// zeropsYamlSeen is the yaml the tap found, or why it found none.
type zeropsYamlSeen struct {
	text string
	err  error
}

func (s zeropsYamlSeen) why() string {
	if s.err == nil {
		return ""
	}
	return " (the archive did not read: " + s.err.Error() + ")"
}

func tapZeropsYaml(archive io.Reader) (io.Reader, *yamlTap) {
	pr, pw := io.Pipe()
	t := &yamlTap{pipe: pw, done: make(chan struct{})}
	go func() {
		defer close(t.done)
		t.seen = scanZeropsYaml(pr)
		_, _ = io.Copy(io.Discard, pr) // the upload never waits on the tap
	}()
	return io.TeeReader(archive, pw), t
}

// finish ends the tap with the upload's outcome and answers what it saw.
func (t *yamlTap) finish(upload error) zeropsYamlSeen {
	if upload != nil {
		_ = t.pipe.CloseWithError(upload)
	} else {
		_ = t.pipe.Close()
	}
	<-t.done
	return t.seen
}

func scanZeropsYaml(r io.Reader) zeropsYamlSeen {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return zeropsYamlSeen{err: err}
	}
	files := map[string]string{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return zeropsYamlSeen{err: err}
		}
		name := strings.TrimPrefix(h.Name, "./")
		if h.Typeflag != tar.TypeReg || (name != zeropsYmlPrimary && name != zeropsYmlFallback) {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(tr, zeropsYamlLimit))
		if err != nil {
			return zeropsYamlSeen{err: err}
		}
		files[name] = string(body)
	}
	if text, ok := files[zeropsYmlPrimary]; ok {
		return zeropsYamlSeen{text: text}
	}
	return zeropsYamlSeen{text: files[zeropsYmlFallback]}
}

// deployError is a commit deploy's own failure, not a Zerops answer: the
// step, the source's words verbatim and its class, which Class states for
// platform.ClassOf.
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
