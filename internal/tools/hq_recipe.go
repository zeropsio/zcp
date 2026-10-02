package tools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// recipePairMountRoot is where the Mate mounts each pair's working directory,
// one per hostname (ops.MountService). It is how the recipe reads a pair's
// files without an SSH round trip.
const recipePairMountRoot = "/var/www"

// recipeOutcome is what one pass did. The reconcile reads Line (empty when
// there is nothing worth saying on a bootstrap response); the agent-facing
// action reads the rest, because a person who ASKED for the recipe wants the
// change's address even on the pass that changed nothing.
type recipeOutcome struct {
	// Line is the reconcile's report, "" when the pass was a no-op.
	Line string
	// Branch is the branch of the change that carries the proposal, "" when
	// nothing is proposed.
	Branch string
	// Proposed names what the proposal adds to main: tier directories, and a
	// top-level file main has none of.
	Proposed []string
	// Change is the number of the Mate's change in the recipe repository, 0
	// when none is open; ChangeURL its address at HQ.
	Change    int
	ChangeURL string
	// Committed is whether this pass pushed anything; Created whether it
	// opened the change.
	Committed bool
	Created   bool
	// OnMain is true when the recipe repository's main already carries every
	// tier this Mate would propose, so nothing is.
	OnMain bool
	// Warnings are the reader's and the composer's — a runtime left out, a
	// storage policy unread, an unverified setup, an unreadable scaling shape.
	Warnings []string
	// Blocked names why nothing happened, for the action to report. Empty on
	// a pass that reached HQ.
	Blocked string
}

// reconcileGroupRecipe proposes to the application's recipe repository the
// tiers its main does not carry yet, composed from this Mate's project, as
// the Mate's own change there (SPEC §3.2c).
//
// Additive, a tier at a time (recipe.Missing). A tier on main is the
// group's — hand-written, or merged from an earlier proposal — and zcp never
// proposes over it: every Mate composes the whole app from its OWN project,
// and on 2026-09-26 a second Mate's proposal replaced the medusa group's
// hand-written tiers (project env, secrets, the production setups), was
// merged, and the next release built production with the dev setup. So a
// group's first recipe lands whole, a tier main lacks is proposed on any
// later pass, and a group whose main has every tier gets nothing.
//
// The proposal is the Mate's change in the recipe repository — the one HQ
// keeps open per repository — titled hq.RecipeProposalTitle, with no
// description: the delivery's flow, on a scratch repository of zcp's own
// rather than a pair's checkout. What the change's branch holds is composed,
// never merged: main's tree with the files main lacks written over it
// (Scratch.TreeWith), so its diff against main only ever adds — the kind
// Core lands by itself — and no conflict can arise. The branch only moves
// forward: a commit on its head, with main as a second parent once main has
// moved, so a changed composition or a moved main updates the same change.
// A group whose main has come to carry every tier while the change is still
// open gets the change brought to main's tree: it adds nothing any more,
// and Core closes an empty change.
//
// It runs on the SAME passes as the repository reconcile and right after it,
// because it needs what that produces: a pair only belongs in the recipe once
// its repository exists, since the recipe's whole job is to record which
// repository builds which runtime. Before any pair has one there is nothing
// to export and this returns silently; while a pair the project runs still
// lacks one, nothing composes and the warnings name it (groupRecipeWaits) —
// a tier proposed without that runtime would stay without it.
//
// Like the repository reconcile it is a reconcile, not a step, and nothing
// it can meet is fatal: an HQ that does not answer, a push another process
// moved the branch past, leaves a working project and a line saying so, and
// the next pass tries again. A Mate that never gets its proposal through
// loses nothing but the proposal.
//
// Idempotent on CONTENT: the recipe is composed from live state on every
// pass, and a branch that already holds the tree it would push is left as
// it is. Nothing about the proposal is stored: each pass composes it anew
// and finds the change in the Mate's state.
func reconcileGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
) string {
	return groupRecipeOutcome(ctx, client, httpClient, rt, stateDir).Line
}

