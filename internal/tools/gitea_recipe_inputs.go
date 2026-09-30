package tools

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// composeGroupRecipeInputs reads the Mate's own project and folds it, with
// what ZCP already knows about each pair, into the composer's inputs.
//
// The platform is the authority on what exists, how it scales and what it
// holds; the metas are the authority on which repository and which setup
// block a pair builds from — neither is derivable from the other, which is
// why both are read. group is the group's slug, the org of its repositories:
// the recipe's name, and the stage and production projects are named after
// it.
//
// A read that decides what a tier carries — the project's variables, a
// pair's own, an object storage's size, a runtime's origin — fails the pass
// instead of composing without it: a tier on the group repo stays there
// (D30), so a tier composed from a failed read would carry the gap for good,
// the way the medusa group's first production came up with no project
// variables at all. The next pass reads again. A read that only refines — a
// scale, a profile, a storage's policy — is a warning.
func composeGroupRecipeInputs(
	ctx context.Context,
	client platform.Client,
	projectID, group, mountRoot string,
	metas, wired []*workflow.ServiceMeta,
) (bundle.GroupRecipeInputs, []string, error) {
	discovered, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return bundle.GroupRecipeInputs{}, nil, fmt.Errorf("could not read this Mate's project: %w", err)
	}
	project, err := client.GetProject(ctx, projectID)
	if err != nil {
		return bundle.GroupRecipeInputs{}, nil, fmt.Errorf("could not read this Mate's project: %w", err)
	}
	projectEnvs, err := inventory.FetchProjectEnvs(ctx, client, projectID)
	if err != nil {
		return bundle.GroupRecipeInputs{}, nil, fmt.Errorf("could not read the project's variables: %w", err)
	}
	live := make(map[string]ops.ServiceInfo, len(discovered.Services))
	for _, svc := range discovered.Services {
		live[svc.Hostname] = svc
	}

	inputs := bundle.GroupRecipeInputs{
		Name:            group,
		Title:           group,
		MateProjectName: discovered.Project.Name,
		CorePackage:     project.Mode,
		ProjectEnvs:     groupRecipeProjectEnvs(projectEnvs),
	}
	var warnings []string
	for _, m := range wired {
		svc, ok := live[m.Hostname]
		if !ok {
			// The meta outlived the service (deleted outside ZCP). It has
			// nothing to contribute and must not stall the others.
			continue
		}
		// An unread scale is the composer's to warn about.
		shape, _ := ops.FetchServiceShape(ctx, client, svc.ServiceID)
		devEnvs, err := groupRecipeServiceEnvs(ctx, client, svc)
		if err != nil {
			return bundle.GroupRecipeInputs{}, nil, err
		}
		var stageEnvs []bundle.ProjectEnvVar
		if stage, ok := live[m.StageHostname]; ok && m.StageHostname != "" {
			if stageEnvs, err = groupRecipeServiceEnvs(ctx, client, stage); err != nil {
				return bundle.GroupRecipeInputs{}, nil, err
			}
		}
		// A pair that has never deployed has no setup recorded — the classic
		// bootstrap route's ordinary state — and refusing there made the whole
		// group recipe unproposable for a Mate that had not shipped yet. The
		// conventional name is the pair's own hostname, the same fallback the
		// git-push deploy already uses, and the pair's zerops.yaml is right
		// there on the mount: reading it lets the composer VERIFY the block
		// rather than warn that it could not.
		yamlBody, _ := readLocalZeropsYAML(filepath.Join(mountRoot, m.Hostname))
		inputs.Runtimes = append(inputs.Runtimes, bundle.GroupRuntime{
			DevHostname:      m.Hostname,
			StageHostname:    m.StageHostname,
			ServiceType:      svc.Type,
			RepoURL:          m.RemoteURL,
			SetupName:        firstNonEmptySetup(m.PrimarySetupName, m.StageSetupName, m.Hostname),
			StageSetupName:   m.StageSetupName,
			ZeropsYAMLBody:   yamlBody,
			SubdomainEnabled: svc.SubdomainEnabled,
			Scaling:          shape.Scaling,
			ServiceEnvs:      devEnvs,
			StageServiceEnvs: stageEnvs,
		})
	}
	if len(inputs.Runtimes) == 0 {
		return bundle.GroupRecipeInputs{}, nil, fmt.Errorf("no wired pair is still running in this project")
	}

	// Every managed dependency the project runs, as it runs: a recipe whose
	// app has no database is not the app. Every other runtime is either a
	// utility built from a public repository, written as it runs, or left
	// out and said.
	haCatalog := schema.Embedded()
	paired := workflow.ManagedRuntimeIndex(wired)
	known := workflow.ManagedRuntimeIndex(metas)
	for _, svc := range discovered.Services {
		switch {
		case svc.IsInfrastructure && topology.IsManagedService(svc.Type):
			entry, entryWarnings, err := groupRecipeManaged(ctx, client, svc)
			if err != nil {
				return bundle.GroupRecipeInputs{}, nil, err
			}
			warnings = append(warnings, entryWarnings...)
			inputs.ManagedServices = append(inputs.ManagedServices, entry)
			if bundle.RulesForType(svc.Type).AcceptsMode && !haCatalog.SupportsHAVariant(svc.Type) {
				inputs.HAIncapable = append(inputs.HAIncapable, svc.Hostname)
			}
		case svc.IsInfrastructure || paired[svc.Hostname] != nil || strings.HasPrefix(svc.Type, "zcp@"):
			continue
		default:
			utility, utilityWarnings, err := groupRecipeUtility(ctx, client, svc, known[svc.Hostname])
			if err != nil {
				return bundle.GroupRecipeInputs{}, nil, err
			}
			warnings = append(warnings, utilityWarnings...)
			if utility != nil {
				inputs.Utilities = append(inputs.Utilities, *utility)
			}
		}
	}
	return inputs, warnings, nil
}

