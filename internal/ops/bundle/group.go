package bundle

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/topology"
)

// The group-wide export: the whole app of one group, as the three tiers a
// published Zerops recipe is made of (guide 2.2 / A2, D12, D13).
//
// # Why this is not BuildExport and not BuildLaunch
//
// BuildExport packages ONE runtime building from its own origin — a
// single-repo snapshot. BuildLaunch composes N runtimes but for the platform's
// launch-production path, which is pipeline-first and therefore emits no
// `buildFromGit` and no `zeropsSetup` at all (the import API rejects
// `zeropsSetup` without a repo, and the platform's credential-less clone
// cannot read a private one).
//
// A recipe file is neither. It is a DOCUMENT in the group repo, read by the
// app and converted before it ever reaches the import API: guide 4.3 turns
// every `buildFromGit` + `zeropsSetup` into `startWithoutCode` and keeps the
// source map, so a Mate joining from the recipe knows which repository each
// runtime is built from (2.4). Dropping the pair here would throw away the
// only record of that mapping — which is exactly what the recipe exists to
// carry. So this composer emits the pair on every tier and leaves the
// pipeline-first transform to the reader.
//
// What it does take from BuildLaunch is the production TRANSFORM: the mode
// suffix stripped off each pair (`apidev`/`apistage` → `api`), the HA floor on
// containers, DEDICATED CPU, HA-promoted managed deps. Stage and Small
// Production run the same transform under two policies — a stage is the shape
// a person shows a client (guide 5.2/D16), so it keeps the single-node,
// SHARED, one-container form and production takes the floor of two.
//
// The project's variables and each runtime's own go into every tier, and a
// secret's value into none: the composer runs unattended, so it decides each
// variable itself (recipeSecret, group_env.go) — config as written, a secret
// as a generator every environment the tier creates expands for itself.

// GroupRuntime is one runtime of the group's app as the Mate's project holds
// it: a dev/stage pair built from one service repository.
type GroupRuntime struct {
	// DevHostname is the pair's dev half — the hostname the AI Agent tier
	// keeps and the one the stage/production rename strips a suffix from.
	DevHostname string
	// StageHostname is the pair's stage half, empty for a mode that has none.
	// It exists only on the AI Agent tier: the group's own stage is a
	// different project entirely (D12/D16).
	StageHostname string
	// ServiceType is the platform type tag, e.g. "nodejs@22".
	ServiceType string
	// RepoURL is the service repository's canonical clone URL — the one the
	// broker returned for this pair (A1, recorded on ServiceMeta.RemoteURL).
	RepoURL string
	// SetupName is the `setup:` block the dev half resolves at build time.
	SetupName string
	// StageSetupName is the stage half's block, as a deploy of the stage half
	// recorded it. Empty is resolved from ZeropsYAMLBody (groupStageSetup) —
	// never taken from SetupName on a guess.
	StageSetupName string
	// ZeropsYAMLBody is the pair's zerops.yaml: it verifies the named setups
	// and names the stage half's when no deploy recorded it.
	ZeropsYAMLBody string
	// ServiceEnvs is the dev half's user-set service variables, written as
	// its `envSecrets` — config as written, secrets generated (recipeSecret).
	ServiceEnvs []ProjectEnvVar
	// StageServiceEnvs is the stage half's. The AI Agent tier's stage half
	// carries them, and so does every group environment, which runs what the
	// stage half runs; a pair with no stage half gives its dev half's.
	StageServiceEnvs []ProjectEnvVar
	// Scaling is the live autoscaling shape. The AI Agent tier reproduces it
	// verbatim; the two transformed tiers reflect it and then apply their
	// policy floors.
	Scaling *Scaling
	// SubdomainEnabled mirrors the pair's public access. A recipe whose app
	// is unreachable is not a recipe, so it is carried on every tier rather
	// than stripped the way launch-production strips it.
	SubdomainEnabled bool
}