// groupRecipeOutcome is the reconcile itself. Split from the reporter so the
// agent-facing action can read what happened rather than parse a sentence.
func groupRecipeOutcome(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
) recipeOutcome {
	comp, early, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir)
	if !ok {
		return early
	}
	outcome := recipeOutcome{Warnings: comp.warnings}
	open := openRecipeChange(comp.state)
	if open != nil {
		outcome.Change = open.Number
		outcome.ChangeURL = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, open.Number)
	}
	var missing []recipe.File
	written, err := writeRecipeChange(ctx, comp.hqc, comp.appID, open, hq.RecipeProposalTitle,
		"recipe: the tiers "+hq.RecipeRepo+" lacks, from "+comp.inputs.MateProjectName,
		func(_ context.Context, _ *hq.Scratch, _ string, paths []string) (map[string]string, error) {
			missing = recipe.Missing(comp.files, paths)
			added := make(map[string]string, len(missing))
			for _, file := range missing {
				added[file.Path] = file.Body
			}
			return added, nil
		})
	if err != nil {
		outcome.Line = err.Error() + " — retrying on the next pass."
		return outcome
	}
	outcome.Proposed = recipeEntries(missing)
	outcome.OnMain = len(missing) == 0
	if written.other != nil {
		// The Mate's open change there is another proposal of its own, and
		// HQ keeps one per repository: it is left as it is.
		outcome.Change, outcome.ChangeURL = 0, ""
		if !outcome.OnMain {
			outcome.Line = fmt.Sprintf("what main of the recipe repository %q lacks (%s) waits for this Mate's change #%d there (%q) to be merged or closed: HQ keeps one open change per Mate per repository",
				hq.RecipeRepo, strings.Join(outcome.Proposed, ", "), written.other.Number, written.other.Title)
		}
		return outcome
	}
	outcome.Committed = written.pushed
	if written.number != 0 {
		outcome.Change, outcome.Created = written.number, written.created
		outcome.ChangeURL = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
		outcome.Branch = comp.hqc.ChangeBranch(written.number)
	}
	switch {
	case outcome.Created:
		outcome.Line = fmt.Sprintf("what main of the recipe repository %q lacks (%s) is proposed as change #%d (%s); it only adds files, so a tier the group already has is never touched",
			hq.RecipeRepo, strings.Join(outcome.Proposed, ", "), outcome.Change, outcome.ChangeURL)
	case outcome.OnMain && outcome.Committed:
		outcome.Line = fmt.Sprintf("main of the recipe repository %q already carries every tier of the recipe, so this Mate's proposal, change #%d (%s), now adds nothing",
			hq.RecipeRepo, outcome.Change, outcome.ChangeURL)
	case outcome.Committed:
		outcome.Line = fmt.Sprintf("the group recipe changed; change #%d (%s) carries the update", outcome.Change, outcome.ChangeURL)
	}
	return outcome
}

// groupRecipeComposition is what the additive reconcile and a scaling
// proposal both know before they touch the recipe repository: HQ, the
// application, the Mate's state there, and the recipe composed from this
// Mate's project.
type groupRecipeComposition struct {
	hqc      hq.Client
	appID    string
	state    hq.MateState
	inputs   bundle.GroupRecipeInputs
	files    []recipe.File
	warnings []string
}

// composeGroupRecipe composes the application's recipe from this Mate's live
// project. ok is false when something stops it, with the outcome saying
// what (Blocked, or a Line for the reconcile).
func composeGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
) (groupRecipeComposition, recipeOutcome, bool) {
	if !rt.InContainer || client == nil || httpClient == nil {
		return groupRecipeComposition{}, recipeOutcome{Blocked: "the group recipe is a Mate's act — there is no Mate environment here"}, false
	}
	hqc, enrolled := openHQ(httpClient)
	if !enrolled {
		return groupRecipeComposition{}, recipeOutcome{Blocked: "this Mate is not enrolled with its HQ yet, so there is no recipe repository to propose to"}, false
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return groupRecipeComposition{}, recipeOutcome{Blocked: "no service has been bootstrapped yet, so there is nothing to export"}, false
	}
	if !slices.ContainsFunc(metas, func(m *workflow.ServiceMeta) bool { return m.IsComplete() && hqPairWired(m) }) {
		return groupRecipeComposition{}, recipeOutcome{Blocked: "no pair has its repository in HQ yet — the recipe names what builds each runtime, so it waits for them"}, false
	}

	repo, err := hqc.EnsureRepo(ctx, hq.RecipeRepo)
	if err != nil {
		return groupRecipeComposition{}, recipeOutcome{Line: fmt.Sprintf("the group recipe is not proposed yet (could not reach the recipe repository %q in HQ: %v) — retrying on the next pass.", hq.RecipeRepo, err)}, false
	}
	// Only pairs the repository pass has given a repository in the recipe's
	// application: a runtime with none has no buildFromGit to name.
	var wired []*workflow.ServiceMeta
	for _, m := range metas {
		if m.IsComplete() && hqPairWired(m) && m.HQ.AppID == repo.AppID {
			wired = append(wired, m)
		}
	}
	sort.Slice(wired, func(i, j int) bool { return wired[i].Hostname < wired[j].Hostname })

	// The Mate's state names the application, and its own change in the
	// recipe repository when one is open: what a proposal moves forward
	// rather than opens again.
	state, err := hqc.Self(ctx)
	if err != nil {
		return groupRecipeComposition{}, recipeOutcome{Line: fmt.Sprintf("could not read this Mate's changes in HQ to propose the recipe (%v) — retrying on the next pass.", err)}, false
	}
	var outcome recipeOutcome
	inputs, readWarnings, err := composeGroupRecipeInputs(ctx, client, rt.ProjectID, recipeName(state.AppName, repo.AppID),
		recipePairMountRoot, hqc.Address(), repo.AppID, metas, wired)
	outcome.Warnings = append(outcome.Warnings, readWarnings...)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe is not proposed yet (%v) — retrying on the next pass.", err)
		return groupRecipeComposition{}, outcome, false
	}
	// This composes unattended, with nobody to classify a variable the way the
	// export and launch flows ask the agent to, so the composer decides each
	// one itself: config as written, a secret as a generator — never its value.
	layout, warnings, err := bundle.BuildGroupRecipe(inputs)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return groupRecipeComposition{}, outcome, false
	}
	outcome.Warnings = append(outcome.Warnings, warnings...)
	files, err := recipe.Build(layout)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return groupRecipeComposition{}, outcome, false
	}
	return groupRecipeComposition{hqc: hqc, appID: repo.AppID, state: state, inputs: inputs, files: files, warnings: outcome.Warnings}, outcome, true
}

