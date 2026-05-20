package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// LaunchSourceState is the read-side source-project state the launch
// workflow needs to compose a bundle. Discovered via Discover + SSH
// (container) or local FS (local). Caller hands this to
// ops.BuildLaunchBundle.
type LaunchSourceState struct {
	// ServiceType is the runtime's platform type tag (e.g. "nodejs@22").
	ServiceType string
	// RepoURL is the live `git remote get-url origin` value.
	RepoURL string
	// GitCommitSHA is the source repo's HEAD commit.
	GitCommitSHA string
	// ZeropsYAMLBody is the verbatim source zerops.yaml content.
	ZeropsYAMLBody string
	// ManagedServices lists managed deps discovered alongside the runtime.
	ManagedServices []ops.ManagedServiceEntry
}

// readSourceState orchestrates source-project state read for the launch
// workflow. Container mode SSH's into the source runtime; local mode
// reads from local FS + local git. Returns a structured PlatformError
// on any sub-step failure; caller maps to source-control blocker.
//
// targetHostname is the runtime service whose source repo holds the
// zerops.yaml + git remote. For a dev/stage pair, this is the dev half
// (where /var/www holds the working tree). For simple/local-only it's
// the single runtime.
//
// workingDir is consulted in local mode only; container mode reads
// from `/var/www` on the source container. Empty workingDir in local
// mode falls back to `os.Getwd()`.
func readSourceState(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	projectID string,
	targetHostname string,
	workingDir string,
) (*LaunchSourceState, error) {
	if targetHostname == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"readSourceState: targetHostname required",
			"Pass targetService=<runtime-hostname> in the launch workflow input.",
		)
	}

	// Discover source services. Drives runtime type + managed dep list.
	// Pass empty hostname so Discover returns all services (we filter
	// locally to pick the runtime + collect managed deps).
	discover, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return nil, fmt.Errorf("discover source project: %w", err)
	}

	var runtimeSvc *ops.ServiceInfo
	for i := range discover.Services {
		if discover.Services[i].Hostname == targetHostname {
			runtimeSvc = &discover.Services[i]
			break
		}
	}
	if runtimeSvc == nil {
		return nil, platform.NewPlatformError(
			platform.ErrServiceNotFound,
			fmt.Sprintf("Runtime service %q not found in source project", targetHostname),
			"Verify the targetService hostname via zerops_discover.",
		)
	}
	if runtimeSvc.IsInfrastructure {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("Service %q is a managed dependency, not a runtime", targetHostname),
			"Pass the buildFromGit-bearing runtime hostname instead.",
		)
	}

	state := &LaunchSourceState{
		ServiceType:     runtimeSvc.Type,
		ManagedServices: collectManagedServices(discover, targetHostname),
	}

	// Env-aware read of zerops.yaml + git remote + git SHA.
	if rt.InContainer {
		if sshDeployer == nil {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidParameter,
				"SSH access unavailable in container mode — launch needs to read /var/www/zerops.yaml + git state from the source runtime",
				"Run ZCP from a properly-configured Zerops container, or use local mode.",
			)
		}
		if state.ZeropsYAMLBody, err = readZeropsYAMLBody(ctx, sshDeployer, targetHostname); err != nil {
			return nil, err
		}
		if state.RepoURL, err = readGitRemoteURL(ctx, sshDeployer, targetHostname); err != nil {
			return nil, err
		}
		if state.GitCommitSHA, err = readGitCommitSHA(ctx, sshDeployer, targetHostname); err != nil {
			return nil, err
		}
		return state, nil
	}

	// Local mode.
	if workingDir == "" {
		wd, getwdErr := os.Getwd()
		if getwdErr != nil {
			return nil, fmt.Errorf("resolve workingDir for local source read: %w", getwdErr)
		}
		workingDir = wd
	}
	if state.ZeropsYAMLBody, err = readLocalZeropsYAML(workingDir); err != nil {
		return nil, err
	}
	if state.RepoURL, err = readLocalGitRemote(ctx, workingDir); err != nil {
		return nil, err
	}
	if state.GitCommitSHA, err = readLocalGitSHA(ctx, workingDir); err != nil {
		return nil, err
	}
	return state, nil
}