// GroupRecipeInputs is the whole group's app in one value.
type GroupRecipeInputs struct {
	// Name is the recipe's identity in a deploy link — the group's slug.
	Name string
	// Title heads the top-level README; Name when empty.
	Title string
	// Intro is the paragraph the recipe pages lift out of that README.
	Intro string
	// MateProjectName is the name of the Mate's OWN project — the AI Agent
	// tier re-creates it, so it carries its name rather than the group's.
	MateProjectName string
	Runtimes        []GroupRuntime
	ManagedServices []ManagedServiceEntry
	// ProjectEnvs is the project's user-set variables, the platform's own and
	// the control plane's already left out: every tier carries them, config
	// under envVariables and secrets under envSecrets (recipeSecret).
	ProjectEnvs []ProjectEnvVar
	// CorePackage is the project's live core package. Every tier's project
	// carries a LIGHT or a SERIOUS; anything else is left to the platform's
	// default.
	CorePackage string
	// Utilities are the runtimes the project builds from a public repository
	// and no Gitea pair — a mailpit, an adminer.
	Utilities []GroupUtility
	// HAIncapable names the managed services whose type the platform ships no
	// `:ha` variant of: the tier that promotes the rest keeps them single-node.
	HAIncapable []string
}

// GroupUtility is a runtime the project builds from a public repository and
// no Gitea pair: a utility such as mailpit, which no Mate develops. Every tier
// writes it as the project runs it — its own hostname, its public build, its
// own scale — with no tier's transform: there is no pair to promote and no
// service repository to name.
type GroupUtility struct {
	Hostname    string
	ServiceType string
	// BuildFromGit is the public repository its active version was built from.
	BuildFromGit string
	// SetupName is the setup its build named; empty lets the platform build
	// the setup named after the hostname, as an import that named none did.
	SetupName        string
	SubdomainEnabled bool
	Scaling          *Scaling
	// ServiceEnvs is its user-set service variables (recipeSecret).
	ServiceEnvs []ProjectEnvVar
}

// groupTierPolicy is one tier's decision set. A tier is a decision, never an
// observation (see the package doc of internal/recipe), so every difference
// between the three is a field here rather than a branch in the composer.
type groupTierPolicy struct {
	index int
	title string
	slug  string
	// summary is the sentence the recipe page lifts for this tier.
	summary string
	// pairs emits both halves of every pair under their own hostnames. Only
	// the AI Agent tier does: it re-creates a Mate's project (D12).
	pairs bool
	// promoteHA resolves every HA-capable managed dep to its `:ha` variant.
	promoteHA bool
	// minContainers is the floor applied to the reflected source count.
	minContainers int
	// cpuMode overrides the reflected source mode when non-empty.
	cpuMode string
	// projectSuffix is appended to the group's name for the tier's project;
	// the AI Agent tier uses MateProjectName instead and leaves it empty.
	projectSuffix string
}

// groupTiers is the published set, in the order and with the numbering the
// recipe layout already uses. Numbers 1 and 2 are the CDE tiers upstream
// recipes carry and a Mate has nothing to say about, so they are absent
// rather than renumbered — the number is part of the directory name.
var groupTiers = []groupTierPolicy{
	{
		index: 0, title: "AI Agent", slug: "ai-agent",
		summary: "The project a Zerops Mate works in: every runtime as a dev/stage pair, with the managed services they share.",
		pairs:   true, minContainers: 0,
	},
	{
		index: 3, title: "Stage", slug: "stage",
		summary:       "One shared environment per group, fed by a branch — the shape a person shows a client.",
		minContainers: 1, cpuMode: "SHARED", projectSuffix: "stage",
	},
	{
		index: 4, title: "Small Production", slug: "small-production",
		summary:       "Production: every runtime replicated, every managed service highly available, dedicated CPU.",
		promoteHA:     true,
		minContainers: runtimeProductionMinContainers, cpuMode: runtimeProductionCPUMode,
		projectSuffix: "production",
	},
}

