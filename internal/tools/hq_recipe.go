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

// recipeLanding says how a Mate's change in the recipe repository reaches
// main (HQ's landing rule, mate-hq spec/protocol.md S16b; spec-mate D37):
// Core lands it by itself, adds or edits alike, unless its merge would
// import, stop or start a service in an environment made from a tier — that
// one waits for a person with recipe_changes_environments. Said after a
// proposal is made; a steer names the call and leaves the landing to it.
const recipeLanding = "HQ lands the change on main by itself; one whose merge would import, stop or start a service in a stage or production environment made from a tier waits for a person to merge it in Review"

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
	// Proposed names what the proposal writes to main: tier directories, a
	// top-level file main has none of, and a tier zcp proposes anew.
	Proposed []string
	// Replaced names the tier directories among Proposed that main carries
	// and zcp proposes anew: its own, from this Mate, whose shape changed
	// (recipeReplacements).
	Replaced []string
	// Change is the number of the Mate's change in the recipe repository, 0
	// when none is open; ChangeURL its address at HQ.
	Change    int
	ChangeURL string
	// Committed is whether this pass pushed anything; Created whether it
	// opened the change.
	Committed bool
	Created   bool
	// OnMain is true when the recipe repository's main already carries every
	// tier this Mate would propose, and none of zcp's own there needs
	// proposing anew, so nothing is.
	OnMain bool
	// Warnings are the reader's and the composer's — a runtime left out, a
	// storage policy unread, an unverified setup, an unreadable scaling shape.
	Warnings []string
	// Blocked names why nothing happened, for the action to report. Empty on
	// a pass that reached HQ.
	Blocked string
	// Required is the step the Mate's setup is not done without: a tier main
	// lacks that the recipe cannot propose, what is missing and the call that
	// fixes it (recipeRequiredStep). Empty when nothing is.
	Required []string
	// decided is a pass that composed against main — whether or not the
	// recipe composed — so Required is its verdict, and recorded.
	decided bool
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
// The proposal is the Mate's change in the recipe repository — its one
// change there opened naming no branch — titled hq.RecipeProposalTitle, with no
// description, composed on a scratch repository of zcp's own rather than a
// pair's checkout. What the change's branch holds is composed,
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
// it can meet is fatal: an HQ that does not answer, another pass still
// running when this one's call ends, a push that moved the branch past this
// one's, leaves a working project and a line saying so, and the next pass
// tries again. A Mate that never gets its proposal through loses nothing but
// the proposal. zcp's start in a Mate is one more pass (GroupRecipeAtStart).
//
// Idempotent on CONTENT: the recipe is composed from live state on every
// pass, and a branch that already holds the tree it would push is left as
// it is. Nothing about the proposal is stored: each pass composes it anew
// and finds the change in the Mate's state. That read is good only until
// another pass writes, so passes run one at a time on the Mate, across every
// zcp it runs (holdRecipePass): the second waits for the first and reads HQ
// afresh — main carrying what the first landed, or the first's change —
// rather than proposing onto a main the first already changed.
func reconcileGroupRecipe(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir string,
) string {
	return groupRecipeOutcome(ctx, client, httpClient, rt, stateDir).Line
}

// GroupRecipeAtStart is the recipe pass zcp's start in a Mate runs
// (server.New), off the startup path: a recipe no earlier pass got through
// goes out without waiting for the next bootstrap or adopt. A start comes
// with every agent session and nobody asked for it (groupRecipeUnasked).
func GroupRecipeAtStart(ctx context.Context, client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) string {
	return groupRecipeUnasked(ctx, client, httpClient, rt, stateDir)
}

