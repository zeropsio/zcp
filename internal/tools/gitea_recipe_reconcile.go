package tools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// giteaRecipeBranchTitle heads the pull request the recipe lands through.
const giteaRecipeBranchTitle = "Mate: the group's import files"

// giteaRecipeOutcome is what one pass did. The reconcile reads Line (empty
// when there is nothing worth saying on a bootstrap response); the agent-facing
// action reads the rest, because a person who ASKED for the export wants the
// pull request's address even on the pass that changed nothing.
type giteaRecipeOutcome struct {
	// Line is the reconcile's report, "" when the pass was a no-op.
	Line string
	// GroupRepo is `{slug}/group`, Fork the bot's copy of it, Branch the
	// fork branch the proposal is made from (ops.GiteaRecipeBranch) — the
	// last two empty when nothing is proposed.
	GroupRepo string
	Fork      string
	Branch    string
	// Proposed names what the proposal adds to main: tier directories, and a
	// top-level file main has none of.
	Proposed []string
	// PullNumber is 0 when no pull request could be read or opened.
	PullNumber int
	// PullURL addresses it on the account's Gitea.
	PullURL string
	// Committed is whether this pass wrote anything; Created whether it
	// opened the pull request.
	Committed bool
	Created   bool
	// OnMain is true when the group repo's main already carries every tier
	// this Mate would propose, so nothing was.
	OnMain bool
	// Closed are this bot's earlier proposals the pass closed.
	Closed []int
	// Warnings are the reader's and the composer's — a runtime left out, a
	// storage policy unread, an unverified setup, an unreadable scaling shape.
	Warnings []string
	// Blocked names why nothing happened, for the action to report. Empty on
	// a pass that reached Gitea.
	Blocked string
}

// reconcileGiteaGroupRecipe proposes to the group repo the recipe tiers its
// main does not carry yet, composed from this Mate's project (guide 2.2, A2).
//
// Additive, a tier at a time (recipe.Missing). A tier on main is the group's —
// hand-written, or merged from an earlier proposal — and zcp never proposes
// over it: every Mate composes the whole app from its OWN project, and on
// 2026-09-26 a second Mate's proposal replaced the medusa group's hand-written
// tiers (project env, secrets, the production setups), was merged, and the
// next release built production with the dev setup. So a group's first recipe
// lands whole, a tier main lacks is proposed on any later pass, and a group
// whose main has every tier gets nothing — no fork, no commit, no pull request
// — while this bot's own proposals still open there, the requests it opened
// under giteaRecipeBranchTitle, are closed. A tier on main changes only
// through a pull request somebody chose to open — a person's, or the Mate's
// own when its person asks, since a registered Mate writes the group repo
// (D31) — never through this reconcile, which leaves such a request open.
//
// A proposal is cut from main's tip, on a fork branch named after that commit
// (ops.GiteaRecipeBranch), so its pull request only ever adds files — the kind
// the group's broker lands by itself. While main stays put an open proposal
// follows the project: a changed composition commits onto the same branch and
// request. Once main moves, a proposal still open is closed and cut again from
// the new tip; a branch from an older main would diff as modifications.
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
// next pass tries again. The recipe is a proposal — the broker lands one that
// only adds files by itself (D23, D30), and anyone with write merges the rest
// — so a Mate that never gets one loses nothing but the proposal.
//
// Idempotent on CONTENT: the export is composed from live state on every
// pass, and PublishGiteaFiles commits only what differs from the branch. An
// unchanged project makes reads and no write, and so does a group whose main
// has every tier and no proposal of this Mate's open.
func reconcileGiteaGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) string {
	return giteaGroupRecipeOutcome(ctx, client, httpClient, rt, stateDir, liveEnvPath).Line
}