// BuildGroupRecipe composes the group's whole app into the published recipe
// layout. Pure composition — no I/O; the caller writes the files (A2's pull
// request) and owns where they land.
//
// Deterministic by construction: runtimes and managed services are sorted, and
// the same project always composes to the same bytes. The pull request this
// feeds is updated on every service change, so a composer that reordered a map
// would commit noise on every pass and make the diff worthless.
//
// Nothing here is fatal that a reconcile could not act on. A missing setup
// block, an unreadable scaling shape, an unclassified secret — each is a
// warning against a tier that still composes, because this runs unattended
// with nobody to ask. The one thing never guessed is what a stage-shaped
// entry builds: a tier naming a runtime whose stage setup nothing names is
// withheld (a warning), and a recipe with no tier left is an error the
// reconcile reports and retries — a tier that lands on the group repo stays
// there, so an absent one is proposed later and a wrong one never heals.
func BuildGroupRecipe(inputs GroupRecipeInputs) (recipe.Layout, []string, error) {
	if strings.TrimSpace(inputs.Name) == "" {
		return recipe.Layout{}, nil, fmt.Errorf("group recipe: Name required (the group's slug)")
	}
	if len(inputs.Runtimes) == 0 {
		return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: at least one runtime required", inputs.Name)
	}
	runtimes := append([]GroupRuntime(nil), inputs.Runtimes...)
	sort.SliceStable(runtimes, func(i, j int) bool { return runtimes[i].DevHostname < runtimes[j].DevHostname })
	for i, r := range runtimes {
		switch {
		case strings.TrimSpace(r.DevHostname) == "":
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime[%d]: DevHostname required", inputs.Name, i)
		case strings.TrimSpace(r.ServiceType) == "":
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime %q: ServiceType required", inputs.Name, r.DevHostname)
		case strings.TrimSpace(r.RepoURL) == "":
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime %q: RepoURL required (chain to the broker's repository call)", inputs.Name, r.DevHostname)
		case strings.TrimSpace(r.SetupName) == "":
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime %q: SetupName required", inputs.Name, r.DevHostname)
		}
	}

	managed := dedupeManagedByHostname(inputs.ManagedServices)
	sort.SliceStable(managed, func(i, j int) bool { return managed[i].Hostname < managed[j].Hostname })
	utilities, mateOnly, utilityWarnings := groupUtilities(inputs.Utilities, runtimes)

	stageSetups := make(map[string]string, len(runtimes))
	unresolved := map[string]string{}
	for _, r := range runtimes {
		setup, why := groupStageSetup(r)
		if why != "" {
			unresolved[r.DevHostname] = why
			continue
		}
		stageSetups[r.DevHostname] = setup
	}

	warnings := append([]string(nil), utilityWarnings...)
	warnings = append(warnings, groupSetupWarnings(runtimes, stageSetups)...)
	priorities, priorityWarnings := groupPriorities(groupApps(runtimes, utilities), inputs.ProjectEnvs)
	warnings = append(warnings, priorityWarnings...)
	haIncapable := make(map[string]bool, len(inputs.HAIncapable))
	for _, host := range inputs.HAIncapable {
		haIncapable[host] = true
	}
	plan := groupPlan{
		inputs: inputs, runtimes: runtimes, utilities: utilities, mateOnly: mateOnly,
		managed: managed, haIncapable: haIncapable, stageSetups: stageSetups, priorities: priorities,
	}

	layout := recipe.Layout{
		Name:  inputs.Name,
		Title: firstNonBlank(inputs.Title, inputs.Name),
		Intro: inputs.Intro,
	}
	withheld := map[string][]string{}
	for _, policy := range groupTiers {
		if blockers := groupTierBlockers(runtimes, policy, unresolved); len(blockers) > 0 {
			for _, host := range blockers {
				withheld[host] = append(withheld[host], policy.title)
			}
			continue
		}
		body, tierWarnings, err := composeGroupTierYAML(plan, policy)
		if err != nil {
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: tier %q: %w", inputs.Name, policy.title, err)
		}
		warnings = append(warnings, tierWarnings...)
		layout.Tiers = append(layout.Tiers, recipe.Tier{
			Index:      policy.index,
			Title:      policy.title,
			Slug:       policy.slug,
			ImportYAML: body,
			Summary:    policy.summary,
		})
	}

	var reasons []string
	for _, r := range runtimes {
		if titles := withheld[r.DevHostname]; len(titles) > 0 {
			reasons = append(reasons, fmt.Sprintf(
				"runtime %q: its stage setup is not recorded and %s — %s withheld until it is (a deploy of its stage half records it, or set-default-setup names it as stageSetup)",
				r.DevHostname, unresolved[r.DevHostname], joinTitles(titles)))
		}
	}
	if len(layout.Tiers) == 0 {
		return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: no tier composes: %s", inputs.Name, strings.Join(reasons, "; "))
	}
	return layout, append(warnings, reasons...), nil
}

// groupStageSetup names the setup a runtime's stage-shaped entries build —
// the AI Agent tier's stage half and the one runtime of every group
// environment. why is non-empty when nothing names it without a guess.
//
// A stage setup is recorded only by a deploy of the stage half, and a Mate
// that joined the group's repositories may never make one. Falling back to
// the dev setup built production with the dev loop's `start: zsc noop` (the
// medusa group, 2026-09-26), so the fallback is the pair's own zerops.yaml:
// the one setup it declares beside the dev one, or its only setup, which
// then serves both halves.
func groupStageSetup(r GroupRuntime) (setup, why string) {
	if recorded := strings.TrimSpace(r.StageSetupName); recorded != "" {
		return recorded, ""
	}
	if strings.TrimSpace(r.ZeropsYAMLBody) == "" {
		return "", "no zerops.yaml was read"
	}
	declared, err := setupNamesInZeropsYAML(r.ZeropsYAMLBody)
	if err != nil {
		return "", fmt.Sprintf("its zerops.yaml does not parse (%v)", err)
	}
	declared = dedupeStrings(declared)
	others := make([]string, 0, len(declared))
	for _, name := range declared {
		if name != r.SetupName {
			others = append(others, name)
		}
	}
	switch {
	case len(declared) == 0:
		return "", "its zerops.yaml declares no setup"
	case len(others) == 1:
		return others[0], ""
	case len(others) == 0:
		return declared[0], ""
	default:
		sort.Strings(others)
		return "", fmt.Sprintf("its zerops.yaml declares %s beside the dev setup %q", strings.Join(others, ", "), r.SetupName)
	}
}

// groupTierBlockers lists the runtimes a tier would need a stage setup for
// and has none: every runtime on a group environment, only those with a
// stage half on the AI Agent tier.
func groupTierBlockers(runtimes []GroupRuntime, policy groupTierPolicy, unresolved map[string]string) []string {
	var blockers []string
	for _, r := range runtimes {
		if _, missing := unresolved[r.DevHostname]; !missing {
			continue
		}
		if policy.pairs && r.StageHostname == "" {
			continue
		}
		blockers = append(blockers, r.DevHostname)
	}
	return blockers
}

// joinTitles reads a list of tier titles as a sentence does: "A", "A and B",
// "A, B and C" — followed by the verb it agrees with.
func joinTitles(titles []string) string {
	switch len(titles) {
	case 0:
		return ""
	case 1:
		return titles[0] + " is"
	default:
		return strings.Join(titles[:len(titles)-1], ", ") + " and " + titles[len(titles)-1] + " are"
	}
}

// groupPlan is what every tier is composed from, decided once.
type groupPlan struct {
	inputs    GroupRecipeInputs
	runtimes  []GroupRuntime
	utilities []GroupUtility
	// mateOnly names the utilities a group environment cannot carry: its
	// runtime takes their hostname.
	mateOnly    map[string]bool
	managed     []ManagedServiceEntry
	haIncapable map[string]bool
	stageSetups map[string]string
	priorities  map[string]int
}

// composeGroupTierYAML renders one tier's whole-project import.yaml.
func composeGroupTierYAML(plan groupPlan, policy groupTierPolicy) (string, []string, error) {
	inputs, runtimes, stageSetups, priorities := plan.inputs, plan.runtimes, plan.stageSetups, plan.priorities
	warnings := make([]string, 0, len(runtimes))
	source := firstNonBlank(inputs.MateProjectName, inputs.Name)

	// Every entry carries its priority, and the file lists the services in
	// the order the platform creates them: highest first, a pair's halves
	// together, the composer's own order kept among equals.
	type rankedItem struct {
		priority int
		item     yamlItem
	}
	ranked := make([]rankedItem, 0, 2*len(runtimes)+len(plan.utilities)+len(plan.managed))
	for _, r := range runtimes {
		type half struct {
			hostname, setup string
			envs            []ProjectEnvVar
		}
		var halves []half
		if policy.pairs {
			halves = append(halves, half{r.DevHostname, r.SetupName, r.ServiceEnvs})
			if r.StageHostname != "" {
				halves = append(halves, half{r.StageHostname, stageSetups[r.DevHostname], r.StageServiceEnvs})
			}
		} else {
			// A group environment runs what the pair's stage half runs: the
			// dev half's setup is the dev loop's, never a stage's.
			halves = append(halves, half{GroupPromotedHostname(r.DevHostname), stageSetups[r.DevHostname], stageHalfEnvs(r)})
		}
		for _, half := range halves {
			entry, entryWarnings := groupRuntimeEntry(r, half.hostname, half.setup, policy)
			if secrets := serviceSecretFields(half.envs, source); len(secrets) > 0 {
				entry["envSecrets"] = secrets
			}
			warnings = append(warnings, entryWarnings...)
			entry["priority"] = priorities[r.DevHostname]
			ranked = append(ranked, rankedItem{priorities[r.DevHostname], yamlItem{fields: orderedFields(entry, serviceKeyOrder)}})
		}
	}
	for _, u := range plan.utilities {
		if !policy.pairs && plan.mateOnly[u.Hostname] {
			continue
		}
		entry := groupUtilityEntry(u, priorities[u.Hostname], source)
		ranked = append(ranked, rankedItem{priorities[u.Hostname], yamlItem{fields: orderedFields(entry, serviceKeyOrder)}})
	}
	for _, m := range plan.managed {
		// A type with no HA variant stays single-node where the rest are
		// promoted: a fabricated `<type>:ha` fails the whole import.
		keepSingle := policy.promoteHA && plan.haIncapable[m.Hostname]
		if keepSingle {
			warnings = append(warnings, fmt.Sprintf(
				"managed service %q (%s) stays single-node on %s: the platform has no HA variant of its type",
				m.Hostname, m.Type, policy.title))
		}
		entry := managedEntryWithRules(m, policy.promoteHA, keepSingle)
		if vertical := managedVertical(m, policy); len(vertical) > 0 {
			entry["verticalAutoscaling"] = vertical
		}
		ranked = append(ranked, rankedItem{managedPriority, yamlItem{fields: orderedFields(entry, serviceKeyOrder)}})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].priority > ranked[j].priority })
	services := make([]yamlItem, 0, len(ranked))
	for _, r := range ranked {
		services = append(services, r.item)
	}

	project := []yamlField{{key: "name", value: groupTierProjectName(inputs, policy)}}
	if core := strings.TrimSpace(inputs.CorePackage); core == "LIGHT" || core == "SERIOUS" {
		project = append(project, yamlField{key: "corePackage", value: core})
	}
	config, secrets := groupEnvFields(inputs.ProjectEnvs, source)
	if len(config) > 0 {
		project = append(project, yamlField{key: "envVariables", value: config})
	}
	if len(secrets) > 0 {
		project = append(project, yamlField{key: "envSecrets", value: secrets})
	}

	body, err := tierDocument{
		header:   groupTierHeader(inputs, policy),
		project:  project,
		services: services,
	}.render()
	if err != nil {
		return "", nil, err
	}
	return body, warnings, nil
}

