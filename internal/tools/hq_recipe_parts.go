package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
)

// A tier on main is the group's, and the additive reconcile never proposes
// over it (reconcileGroupRecipe). A change to the app's set of parts — a
// stage a pair gained or lost (spec-mate §2.8a.4) — still has to reach it, or
// production never runs the new part and keeps running a removed one. So it
// has a narrow way back, the scaling proposal's (hq_recipe_scaling.go): the
// reconcile says which part main's tiers lack or still carry and names the
// call, and that call proposes adding or removing that ONE part's service in
// each tier file on main, every other line as main has it, as the Mate's
// change in the recipe repository (§2.8a.5), which HQ lands by itself unless
// its merge would change an environment made from a tier (recipeLanding). The
// service added is the composer's entry, under the hostname the tier holds
// for the part or the one the naming rule gives a new part — never one
// recomputed for a service a tier already carries. A part on a repository
// no tier carries yet is the pass's own to propose (recipeNewPartsTurn).

// recipeServiceTitle is the title of a part's proposal; hosts joined by
// ", " title the pass's proposal of several new parts (recipeNewPartsTurn).
func recipeServiceTitle(host string) string {
	return "Mate: " + host + " in the group recipe"
}

// recipeTitleHosts are the hosts a part's or a managed service's proposal
// title names, none for any other title.
func recipeTitleHosts(title string) []string {
	rest, ok := strings.CutPrefix(title, "Mate: ")
	if !ok {
		return nil
	}
	if rest, ok = strings.CutSuffix(rest, " in the group recipe"); !ok {
		return nil
	}
	hosts := strings.Split(rest, ", ")
	for _, host := range hosts {
		if host == "" || strings.ContainsAny(host, " '") {
			return nil
		}
	}
	return hosts
}

// recipeProposes reports whether change is a proposal naming host.
func recipeProposes(change *hq.MateChange, host string) bool {
	return change != nil && change.Title != nil && slices.Contains(recipeTitleHosts(*change.Title), host)
}

// recipePart is a part of the app as the recipe names it: the repository a
// service builds from and the setup the part's group environments build
// (recipePartSetup) — the AI Agent tier's stage deploying Mate's preview
// variant of a setup and a group environment's runtime building the setup it
// extends are one part.
type recipePart struct{ repo, setup string }

// recipeNamedPart is a part and the host that names it: its stage as the AI
// Agent tier lists it, or else a tier's runtime.
type recipeNamedPart struct {
	part recipePart
	host string
}

// recipePartSetup is the setup held names its part by: the setup a group
// environment builds for it, read through the pair's zerops.yaml
// (bundle.GroupEnvironmentSetup) — a stage's own setup unless it is Mate's
// preview variant of another — while a dev half, which no group environment
// builds, keeps its own.
func recipePartSetup(runtimes []bundle.GroupRuntime, held bundle.HeldRuntime) string {
	for _, r := range runtimes {
		if r.DevHostname == held.Hostname {
			return held.SetupName
		}
	}
	for _, r := range runtimes {
		if topology.SameRepoURL(r.RepoURL, held.RepoURL) {
			return bundle.GroupEnvironmentSetup(r, held.SetupName)
		}
	}
	return held.SetupName
}

// recipeServiceTier is what a proposal writes to one tier file on main:
// main's body with services added or removed, or main's own when nothing
// changes or the edit refused (why, in refused).
type recipeServiceTier struct {
	path, body     string
	added, removed []string
	refused        string
}

// recipeTiers is what a part's proposal is planned from: this Mate's
// composed tier files, main's body of each tier main holds, by path, and the
// composed runtimes, which tell a pair's dev half — a dev service, or a dev
// pool, which a tier holds with no build — from its stages. retiredDevs are
// the dev services a pool took over, a tier the group keeps may still name
// as a dev half.
type recipeTiers struct {
	composed    []recipe.File
	main        map[string]string
	runtimes    []bundle.GroupRuntime
	retiredDevs map[string]bool
}

// readRecipeTiers reads the tiers comp's main holds beside its composition.
func readRecipeTiers(ctx context.Context, comp groupRecipeComposition) (recipeTiers, error) {
	t := recipeTiers{composed: recipeTierFiles(comp.files), main: map[string]string{}, runtimes: comp.inputs.Runtimes, retiredDevs: comp.retiredDevs}
	for _, file := range t.composed {
		body, found, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return recipeTiers{}, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if found {
			t.main[file.Path] = body
		}
	}
	return t, nil
}