// groupRecipeUnasked is a recipe pass nobody asked for — zcp's start in a
// Mate, a tool call that changed what the recipe reads (RecipeFollower) — so
// a proposal a person closed unmerged is not opened again here; bootstrap,
// adopt and the agent's ask still propose it. A pass with nothing to compose
// asks HQ nothing.
func groupRecipeUnasked(ctx context.Context, client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) string {
	hqc, _, _, ready := recipePassReady(client, httpClient, rt, stateDir)
	if !ready {
		return ""
	}
	if state, err := hqc.Self(ctx); err == nil && recipeDeclinedAtStart(ctx, hqc, state) {
		return ""
	}
	return reconcileGroupRecipe(ctx, client, httpClient, rt, stateDir)
}

// recipeDeclinedAtStart is recipeDeclined before the start's pass composes
// anything: main of the recipe repository is fetched only for a closed
// proposal a merge came before, the one case git has to tell. A main that
// cannot be read leaves the close a decline.
func recipeDeclinedAtStart(ctx context.Context, hqc hq.Client, state hq.MateState) bool {
	closed := recipeClosedProposal(state, hq.RecipeProposalTitle)
	switch {
	case closed == nil:
		return false
	case state.AppID == nil || len(recipeEarlierMerges(state, *closed)) == 0:
		return true
	}
	scratch, main, _, err := openRecipeMain(ctx, hqc, *state.AppID)
	if err != nil {
		return true
	}
	defer scratch.Close()
	return recipeDeclined(ctx, hqc, scratch, main, state, hq.RecipeProposalTitle)
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
	// A pass that stops before asking HQ anything takes no turn and writes
	// nothing: every zcp start in a Mate is one.
	if _, _, blocked, ready := recipePassReady(client, httpClient, rt, stateDir); !ready {
		return blocked
	}
	release, err := holdRecipePass(ctx, stateDir)
	if err != nil {
		return recipeOutcome{Line: recipePassHeldLine(err)}
	}
	defer release()
	comp, early, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir)
	if early.decided {
		writeRecipeRequired(stateDir, early.Required)
	}
	if !ok {
		return early
	}
	defer comp.close()
	outcome := recipeOutcome{Warnings: comp.warnings, Required: early.Required}
	open := openRecipeChange(comp.state)
	if open != nil {
		outcome.Change = open.Number
		outcome.ChangeURL = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, open.Number)
	}
	var missing, replaced []recipe.File
	written, err := writeRecipeChange(ctx, comp, open, hq.RecipeProposalTitle,
		"recipe: the tiers "+hq.RecipeRepo+" lacks, or zcp wrote from this Mate before its project changed shape, from "+comp.inputs.MateProjectName,
		func(ctx context.Context, _ *hq.Scratch, _ string, paths []string) (map[string]string, error) {
			missing = recipe.Missing(comp.files, paths)
			// What main lacks goes first and alone: a tier HQ sees for the
			// first time imports nothing, so a change that only adds tiers
			// changes no environment and Core lands it by itself, never held
			// back beside a tier proposed anew whose merge would. A tier of
			// zcp's own to propose anew follows once main has every tier —
			// unless a person closed such a proposal unmerged and no recipe
			// change of this Mate's merged since.
			if len(missing) == 0 && !recipeDeclined(ctx, comp.hqc, comp.scratch, comp.main, comp.state, hq.RecipeProposalTitle) {
				var err error
				if replaced, err = recipeReplacements(ctx, comp); err != nil {
					return nil, err
				}
				// One the change already proposes in that shape is pushed
				// again only for a new one.
				if replaced, err = recipeHeldReplacements(ctx, comp, open, replaced); err != nil {
					return nil, err
				}
			}
			written := make(map[string]string, len(missing)+len(replaced))
			for _, file := range append(append([]recipe.File(nil), missing...), replaced...) {
				written[file.Path] = file.Body
			}
			return written, nil
		})
	if err != nil {
		outcome.Line = err.Error() + " — retrying on the next pass."
		return outcome
	}
	if written.edited {
		// The change is the Mate's agent's or a person's now: zcp proposes
		// nothing on it, and says so on every pass while it stays open.
		outcome.Change, outcome.Branch = written.number, written.branch
		outcome.ChangeURL = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
		outcome.Line = recipeEditedLine(outcome.Change, outcome.ChangeURL)
		return outcome
	}
	outcome.Proposed = recipeEntries(append(append([]recipe.File(nil), missing...), replaced...))
	outcome.Replaced = recipeEntries(replaced)
	outcome.OnMain = len(missing) == 0 && len(replaced) == 0
	if written.other != nil {
		// The Mate's open change there is another proposal of its own, and
		// this Mate keeps one recipe proposal open at a time: it is left as
		// it is.
		outcome.Change, outcome.ChangeURL = 0, ""
		if !outcome.OnMain {
			outcome.Line = fmt.Sprintf("what main of the recipe repository %q lacks (%s) waits for this Mate's change #%d there (%q) to be merged or closed: this Mate keeps one recipe proposal open at a time",
				hq.RecipeRepo, strings.Join(outcome.Proposed, ", "), written.other.number, written.other.title)
		}
		return outcome
	}
	outcome.Committed = written.pushed
	if written.number != 0 {
		outcome.Change, outcome.Created = written.number, written.created
		outcome.ChangeURL = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
		outcome.Branch = written.branch
	}
	switch {
	case outcome.Created && len(replaced) > 0:
		outcome.Line = fmt.Sprintf("the tiers zcp wrote from this Mate on main of the recipe repository %q (%s) no longer describe its project — the services they create changed — so change #%d (%s) proposes them anew, and a tier a person or another Mate wrote or changed is left as it is. %s",
			hq.RecipeRepo, strings.Join(outcome.Replaced, ", "), outcome.Change, outcome.ChangeURL, recipeLanding)
	case outcome.Created:
		outcome.Line = fmt.Sprintf("what main of the recipe repository %q lacks (%s) is proposed as change #%d (%s); it only adds files, so a tier the group already has is never touched",
			hq.RecipeRepo, strings.Join(outcome.Proposed, ", "), outcome.Change, outcome.ChangeURL)
	case outcome.OnMain && outcome.Committed:
		outcome.Line = fmt.Sprintf("main of the recipe repository %q already carries every tier of the recipe, so this Mate's proposal, change #%d (%s), now adds nothing",
			hq.RecipeRepo, outcome.Change, outcome.ChangeURL)
	case outcome.Committed:
		outcome.Line = fmt.Sprintf("the group recipe changed; change #%d (%s) carries the update", outcome.Change, outcome.ChangeURL)
	}
	// A part the tiers on main lack or still carry, and a managed service
	// they lack, is the targeted proposal's, never this one's
	// (hq_recipe_parts.go, hq_recipe_managed.go): said, with its call — but
	// not beside tiers this change proposes anew, which carry them already.
	if len(replaced) > 0 {
		return outcome
	}
	for _, steerOf := range []func(context.Context, groupRecipeComposition) (string, error){recipePartSteer, recipeManagedSteer} {
		steer, err := steerOf(ctx, comp)
		switch {
		case err != nil:
			outcome.Warnings = append(outcome.Warnings, err.Error())
		case steer != "" && outcome.Line != "":
			outcome.Line += "; " + steer
		case steer != "":
			outcome.Line = steer
		}
	}
	return outcome
}