// groupTierHeader is what a tier says about itself above its project: what
// the tier is, that zcp wrote it and from which Mate, and that it is the
// group's to change — zcp proposes only the tiers the group repo's main
// lacks (D30), so a person's edit is never written over.
func groupTierHeader(inputs GroupRecipeInputs, policy groupTierPolicy) []string {
	return []string{
		"The " + policy.title + " tier. " + policy.summary,
		fmt.Sprintf("zcp wrote this file from the Mate %q. It is the group's now: a person may edit it, and zcp never proposes over a tier the group repo already carries.",
			firstNonBlank(inputs.MateProjectName, inputs.Name)),
		"Priority is the order services are created in, each wave deployed before the next starts: the managed services first, then every runtime before the runtimes that reference it.",
	}
}

// groupApps is what the priorities rank: every runtime pair, by any name it
// goes by, with the values that reference what it needs up first — its
// zerops.yaml's variables and its own service variables — and every utility
// with its own variables.
func groupApps(runtimes []GroupRuntime, utilities []GroupUtility) []groupApp {
	apps := make([]groupApp, 0, len(runtimes)+len(utilities))
	for _, u := range utilities {
		sources := make([]string, 0, len(u.ServiceEnvs))
		for _, env := range u.ServiceEnvs {
			sources = append(sources, env.Value)
		}
		apps = append(apps, groupApp{key: u.Hostname, hostnames: []string{u.Hostname}, sources: sources})
	}
	for _, r := range runtimes {
		sources := zeropsYAMLEnvValues(r.ZeropsYAMLBody)
		for _, env := range append(append([]ProjectEnvVar(nil), r.ServiceEnvs...), r.StageServiceEnvs...) {
			sources = append(sources, env.Value)
		}
		apps = append(apps, groupApp{
			key:       r.DevHostname,
			hostnames: []string{r.DevHostname, r.StageHostname, GroupPromotedHostname(r.DevHostname)},
			sources:   sources,
		})
	}
	return apps
}

