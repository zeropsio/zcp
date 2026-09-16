package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// giteaRecipeBranchTitle heads the pull request the recipe lands through.
const giteaRecipeBranchTitle = "Mate: the group's import files"

// reconcileGiteaGroupRecipe proposes this Mate's project to the group repo as
// the recipe, and keeps that proposal current (guide 2.2, A2).
//
// It runs on the SAME passes as A1's repository reconcile and right after it,
// because it needs what A1 produces: a pair only belongs in the recipe once
// its service repository exists, since the recipe's whole job is to record
// which repository builds which runtime (2.4). Before that there is nothing
// to export and this returns silently.
//
// Like A1 it is a reconcile, not a step, and nothing it can meet is fatal. A
// Mate whose bot cannot fork, whose group repo has not been created yet, or
// whose Gitea is down keeps a working project and gets a line saying so; the
// next pass tries again. The recipe is a proposal — a person with production
// rights merges it (D13) — so a Mate that never gets one loses nothing but
// the proposal.
//
// Idempotent on CONTENT: the export is composed from live state on every
// pass, and PublishGiteaFiles commits only what differs from the branch. An
// unchanged project makes two reads and no commit.
func reconcileGiteaGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) string {
	if !rt.InContainer || client == nil || httpClient == nil {
		return ""
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return ""
	}
	// Only pairs A1 has already given a repository: the group repo's org comes
	// from one of them, and a runtime with no repository has no buildFromGit
	// to name.
	wired := make([]*workflow.ServiceMeta, 0, len(metas))
	for _, m := range metas {
		if m != nil && m.IsComplete() && m.Gitea != nil && m.Gitea.FullName != "" && m.RemoteURL != "" {
			wired = append(wired, m)
		}
	}
	if len(wired) == 0 {
		return ""
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })

	wiring := ops.ReadGiteaWiring(giteaEnvLookup(liveEnvPath))
	if !wiring.Ready() {
		// A1's reconcile already reports which variable is missing, on the
		// same pass. Saying it twice is noise.
		return ""
	}
	groupRepo := ops.GroupRepoFullName(wired[0].Gitea.FullName)
	if groupRepo == "" {
		return ""
	}

	identity, err := ops.DeriveGiteaIdentity(ctx, httpClient, wiring.GiteaURL, wiring.Token)
	if err != nil {
		return fmt.Sprintf("the group recipe is not proposed yet (could not read this Mate's bot identity: %v) — retrying on the next pass.", err)
	}
	branch := ops.GiteaMateBranch(identity.Name)
	if branch == "" {
		return ""
	}

	inputs, err := composeGroupRecipeInputs(ctx, client, rt.ProjectID, wired)
	if err != nil {
		return fmt.Sprintf("the group recipe is not proposed yet (%v) — retrying on the next pass.", err)
	}
	// No classifications: this composes unattended, and the classification map
	// is an agent decision made in the export/launch flows. The secret-safe
	// default is what protects the repo — an unclassified user-set service env
	// emits REPLACE_ME, never its value.
	layout, _, err := bundle.BuildGroupRecipe(inputs, nil)
	if err != nil {
		return fmt.Sprintf("the group recipe does not compose yet (%v).", err)
	}
	files, err := recipe.Build(layout)
	if err != nil {
		return fmt.Sprintf("the group recipe does not compose yet (%v).", err)
	}

	// The bot is a reader on the group's org, never a writer on {slug}/group
	// (measured on Gitea 1.27.2) — so it proposes from its own fork.
	fork, err := ops.EnsureGiteaFork(ctx, httpClient, wiring.GiteaURL, wiring.Token, groupRepo, identity.Name)
	if err != nil {
		return fmt.Sprintf("could not fork %s to propose the recipe (%v) — the project is unaffected; retrying on the next pass.", groupRepo, err)
	}

	base := wired[0].Gitea.DefaultBranch
	if base == "" {
		base = giteaProtectedBase
	}
	committed, err := ops.PublishGiteaFiles(ctx, httpClient, wiring.GiteaURL, wiring.Token,
		fork, branch, base, "recipe: the group's import files, from "+inputs.MateProjectName, files)
	if err != nil {
		return fmt.Sprintf("could not write the recipe to %s (%v) — retrying on the next pass.", fork, err)
	}

	number, created, err := ops.EnsureGiteaPullRequest(ctx, httpClient, wiring.GiteaURL, wiring.Token,
		groupRepo, fork, branch, base, giteaRecipeBranchTitle)
	if err != nil {
		return fmt.Sprintf("the recipe is on %s@%s but proposing it to %s failed (%v) — retrying on the next pass.", fork, branch, groupRepo, err)
	}
	switch {
	case created:
		return fmt.Sprintf("the group recipe is proposed to %s as pull request #%d (from %s@%s); a person with production rights merges it", groupRepo, number, fork, branch)
	case committed:
		return fmt.Sprintf("the group recipe changed; pull request #%d on %s carries the update", number, groupRepo)
	default:
		return ""
	}
}