// isControlPlaneEnv reports a variable that is the Mate's wiring, never the
// app's: zcp's own key and agents, a git or launch token, the Gitea bot's
// token and the addresses it reaches Gitea and the broker by. A group's
// environment gets its own, so none of them is copied — not even as a
// generated secret.
func isControlPlaneEnv(key string) bool {
	switch key {
	case ops.GiteaTokenEnvKey, ops.GiteaURLEnvKey, ops.MateBrokerURLEnvKey:
		return true
	}
	return topology.IsClassifyInfrastructure(key)
}

// groupRecipeProjectEnvs is the project's variables the recipe carries: the
// user-set ones, the control plane's left out. The platform's own (SYSTEM:
// envIsolation, the subdomain host, CDN addresses) are made anew by every
// project the tier creates.
func groupRecipeProjectEnvs(envs []platform.ProjectEnvVar) []bundle.ProjectEnvVar {
	out := make([]bundle.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if env.Type == platform.ProjectEnvSystem || isControlPlaneEnv(env.Key) {
			continue
		}
		out = append(out, bundle.ProjectEnvVar{Key: env.Key, Value: env.Content, Sensitive: env.Sensitive})
	}
	return out
}

// groupRecipeServiceEnvs reads a runtime's own user-set variables — the
// layer its zerops.yaml does not rebuild — the control plane's left out.
func groupRecipeServiceEnvs(ctx context.Context, client platform.Client, svc ops.ServiceInfo) ([]bundle.ProjectEnvVar, error) {
	envs, err := ops.FetchServiceUserEnvs(ctx, client, svc.ServiceID)
	if err != nil {
		return nil, fmt.Errorf("could not read %s's variables: %w", svc.Hostname, err)
	}
	out := make([]bundle.ProjectEnvVar, 0, len(envs))
	for _, env := range envs {
		if isControlPlaneEnv(env.Key) {
			continue
		}
		out = append(out, bundle.ProjectEnvVar{Key: env.Key, Value: env.Content, Sensitive: env.Sensitive})
	}
	return out, nil
}