// partOf is the part host names in the composed tiers or in main's — a
// stage of the Mate's, a tier's own runtime, or a part's dev pool, which
// names its stage's part — and whether one does.
func (t recipeTiers) partOf(host string) (recipePart, bool) {
	for _, r := range t.runtimes {
		if r.DevPool && r.DevHostname == host && len(r.Stages) > 0 {
			host = r.Stages[0].Hostname
		}
	}
	for _, file := range t.composed {
		bodies := []string{file.Body}
		if body, found := t.main[file.Path]; found {
			bodies = append(bodies, body)
		}
		for _, body := range bodies {
			for _, held := range bundle.HeldRuntimes(body) {
				if held.Hostname == host {
					return recipePart{held.RepoURL, recipePartSetup(t.runtimes, held)}, true
				}
			}
		}
	}
	return recipePart{}, false
}

// carries reports whether a tier on main holds a service built from repo.
func (t recipeTiers) carries(repo string) bool {
	for _, body := range t.main {
		if slices.ContainsFunc(bundle.HeldRuntimes(body), func(held bundle.HeldRuntime) bool { return held.RepoURL == repo }) {
			return true
		}
	}
	return false
}

// devHalf reports whether host is a pair's dev half — the dev service a
// pool took over among them.
func (t recipeTiers) devHalf(host string) bool {
	return t.retiredDevs[host] || slices.ContainsFunc(t.runtimes, func(r bundle.GroupRuntime) bool { return r.DevHostname == host })
}