// groupRecipeComposition is what the additive reconcile and a targeted
// proposal both know before they write to the recipe repository: HQ, the
// application, the Mate's state there, main of the recipe repository as a
// scratch fetched it, and the recipe composed from this Mate's project with
// the hostnames main's tiers hold. close releases the scratch.
type groupRecipeComposition struct {
	hqc     hq.Client
	appID   string
	state   hq.MateState
	scratch *hq.Scratch
	main    string
	paths   []string
	// mateID is this Mate's project, which zcp's mark on a tier names;
	// owned the tier files on main zcp wrote from it and nobody changed
	// since (recipeOwnedTiers).
	mateID string
	owned  map[string]bool
	// stateDir is zcp's state directory, where it keeps what it last pushed
	// on each recipe change (recipePushedDir).
	stateDir string
	// retiredDevs are the dev services a pool took over (a pool-keyed
	// pair's migratedFrom): a tier the group keeps may still name one as its
	// dev half, which no targeted proposal removes.
	retiredDevs map[string]bool
	inputs      bundle.GroupRecipeInputs
	files       []recipe.File
	warnings    []string
}

func (c groupRecipeComposition) close() { c.scratch.Close() }

// openRecipeMain fetches main of the recipe repository into a scratch: its
// head and the paths it holds. A recipe repository HQ made with an
// application has no main until its first recipe lands: its head is then ""
// and it holds nothing, so the first recipe is proposed whole on a root
// commit. An error reads as a line's cause.
func openRecipeMain(ctx context.Context, hqc hq.Client, appID string) (*hq.Scratch, string, []string, error) {
	scratch, err := hqc.OpenScratch(ctx, appID, hq.RecipeRepo)
	if err != nil {
		return nil, "", nil, fmt.Errorf("could not prepare the recipe's scratch repository (%w)", err)
	}
	main, _, err := scratch.Fetch(ctx, hqBase)
	if err != nil {
		scratch.Close()
		return nil, "", nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hqBase, hq.RecipeRepo, err)
	}
	paths, err := scratch.Paths(ctx, main)
	if err != nil {
		scratch.Close()
		return nil, "", nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hqBase, hq.RecipeRepo, err)
	}
	return scratch, main, paths, nil
}