// openRecipeChange is the Mate's open change in the recipe repository, nil
// when none is open.
func openRecipeChange(state hq.MateState) *hq.MateChange {
	for i := range state.Changes {
		if c := &state.Changes[i]; c.Repo == hq.RecipeRepo && c.State == hq.ChangeOpen {
			return c
		}
	}
	return nil
}

// recipeWrite is what a write to the Mate's change in the recipe repository
// did: the change that carries it (0 when none is), whether this call opened
// it, whether anything was pushed, and — when the Mate's open change there is
// another of its proposals, under another title — that change, left as it
// is: HQ keeps one open change per Mate per repository.
type recipeWrite struct {
	number  int
	created bool
	pushed  bool
	other   *hq.Change
}

// recipeFiles answers what a proposal writes over main's tree, path to body,
// from main as the scratch fetched it (its head, and the paths it holds).
type recipeFiles func(ctx context.Context, scratch *hq.Scratch, main string, paths []string) (map[string]string, error)

// writeRecipeChange brings the Mate's change titled title in the recipe
// repository to main's tree with what files writes over it, opening the
// change when that differs from main and none is open, and pushing its
// branch forward when what it holds differs. The branch only moves forward:
// a commit on its head, with main as a second parent once main has moved;
// written as main has it, the change adds nothing. A Mate's open change
// there under another title is another proposal of its own, and it is left
// as it is (recipeWrite.other). An error reads as the line's cause.
func writeRecipeChange(ctx context.Context, hqc hq.Client, appID string, open *hq.MateChange, title, message string, files recipeFiles) (recipeWrite, error) {
	scratch, err := hqc.OpenScratch(ctx, appID, hq.RecipeRepo)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not prepare the recipe's scratch repository (%w)", err)
	}
	defer scratch.Close()
	main, found, err := scratch.Fetch(ctx, hqBase)
	switch {
	case err != nil:
		return recipeWrite{}, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hqBase, hq.RecipeRepo, err)
	case !found:
		return recipeWrite{}, fmt.Errorf("the recipe repository %q has no %q yet, so there is nothing to propose the recipe to", hq.RecipeRepo, hqBase)
	}
	paths, err := scratch.Paths(ctx, main)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hqBase, hq.RecipeRepo, err)
	}
	written, err := files(ctx, scratch, main, paths)
	if err != nil {
		return recipeWrite{}, err
	}
	tree, err := scratch.TreeWith(ctx, main, written)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not compose the recipe's tree (%w)", err)
	}
	mainTree, err := scratch.Tree(ctx, main)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hqBase, hq.RecipeRepo, err)
	}
	if open == nil && tree == mainTree {
		return recipeWrite{}, nil
	}

	// HQ answers the Mate's open change rather than opening a second one,
	// so asking is how a write learns whether the open one is its own.
	opened, err := hqc.OpenChange(ctx, hq.RecipeRepo, title)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not open the Mate's change in the recipe repository %q (%w)", hq.RecipeRepo, err)
	}
	if !opened.Created && opened.Change.Title != title {
		return recipeWrite{other: &opened.Change}, nil
	}
	write := recipeWrite{number: opened.Change.Number, created: opened.Created}
	head := ""
	if !opened.Created {
		if head, _, err = scratch.Fetch(ctx, hqc.ChangeBranch(write.number)); err != nil {
			return recipeWrite{}, fmt.Errorf("could not read change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
		}
	}
	parents, err := recipeParents(ctx, scratch, head, main, tree)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not read change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
	}
	if parents == nil {
		return write, nil
	}
	commit, err := scratch.Commit(ctx, hq.CommitSpec{Tree: tree, Parents: parents, Message: message,
		Name: ops.DeployGitIdentity.Name, Email: ops.DeployGitIdentity.Email})
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not commit the recipe (%w)", err)
	}
	if err := scratch.Push(ctx, commit, hqc.ChangeBranch(write.number)); err != nil {
		return recipeWrite{}, fmt.Errorf("could not push the recipe to change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
	}
	write.pushed = true
	return write, nil
}