// partHosts are composedBody's entries of part, in its order: each runtime
// built for it and, beside a stage of it, its pair's dev pool — and its dev
// service too with devs, which a new part brings along.
func (t recipeTiers) partHosts(composedBody string, part recipePart, devs bool) []string {
	of := map[string]bool{}
	for _, held := range bundle.HeldRuntimes(composedBody) {
		if held.RepoURL == part.repo && recipePartSetup(t.runtimes, held) == part.setup {
			of[held.Hostname] = true
		}
	}
	for _, r := range t.runtimes {
		if (r.DevPool || devs) && slices.ContainsFunc(r.Stages, func(s bundle.GroupStage) bool { return of[s.Hostname] }) {
			of[r.DevHostname] = true
		}
	}
	var hosts []string
	for _, host := range bundle.TierHostnames(composedBody) {
		if of[host] {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// tierEdit is what bringing part into line with this Mate does to one tier,
// body as main has it with what the proposal added before, composedBody as
// the Mate composes it. A new part — one whose repository no tier on main
// carries — gains every entry the composer writes for it there, after the
// tier's last runtime. A tier that carries a service of the part's
// repository gains the part's runtime — beside a stage, its dev pool — after
// the repository's last service where it lacks the part, and loses it where
// the composed tier does not carry it; a tier that carries none of a
// repository another tier carries is one people wrote without it, and is
// left alone, as is a dev half.
func (t recipeTiers) tierEdit(composedBody, body string, part recipePart) (string, []string, []string, error) {
	if !t.carries(part.repo) {
		return addTierHosts(body, composedBody, t.partHosts(composedBody, part, true), bundle.LastTierRuntime(body))
	}
	onMain, carried, anchor := "", false, ""
	for _, held := range bundle.HeldRuntimes(body) {
		if held.RepoURL != part.repo {
			continue
		}
		carried, anchor = true, held.Hostname
		if recipePartSetup(t.runtimes, held) == part.setup {
			onMain = held.Hostname
		}
	}
	hosts := t.partHosts(composedBody, part, false)
	switch {
	case !carried:
		return body, nil, nil, nil
	case len(hosts) > 0 && onMain == "":
		return addTierHosts(body, composedBody, hosts, anchor)
	case len(hosts) == 0 && onMain != "" && !t.devHalf(onMain):
		edited, err := bundle.RemoveTierService(body, onMain)
		return edited, nil, []string{onMain}, err
	}
	return body, nil, nil, nil
}

// addTierHosts adds composedBody's entries of hosts that body lacks, each
// after the one before, the first after after.
func addTierHosts(body, composedBody string, hosts []string, after string) (string, []string, []string, error) {
	var added []string
	for _, host := range hosts {
		if bundle.TierListsService(body, host) {
			continue
		}
		edited, err := bundle.AddTierService(body, composedBody, host, after)
		if err != nil {
			return body, nil, nil, err
		}
		body, after, added = edited, host, append(added, host)
	}
	return body, added, nil, nil
}

// plan is, for each tier file on main, what bringing parts into line with
// this Mate does (tierEdit), one part after another; a part an edit
// refuses is left out of that tier, and why is said.
func (t recipeTiers) plan(parts []recipePart) []recipeServiceTier {
	var tiers []recipeServiceTier
	for _, file := range t.composed {
		mainBody, found := t.main[file.Path]
		if !found {
			continue
		}
		tier := recipeServiceTier{path: file.Path, body: mainBody}
		var refused []string
		for _, part := range parts {
			edited, added, removed, err := t.tierEdit(file.Body, tier.body, part)
			if err != nil {
				refused = append(refused, err.Error())
				continue
			}
			tier.body = edited
			tier.added = append(tier.added, added...)
			tier.removed = append(tier.removed, removed...)
		}
		tier.refused = strings.Join(refused, "; ")
		tiers = append(tiers, tier)
	}
	return tiers
}

// newParts are this Mate's new parts: each whose repository no tier on main
// carries, which a tier main holds gains (plan) — named by its stage, never
// its dev half.
func (t recipeTiers) newParts() []recipeNamedPart {
	seen := map[recipePart]bool{}
	var parts []recipeNamedPart
	for _, file := range t.composed {
		for _, held := range bundle.HeldRuntimes(file.Body) {
			part := recipePart{held.RepoURL, recipePartSetup(t.runtimes, held)}
			paired := slices.ContainsFunc(t.runtimes, func(r bundle.GroupRuntime) bool { return r.DevHostname == held.Hostname && len(r.Stages) > 0 })
			if seen[part] || paired || t.carries(part.repo) {
				continue
			}
			seen[part] = true
			if len(serviceTierEdits(t.plan([]recipePart{part}))) > 0 {
				parts = append(parts, recipeNamedPart{part, held.Hostname})
			}
		}
	}
	slices.SortFunc(parts, func(a, b recipeNamedPart) int { return strings.Compare(a.host, b.host) })
	return parts
}

// recipePartOf is the part host names in this Mate's composed tiers or in
// main's (recipeTiers.partOf), and whether one does.
func recipePartOf(ctx context.Context, comp groupRecipeComposition, host string) (recipePart, bool, error) {
	t, err := readRecipeTiers(ctx, comp)
	if err != nil {
		return recipePart{}, false, err
	}
	part, found := t.partOf(host)
	return part, found, nil
}

// recipeTierFiles are the composed recipe's tier files.
func recipeTierFiles(files []recipe.File) []recipe.File {
	var tiers []recipe.File
	for _, file := range files {
		if strings.HasSuffix(file.Path, "/import.yaml") {
			tiers = append(tiers, file)
		}
	}
	return tiers
}

// planRecipeService is, for each tier file on main, what bringing part into
// line with this Mate does (recipeTiers.tierEdit).
func planRecipeService(ctx context.Context, comp groupRecipeComposition, part recipePart) ([]recipeServiceTier, error) {
	t, err := readRecipeTiers(ctx, comp)
	if err != nil {
		return nil, err
	}
	return t.plan([]recipePart{part}), nil
}

// serviceTierEdits reads the tiers a proposal changes as "Tier: adds x, y".
func serviceTierEdits(tiers []recipeServiceTier) []string {
	var out []string
	for _, tier := range tiers {
		var what []string
		if len(tier.added) > 0 {
			what = append(what, "adds "+strings.Join(tier.added, ", "))
		}
		if len(tier.removed) > 0 {
			what = append(what, "removes "+strings.Join(tier.removed, ", "))
		}
		if len(what) > 0 {
			out = append(out, tierTitle(tier.path)+": "+strings.Join(what, ", "))
		}
	}
	return out
}

// refusedServiceTiers reads the tiers left as main has them, and why.
func refusedServiceTiers(tiers []recipeServiceTier) string {
	var out []string
	for _, tier := range tiers {
		if tier.refused != "" {
			out = append(out, tierTitle(tier.path)+" ("+tier.refused+")")
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " Left as main has it: " + strings.Join(out, "; ") + "."
}

// recipePartSteer is what the reconcile says when main's tiers do not carry
// this Mate's parts of a repository they carry as it runs them: for each
// part a tier lacks or still carries, the call that proposes it — named by
// the Mate's stage, as the AI Agent tier lists it, or else by a tier's
// runtime. A new part is the pass's own to propose (recipeNewPartsTurn),
// and a part whose proposal is the Mate's open change already is not said
// again. "" when every tier carries the parts as the Mate runs them.
func recipePartSteer(ctx context.Context, comp groupRecipeComposition) (string, error) {
	t, err := readRecipeTiers(ctx, comp)
	if err != nil {
		return "", err
	}
	var candidates []recipeNamedPart
	seen := map[recipePart]bool{}
	for _, file := range t.composed {
		for _, held := range append(bundle.HeldRuntimes(file.Body), bundle.HeldRuntimes(t.main[file.Path])...) {
			part := recipePart{held.RepoURL, recipePartSetup(t.runtimes, held)}
			if !seen[part] && t.carries(part.repo) {
				seen[part] = true
				candidates = append(candidates, recipeNamedPart{part, held.Hostname})
			}
		}
	}
	open := openRecipeChange(comp.state)
	var calls []string
	for _, c := range candidates {
		edits := serviceTierEdits(t.plan([]recipePart{c.part}))
		if len(edits) == 0 || recipeProposes(open, c.host) {
			continue
		}
		calls = append(calls, fmt.Sprintf("zerops_workflow action=\"group-recipe\" service=%q (%s)", c.host, strings.Join(edits, "; ")))
	}
	if len(calls) == 0 {
		return "", nil
	}
	return fmt.Sprintf("the tiers on %q of the recipe repository %q do not carry this Mate's parts as it runs them — to propose each as the Mate's recipe change, call %s",
		hqBase, hq.RecipeRepo, strings.Join(calls, ", then ")), nil
}

// handleGroupRecipeService is `zerops_workflow action="group-recipe"
// service=<host>`: the Mate's change in the recipe repository, titled for
// host, that adds host's part's service to each tier file on main lacking it,
// or removes it from each still carrying it, and changes nothing else. Every
// tier that carries the part's repository is written as this proposal has
// it, over main: an earlier proposal for the same host moves forward on the
// same change, and one with nothing left to propose is brought to main's
// tree.
func handleGroupRecipeService(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, host string,
	schemas *schema.Cache,
) (*mcp.CallToolResult, any, error) {
	comp, outcome, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir, schemas)
	if !ok {
		reason := outcome.Blocked
		if reason == "" {
			reason = outcome.Line
		}
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			"The service proposal was not made: "+reason,
			"Retry once this Mate is linked to its HQ and a pair has its repository there."), WithRecoveryStatus()), nil, nil
	}
	defer comp.close()
	// A managed service the Mate's project runs is proposed for any app
	// (hq_recipe_managed.go); anything else is a part of the app.
	what, title := "the managed service "+host, recipeServiceTitle(host)
	plan := func(ctx context.Context) ([]recipeServiceTier, error) { return planRecipeManaged(ctx, comp, host) }
	if !recipeManagedService(comp, host) {
		t, err := readRecipeTiers(ctx, comp)
		if err != nil {
			return serviceProposalFailed(err.Error()), nil, nil
		}
		part, found := t.partOf(host)
		if open := openRecipeChange(comp.state); !found && open != nil && open.Title != nil && *open.Title == recipeServiceTitle(host) {
			return withdrawRecipeService(ctx, comp, open, host), nil, nil
		}
		if !found {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("The service proposal was not made: this Mate's project runs no managed service %q, and neither this Mate's recipe nor the tiers on %q of the recipe repository %q name a service %q built from a repository with a setup.", host, hqBase, hq.RecipeRepo, host),
				"Name a managed service this Mate's project runs, a stage of this Mate's pair, or a runtime a tier on main carries."), WithRecoveryStatus()), nil, nil
		}
		what = host + "'s part"
		parts := []recipePart{part}
		if open := openRecipeChange(comp.state); recipeProposes(open, host) {
			// The pass's one proposal of several new parts, host's among
			// them, moves forward whole.
			title, parts = *open.Title, nil
			for _, h := range recipeTitleHosts(title) {
				if p, ok := t.partOf(h); ok {
					parts = append(parts, p)
				}
			}
		}
		plan = func(context.Context) ([]recipeServiceTier, error) { return t.plan(parts), nil }
	}
	var tiers []recipeServiceTier
	written, err := writeRecipeChange(ctx, comp, openRecipeChange(comp.state), title,
		"recipe: "+host+" in the group recipe, from "+comp.inputs.MateProjectName,
		func(ctx context.Context, _ *hq.Scratch, _ string, _ []string) (map[string]string, error) {
			planned, err := plan(ctx)
			tiers = planned
			files := make(map[string]string, len(planned))
			for _, tier := range planned {
				files[tier.path] = tier.body
			}
			return files, err
		})
	if err != nil {
		return serviceProposalFailed(err.Error()), nil, nil
	}
	edits := serviceTierEdits(tiers)
	result := map[string]any{"groupRepo": hq.RecipeRepo}
	if written.edited && len(edits) > 0 {
		return recipeEditedRefusal("The service proposal", comp, written), nil, nil
	}
	if written.other != nil && len(edits) > 0 {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The service proposal was not made: this Mate's change #%d in the recipe repository %q (%q) is open, and this Mate keeps one recipe proposal open at a time.",
				written.other.number, hq.RecipeRepo, written.other.title),
			fmt.Sprintf("Retry once change #%d is merged or closed (%s).", written.other.number,
				comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.other.number))), WithRecoveryStatus()), nil, nil
	}
	if written.number != 0 {
		result["change"] = written.number
		result["changeUrl"] = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	}
	if len(edits) == 0 {
		message := fmt.Sprintf("The tiers on %q of the recipe repository %q already carry %s as this Mate runs it — nothing to propose.", hqBase, hq.RecipeRepo, what)
		if written.pushed {
			message += fmt.Sprintf(" Change #%d, which proposed it before, now adds nothing.", written.number)
		}
		result["message"] = message + refusedServiceTiers(tiers) + recipeEditSteer
		return jsonResult(result), nil, nil
	}
	result["branch"] = written.branch
	result["tiers"] = edits
	message := fmt.Sprintf("Proposed %s to the recipe repository %q as change #%d (%s): %s — every other line of each tier as main has it. %s.%s",
		what, hq.RecipeRepo, written.number, result["changeUrl"], strings.Join(edits, "; "), recipeLanding, refusedServiceTiers(tiers))
	if recipeManagedService(comp, host) {
		message += fmt.Sprintf(" Merging it imports %s into every stage or production environment made from the tiers it changes.", host)
	}
	result["message"] = message
	return jsonResult(result), nil, nil
}