// recipeHeld is what the group tiers on main hold of the app's parts: the
// hostname each gave a part when it entered it (spec-mate §2.8a.5), which the
// composer reads back rather than derives again. A tier zcp owns there
// (owned) holds nothing: it is zcp's own composition, named by the rule it
// still follows, and holding its names would keep a name an older rule gave
// (a part named after its repository) in the tier proposed in its place.
func recipeHeld(ctx context.Context, scratch *hq.Scratch, main string, owned map[string]bool) ([]bundle.HeldRuntime, error) {
	var held []bundle.HeldRuntime
	for _, tier := range []string{hq.RecipeTierStage, hq.RecipeTierProduction} {
		if owned[hq.RecipeTierPaths[tier]] {
			continue
		}
		body, found, err := scratch.File(ctx, main, hq.RecipeTierPaths[tier])
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", hq.RecipeTierPaths[tier], hq.RecipeRepo, err)
		}
		if found {
			held = append(held, bundle.HeldRuntimes(body)...)
		}
	}
	return held, nil
}

// recipePassReady is what a recipe pass knows before it asks HQ anything:
// whether this is a Mate linked to its HQ with a pair whose repository is
// there — with none, there is nothing to compose — and, when it is, its HQ
// client and the service records. blocked says why not.
func recipePassReady(client platform.Client, httpClient ops.HTTPDoer, rt runtime.Info, stateDir string) (hqc hq.Client, metas []*workflow.ServiceMeta, blocked recipeOutcome, ready bool) {
	if !rt.InContainer || !rt.MateEnabled || client == nil || httpClient == nil {
		return hq.Client{}, nil, recipeOutcome{Blocked: "the group recipe is a Mate's act — there is no Mate environment here"}, false
	}
	hqc, linked := openHQ(httpClient, rt)
	if !linked {
		return hq.Client{}, nil, recipeOutcome{Blocked: "this Mate is not linked to its HQ yet, so there is no recipe repository to propose to"}, false
	}
	metas, err := workflow.ListServiceMetas(stateDir)
	if err != nil || len(metas) == 0 {
		return hq.Client{}, nil, recipeOutcome{Blocked: "no service has been bootstrapped yet, so there is nothing to export"}, false
	}
	if !slices.ContainsFunc(metas, func(m *workflow.ServiceMeta) bool { return m.IsComplete() && hqPairWired(m) }) {
		return hq.Client{}, nil, recipeOutcome{Blocked: "no pair has its repository in HQ yet — the recipe names what builds each runtime, so it waits for them"}, false
	}
	return hqc, metas, recipeOutcome{}, true
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
	hqc, metas, blocked, ready := recipePassReady(client, httpClient, rt, stateDir)
	if !ready {
		return groupRecipeComposition{}, blocked, false
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
		recipePairMountRoot, hqc.Address(), hqc.OrgID(), repo.AppID, metas, wired, recipeMainYAML(hqc, repo.AppID, stateDir))
	outcome.Warnings = append(outcome.Warnings, readWarnings...)
	if err != nil {
		outcome.Line = fmt.Sprintf("the group recipe is not proposed yet (%v) — retrying on the next pass.", err)
		return groupRecipeComposition{}, outcome, false
	}
	scratch, main, paths, err := openRecipeMain(ctx, hqc, repo.AppID)
	if err != nil {
		outcome.Line = err.Error() + " — retrying on the next pass."
		return groupRecipeComposition{}, outcome, false
	}
	comp := groupRecipeComposition{hqc: hqc, appID: repo.AppID, state: state, scratch: scratch, main: main, paths: paths, mateID: rt.ProjectID, stateDir: stateDir, retiredDevs: map[string]bool{}}
	for _, m := range wired {
		if m.DevIsPool() && m.DevPool.MigratedFrom != nil {
			comp.retiredDevs[m.DevPool.MigratedFrom.Hostname] = true
		}
	}
	if comp.owned, err = recipeOwnedTiers(ctx, scratch, main, comp.mateID); err != nil {
		comp.close()
		outcome.Line = err.Error() + " — retrying on the next pass."
		return groupRecipeComposition{}, outcome, false
	}
	if inputs.Held, err = recipeHeld(ctx, scratch, main, comp.owned); err != nil {
		comp.close()
		outcome.Line = err.Error() + " — retrying on the next pass."
		return groupRecipeComposition{}, outcome, false
	}
	// This composes unattended, with nobody to classify a variable the way the
	// export and launch flows ask the agent to, so the composer decides each
	// one itself: config as written, a secret as a generator — never its value.
	layout, warnings, err := bundle.BuildGroupRecipe(inputs)
	// A tier main lacks that the recipe cannot propose is a step the
	// Mate's setup is not done without: required, until a pass proposes it.
	outcome.decided = true
	outcome.Required = recipeRequired(inputs, layout, paths)
	if err != nil {
		comp.close()
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return groupRecipeComposition{}, outcome, false
	}
	outcome.Warnings = append(outcome.Warnings, warnings...)
	files, err := recipe.Build(layout)
	if err != nil {
		comp.close()
		outcome.Line = fmt.Sprintf("the group recipe does not compose yet (%v).", err)
		return groupRecipeComposition{}, outcome, false
	}
	comp.inputs, comp.files, comp.warnings = inputs, files, outcome.Warnings
	return comp, outcome, true
}