// groupUtilities is the utilities every tier writes, sorted, and those a
// group environment cannot carry because its runtime takes their hostname.
// A utility the reader did not describe whole is left out and said.
func groupUtilities(in []GroupUtility, runtimes []GroupRuntime) ([]GroupUtility, map[string]bool, []string) {
	promoted := make(map[string]string, len(runtimes))
	for _, r := range runtimes {
		promoted[GroupPromotedHostname(r.DevHostname)] = r.DevHostname
	}
	var utilities []GroupUtility
	var warnings []string
	mateOnly := map[string]bool{}
	for _, u := range in {
		if strings.TrimSpace(u.Hostname) == "" || strings.TrimSpace(u.ServiceType) == "" || strings.TrimSpace(u.BuildFromGit) == "" {
			warnings = append(warnings, fmt.Sprintf("utility %q is left out of the recipe: its hostname, type or public build is unknown", u.Hostname))
			continue
		}
		if pair, taken := promoted[u.Hostname]; taken {
			mateOnly[u.Hostname] = true
			warnings = append(warnings, fmt.Sprintf(
				"utility %q stays on the AI Agent tier only: the group environments name the runtime of %q %q too",
				u.Hostname, pair, u.Hostname))
		}
		utilities = append(utilities, u)
	}
	sort.SliceStable(utilities, func(i, j int) bool { return utilities[i].Hostname < utilities[j].Hostname })
	return utilities, mateOnly, warnings
}