func serviceProposalFailed(reason string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(platform.ErrAPIError,
		"The service proposal was not made: "+reason,
		"Retry the call; the project is unaffected."), WithRecoveryStatus())
}

// withdrawRecipeService is a service= call for host while the Mate's open
// change in the recipe repository is host's proposal and nothing names host
// any more — a managed service the Mate has since deleted: the change is
// brought to main's tree, so it adds nothing and Core closes it.
func withdrawRecipeService(ctx context.Context, comp groupRecipeComposition, open *hq.MateChange, host string) *mcp.CallToolResult {
	written, err := writeRecipeChange(ctx, comp, open, recipeServiceTitle(host),
		"recipe: "+host+" withdrawn, from "+comp.inputs.MateProjectName,
		func(context.Context, *hq.Scratch, string, []string) (map[string]string, error) {
			return map[string]string{}, nil
		})
	if err != nil {
		return serviceProposalFailed(err.Error())
	}
	if written.edited {
		return recipeEditedRefusal("The withdrawal of "+host+"'s proposal", comp, written)
	}
	url := comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	return jsonResult(map[string]any{
		"groupRepo": hq.RecipeRepo,
		"change":    written.number,
		"changeUrl": url,
		"message": fmt.Sprintf("This Mate's project no longer runs %s, so change #%d (%s), which proposed it to the recipe repository %q, is withdrawn: it now adds nothing.",
			host, written.number, url, hq.RecipeRepo),
	})
}

