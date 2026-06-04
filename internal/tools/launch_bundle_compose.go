package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// composeLaunchBundleInputs builds the LaunchBundleInputs payload for
// the composer from a list of resolved runtimes + gate-validated
// RepoURLs. Handles per-runtime SSH/local source read (zerops.yaml +
// git SHA) and aggregates ManagedServices once across all promoted
// runtimes (deduplicated by hostname so monorepo + shared infra
// emits one entry each in the bundle).
//
// Returns the inputs ready for ops.BuildLaunchBundle plus a possibly-
// non-empty list of warnings the caller appends to the response /
// bundle. Per-runtime read failures surface as platform errors so the
// caller wraps them into the standard launch failure response.
//
// The RepoURL on each LaunchRuntimeInput comes from the matching
// LaunchSourceControlCheck.MetaRemoteURL — never live SSH (P-LP-10
// invariant; gate validates meta vs live alignment once at the
// boundary, composer trusts the validated value).
func composeLaunchBundleInputs(
	ctx context.Context,
	client platform.Client,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	sourceProjectID string,
	productionProjectName string,
	runtimes []resolvedLaunchRuntime,
	gateChecks []*LaunchSourceControlCheck,
	projectEnvs []bundle.ProjectEnvVar,
	keepNonHA []string,
	variant bundle.Variant,
) (bundle.LaunchBundleInputs, []string, error) {
	if len(runtimes) == 0 {
		return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("composeLaunchBundleInputs: at least one runtime required")
	}
	if len(runtimes) != len(gateChecks) {
		return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("composeLaunchBundleInputs: runtimes (%d) and gateChecks (%d) must align 1:1", len(runtimes), len(gateChecks))
	}

	// Discover once — shared across all runtimes for managed-dep
	// aggregation and per-runtime type lookup.
	discover, err := ops.Discover(ctx, client, sourceProjectID, "", false, false, false)
	if err != nil {
		return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("discover source project: %w", err)
	}

	// Build per-runtime LaunchRuntimeInput entries. Each runtime reads
	// its own zerops.yaml + git SHA from its push hostname (monorepo
	// runtimes sharing /var/www return identical bodies; the composer
	// dedupes on body equality when scanning cross-service refs).
	var warnings []string
	bundleRuntimes := make([]bundle.LaunchRuntimeInput, 0, len(runtimes))
	excludeHosts := make([]string, 0, len(runtimes))
	for i, r := range runtimes {
		check := gateChecks[i]
		if check == nil || check.MetaRemoteURL == "" {
			return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("composeLaunchBundleInputs: runtime %q has empty gate-validated RepoURL — gate must run before compose", r.PushHostname)
		}
		runtimeSvc := findRuntimeServiceByHostname(discover, r.PushHostname)
		if runtimeSvc == nil {
			return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("composeLaunchBundleInputs: runtime %q not found in source-project discover output", r.PushHostname)
		}
		yamlBody, sha, readErr := readLaunchRuntimeSource(ctx, sshDeployer, rt, r.PushHostname)
		if readErr != nil {
			return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("read source state for %q: %w", r.PushHostname, readErr)
		}
		if strings.TrimSpace(yamlBody) == "" {
			return bundle.LaunchBundleInputs{}, nil, fmt.Errorf("source zerops.yaml is missing for %q — write it (with the runtime's setup block), commit, push, then re-call publish", r.PushHostname)
		}
		// GAP0-1: carry the runtime's USER-set service env (Type=SECRET)
		// so a key set via `zerops_env set serviceHostname=X` survives the
		// promotion. A read failure is non-fatal (warn + omit) — never
		// blocks the launch.
		secretEnvs, secErr := ops.FetchServiceSecretEnvs(ctx, client, runtimeSvc.ServiceID)
		if secErr != nil {
			warnings = append(warnings, fmt.Sprintf("read service secrets for %q: %v (service envSecrets omitted from bundle)", r.PushHostname, secErr))
		}
		// R7: reflect the live source scaling into the promoted runtime (the
		// composer applies the named production transforms). Non-fatal — a read
		// failure yields nil and the composer falls back to the prod policy floor.
		scaling, _ := ops.FetchServiceScaling(ctx, client, runtimeSvc.ServiceID)
		bundleRuntimes = append(bundleRuntimes, bundle.LaunchRuntimeInput{
			ProdHostname:   r.ProdHostname,
			ServiceType:    runtimeSvc.Type,
			SetupName:      r.SetupName,
			RepoURL:        check.MetaRemoteURL,
			GitCommitSHA:   sha,
			ZeropsYAMLBody: yamlBody,
			ServiceEnvs:    serviceSecretsToBundleEnvs(secretEnvs),
			Scaling:        scaling,
		})
		excludeHosts = append(excludeHosts, r.PushHostname)
	}

	managed := collectManagedServicesExcluding(discover, excludeHosts)

	return bundle.LaunchBundleInputs{
		SourceProjectID:   sourceProjectID,
		TargetProjectName: productionProjectName,
		Runtimes:          bundleRuntimes,
		ProjectEnvs:       projectEnvs,
		ManagedServices:   managed,
		KeepNonHA:         keepNonHA,
		Variant:           variant,
	}, warnings, nil
}