// recipeRequired is the required step for the tiers main (holding paths)
// lacks and layout does not compose — withheld by a part whose stage setup
// nothing tells (bundle.GroupRecipeBlockers) — none when main has them, the
// recipe composes them, or nothing names why.
func recipeRequired(inputs bundle.GroupRecipeInputs, layout recipe.Layout, paths []string) []string {
	composed := map[string]bool{}
	for _, tier := range layout.Tiers {
		composed[fmt.Sprintf("%d — %s", tier.Index, tier.Title)] = true
	}
	var withheld []string
	for _, dir := range recipeTiersLacking(paths) {
		if !composed[dir] {
			withheld = append(withheld, dir)
		}
	}
	blockers := bundle.GroupRecipeBlockers(inputs)
	if len(withheld) == 0 || len(blockers) == 0 {
		return nil
	}
	pooled := map[string]bool{}
	for _, r := range inputs.Runtimes {
		pooled[r.DevHostname] = r.DevPool
	}
	return []string{recipeRequiredStep(blockers, pooled, withheld)}
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
// did: the change that carries it (0 when none is) and the branch HQ binds it
// to, whether this call opened it, whether anything was pushed, and — when the Mate's open change there is
// another of its proposals, under another title — that change, left as it
// is: this Mate keeps one recipe proposal open at a time, its one change
// opened naming no branch — proposals touch the same tier files, so two at
// once would only conflict.
type recipeWrite struct {
	number  int
	branch  string
	created bool
	pushed  bool
	other   *otherRecipeChange
	// edited is a change whose branch holds a commit zcp did not write — the
	// Mate's agent's or a person's — and nothing was pushed onto it.
	edited bool
}

// otherRecipeChange is the Mate's open change in the recipe repository that
// a write is not for: its number and its title.
type otherRecipeChange struct {
	number int
	title  string
}

// recipeFiles answers what a proposal writes over main's tree, path to body,
// from main as the scratch fetched it (its head, and the paths it holds).
type recipeFiles func(ctx context.Context, scratch *hq.Scratch, main string, paths []string) (map[string]string, error)

// writeRecipeChange brings the Mate's change titled title in the recipe
// repository to main's tree with what files writes over it, opening the
// change when that differs from main and none is open, and pushing its
// branch forward when that differs from what zcp last pushed there. The
// branch only moves forward: a commit on its head, with main as a second
// parent once main has moved; written as main has it, the change adds
// nothing. A branch holding anything but what zcp last pushed is someone
// else's, and nothing is pushed onto it (recipeWrite.edited,
// recipeChangeIsZcps). A Mate's open change there under another title is
// another proposal of its own, and it is left as it is (recipeWrite.other).
// An error reads as the line's cause.
func writeRecipeChange(ctx context.Context, comp groupRecipeComposition, open *hq.MateChange, title, message string, files recipeFiles) (recipeWrite, error) {
	hqc, scratch, main := comp.hqc, comp.scratch, comp.main
	written, err := files(ctx, scratch, main, comp.paths)
	if err != nil {
		return recipeWrite{}, err
	}
	// A tier zcp owns stays its own through zcp's own edits of it — a
	// part's, a managed service's, a scale's: its mark follows the edit.
	for path, body := range written {
		if comp.owned[path] {
			written[path] = bundle.StampTier(body, comp.mateID)
		}
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

	write, err := ownRecipeChange(ctx, hqc, open, title)
	if err != nil || write.other != nil {
		return write, err
	}
	head := ""
	if !write.created {
		if head, _, err = scratch.Fetch(ctx, write.branch); err != nil {
			return recipeWrite{}, fmt.Errorf("could not read change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
		}
	}
	// The branch is zcp's only while it holds what zcp last pushed there
	// (hq_recipe_respect.go): anyone else's commit makes the change theirs.
	if head != "" {
		own, err := recipeChangeIsZcps(ctx, comp, write.number, head)
		if err != nil {
			return recipeWrite{}, fmt.Errorf("could not read change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
		}
		if !own {
			write.edited = true
			return write, nil
		}
	}
	parents, push, err := recipeParents(ctx, scratch, head, main, tree)
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not read change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
	}
	if !push {
		// The branch holds tree already: kept as what zcp proposed, for a
		// change no record named yet.
		if pushed, ok := readRecipePushed(comp.stateDir, comp.appID, write.number); !ok || pushed != tree {
			writeRecipePushed(comp.stateDir, comp.appID, write.number, tree)
		}
		return write, nil
	}
	commit, err := scratch.Commit(ctx, hq.CommitSpec{Tree: tree, Parents: parents, Message: message,
		Name: ops.DeployGitIdentity.Name, Email: ops.DeployGitIdentity.Email})
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not commit the recipe (%w)", err)
	}
	if err := scratch.Push(ctx, commit, write.branch); err != nil {
		return recipeWrite{}, fmt.Errorf("could not push the recipe to change #%d of the recipe repository %q (%w)", write.number, hq.RecipeRepo, err)
	}
	writeRecipePushed(comp.stateDir, comp.appID, write.number, tree)
	write.pushed = true
	return write, nil
}

// ownRecipeChange is the Mate's change titled title in the recipe repository
// a write goes on: open, the Mate's open change there, when its title is
// title; opened when none is open; and the open one as the other change when
// its title is another. A write goes on the branch HQ binds the change to — a
// change handed to the Mate stays on its gone Mate's.
func ownRecipeChange(ctx context.Context, hqc hq.Client, open *hq.MateChange, title string) (recipeWrite, error) {
	if open != nil && open.Title != nil {
		if *open.Title != title {
			return recipeWrite{other: &otherRecipeChange{number: open.Number, title: *open.Title}}, nil
		}
		return recipeWrite{number: open.Number, branch: hqc.BranchOf(open.Number, open.Branch)}, nil
	}
	// No open change, or an HQ whose state names no title: HQ updates are
	// deferred, so a Mate meets an HQ from before titles for a while. HQ
	// answers an opener naming no branch with the Mate's one change opened
	// so — its default change — rather than opening a second one, so asking
	// to open is how such a write learns whether the open one is its own.
	opened, err := hqc.OpenChange(ctx, hq.RecipeRepo, title, "")
	if err != nil {
		return recipeWrite{}, fmt.Errorf("could not open the Mate's change in the recipe repository %q (%w)", hq.RecipeRepo, err)
	}
	if !opened.Created && opened.Change.Title != title {
		return recipeWrite{other: &otherRecipeChange{number: opened.Change.Number, title: opened.Change.Title}}, nil
	}
	return recipeWrite{number: opened.Change.Number, branch: hqc.BranchOf(opened.Change.Number, opened.Change.Branch), created: opened.Created}, nil
}

// recipeParents are the parents of the commit that brings the change's
// branch, at head ("" when it has none yet), to tree: main alone for a
// branch not pushed yet, none while main has no commit ("") either; head,
// and main too once main is no ancestor of it. push is false when head
// already holds tree on top of main — nothing to push.
func recipeParents(ctx context.Context, scratch *hq.Scratch, head, main, tree string) (parents []string, push bool, err error) {
	switch {
	case head == "" && main == "":
		return nil, true, nil
	case head == "":
		return []string{main}, true, nil
	}
	onMain := main == ""
	if !onMain {
		if onMain, err = scratch.IsAncestor(ctx, main, head); err != nil {
			return nil, false, err
		}
	}
	held, err := scratch.Tree(ctx, head)
	if err != nil {
		return nil, false, err
	}
	switch {
	case onMain && held == tree:
		return nil, false, nil
	case onMain:
		return []string{head}, true, nil
	}
	return []string{head, main}, true, nil
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
	if len(outcome.Required) > 0 {
		result["required"] = outcome.Required
	}
	switch {
	case outcome.Line != "":
		result["message"] = strings.ToUpper(outcome.Line[:1]) + outcome.Line[1:] + "."
	case outcome.OnMain:
		result["message"] = fmt.Sprintf(
			"Main of the recipe repository %q already carries every tier of the recipe, so no tier is proposed: zcp adds only the tiers main lacks. To carry a service's scale into the tiers on main, call zerops_workflow action=\"group-recipe\" with scaling set to the service's hostname: it proposes only that service's verticalAutoscaling, floors applied, as the Mate's recipe change. To carry a managed service this project gained (a database, a cache, a storage), or a part of the app added or removed, call it with service set to the hostname: it proposes only that service added to, or removed from, each tier on main, as the Mate's recipe change.",
			hq.RecipeRepo)
	default:
		result["message"] = fmt.Sprintf(
			"What main lacks of the group recipe (%s) is already proposed; change #%d (%s) carries it.",
			strings.Join(outcome.Proposed, ", "), outcome.Change, outcome.ChangeURL)
	}
	return jsonResult(result), nil, nil
}