// A new part — one in a repository no tier on main carries, which a Mate
// adds by bootstrapping a pair on a new repository in HQ — reaches main by
// no steer: the new Mates and environments made from the tiers would never
// get it while nobody calls for it (Bob Shock's firma and dakar, 2026-11-20).
// So the pass proposes it itself, every new part in one change, as the
// Mate's one open recipe proposal; a person who closes it unmerged is not
// asked again until a later recipe change of the Mate merges
// (recipeDeclined). Status says what stands (recipeNewPartsSteps).

// recipeNewPartsTurn is the pass's own proposal of this Mate's new parts
// (recipeTiers.newParts), those a person declined left out: the change
// carrying them all, moved forward or opened, or left for a later pass while
// another recipe change of the Mate is open. line says what it did, ""
// when nothing moved; parts is what status says of them, nil when none.
func recipeNewPartsTurn(ctx context.Context, comp groupRecipeComposition) (line string, parts *recipeNewParts, err error) {
	t, err := readRecipeTiers(ctx, comp)
	if err != nil {
		return "", nil, err
	}
	hosts, proposed := recipeNewPartsProposal(t, func(host string) bool {
		return recipeDeclined(ctx, comp.scratch, comp.main, comp.state, func(title string) bool { return slices.Contains(recipeTitleHosts(title), host) })
	})
	if len(hosts) == 0 {
		return "", nil, nil
	}
	named := strings.Join(hosts, ", ")
	tiers := t.plan(proposed)
	written, err := writeRecipeChange(ctx, comp, openRecipeChange(comp.state), recipeServiceTitle(named),
		"recipe: "+named+" in the group recipe, from "+comp.inputs.MateProjectName,
		func(context.Context, *hq.Scratch, string, []string) (map[string]string, error) {
			files := make(map[string]string, len(tiers))
			for _, tier := range tiers {
				files[tier.path] = tier.body
			}
			return files, nil
		})
	if err != nil {
		return "", &recipeNewParts{Hosts: hosts}, err
	}
	parts = &recipeNewParts{Hosts: hosts}
	switch {
	case written.other != nil && slices.ContainsFunc(recipeTitleHosts(written.other.title), func(host string) bool { return slices.Contains(hosts, host) }):
		// A part's own proposal, the agent's service= call, carries some of
		// them: status names that change.
		parts.Change, parts.ChangeURL = written.other.number, comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.other.number)
	case written.number != 0 && written.other == nil:
		parts.Change, parts.ChangeURL = written.number, comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	}
	switch {
	case written.other != nil:
		return fmt.Sprintf("this Mate's new parts %s wait for its change #%d in the recipe repository %q (%q) to be merged or closed: it keeps one recipe proposal open at a time, and the next pass proposes them",
			named, written.other.number, hq.RecipeRepo, written.other.title), parts, nil
	case written.edited:
		return recipeEditedLine(parts.Change, parts.ChangeURL), parts, nil
	case written.created || written.pushed:
		return fmt.Sprintf("this Mate's new parts %s are proposed to the tiers on %q of the recipe repository %q as change #%d (%s): %s — every other line of each tier as main has it. %s%s",
			named, hqBase, hq.RecipeRepo, parts.Change, parts.ChangeURL, strings.Join(serviceTierEdits(tiers), "; "), recipeLanding, strings.TrimSuffix(refusedServiceTiers(tiers), ".")), parts, nil
	}
	return "", parts, nil
}