// serviceSecretsToBundleEnvs converts the runtime's USER-set service env
// layer (Type=SECRET) into the composer's bundle.ProjectEnvVar shape for
// the runtime entry's envSecrets (GAP0-1). Shared by the export + launch
// handlers.
func serviceSecretsToBundleEnvs(envs []platform.ServiceEnvVar) []bundle.ProjectEnvVar {
	if len(envs) == 0 {
		return nil
	}
	out := make([]bundle.ProjectEnvVar, 0, len(envs))
	for _, e := range envs {
		out = append(out, bundle.ProjectEnvVar{Key: e.Key, Value: e.Content})
	}
	return out
}

// findRuntimeServiceByHostname returns the ServiceInfo for the given
// runtime hostname or nil when absent. Discover already filtered by
// project; we just lookup-by-hostname here.
func findRuntimeServiceByHostname(d *ops.DiscoverResult, hostname string) *ops.ServiceInfo {
	if d == nil {
		return nil
	}
	for i := range d.Services {
		if d.Services[i].Hostname == hostname && !d.Services[i].IsInfrastructure {
			return &d.Services[i]
		}
	}
	return nil
}

// collectManagedServicesExcluding wraps collectManagedServices for
// the multi-runtime case where every promoted runtime hostname must
// be excluded from the managed-dep list. Falls back to the legacy
// single-exclusion path when only one hostname is supplied.
func collectManagedServicesExcluding(discover *ops.DiscoverResult, excludeHostnames []string) []ops.ManagedServiceEntry {
	if discover == nil {
		return nil
	}
	excludeSet := make(map[string]bool, len(excludeHostnames))
	for _, h := range excludeHostnames {
		excludeSet[h] = true
	}
	var out []ops.ManagedServiceEntry
	for _, svc := range discover.Services {
		if !svc.IsInfrastructure {
			continue
		}
		if excludeSet[svc.Hostname] {
			continue
		}
		out = append(out, ops.ManagedServiceEntry{
			Hostname: svc.Hostname,
			Type:     svc.Type,
			Mode:     svc.Mode,
		})
	}
	return out
}

// readLaunchRuntimeSource is the env-aware per-runtime source read:
// container mode SSHes into the push hostname's /var/www; local mode
// reads from the current working directory. Returns (zeropsYAMLBody,
// gitCommitSHA, error).
func readLaunchRuntimeSource(ctx context.Context, sshDeployer ops.SSHDeployer, rt runtime.Info, pushHostname string) (string, string, error) {
	if rt.InContainer {
		if sshDeployer == nil {
			return "", "", fmt.Errorf("readLaunchRuntimeSource: SSH deployer unavailable in container mode")
		}
		body, err := readZeropsYAMLBody(ctx, sshDeployer, pushHostname)
		if err != nil {
			return "", "", err
		}
		sha, err := readGitCommitSHA(ctx, sshDeployer, pushHostname)
		if err != nil {
			return "", "", err
		}
		return body, sha, nil
	}
	body, err := readLocalZeropsYAML("")
	if err != nil {
		return "", "", err
	}
	sha, err := readLocalGitSHA(ctx, "")
	if err != nil {
		return "", "", err
	}
	return body, sha, nil
}