// giteaGroupRecipeOutcome is the reconcile itself. Split from the reporter so
// the agent-facing action can read what happened rather than parse a sentence.
func giteaGroupRecipeOutcome(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) giteaRecipeOutcome {
	if !rt.InContainer || client == nil || httpClient == nil {
		return giteaRecipeOutcome{Blocked: "the group recipe is a Mate's act — there is no Mate environment here"}
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return giteaRecipeOutcome{Blocked: "no service has been bootstrapped yet, so there is nothing to export"}
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
		return giteaRecipeOutcome{Blocked: "no pair has its Gitea repository yet — the recipe names what builds each runtime, so it waits for them"}
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })

	wiring := ops.ReadGiteaWiring(giteaEnvLookup(liveEnvPath))
	if !wiring.Ready() {
		// A1's reconcile already reports which variable is missing, on the
		// same pass, so Line stays empty — but a person who asked deserves
		// the reason.
		return giteaRecipeOutcome{Blocked: "waiting for Gitea (" + strings.Join(wiring.MissingKeys(), ", ") + " not on this service yet)"}
	}
	groupRepo := ops.GroupRepoFullName(wired[0].Gitea.FullName)
	if groupRepo == "" {
		return giteaRecipeOutcome{Blocked: "the pair's repository does not name an org, so the group repo cannot be derived"}
	}

	identity, err := ops.DeriveGiteaIdentity(ctx, httpClient, wiring.GiteaURL, wiring.Token)
	if err != nil {
		return giteaRecipeOutcome{Line: fmt.Sprintf("the group recipe is not proposed yet (could not read this Mate's bot identity: %v) — retrying on the next pass.", err)}
	}
	if identity.Name == "" {
		return giteaRecipeOutcome{Blocked: "the Gitea bot has no login, so there is no fork to propose from"}
	}
	outcome := giteaRecipeOutcome{GroupRepo: groupRepo}

	// The group's slug is the org of its repositories: the recipe's name, and
	// what the stage and production projects are named after.
	group, _, _ := strings.Cut(groupRepo, "/")
	inputs, readWarnings, err := composeGroupRecipeInputs(ctx, client, rt.ProjectID, group, giteaPairMountRoot, metas, wired)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe is not proposed yet (%v) — retrying on the next pass.", err)
		return outcome
	}
	// This composes unattended, with nobody to classify a variable the way the
	// export and launch flows ask the agent to, so the composer decides each
	// one itself: config as written, a secret as a generator — never its value.
	layout, warnings, err := bundle.BuildGroupRecipe(inputs)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return outcome
	}
	outcome.Warnings = append(outcome.Warnings, readWarnings...)
	outcome.Warnings = append(outcome.Warnings, warnings...)
	files, err := recipe.Build(layout)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return outcome
	}

	base := wired[0].Gitea.DefaultBranch
	if base == "" {
		base = giteaProtectedBase
	}
	main, err := ops.ReadGiteaBranchFiles(ctx, httpClient, wiring.GiteaURL, wiring.Token, groupRepo, base)
	if err != nil {
		outcome.Line = fmt.Sprintf("could not read %s@%s to see which tiers it lacks (%v) — retrying on the next pass.", groupRepo, base, err)
		return outcome
	}
	if main.Head == "" {
		outcome.Line = fmt.Sprintf("the group repo %s has no %s yet, so there is nothing to propose the recipe to — retrying on the next pass.", groupRepo, base)
		return outcome
	}
	missing := recipe.Missing(files, main.Paths)

	if len(missing) == 0 {
		outcome.OnMain = true
		closed, closeErr := ops.CloseGiteaPullRequests(ctx, httpClient, wiring.GiteaURL, wiring.Token, groupRepo, identity.Name, giteaRecipeBranchTitle, base, "")
		outcome.Closed = closed
		switch {
		case closeErr != nil:
			outcome.Line = fmt.Sprintf("%s@%s already carries every tier of the recipe, but closing this Mate's earlier proposal failed (%v) — retrying on the next pass.", groupRepo, base, closeErr)
		case len(closed) > 0:
			outcome.Line = fmt.Sprintf("%s@%s already carries every tier of the recipe, so this Mate's earlier proposal %s is closed", groupRepo, base, pullNumbers(closed))
		}
		return outcome
	}
	outcome.Proposed = recipeEntries(missing)

	// The proposal is made from the bot's own fork. A registered Mate writes
	// {slug}/group too (D31), but the reconcile needs none of that: a fork
	// serves whether or not the broker has made the bot a writer — a broker
	// from before D31 never does — and it keeps zcp's recipe/ branches out of
	// the group repo's own.
	fork, err := ops.EnsureGiteaFork(ctx, httpClient, wiring.GiteaURL, wiring.Token, groupRepo, identity.Name)
	if err != nil {
		outcome.Line = fmt.Sprintf("could not fork %s to propose the recipe (%v) — the project is unaffected; retrying on the next pass.", groupRepo, err)
		return outcome
	}
	outcome.Fork = fork
	branch := ops.GiteaRecipeBranch(main.Head)
	outcome.Branch = branch

	// Close first: a proposal from an older main is withdrawn before its
	// replacement opens, so the group never holds two of this Mate's at once.
	closed, closeErr := ops.CloseGiteaPullRequests(ctx, httpClient, wiring.GiteaURL, wiring.Token, groupRepo, identity.Name, giteaRecipeBranchTitle, base, branch)
	outcome.Closed = closed
	if closeErr != nil {
		outcome.Line = fmt.Sprintf("could not close this Mate's earlier recipe proposal on %s (%v) — retrying on the next pass.", groupRepo, closeErr)
		return outcome
	}
	if _, err := ops.EnsureGiteaProposalBranch(ctx, httpClient, wiring.GiteaURL, wiring.Token, fork, branch, base, main.Head); err != nil {
		outcome.Line = fmt.Sprintf("could not cut %s@%s from %s@%s (%v) — retrying on the next pass.", fork, branch, groupRepo, base, err)
		return outcome
	}
	committed, err := ops.PublishGiteaFiles(ctx, httpClient, wiring.GiteaURL, wiring.Token,
		fork, branch, base, "recipe: the tiers "+groupRepo+" lacks, from "+inputs.MateProjectName, missing)
	if err != nil {
		outcome.Line = fmt.Sprintf("could not write the recipe to %s (%v) — retrying on the next pass.", fork, err)
		return outcome
	}
	outcome.Committed = committed

	number, created, err := ops.EnsureGiteaPullRequest(ctx, httpClient, wiring.GiteaURL, wiring.Token,
		groupRepo, fork, branch, base, giteaRecipeBranchTitle)
	if err != nil {
		outcome.Line = fmt.Sprintf("the recipe is on %s@%s but proposing it to %s failed (%v) — retrying on the next pass.", fork, branch, groupRepo, err)
		return outcome
	}
	outcome.PullNumber = number
	outcome.Created = created
	if number != 0 {
		outcome.PullURL = fmt.Sprintf("%s/%s/pulls/%d", strings.TrimRight(wiring.GiteaURL, "/"), groupRepo, number)
	}
	switch {
	case created:
		outcome.Line = fmt.Sprintf("what %s@%s lacks of the group recipe (%s) is proposed as pull request #%d (from %s@%s); it only adds files, so a tier the group already has is never touched",
			groupRepo, base, strings.Join(outcome.Proposed, ", "), number, fork, branch)
	case committed:
		outcome.Line = fmt.Sprintf("the group recipe changed; pull request #%d on %s carries the update", number, groupRepo)
	}
	if len(closed) > 0 && outcome.Line != "" {
		outcome.Line += fmt.Sprintf("; this Mate's earlier proposal %s, cut from an older %s, is closed", pullNumbers(closed), base)
	}
	return outcome
}