// recipeParents are the parents of the commit that brings the change's
// branch, at head ("" when it has none yet), to tree: main alone for a
// branch not pushed yet; head, and main too once main is no ancestor of it.
// Nil when head already holds tree on top of main — nothing to push.
func recipeParents(ctx context.Context, scratch *hq.Scratch, head, main, tree string) ([]string, error) {
	if head == "" {
		return []string{main}, nil
	}
	onMain, err := scratch.IsAncestor(ctx, main, head)
	if err != nil {
		return nil, err
	}
	held, err := scratch.Tree(ctx, head)
	if err != nil {
		return nil, err
	}
	switch {
	case onMain && held == tree:
		return nil, nil
	case onMain:
		return []string{head}, nil
	}
	return []string{head, main}, nil
}

// recipeName names the recipe, and the stage and production projects its
// tiers create: the slug of the application's name, or its id while HQ
// names it nothing.
func recipeName(appName *string, appID string) string {
	if appName == nil || strings.TrimSpace(*appName) == "" {
		return appID
	}
	return recipeSlug(*appName)
}

// recipeSlug is main's group-slug rule (client-runtime groupRegistry.ts
// groupSlugBase): the name in lowercase ASCII letters and digits, accents
// dropped, every other run a dash; prefixed "group-" when it does not start
// with a letter, cut to 30 never ending on a dash, and "group" when under 2.
func recipeSlug(name string) string {
	const fallback, maxLen, minLen = "group", 30, 2
	var b strings.Builder
	dash := false
	for _, r := range norm.NFKD.String(name) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" || slug[0] < 'a' || slug[0] > 'z' {
		slug = fallback + "-" + slug
	}
	slug = strings.TrimRight(slug[:min(len(slug), maxLen)], "-")
	if len(slug) < minLen {
		return fallback
	}
	return slug
}

// recipeEntries names what a set of recipe files adds, the way a person reads
// the recipe repository: each tier directory once, and a top-level file by
// its name.
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

// handleGroupRecipe is the agent's way to ask for the recipe outright
// (`zerops_workflow action="group-recipe"`). The reconcile already runs on
// every bootstrap and adopt pass, so this exists for the other direction: a
// person says "propose the recipe now", or wants the change's address after
// a service change, without waiting for the next pass.
//
// Same code path as the reconcile — there is no second composer and no second
// idempotence rule. What differs is the reporting: a pass that changed nothing
// is silent on a bootstrap response and ANSWERS here, with the change it
// found or the fact that main already has every tier, because being asked is
// itself a reason to say where the recipe is.
func handleGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
) (*mcp.CallToolResult, any, error) {
	outcome := groupRecipeOutcome(ctx, client, httpClient, rt, stateDir)
	if outcome.Blocked != "" {
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"The group recipe was not proposed: "+outcome.Blocked,
			"The recipe is proposed automatically once a dev pair has its repository in HQ; nothing else is blocked meanwhile.",
		), WithRecoveryStatus()), nil, nil
	}

	result := map[string]any{
		"groupRepo": hq.RecipeRepo,
		"committed": outcome.Committed,
	}
	if outcome.OnMain {
		result["onMain"] = true
	}
	if outcome.Branch != "" {
		result["branch"] = outcome.Branch
	}
	if len(outcome.Proposed) > 0 {
		result["proposed"] = outcome.Proposed
	}
	if outcome.Change != 0 {
		result["change"] = outcome.Change
		result["changeUrl"] = outcome.ChangeURL
	}
	if len(outcome.Warnings) > 0 {
		result["warnings"] = outcome.Warnings
	}
	switch {
	case outcome.Line != "":
		result["message"] = strings.ToUpper(outcome.Line[:1]) + outcome.Line[1:] + "."
	case outcome.OnMain:
		result["message"] = fmt.Sprintf(
			"Main of the recipe repository %q already carries every tier of the recipe, so nothing is proposed: a tier on main is the group's, and zcp never proposes over it. A person changes a tier in HQ.",
			hq.RecipeRepo)
	default:
		result["message"] = fmt.Sprintf(
			"What main lacks of the group recipe (%s) is already proposed; change #%d (%s) carries it.",
			strings.Join(outcome.Proposed, ", "), outcome.Change, outcome.ChangeURL)
	}
	return jsonResult(result), nil, nil
}
