package ops

import (
	"context"
	"errors"

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
func CommitDeploy(ctx context.Context, client platform.Client, source DeploySource, req CommitDeployRequest) (CommitDeployResult, error) {
	return CommitDeployResult{}, errors.New("not implemented")
}