// recipeEntries names what a set of recipe files adds, the way a person reads
// the group repo: each tier directory once, and a top-level file by its name.
func recipeEntries(files []recipe.File) []string {
	var entries []string
	for _, file := range files {
		entry, _, _ := strings.Cut(file.Path, "/")
		if !slices.Contains(entries, entry) {
			entries = append(entries, entry)
		}
	}
	sort.Strings(entries)
	return entries
}

// pullNumbers reads a list of pull-request numbers as "#9" or "#9, #11".
func pullNumbers(numbers []int) string {
	out := make([]string, 0, len(numbers))
	for _, number := range numbers {
		out = append(out, fmt.Sprintf("#%d", number))
	}
	return strings.Join(out, ", ")
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

// handleGroupRecipe is the agent's way to ask for the export outright
// (`zerops_workflow action="group-recipe"`). The reconcile already runs on
// every bootstrap and adopt pass, so this exists for the other direction: a
// person says "propose the recipe now", or wants the pull request's address
// after a service change, without waiting for the next pass.
//
// Same code path as the reconcile — there is no second composer and no second
// idempotence rule. What differs is the reporting: a pass that changed nothing
// is silent on a bootstrap response and ANSWERS here, with the pull request it
// found or the fact that main already has every tier, because being asked is
// itself a reason to say where the recipe is.
func handleGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
	liveEnvPath string,
) (*mcp.CallToolResult, any, error) {
	outcome := giteaGroupRecipeOutcome(ctx, client, httpClient, rt, stateDir, liveEnvPath)
	if outcome.Blocked != "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"The group recipe was not proposed: "+outcome.Blocked,
			"The recipe is proposed automatically once a dev pair has its Gitea repository and the Mate's Gitea variables have landed; nothing else is blocked meanwhile.",
		), WithRecoveryStatus()), nil, nil
	}

	result := map[string]any{
		"groupRepo": outcome.GroupRepo,
		"committed": outcome.Committed,
	}
	if outcome.OnMain {
		result["onMain"] = true
	}
	if outcome.Fork != "" {
		result["fork"] = outcome.Fork
	}
	if outcome.Branch != "" {
		result["branch"] = outcome.Branch
	}
	if len(outcome.Proposed) > 0 {
		result["proposed"] = outcome.Proposed
	}
	if len(outcome.Closed) > 0 {
		result["closedPullRequests"] = outcome.Closed
	}
	if outcome.PullNumber != 0 {
		result["pullRequest"] = outcome.PullNumber
		result["pullRequestUrl"] = outcome.PullURL
	}
	if len(outcome.Warnings) > 0 {
		result["warnings"] = outcome.Warnings
	}
	switch {
	case outcome.Line != "":
		result["message"] = strings.ToUpper(outcome.Line[:1]) + outcome.Line[1:] + "."
	case outcome.OnMain:
		result["message"] = fmt.Sprintf(
			"The group repo %s already carries every tier of the recipe on its main, so nothing is proposed: a tier on main is the group's, and zcp never proposes over it. To change one, open a pull request against its main — you write the group repo, and may merge the pull request when the person asks.",
			outcome.GroupRepo)
	case outcome.PullNumber != 0:
		result["message"] = fmt.Sprintf(
			"What main lacks of the group recipe (%s) is already proposed; pull request #%d on %s carries it.",
			strings.Join(outcome.Proposed, ", "), outcome.PullNumber, outcome.GroupRepo)
	default:
		result["message"] = "The group recipe is written to " + outcome.Fork + "@" + outcome.Branch +
			", but no pull request could be read or opened on " + outcome.GroupRepo + " — retry, or open it by hand."
	}
	return jsonResult(result), nil, nil
}