// recipeNewPartsProposal is the one proposal of new parts a pass makes:
// every new part of t (recipeTiers.newParts) but those declined says a
// person declined, by its stage's host, and the parts themselves.
func recipeNewPartsProposal(t recipeTiers, declined func(host string) bool) (hosts []string, parts []recipePart) {
	for _, p := range t.newParts() {
		if !declined(p.host) {
			hosts, parts = append(hosts, p.host), append(parts, p.part)
		}
	}
	return hosts, parts
}

// recipeNewParts is what the last pass left of this Mate's new parts: their
// hosts and the change proposing them, 0 when none does.
type recipeNewParts struct {
	Hosts     []string `json:"hosts"`
	Change    int      `json:"change,omitempty"`
	ChangeURL string   `json:"changeUrl,omitempty"`
}

// recipeNewPartsSteps is what status says of the new parts the last pass
// left (parts): while no change proposes them, a required step naming the
// call that does; while their change is open — state, when heard, says so —
// one line naming it, which lands as HQ decides; none once it is not, for
// the next pass to decide.
func recipeNewPartsSteps(parts *recipeNewParts, state *hq.MateState) []string {
	if parts == nil || len(parts.Hosts) == 0 {
		return nil
	}
	named := strings.Join(parts.Hosts, ", ")
	if parts.Change == 0 {
		return []string{fmt.Sprintf("the tiers on %q of the recipe repository %q — what new Mates and the group's stage and production are made from — lack this Mate's new parts %s: call zerops_workflow action=\"group-recipe\" to propose them as the Mate's recipe change (it waits while another recipe change of the Mate is open, and says which)",
			hqBase, hq.RecipeRepo, named)}
	}
	if state != nil && !slices.ContainsFunc(state.Changes, func(c hq.MateChange) bool {
		return c.Repo == hq.RecipeRepo && c.Number == parts.Change && c.State == hq.ChangeOpen
	}) {
		return nil
	}
	return []string{fmt.Sprintf("this Mate's new parts %s reach the tiers new Mates and the group's stage and production are made from only once change #%d (%s) of the recipe repository %q lands: %s",
		named, parts.Change, parts.ChangeURL, hq.RecipeRepo, recipeLanding)}
}