// groupRecipeManaged reads a managed service as it runs: its profile and
// scale, and an object storage's size and access policy.
func groupRecipeManaged(ctx context.Context, client platform.Client, svc ops.ServiceInfo) (bundle.ManagedServiceEntry, []string, error) {
	entry := bundle.ManagedServiceEntry{Hostname: svc.Hostname, Type: svc.Type, Mode: svc.Mode}
	if shape, err := ops.FetchServiceShape(ctx, client, svc.ServiceID); err == nil {
		entry.Profile, entry.Scaling = shape.Profile, shape.Scaling
	}
	if !bundle.RulesForType(svc.Type).RequiresObjectStorageSize {
		return entry, nil, nil
	}
	storage, err := ops.FetchObjectStorageShape(ctx, client, svc.ServiceID, svc.Hostname)
	if err != nil {
		return bundle.ManagedServiceEntry{}, nil, fmt.Errorf("could not read %s's size: %w", svc.Hostname, err)
	}
	entry.QuotaGBytes = storage.SizeGB
	entry.ObjectStoragePolicy, entry.ObjectStorageRawPolicy = storage.Policy, storage.RawPolicy
	var warnings []string
	if storage.PolicyUnread != "" {
		warnings = append(warnings, fmt.Sprintf(
			"object storage %q: its access policy is not in the recipe (%s), so the tiers leave the platform's default, private — set objectStoragePolicy on them if the bucket is public",
			svc.Hostname, storage.PolicyUnread))
	}
	return entry, warnings, nil
}

// groupRecipeUtility reads a runtime no Gitea pair builds: a utility when its
// active version was built from a public repository, left out and said
// otherwise.
func groupRecipeUtility(ctx context.Context, client platform.Client, svc ops.ServiceInfo, meta *workflow.ServiceMeta) (*bundle.GroupUtility, []string, error) {
	shape, err := ops.FetchServiceShape(ctx, client, svc.ServiceID)
	if err != nil {
		return nil, nil, fmt.Errorf("could not read where %s is built from: %w", svc.Hostname, err)
	}
	if !isPublicGitHost(shape.PublicGitURL) {
		return nil, []string{fmt.Sprintf(
			"runtime %q is not in the recipe: it has no Gitea repository yet and was not built from a public one", svc.Hostname)}, nil
	}
	envs, err := groupRecipeServiceEnvs(ctx, client, svc)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	setup := recordedSetup(meta, svc.Hostname)
	if setup == "" && shape.ExplicitSetup {
		warnings = append(warnings, fmt.Sprintf(
			"utility %q was built with a zeropsSetup the platform does not return, so the tiers name none and the platform builds the setup named %q",
			svc.Hostname, svc.Hostname))
	}
	return &bundle.GroupUtility{
		Hostname:         svc.Hostname,
		ServiceType:      svc.Type,
		BuildFromGit:     shape.PublicGitURL,
		SetupName:        setup,
		SubdomainEnabled: svc.SubdomainEnabled,
		Scaling:          shape.Scaling,
		ServiceEnvs:      envs,
	}, warnings, nil
}

// publicGitHosts are the hosts an import's buildFromGit can clone from
// without a credential.
var publicGitHosts = map[string]bool{"github.com": true, "gitlab.com": true}

// isPublicGitHost reports a repository URL on a public git host.
func isPublicGitHost(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return publicGitHosts[strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")]
}

// recordedSetup is the setup a runtime's meta records for its half, "" when
// no meta holds it.
func recordedSetup(meta *workflow.ServiceMeta, hostname string) string {
	switch {
	case meta == nil:
		return ""
	case meta.Hostname == hostname:
		return meta.PrimarySetupName
	default:
		return meta.StageSetupName
	}
}

// firstNonEmptySetup picks the first setup-block name that is there. The
// caller passes the recorded names first and the conventional one — the dev
// hostname — last, so a recorded block always wins and a pair that has never
// deployed still names something the recipe can build.
func firstNonEmptySetup(names ...string) string {
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			return name
		}
	}
	return ""
}