// readGitCommitSHA SSH-reads the source repo's HEAD commit SHA. Captured
// into SourceSnapshot.GitCommitSHA for the immutability guard.
func readGitCommitSHA(ctx context.Context, ssh ops.SSHDeployer, hostname string) (string, error) {
	cmd := fmt.Sprintf(`cd %s 2>/dev/null && git rev-parse HEAD 2>/dev/null || true`, exportRepoRoot)
	out, err := ssh.ExecSSH(ctx, hostname, cmd)
	if err != nil {
		return "", platform.NewPlatformError(
			platform.ErrSSHDeployFailed,
			fmt.Sprintf("Read git HEAD SHA from %q: %v", hostname, err),
			"Verify the container is reachable via SSH.",
		)
	}
	return strings.TrimSpace(string(out)), nil
}

// readLocalZeropsYAML reads zerops.yaml from the local working directory.
// Tries `.yaml` first, then `.yml`. Empty content (file missing) is
// returned without error so the caller chains to launch-write-prod-setup
// the same way SSH-read empties do.
func readLocalZeropsYAML(workingDir string) (string, error) {
	for _, name := range []string{"zerops.yaml", "zerops.yml"} {
		data, err := os.ReadFile(filepath.Join(workingDir, name))
		if err == nil {
			return strings.TrimRight(string(data), "\n"), nil
		}
		if !os.IsNotExist(err) {
			return "", platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("Read %s in %q: %v", name, workingDir, err),
				"",
			)
		}
	}
	return "", nil
}

// readLocalGitRemote reads the local repo's `git remote get-url origin`.
// Empty stdout = no remote configured; caller surfaces source-control
// blocker. Both "not a git repo" and "no origin" surface as
// exit-status-1; the function intentionally swallows that error and
// returns empty so caller chains to setup-git-push (same path as
// container-mode empty remote).
//
//nolint:unparam // error return preserved for symmetry with container-mode read path; future enhancements may need to distinguish "not a repo" from "no origin"
func readLocalGitRemote(ctx context.Context, workingDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = workingDir
	out, err := cmd.Output()
	if err != nil {
		return "", nil //nolint:nilerr // exit-status-1 means "not a repo" or "no origin" — both surface as empty per the source-control gate contract; caller chains to setup-git-push the same way container-mode does
	}
	return strings.TrimSpace(string(out)), nil
}

// readLocalGitSHA reads the local repo's `git rev-parse HEAD`. Empty
// stdout = no commits; caller surfaces source-control blocker.
// Symmetric error handling with readLocalGitRemote.
//
//nolint:unparam // see readLocalGitRemote
func readLocalGitSHA(ctx context.Context, workingDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = workingDir
	out, err := cmd.Output()
	if err != nil {
		return "", nil //nolint:nilerr // exit-status-1 means "not a repo" or "no origin" — both surface as empty per the source-control gate contract; caller chains to setup-git-push the same way container-mode does
	}
	return strings.TrimSpace(string(out)), nil
}

// hasSetupProd reports whether the source zerops.yaml carries a
// `setup: prod` block. Launch publish requires this — the agent writes
// it (guided by launch-write-prod-setup atom) before publish.
func hasSetupProd(zeropsYAMLBody string) bool {
	return hasSetupNamed(zeropsYAMLBody, "prod")
}

// hasSetupNamed reports whether the source zerops.yaml carries a block
// named `setup: <name>`. Used by the launch source-control gate so the
// agent can target a non-canonical setup name via
// WorkflowInput.ProdSetupNameOverride.
func hasSetupNamed(zeropsYAMLBody, name string) bool {
	if name == "" {
		return false
	}
	names, _ := listSetupNames(zeropsYAMLBody)
	return slices.Contains(names, name)
}

// _ keeps workflow.ServiceMeta in scope for the helper file even when
// the launch read path doesn't currently consult it. ServiceMeta carries
// remote-URL drift state used by the export workflow's refreshRemoteURLCache;
// launch may consult it in a follow-up to cache the live remote.
var _ = workflow.ServiceMeta{}