// groupUtilityEntry writes a utility as the project runs it: its public build
// and its own scale, on every tier alike.
func groupUtilityEntry(u GroupUtility, priority int, source string) map[string]any {
	entry := map[string]any{
		"hostname":     u.Hostname,
		"type":         u.ServiceType,
		"priority":     priority,
		"buildFromGit": topology.CanonicalRepoURL(u.BuildFromGit),
	}
	if u.SetupName != "" {
		entry["zeropsSetup"] = u.SetupName
	}
	if u.SubdomainEnabled {
		entry["enableSubdomainAccess"] = true
	}
	projectScaling(entry, u.Scaling)
	if secrets := serviceSecretFields(u.ServiceEnvs, source); len(secrets) > 0 {
		entry["envSecrets"] = secrets
	}
	return entry
}

// managedVertical is the vertical scale a managed service is written with:
// as it runs, on every tier but the one that decides a profile-bearing
// service's scale — Small Production sets the production profile, and a
// dev-sized database's bounds beside it would cap production. An object
// storage has none; its size is objectStorageSize.
func managedVertical(m ManagedServiceEntry, policy groupTierPolicy) map[string]any {
	if m.Scaling == nil || RulesForType(m.Type).RequiresObjectStorageSize {
		return nil
	}
	if policy.promoteHA && topology.IsProfileBearing(m.Type) {
		return nil
	}
	shape := map[string]any{}
	projectScaling(shape, m.Scaling)
	vertical, _ := shape["verticalAutoscaling"].(map[string]any)
	return vertical
}

// stageHalfEnvs is what a group environment's runtime carries of the pair's
// own variables: the stage half's, or the dev half's when it has no stage.
func stageHalfEnvs(r GroupRuntime) []ProjectEnvVar {
	if r.StageHostname == "" {
		return r.ServiceEnvs
	}
	return r.StageServiceEnvs
}