// composeGroupRecipeInputs reads the Mate's own project and folds it, with
// what ZCP already knows about each pair, into the composer's inputs.
//
// The platform is the authority on what exists and how it scales; the metas
// are the authority on which repository and which setup block a pair builds
// from — neither is derivable from the other, which is why both are read.
func composeGroupRecipeInputs(
	ctx context.Context,
	client platform.Client,
	projectID string,
	wired []*workflow.ServiceMeta,
) (bundle.GroupRecipeInputs, error) {
	discovered, err := ops.Discover(ctx, client, projectID, "", false, false, false)
	if err != nil {
		return bundle.GroupRecipeInputs{}, fmt.Errorf("could not read this Mate's project: %w", err)
	}
	live := make(map[string]ops.ServiceInfo, len(discovered.Services))
	for _, svc := range discovered.Services {
		live[svc.Hostname] = svc
	}

	inputs := bundle.GroupRecipeInputs{
		Name:            discovered.Project.Name,
		Title:           discovered.Project.Name,
		MateProjectName: discovered.Project.Name,
	}
	for _, m := range wired {
		svc, ok := live[m.Hostname]
		if !ok {
			// The meta outlived the service (deleted outside ZCP). It has
			// nothing to contribute and must not stall the others.
			continue
		}
		scaling, scalingErr := ops.FetchServiceScaling(ctx, client, svc.ServiceID)
		if scalingErr != nil {
			scaling = nil
		}
		inputs.Runtimes = append(inputs.Runtimes, bundle.GroupRuntime{
			DevHostname:      m.Hostname,
			StageHostname:    m.StageHostname,
			ServiceType:      svc.Type,
			RepoURL:          m.RemoteURL,
			SetupName:        firstNonEmptySetup(m.PrimarySetupName, m.StageSetupName),
			StageSetupName:   m.StageSetupName,
			SubdomainEnabled: svc.SubdomainEnabled,
			Scaling:          scaling,
		})
	}
	if len(inputs.Runtimes) == 0 {
		return bundle.GroupRecipeInputs{}, fmt.Errorf("no wired pair is still running in this project")
	}

	// Every managed dependency the project runs, as it runs: a recipe whose
	// app has no database is not the app.
	for _, svc := range discovered.Services {
		if !svc.IsInfrastructure || !topology.IsManagedService(svc.Type) {
			continue
		}
		profile, profileErr := ops.FetchServiceProfile(ctx, client, svc.ServiceID)
		if profileErr != nil {
			profile = ""
		}
		inputs.ManagedServices = append(inputs.ManagedServices, bundle.ManagedServiceEntry{
			Hostname: svc.Hostname,
			Type:     svc.Type,
			Mode:     svc.Mode,
			Profile:  profile,
		})
	}
	return inputs, nil
}

// firstNonEmptySetup picks the first recorded setup-block name. A pair with
// none recorded yet is named after its dev hostname's conventional block by
// the composer's caller — here an empty string is left empty so the composer
// rejects it rather than inventing one.
func firstNonEmptySetup(names ...string) string {
	for _, name := range names {
		if strings.TrimSpace(name) != "" {
			return name
		}
	}
	return ""
}

// reconcileGitea runs both Gitea reconciles, in the one order that works: a
// pair gets its repository (A1), and only then can the recipe name what builds
// it (A2). Every bootstrap and adopt pass calls this; both halves are silent
// when there is nothing to do.
func reconcileGitea(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	sshDeployer ops.SSHDeployer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) []string {
	lines := reconcileGiteaRepositories(ctx, client, httpClient, sshDeployer, rt, stateDir, liveEnvPath)
	if line := reconcileGiteaGroupRecipe(ctx, client, httpClient, rt, stateDir, liveEnvPath); line != "" {
		lines = append(lines, line)
	}
	return lines
}