// groupRuntimeEntry composes one runtime's services[] entry under a policy.
func groupRuntimeEntry(
	r GroupRuntime,
	hostname, setupName string,
	policy groupTierPolicy,
) (map[string]any, []string) {
	var warnings []string
	// No `mode`: a runtime is always HA on the platform, so a mode/variant on
	// one is ignored — replica count is the minContainers axis below.
	entry := map[string]any{
		"hostname":     hostname,
		"type":         r.ServiceType,
		"buildFromGit": topology.CanonicalRepoURL(r.RepoURL),
		"zeropsSetup":  setupName,
	}
	if r.SubdomainEnabled {
		entry["enableSubdomainAccess"] = true
	}

	// Reflect the live scaling first (identity), then apply the tier's named
	// transforms — never a silent override.
	if w := projectScaling(entry, r.Scaling); w != "" && policy.index == 0 {
		// Only the identity tier loses information when the shape is
		// unreadable; the transformed tiers write their own floor anyway.
		warnings = append(warnings, fmt.Sprintf("%s: %s", hostname, w))
	}
	if policy.minContainers > 0 {
		minContainers := policy.minContainers
		if current, ok := entry["minContainers"].(int); ok && current > minContainers {
			minContainers = current
		}
		entry["minContainers"] = minContainers
		// A source maxContainers below the floored minimum is an interval the
		// platform rejects — raise it rather than emit min > max.
		if maxContainers, ok := entry["maxContainers"].(int); ok && maxContainers < minContainers {
			entry["maxContainers"] = minContainers
		}
	}
	if policy.cpuMode != "" {
		vertical, ok := entry["verticalAutoscaling"].(map[string]any)
		if !ok {
			vertical = map[string]any{}
			entry["verticalAutoscaling"] = vertical
		}
		vertical["cpuMode"] = policy.cpuMode
	}
	return entry, warnings
}

// groupSetupWarnings reports every runtime naming a setup its zerops.yaml does
// not declare. A warning rather than an error: this composes in a reconcile,
// and a recipe missing one setup name is still worth proposing. stageSetups
// is groupStageSetup's answer per dev hostname; one read from the yaml is
// declared by construction, a recorded one is checked like the dev half's.
func groupSetupWarnings(runtimes []GroupRuntime, stageSetups map[string]string) []string {
	var warnings []string
	for _, r := range runtimes {
		if strings.TrimSpace(r.ZeropsYAMLBody) == "" {
			warnings = append(warnings, fmt.Sprintf(
				"runtime %q: no zerops.yaml was read, so its setup %q is unverified in the recipe", r.DevHostname, r.SetupName))
			continue
		}
		declared, err := setupNamesInZeropsYAML(r.ZeropsYAMLBody)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("runtime %q: zerops.yaml does not parse (%v) — its setup is unverified", r.DevHostname, err))
			continue
		}
		for _, wanted := range dedupeStrings([]string{r.SetupName, stageSetups[r.DevHostname]}) {
			if !slices.Contains(declared, wanted) {
				warnings = append(warnings, fmt.Sprintf(
					"runtime %q: setup %q is not declared in its zerops.yaml (declared: %s) — the tier names it anyway; fix the yaml or the recorded setup",
					r.DevHostname, wanted, strings.Join(declared, ", ")))
			}
		}
	}
	return warnings
}

// groupTierProjectName names the project a tier creates. The AI Agent tier
// re-creates a MATE — a per-person project (D12) — so it carries the Mate's
// own name; the shared tiers are the group's and carry the group's.
func groupTierProjectName(inputs GroupRecipeInputs, policy groupTierPolicy) string {
	if policy.projectSuffix == "" {
		return firstNonBlank(inputs.MateProjectName, inputs.Name)
	}
	return inputs.Name + " " + policy.projectSuffix
}

// GroupPromotedHostname strips the pair's mode suffix: `apidev`/`apistage` →
// `api`. A shared environment has one runtime per app, not a pair — and it is
// the name a pair's workflow asks the broker to deploy (measured 2026-09-17:
// a workflow naming `appdev` asked a stage whose runtime is `app`).
func GroupPromotedHostname(hostname string) string {
	for _, suffix := range []string{"-dev", "-stage", "stage", "dev"} {
		if trimmed, ok := strings.CutSuffix(hostname, suffix); ok && trimmed != "" {
			return trimmed
		}
	}
	return hostname
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
