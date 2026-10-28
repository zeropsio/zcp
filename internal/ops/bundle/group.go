package bundle

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
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
// it: a pair built from one service repository — one dev half and its
// stages, each stage a part of the app (spec-mate §2.8a.4). A monorepo whose
// every part has its own dev pool is one pair per part, all on one
// repository: each pool keys the pair of its part's stage.
type GroupRuntime struct {
	// DevHostname is the pair's dev half — the hostname the AI Agent tier
	// keeps: a dev service, or the part's dev pool.
	DevHostname string
	// DevPool marks a dev half that is a dev pool (swarm@1): the AI Agent
	// tier writes it as zcp imports a part's pool — no build and no setup
	// (its image is deployed from a lease, never by an import), no container
	// until a lease asks, its own scale and service variables — and nothing
	// of it is typed, scaled or named after it: its parts take their stages'.
	DevPool bool
	// Stages are the pair's stages in ServiceMeta.Stages order, none for a
	// mode that has no stage. They exist under their own hostnames only on
	// the AI Agent tier: the group's own stage is a different project
	// entirely (D12/D16), where each is one runtime of its part.
	Stages []GroupStage
	// ServiceType is the dev half's platform type tag, e.g. "nodejs@22" —
	// "swarm@1" for a dev pool. A part is typed by its own stage
	// (groupPartFacts), never by this unless it is the dev half's own.
	ServiceType string
	// RepoURL is the pair's repository in HQ, the address its buildFromGit
	// names (hq.RepoURLAt).
	RepoURL string
	// SetupName is the `setup:` block the dev half resolves at build time.
	SetupName string
	// ZeropsYAMLBody is the pair's zerops.yaml: it verifies the named setups
	// and names a lone stage's when no deploy recorded it.
	ZeropsYAMLBody string
	// ServiceEnvs is the dev half's user-set service variables, written as
	// its `vault:` — config as written, secrets generated (recipeSecret).
	ServiceEnvs []ProjectEnvVar
	// Scaling is the live autoscaling shape. The AI Agent tier reproduces it
	// verbatim; the two transformed tiers reflect it and then apply their
	// policy floors.
	Scaling *Scaling
	// SubdomainEnabled mirrors the pair's public access. A recipe whose app
	// is unreachable is not a recipe, so it is carried on every tier rather
	// than stripped the way launch-production strips it.
	SubdomainEnabled bool
}

// GroupStage is one stage of a pair: a part of the app, which a group
// environment runs as one runtime building the stage's setup.
type GroupStage struct {
	Hostname string
	// ServiceType is the stage's own platform type as the live project runs
	// it, "" when the stage was not read (it does not run). Set, the stage's
	// Scaling and SubdomainEnabled are its own too; unset, the part is typed
	// by its setup's run.base and a dev service's scale and access stand in
	// (groupPartFacts).
	ServiceType string
	// Scaling is the stage's live autoscaling shape, nil when unread.
	Scaling *Scaling
	// SubdomainEnabled mirrors the stage's public access.
	SubdomainEnabled bool
	// SetupName is the setup the stage builds, as zcp recorded it. Empty on a
	// pair's only stage is resolved from ZeropsYAMLBody (groupStageSetup) —
	// never taken from the dev half's on a guess; empty on one of several
	// stages withholds the tiers that need it.
	SetupName string
	// ServiceEnvs is the stage's user-set service variables. The AI Agent
	// tier's stage carries them, and so does its part's runtime in every
	// group environment, which runs what the stage runs; a pair with no stage
	// gives its dev half's.
	ServiceEnvs []ProjectEnvVar
}

// HeldRuntime is a runtime a group tier on main already carries: the part it
// builds — its repository and setup — under the hostname the tier gave it
// when the part first entered it. The tier holds that name; the composer
// reads it back and never derives it again.
type HeldRuntime struct {
	Hostname  string
	RepoURL   string
	SetupName string
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
	// MateProjectID is the Mate's own project id: every tier carries it in
	// zcp's mark (StampTier), which tells a tier zcp composed from this Mate
	// and nobody changed since from one a person or another Mate wrote. ""
	// writes no mark.
	MateProjectID string
	// MateSubdomainHost is the Mate's own project's subdomain host as the
	// platform names it (`1df2.prg1.zerops.app`, or its first label alone):
	// every public address of one of its services is
	// `<hostname>-<first label>[-<port>].<region>.zerops.app`. "" names none.
	MateSubdomainHost string
	// MateOnly is the person's choice of what serves only this Mate, by
	// hostname — a utility, or a stage of a pair by the Mate's hostname:
	// true keeps it on the AI Agent tier, with a volume only it uses; false
	// carries it in every tier. Either wins over zcp's own rules (the
	// address rule, the dev tooling); a hostname it does not name is left to
	// them.
	MateOnly        map[string]bool
	Runtimes        []GroupRuntime
	ManagedServices []ManagedServiceEntry
	// ProjectEnvs is the project's user-set variables, the platform's own and
	// the control plane's already left out: every tier carries them in the
	// project's `vault:`, config as written, secrets generated (recipeSecret).
	ProjectEnvs []ProjectEnvVar
	// CorePackage is the project's live core package. Every tier's project
	// carries a LIGHT or a SERIOUS; anything else is left to the platform's
	// default.
	CorePackage string
	// Utilities are the runtimes the project builds from a public repository
	// and no pair — a mailpit, an adminer.
	Utilities []GroupUtility
	// Boxes are the Zerops Boxes the project runs (GroupBox).
	Boxes []GroupBox
	// ProjectHostnames are the hostnames of every service the Mate's project
	// runs — carried by the recipe or not: what a tier's runtimes may read.
	// A setup a tier builds that reads one the tier does not carry refuses
	// the recipe (groupTierClosure); none names nothing to refuse.
	ProjectHostnames []string
	// HAIncapable names the managed services whose type the platform ships no
	// `:ha` variant of: the tier that promotes the rest keeps them single-node.
	HAIncapable []string
	// Held is what the group tiers on main already carry (HeldRuntimes): a
	// part a tier holds keeps the hostname the tier gave it.
	Held []HeldRuntime
}

// GroupUtility is a runtime the project builds from a public repository and
// no pair: a utility such as mailpit, which no Mate develops. Every tier
// writes it as the project runs it — its own hostname, its public build, its
// own scale — with no tier's transform: there is no pair to promote and no
// service repository to name.
type GroupUtility struct {
	Hostname    string
	ServiceType string
	// BuildFromGit is the public repository its active version was built
	// from. The tiers carry its repository alone; a URL that carries a user
	// names a private one, and the tiers name no build for it.
	BuildFromGit string
	// SetupName is the setup its build named; empty lets the platform build
	// the setup named after the hostname, as an import that named none did.
	SetupName        string
	SubdomainEnabled bool
	Scaling          *Scaling
	// ServiceEnvs is its user-set service variables (recipeSecret).
	ServiceEnvs []ProjectEnvVar
}

// GroupBox is a Zerops Box the project runs (topology.IsSandboxType):
// infrastructure an app uses, never a part of it. A tier carries it where a
// runtime the tier carries reads it, and no tier carries one nothing reads —
// an agent's own Box is no part of the app.
type GroupBox struct {
	Hostname    string
	ServiceType string
	// SubdomainEnabled mirrors the Box's public access, which its sandboxes'
	// port URLs need.
	SubdomainEnabled bool
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
		summary: "The project a Zerops Mate works in: every part as its dev pool or dev service and its stage, with the services they share.",
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
// block, an unreadable scaling shape, a reference cycle, a managed type with
// no HA variant — each is a warning against a tier that still composes,
// because this runs unattended with nobody to ask. The one thing never
// guessed is what a stage-shaped entry builds: a tier naming a runtime whose
// stage setup nothing names is withheld (a warning), and a recipe with no
// tier left is an error the reconcile reports and retries — a tier that
// lands on the group repo stays there, so an absent one is proposed later
// and a wrong one never heals. So is a tier that reads a service it does not
// carry, which refuses the whole recipe as a *TierClosureError.
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
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime %q: RepoURL required (the pair's repository in HQ)", inputs.Name, r.DevHostname)
		case strings.TrimSpace(r.SetupName) == "":
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: runtime %q: SetupName required", inputs.Name, r.DevHostname)
		}
	}

	managed := dedupeManagedByHostname(inputs.ManagedServices)
	sort.SliceStable(managed, func(i, j int) bool { return managed[i].Hostname < managed[j].Hostname })

	parts := make(map[string][]groupPart, len(runtimes))
	unresolved := map[string]string{}
	var frameWarnings []string
	for _, r := range runtimes {
		setups, why := groupPartSetups(r)
		if why != "" {
			unresolved[r.DevHostname] = why
			continue
		}
		var partWarnings []string
		parts[r.DevHostname], partWarnings = groupPartsOf(r, setups)
		frameWarnings = append(frameWarnings, partWarnings...)
	}
	// What serves only the Mate is the person's to say (MateOnly); where they
	// said nothing, a runtime whose own variables name the address of a
	// service no group environment runs — a dev half, the Mate's own
	// infrastructure, a utility already so wired — is the Mate's. A stage
	// that is gets no name in the group environments, a utility stays on the
	// AI Agent tier.
	mateLabel := mateSubdomainLabel(inputs.MateSubdomainHost)
	mateHosts := mateOwnHosts(inputs)
	utilityWires := wiredUtilities(inputs.Utilities, mateHosts, mateLabel, inputs.MateOnly)
	wiredStages, ruled, stageWarnings := markMateWiredStages(runtimes, parts, mateHosts, mateLabel, inputs.MateOnly)
	boxes, boxWarnings := groupBoxes(inputs.Boxes)
	// Every service the project runs is what a tier's runtimes may read: a
	// Box a tier carries where they read it, anything else it must carry
	// already (groupTierClosure).
	projectServices := map[string]bool{}
	for _, host := range append(boxHostnames(boxes), inputs.ProjectHostnames...) {
		if host = strings.TrimSpace(host); host != "" {
			projectServices[host] = true
		}
	}
	reader := newServiceReader(slices.Sorted(maps.Keys(projectServices)))
	// A public address of a service of the Mate's own project written out is
	// what every tier writes as the environment's own (tierAddresses).
	mate := mateAddressReader{label: mateLabel, hosts: mateServiceHosts(inputs, boxes)}
	nameWarnings := nameGroupParts(runtimes, parts, inputs.Held, managed, boxesAnyPartReads(reader, boxes, runtimes, parts, inputs.Utilities, mate))
	utilities, mateOnly, wired, utilityWarnings := groupUtilities(inputs.Utilities, runtimes, parts, utilityWires, inputs.MateOnly)
	for _, host := range wired {
		if !utilityWires[host].chosen {
			ruled = append(ruled, host)
		}
	}
	wired = append(wired, wiredStages...)
	sort.Strings(wired)
	utilityWarnings = append(utilityWarnings, stageWarnings...)
	utilityWarnings = append(utilityWarnings, markMateOwnVolumes(managed, runtimes, parts, utilities, inputs.ProjectEnvs, wired, mateOnly)...)
	// Where the address rule alone decided, the pass says so once: the
	// choice is the person's.
	if line := mateRuleLine(ruled); line != "" {
		utilityWarnings = append(utilityWarnings, line)
	}

	warnings := append([]string(nil), utilityWarnings...)
	warnings = append(warnings, boxWarnings...)
	warnings = append(warnings, frameWarnings...)
	warnings = append(warnings, groupStoragePolicyWarnings(managed)...)
	warnings = append(warnings, groupSetupWarnings(runtimes, parts)...)
	warnings = append(warnings, nameWarnings...)
	priorities, priorityWarnings := groupPriorities(groupApps(runtimes, parts, utilities), inputs.ProjectEnvs)
	warnings = append(warnings, priorityWarnings...)
	haIncapable := make(map[string]bool, len(inputs.HAIncapable))
	for _, host := range inputs.HAIncapable {
		haIncapable[host] = true
	}
	plan := groupPlan{
		inputs: inputs, runtimes: runtimes, utilities: utilities, mateOnly: mateOnly,
		managed: managed, haIncapable: haIncapable, parts: parts, priorities: priorities,
		promoter: newPairPromoter(pairRenames(runtimes, parts)),
		boxes:    boxes, reader: reader, projectServices: projectServices,
		mate: mate,
	}

	layout := recipe.Layout{
		Name:  inputs.Name,
		Title: firstNonBlank(inputs.Title, inputs.Name),
		Intro: inputs.Intro,
	}
	withheld := map[string][]string{}
	carriedBoxes := map[string]bool{}
	var unclosed []string
	var kept []keptAddress
	for _, policy := range groupTiers {
		if blockers := groupTierBlockers(runtimes, policy, unresolved); len(blockers) > 0 {
			for _, host := range blockers {
				withheld[host] = append(withheld[host], policy.title)
			}
			continue
		}
		tier, err := composeGroupTierYAML(plan, policy)
		if err != nil {
			return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: tier %q: %w", inputs.Name, policy.title, err)
		}
		body := tier.body
		if mate := strings.TrimSpace(inputs.MateProjectID); mate != "" {
			body = StampTier(body, mate)
		}
		warnings = append(warnings, tier.warnings...)
		for _, host := range tier.boxes {
			carriedBoxes[host] = true
		}
		unclosed = append(unclosed, tier.unclosed...)
		kept = append(kept, tier.kept...)
		layout.Tiers = append(layout.Tiers, recipe.Tier{
			Index:      policy.index,
			Title:      policy.title,
			Slug:       policy.slug,
			ImportYAML: body,
			Summary:    policy.summary,
		})
	}

	if len(unclosed) > 0 {
		return recipe.Layout{}, nil, &TierClosureError{Recipe: inputs.Name, Reads: unclosed}
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
	warnings = append(warnings, groupBoxWarnings(boxes, carriedBoxes)...)
	warnings = append(warnings, keptAddressWarnings(kept)...)
	return layout, append(warnings, reasons...), nil
}

// groupPart is one part of a pair: the stage that runs it in the Mate's
// project ("" for a pair with none), the setup it builds there, the setup a
// group environment builds (groupEnvironmentSetup: the stage's own, or the
// setup Mate's preview variant extends), the variables its runtime carries,
// what its runtime is (facts: its type, scale and access, from its own
// stage), and the hostname a group environment runs it under — "" while it
// has none it can use (nameGroupParts), no type (facts.serviceType ""), or
// its stage is wired to the Mate (mateWired: markMateWiredStages), which
// only the AI Agent tier carries.
type groupPart struct {
	stage, setup, environmentSetup, hostname string
	envs                                     []ProjectEnvVar
	facts                                    entryFacts
	mateWired                                bool
}

// entryFacts is what one runtime entry of a tier is: its platform type, its
// live scale and its public access — each read from the service the entry
// stands for.
type entryFacts struct {
	serviceType string
	scaling     *Scaling
	subdomain   bool
}

// devHalfFacts are a dev service's own facts.
func devHalfFacts(r GroupRuntime) entryFacts {
	return entryFacts{serviceType: r.ServiceType, scaling: r.Scaling, subdomain: r.SubdomainEnabled}
}

// groupPartFacts is what a part's runtime is, from its own part: a stage the
// live project runs gives its own type, scale and access; a stage not read is
// typed by its setup's run.base, a dev service's scale and access standing
// in (a pool's are a pool's, never a part's). A pair with no stage runs its
// part on its dev service. why is non-empty when nothing types the part
// without a guess — a pool's stage that does not run and whose setup names no
// run.base — and the part is left out of every tier then; warning says a
// part typed after its dev service as the last resort.
func groupPartFacts(r GroupRuntime, stage *GroupStage, setup string) (facts entryFacts, warning, why string) {
	if stage != nil && strings.TrimSpace(stage.ServiceType) != "" {
		return entryFacts{serviceType: stage.ServiceType, scaling: stage.Scaling, subdomain: stage.SubdomainEnabled}, "", ""
	}
	if stage == nil && !r.DevPool {
		return devHalfFacts(r), "", ""
	}
	if !r.DevPool {
		facts.scaling, facts.subdomain = r.Scaling, r.SubdomainEnabled
	}
	if base := setupRunBase(r.ZeropsYAMLBody, setup); base != "" {
		facts.serviceType = base
		return facts, "", ""
	}
	part := r.DevHostname
	if stage != nil {
		part = stage.Hostname
	}
	if !r.DevPool {
		facts.serviceType = r.ServiceType
		return facts, fmt.Sprintf("part %q of %q is typed %s, as its dev service: its stage does not run and its setup %q names no run.base", part, r.DevHostname, r.ServiceType, setup), ""
	}
	return entryFacts{}, "", fmt.Sprintf("part %q of %q is left out of every tier: its stage does not run and its setup %q names no run.base, so nothing types it — deploy the stage, or give the setup a run.base", part, r.DevHostname, setup)
}

// setupRunBase is the run.base a zerops.yaml's setup names — its own, else
// that of the one setup it extends — "" when neither names one or the body
// does not parse.
func setupRunBase(body, setup string) string {
	entries := zeropsYAMLSetupEntries(body)
	for seen := map[string]bool{}; setup != "" && !seen[setup]; {
		seen[setup] = true
		entry, ok := entries[setup]
		if !ok {
			return ""
		}
		if run, ok := entry["run"].(map[string]any); ok {
			if base, ok := run["base"].(string); ok && strings.TrimSpace(base) != "" {
				return strings.TrimSpace(base)
			}
		}
		bases := yamlExtendsNames(entry["extends"])
		if len(bases) != 1 {
			return ""
		}
		setup = bases[0]
	}
	return ""
}

// groupPartSetups names the setup of each of a runtime's parts, in Stages
// order: a pair with one stage or none has one part, whose setup
// groupStageSetup names; a pair of several takes each stage's recorded
// setup, since a zerops.yaml declaring several tells no stage which is its.
// why is non-empty when one is not named without a guess.
func groupPartSetups(r GroupRuntime) ([]string, string) {
	if len(r.Stages) <= 1 {
		setup, why := groupStageSetup(r)
		if why != "" {
			return nil, why
		}
		return []string{setup}, ""
	}
	setups := make([]string, 0, len(r.Stages))
	for _, stage := range r.Stages {
		if strings.TrimSpace(stage.SetupName) == "" {
			return nil, fmt.Sprintf("its stage %q is one of %d, which no zerops.yaml tells apart", stage.Hostname, len(r.Stages))
		}
		setups = append(setups, stage.SetupName)
	}
	return setups, ""
}

// groupPartsOf is a resolved runtime's parts: one per stage with its own
// variables and facts, or the dev half's one part, with its variables, for
// a pair with no stage — and the warnings for a part a group environment
// builds framed for Mate's preview pane, typed after its dev service, or
// left out for want of a type.
func groupPartsOf(r GroupRuntime, setups []string) ([]groupPart, []string) {
	var warnings []string
	part := func(stage *GroupStage, setup string, envs []ProjectEnvVar) groupPart {
		environmentSetup, warning := groupEnvironmentSetup(r, setup)
		if warning != "" {
			warnings = append(warnings, warning)
		}
		facts, typedWarning, why := groupPartFacts(r, stage, environmentSetup)
		for _, w := range []string{typedWarning, why} {
			if w != "" {
				warnings = append(warnings, w)
			}
		}
		p := groupPart{setup: setup, environmentSetup: environmentSetup, envs: envs, facts: facts}
		if stage != nil {
			p.stage = stage.Hostname
		}
		return p
	}
	if len(r.Stages) == 0 {
		return []groupPart{part(nil, setups[0], r.ServiceEnvs)}, warnings
	}
	parts := make([]groupPart, 0, len(r.Stages))
	for i := range r.Stages {
		parts = append(parts, part(&r.Stages[i], setups[i], r.Stages[i].ServiceEnvs))
	}
	return parts, warnings
}

// nameGroupParts gives each part the hostname a group environment runs it
// under. A part a group tier on main holds keeps the tier's name — the first
// tier's in Held, when people renamed it in one — read back and never derived
// again; a part no tier holds yet is named once, by the rule a tier then
// keeps: the part's own name, its stage's stem (GroupPromotedHostname:
// `apistage` → `api`), or the dev half's stem for a pair with no stage —
// every part alike, never the repository's name for one of them. A part
// nothing types (groupPartFacts) gets no name, nor does one whose stage is
// wired to the Mate (mateWired), even one a tier holds. A new name that is no
// hostname, or that another service of the group environments already has —
// a managed service, or a Box a part reads (reserved) — is not changed on a
// guess: the part is left out of them and said.
func nameGroupParts(runtimes []GroupRuntime, parts map[string][]groupPart, held []HeldRuntime, managed []ManagedServiceEntry, reserved []string) []string {
	type partKey struct{ repo, setup string }
	heldBy := map[partKey]string{}
	for _, h := range held {
		repo, private := publicRepoURL(h.RepoURL)
		key := partKey{repo, h.SetupName}
		if _, named := heldBy[key]; !named && !private && h.SetupName != "" && h.Hostname != "" {
			heldBy[key] = h.Hostname
		}
	}
	taken := map[string]bool{}
	for _, m := range managed {
		taken[m.Hostname] = true
	}
	for _, host := range reserved {
		taken[host] = true
	}
	var fresh []*groupPart
	freshOwner := map[*groupPart]string{}
	for _, r := range runtimes {
		repo, private := publicRepoURL(r.RepoURL)
		list := parts[r.DevHostname]
		for i := range list {
			part := &list[i]
			if part.facts.serviceType == "" || part.mateWired {
				continue
			}
			if host, ok := heldBy[partKey{repo, part.environmentSetup}]; ok && !private {
				part.hostname = host
				taken[host] = true
				continue
			}
			part.hostname = GroupPromotedHostname(firstNonBlank(part.stage, r.DevHostname))
			fresh = append(fresh, part)
			freshOwner[part] = r.DevHostname
		}
	}
	var warnings []string
	for _, part := range fresh {
		name := part.hostname
		switch {
		case platform.ValidateHostname(name) != nil:
			warnings = append(warnings, fmt.Sprintf(
				"part %q of %q is left out of the group environments: its name is no hostname (lowercase letters and digits, starting with a letter) — rename its setup, or add it to the tiers by hand",
				part.environmentSetup, freshOwner[part]))
			part.hostname = ""
		case taken[name]:
			warnings = append(warnings, fmt.Sprintf(
				"part %q of %q is left out of the group environments: another of their services is already named %q — add it to the tiers by hand under a name of its own",
				part.environmentSetup, freshOwner[part], name))
			part.hostname = ""
		default:
			taken[name] = true
		}
	}
	return warnings
}

// groupStageSetup names the setup a lone-stage runtime's stage-shaped entries
// build — the AI Agent tier's stage half and the one runtime of every group
// environment. why is non-empty when nothing names it without a guess.
//
// A stage setup is recorded only by a deploy of the stage half, and a Mate
// that joined the group's repositories may never make one. Falling back to
// the dev setup built production with the dev loop's `start: zsc noop` (the
// medusa group, 2026-09-26), so the fallback is the pair's own zerops.yaml:
// the one setup it declares beside the dev one, or its only setup, which
// then serves both halves.
func groupStageSetup(r GroupRuntime) (setup, why string) {
	if len(r.Stages) == 1 && strings.TrimSpace(r.Stages[0].SetupName) != "" {
		return strings.TrimSpace(r.Stages[0].SetupName), ""
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
		if policy.pairs && len(r.Stages) == 0 {
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
	// mateOnly names the utilities and managed services a group environment
	// does not carry: a utility whose hostname its runtime takes, and what
	// serves the Mate itself (groupUtilities, markMateOwnVolumes).
	mateOnly    map[string]bool
	managed     []ManagedServiceEntry
	haIncapable map[string]bool
	// parts are each resolved runtime's parts, by dev hostname.
	parts      map[string][]groupPart
	priorities map[string]int
	// promoter names a group environment's own runtimes in a value.
	promoter *pairPromoter
	// boxes are the Boxes the project runs, sorted.
	boxes []GroupBox
	// projectServices are the hostnames of every service the project runs,
	// and reader reads them in a setup's variables.
	projectServices map[string]bool
	reader          *serviceReader
	// mate tells a public address of a service of the Mate's own project
	// written out in a value, which every tier writes as the environment's
	// own (tierAddresses).
	mate mateAddressReader
}

// groupTier is one composed tier: its import.yaml, its warnings, the Boxes
// it carries, each read of a service the project runs that it does not
// carry (groupTierClosure), and each address of the Mate's own project it
// kept as written for a person to set (tierAddresses).
type groupTier struct {
	body     string
	warnings []string
	boxes    []string
	unclosed []string
	kept     []keptAddress
}

// addressWriter is the writer of the Mate's own addresses for the tier
// policy names, promote naming a group environment's own runtimes (nil on
// the AI Agent tier): it knows what the tier carries and builds once the
// tier's services are composed.
func (plan groupPlan) addressWriter(policy groupTierPolicy, promote func(string) string) *tierAddresses {
	t := &tierAddresses{
		mate: plan.mate, title: policy.title, promote: promote,
		builds: map[string]addressBuild{}, carried: map[string]bool{},
	}
	if promote != nil {
		t.renames = plan.promoter.renames
	}
	return t
}

// mateServiceHosts is every service of the Mate's own project the inputs
// name, lowercase: each hostname the project runs, each pair's halves, the
// managed services, the utilities and the Boxes.
func mateServiceHosts(inputs GroupRecipeInputs, boxes []GroupBox) map[string]bool {
	hosts := map[string]bool{}
	add := func(host string) {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			hosts[host] = true
		}
	}
	for _, host := range inputs.ProjectHostnames {
		add(host)
	}
	for _, r := range inputs.Runtimes {
		add(r.DevHostname)
		for _, stage := range r.Stages {
			add(stage.Hostname)
		}
	}
	for _, m := range inputs.ManagedServices {
		add(m.Hostname)
	}
	for _, u := range inputs.Utilities {
		add(u.Hostname)
	}
	for _, b := range boxes {
		add(b.Hostname)
	}
	return hosts
}

// composeGroupTierYAML renders one tier's whole-project import.yaml.
func composeGroupTierYAML(plan groupPlan, policy groupTierPolicy) (groupTier, error) {
	inputs, runtimes, priorities := plan.inputs, plan.runtimes, plan.priorities
	warnings := make([]string, 0, len(runtimes))
	source := firstNonBlank(inputs.MateProjectName, inputs.Name)
	// The AI Agent tier re-creates the Mate's pairs under their own names; a
	// group environment names its own runtimes. Both write an address of a
	// service of the Mate's own project as the address of the service in the
	// environment the tier makes (tierAddresses).
	var promote func(string) string
	if !policy.pairs {
		promote = plan.promoter.promote
	}
	addresses := plan.addressWriter(policy, promote)

	// Every entry carries its priority, and the file lists the services in
	// the order the platform creates them: highest first, a pair's halves
	// together, the composer's own order kept among equals. An entry's own
	// variables are written once the tier knows everything it carries — the
	// Boxes its runtimes read among it — since an address names only what the
	// tier carries (tierAddresses): vault writes them through the tier's
	// writer for owner, whose own address is own's.
	type rankedEntry struct {
		priority   int
		comment    string
		entry      map[string]any
		owner, own string
		vault      func(write valueWriter) []yamlField
	}
	ranked := make([]rankedEntry, 0, 2*len(runtimes)+len(plan.utilities)+len(plan.managed)+len(plan.boxes))
	// readers are the tier's runtimes, pools and utilities and what they read
	// with (tierReader): the tier carries the Box they read, and must carry
	// what the setups it builds read (groupTierClosure). An address of the
	// Mate's own project written out reads nothing, so they read the values
	// as written, renamed.
	var readers []tierReader
	for _, r := range runtimes {
		type half struct {
			hostname, setup string
			envs            []ProjectEnvVar
			facts           entryFacts
		}
		var halves []half
		if policy.pairs {
			if r.DevPool {
				entry, comment := groupPoolEntry(r, priorities[r.DevHostname])
				ranked = append(ranked, rankedEntry{
					priority: priorities[r.DevHostname], comment: comment, entry: entry,
					owner: fmt.Sprintf("dev pool %q", r.DevHostname), own: r.DevHostname,
					vault: func(write valueWriter) []yamlField { return groupPoolVault(r.ServiceEnvs, source, write) },
				})
				// Its leases deploy the setup its image is.
				readers = append(readers, tierReader{hostname: r.DevHostname, setup: r.SetupName, body: r.ZeropsYAMLBody, pool: true, vault: writtenValues(r.ServiceEnvs, nil, plan.mate)})
			} else {
				halves = append(halves, half{r.DevHostname, r.SetupName, r.ServiceEnvs, devHalfFacts(r)})
			}
			for _, part := range plan.parts[r.DevHostname] {
				if part.stage != "" && part.facts.serviceType != "" {
					halves = append(halves, half{part.stage, part.setup, part.envs, part.facts})
				}
			}
		} else {
			// A group environment runs what the pair's stages run, a runtime
			// per part: the dev half's setup is the dev loop's, never a
			// stage's, and a stage wired to the Mate serves the Mate alone.
			for _, part := range plan.parts[r.DevHostname] {
				if part.hostname != "" && !part.mateWired {
					halves = append(halves, half{part.hostname, part.environmentSetup, part.envs, part.facts})
				}
			}
		}
		for _, half := range halves {
			readers = append(readers, tierReader{hostname: half.hostname, setup: half.setup, body: r.ZeropsYAMLBody, vault: writtenValues(half.envs, promote, plan.mate)})
			entry, entryWarnings := groupRuntimeEntry(r, half.facts, half.hostname, half.setup, policy)
			warnings = append(warnings, entryWarnings...)
			entry["priority"] = priorities[r.DevHostname]
			comment := ""
			if _, built := entry["buildFromGit"]; !built {
				comment = privateBuildComment(source)
			}
			addresses.builds[half.hostname] = addressBuild{body: r.ZeropsYAMLBody, setup: half.setup}
			ranked = append(ranked, rankedEntry{
				priority: priorities[r.DevHostname], comment: comment, entry: entry,
				owner: fmt.Sprintf("runtime %q", half.hostname), own: half.hostname,
				vault: func(write valueWriter) []yamlField { return groupVaultFields(half.envs, source, write) },
			})
		}
	}
	for _, u := range plan.utilities {
		if !policy.pairs && plan.mateOnly[u.Hostname] {
			continue
		}
		entry, comment := groupUtilityEntry(u, priorities[u.Hostname], source)
		ranked = append(ranked, rankedEntry{
			priority: priorities[u.Hostname], comment: comment, entry: entry,
			owner: fmt.Sprintf("runtime %q", u.Hostname), own: u.Hostname,
			vault: func(write valueWriter) []yamlField { return groupVaultFields(u.ServiceEnvs, source, write) },
		})
		// Its own variables are all the recipe reads of it.
		readers = append(readers, tierReader{hostname: u.Hostname, vault: writtenValues(u.ServiceEnvs, promote, plan.mate)})
	}
	for _, m := range plan.managed {
		if !policy.pairs && plan.mateOnly[m.Hostname] {
			continue
		}
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
		ranked = append(ranked, rankedEntry{priority: managedPriority, entry: entry})
	}
	// A Box only where a runtime of this tier reads it.
	var carriedBoxes []string
	read := tierReads(plan.reader, readers)
	for _, b := range plan.boxes {
		if len(read[b.Hostname]) == 0 {
			continue
		}
		entry, comment := groupBoxEntry(b, read[b.Hostname])
		ranked = append(ranked, rankedEntry{priority: managedPriority, comment: comment, entry: entry})
		carriedBoxes = append(carriedBoxes, b.Hostname)
	}
	// What the tier carries is now known: the closure check reads it from the
	// services as written, and so does the rewrite.
	for _, r := range ranked {
		if host, ok := r.entry["hostname"].(string); ok {
			addresses.carried[host] = true
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].priority > ranked[j].priority })
	services := make([]yamlItem, 0, len(ranked))
	for _, r := range ranked {
		if r.vault != nil {
			if vault := r.vault(addresses.writer(r.owner, r.own)); len(vault) > 0 {
				r.entry["vault"] = vault
			}
		}
		services = append(services, yamlItem{comment: r.comment, fields: orderedFields(r.entry, serviceKeyOrder)})
	}
	unclosed := groupTierClosure(plan.reader, readers, tierHostnames(services), plan.projectServices, policy.title)

	project := []yamlField{{key: "name", value: groupTierProjectName(inputs, policy)}}
	if core := strings.TrimSpace(inputs.CorePackage); core == "LIGHT" || core == "SERIOUS" {
		project = append(project, yamlField{key: "corePackage", value: core})
	}
	if vault := groupVaultFields(inputs.ProjectEnvs, source, addresses.writer("", "")); len(vault) > 0 {
		project = append(project, yamlField{key: "vault", value: vault})
	}

	body, err := tierDocument{
		header:   groupTierHeader(inputs, policy),
		project:  project,
		services: services,
	}.render()
	if err != nil {
		return groupTier{}, err
	}
	return groupTier{body: body, warnings: warnings, boxes: carriedBoxes, unclosed: unclosed, kept: addresses.kept}, nil
}

// groupTierHeader is what a tier says about itself above its project: what
// the tier is, that zcp wrote it and from which Mate, what its mark means
// (StampTier), and that it is the group's to change — zcp adds only a whole tier the group repo's main lacks
// (D30), and into one main carries proposes nothing but a service's scale
// (D34), a managed service the Mate's project gained, for every app, and, for
// an app of several parts, a part's service added or removed (spec-mate
// §2.8a.5), as the Mate's change, so a person's edit is never written over.
// It says nothing of who lands that change: that is HQ's rule (D37), which a
// file on main outlives.
func groupTierHeader(inputs GroupRecipeInputs, policy groupTierPolicy) []string {
	proposes := "only a service's scale (group-recipe scaling=<hostname>) or a managed service added to the Mate's project (group-recipe service=<hostname>)"
	if groupHasSeveralParts(inputs.Runtimes) {
		proposes = "only a service's scale (group-recipe scaling=<hostname>), a managed service added to the Mate's project or a part's service added or removed (group-recipe service=<hostname>)"
	}
	marked := strings.TrimSpace(inputs.MateProjectID) != ""
	anew := ""
	if marked {
		anew = " — and this whole file anew while it stays exactly as zcp wrote it (the mark below)"
	}
	header := []string{
		"The " + policy.title + " tier. " + policy.summary,
		fmt.Sprintf("zcp wrote this file from the Mate %q. It is the group's now: a person may edit it. zcp adds only a whole tier the group repo lacks; into a tier it already carries, zcp proposes %s, as the Mate's change%s.",
			firstNonBlank(inputs.MateProjectName, inputs.Name), proposes, anew),
		"Priority is the order services are created in, each wave deployed before the next starts: the managed services first, then every runtime before the runtimes whose build reads it.",
	}
	if marked {
		header = append(header, "The line below is zcp's mark: the Mate's project this file was composed from and a digest of the file. While the file stays exactly as zcp wrote it, zcp proposes it anew when that Mate's project changes shape — a part added or removed, a dev pool, a runtime's type; an edit of any line makes the file the group's alone.")
	}
	return header
}

// groupHasSeveralParts reports an app of several parts: a repository whose
// pairs run more than one stage between them — one dev service with a stage
// per part, or a pool-keyed pair per part on one repository.
func groupHasSeveralParts(runtimes []GroupRuntime) bool {
	parts := map[string]int{}
	for _, r := range runtimes {
		repo, _ := publicRepoURL(r.RepoURL)
		if repo == "" {
			repo = r.DevHostname
		}
		parts[repo] += max(len(r.Stages), 1)
		if parts[repo] > 1 {
			return true
		}
	}
	return false
}

// groupApps is what the priorities rank: every runtime pair, by any name it
// goes by, with the values its build reads — its zerops.yaml's build
// variables and the runtime variables they lift, its own service variables
// among them — and every utility, whose build is the platform's from a
// public repository and reads nothing of the project's.
func groupApps(runtimes []GroupRuntime, parts map[string][]groupPart, utilities []GroupUtility) []groupApp {
	apps := make([]groupApp, 0, len(runtimes)+len(utilities))
	for _, u := range utilities {
		apps = append(apps, groupApp{key: u.Hostname, hostnames: []string{u.Hostname}})
	}
	for _, r := range runtimes {
		serviceEnvs := map[string]string{}
		envs := append([]ProjectEnvVar(nil), r.ServiceEnvs...)
		hostnames := make([]string, 0, 1+len(r.Stages)+len(parts[r.DevHostname]))
		hostnames = append(hostnames, r.DevHostname)
		for _, stage := range r.Stages {
			envs = append(envs, stage.ServiceEnvs...)
			hostnames = append(hostnames, stage.Hostname)
		}
		for _, env := range envs {
			serviceEnvs[env.Key] = env.Value
		}
		for _, part := range parts[r.DevHostname] {
			hostnames = append(hostnames, part.hostname)
		}
		sources := BuildReadValues(r.ZeropsYAMLBody, "", serviceEnvs)
		apps = append(apps, groupApp{key: r.DevHostname, hostnames: hostnames, sources: sources})
	}
	return apps
}

// groupUtilities is the utilities every tier writes, sorted, and those a
// group environment does not carry: one whose hostname its runtime takes,
// one the person chose as serving only this Mate, a Mate's own dev tooling
// (devOnlyUtility) and one wired to the Mate (wires, wiredUtilities) — the
// chosen and the wired it also names, sorted, in wired. One the person chose
// to carry (choices false) is carried whatever zcp's rules read. A utility
// the reader did not describe whole is left out and said.
func groupUtilities(in []GroupUtility, runtimes []GroupRuntime, parts map[string][]groupPart, wires map[string]mateWire, choices map[string]bool) (utilities []GroupUtility, mateOnly map[string]bool, wired, warnings []string) {
	promoted := make(map[string]string, len(runtimes))
	for _, r := range runtimes {
		for _, part := range parts[r.DevHostname] {
			if part.hostname != "" {
				promoted[part.hostname] = r.DevHostname
			}
		}
	}
	mateOnly = map[string]bool{}
	for _, u := range in {
		if strings.TrimSpace(u.Hostname) == "" || strings.TrimSpace(u.ServiceType) == "" || strings.TrimSpace(u.BuildFromGit) == "" {
			warnings = append(warnings, fmt.Sprintf("utility %q is left out of the recipe: its hostname, type or public build is unknown", u.Hostname))
			continue
		}
		wire, isWired := wires[u.Hostname]
		carry, said := choices[u.Hostname]
		carry = said && !carry
		switch pair, taken := promoted[u.Hostname]; {
		case taken:
			mateOnly[u.Hostname] = true
			warnings = append(warnings, fmt.Sprintf(
				"utility %q stays on the AI Agent tier only: the group environments name the runtime of %q %q too",
				u.Hostname, pair, u.Hostname))
		case carry:
		case isWired && wire.chosen:
			mateOnly[u.Hostname] = true
			wired = append(wired, u.Hostname)
			warnings = append(warnings, wire.warning(u.Hostname))
		case devOnlyUtility(u):
			mateOnly[u.Hostname] = true
			warnings = append(warnings, fmt.Sprintf(
				"utility %q stays on the AI Agent tier only: it is a Mate's own dev tooling (a mail catcher, a database admin UI), which a stage or a production does not run — a value there naming it points nowhere until a person sets it",
				u.Hostname))
		case isWired:
			mateOnly[u.Hostname] = true
			wired = append(wired, u.Hostname)
			warnings = append(warnings, wire.warning(u.Hostname))
		}
		utilities = append(utilities, u)
	}
	sort.SliceStable(utilities, func(i, j int) bool { return utilities[i].Hostname < utilities[j].Hostname })
	sort.Strings(wired)
	return utilities, mateOnly, wired, warnings
}

// groupUtilityEntry writes a utility as the project runs it: its public build
// and its own scale, on every tier alike; the tier writes its own variables
// beside them. A build URL that carried a user carried a credential, so the
// repository is private: the entry names no build — nor the setup, which the
// platform builds only from a repository — and the comment it returns says
// the source is set by hand.
func groupUtilityEntry(u GroupUtility, priority int, source string) (map[string]any, string) {
	entry := map[string]any{
		"hostname": u.Hostname,
		"type":     u.ServiceType,
		"priority": priority,
	}
	var comment string
	if repo, private := publicRepoURL(u.BuildFromGit); private {
		comment = privateBuildComment(source)
	} else {
		entry["buildFromGit"] = repo
		if u.SetupName != "" {
			entry["zeropsSetup"] = u.SetupName
		}
	}
	if u.SubdomainEnabled {
		entry["enableSubdomainAccess"] = true
	}
	projectScaling(entry, u.Scaling)
	return entry, comment
}

// privateBuildComment heads an entry whose build the tiers do not name.
func privateBuildComment(source string) string {
	return fmt.Sprintf("Built from a private repository in %s; set its source by hand.", source)
}

// publicRepoURL is a repository URL as a recipe carries it: canonical, with
// no query and no fragment. private reports a URL that carried a user — a
// token, or a password beside one — or does not parse: a private repository
// the recipe names nowhere.
func publicRepoURL(raw string) (repo string, private bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return "", true
	}
	u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = "", false, "", ""
	return topology.CanonicalRepoURL(u.String()), false
}

// groupStoragePolicyWarnings says which object storage keeps a policy the
// tiers do not carry: a custom one — a document, which can hold a secret
// condition such as a Referer the bucket trusts — or one the platform does
// not name. managedEntryWithRules writes neither, so the tiers leave the
// platform's default, private.
func groupStoragePolicyWarnings(managed []ManagedServiceEntry) []string {
	var warnings []string
	for _, m := range managed {
		if m.ObjectStoragePolicy != "" && !namedObjectStoragePolicies[m.ObjectStoragePolicy] {
			warnings = append(warnings, fmt.Sprintf(
				"object storage %q: its %s access policy is not in the recipe, since a policy document can hold a secret condition — the tiers leave the platform's default, private; set its policy on them by hand",
				m.Hostname, m.ObjectStoragePolicy))
		}
	}
	return warnings
}

// managedVertical is the vertical scale a managed service is written with:
// as it runs, on every tier but the one that decides a profile-bearing
// service's scale — Small Production sets the production profile, and a
// dev-sized database's bounds beside it would cap production. An object
// storage has none; its size is objectStorageSize.
func managedVertical(m ManagedServiceEntry, policy groupTierPolicy) map[string]any {
	if RulesForType(m.Type).RequiresObjectStorageSize {
		return nil
	}
	if policy.promoteHA && topology.IsProfileBearing(m.Type) {
		return nil
	}
	if m.Scaling == nil {
		// A scale that could not be read writes no block: a floor alone
		// would read as the whole scale and drop the rest.
		return nil
	}
	shape := map[string]any{}
	projectScaling(shape, m.Scaling)
	vertical, _ := shape["verticalAutoscaling"].(map[string]any)
	return ApplyResourceFloor(m.Type, vertical)
}

// resourceFloor is the least a kind of service is written with in a group
// recipe: the minimal viable resources it needs to come up and do its work
// on a fresh project, not a size for its load.
type resourceFloor struct {
	minRAM       float64
	minFreeRAMGB float64
}

// resourceFloors are the kinds that get a floor, by canonical base name. A
// search engine reindexes in a burst that grows memory faster than vertical
// autoscaling reacts — a Meilisearch stood up from a group recipe at 1 GB ran
// out of memory reindexing — so it needs room up front and a buffer. Every
// other kind keeps the source's numbers: runtimes are sized by their recipe
// and their dev halves should stay light, a database's production scale is
// its profile, an object storage has a size rather than memory, and no other
// kind has failed for want of a floor.
var resourceFloors = map[string]resourceFloor{
	"meilisearch":   {minRAM: 2, minFreeRAMGB: 0.5},
	"elasticsearch": {minRAM: 2, minFreeRAMGB: 0.5},
	"typesense":     {minRAM: 2, minFreeRAMGB: 0.5},
}

// ApplyResourceFloor raises a service's vertical scale to its kind's floor
// (resourceFloors): each value the max of the source and the floor, maxRam
// raised only when it would fall below minRam. It never lowers a value, and a
// kind with no floor is returned as it came.
func ApplyResourceFloor(serviceType string, vertical map[string]any) map[string]any {
	floor, ok := resourceFloors[topology.CanonicalBaseName(serviceType)]
	if !ok {
		return vertical
	}
	if vertical == nil {
		vertical = map[string]any{}
	}
	if scalingNumber(vertical["minRam"]) < floor.minRAM {
		vertical["minRam"] = floor.minRAM
	}
	if maxRAM, ok := vertical["maxRam"]; ok && scalingNumber(maxRAM) < scalingNumber(vertical["minRam"]) {
		vertical["maxRam"] = vertical["minRam"]
	}
	if scalingNumber(vertical["minFreeRamGB"]) < floor.minFreeRAMGB {
		vertical["minFreeRamGB"] = floor.minFreeRAMGB
	}
	return vertical
}

// scalingNumber reads a vertical scale value as a number; 0 when absent.
func scalingNumber(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

// groupRuntimeEntry composes one runtime's services[] entry under a policy:
// what it is — its type, scale and access — from facts, the service it
// stands for; what builds it from the pair.
func groupRuntimeEntry(
	r GroupRuntime,
	facts entryFacts,
	hostname, setupName string,
	policy groupTierPolicy,
) (map[string]any, []string) {
	var warnings []string
	// No `mode`: a runtime is always HA on the platform, so a mode/variant on
	// one is ignored — replica count is the minContainers axis below.
	entry := map[string]any{
		"hostname": hostname,
		"type":     facts.serviceType,
	}
	// A pair's URL comes from a git-push setup that refuses a user, and is
	// guarded the way a utility's is all the same: a user is a credential,
	// and the tier then names no build — nor the setup, which the platform
	// builds only from a repository.
	if repo, private := publicRepoURL(r.RepoURL); !private {
		entry["buildFromGit"] = repo
		entry["zeropsSetup"] = setupName
	}
	if facts.subdomain {
		entry["enableSubdomainAccess"] = true
	}

	// Reflect the live scaling first (identity), then apply the tier's named
	// transforms — never a silent override.
	if w := projectScaling(entry, facts.scaling); w != "" && policy.index == 0 {
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
// and a recipe missing one setup name is still worth proposing. parts holds
// each part's setups per dev hostname — its stage's and the one a group
// environment builds; one read from the yaml is declared by construction, a
// recorded one is checked like the dev half's.
func groupSetupWarnings(runtimes []GroupRuntime, parts map[string][]groupPart) []string {
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
		// Each setup a tier names: the dev half's and each stage's on the AI
		// Agent tier, and the one a group environment builds for each part —
		// the setup a preview variant extends where the stage builds one.
		wants := make([]string, 0, 1+2*len(parts[r.DevHostname]))
		wants = append(wants, r.SetupName)
		for _, part := range parts[r.DevHostname] {
			wants = append(wants, part.setup, part.environmentSetup)
		}
		for _, wanted := range dedupeStrings(wants) {
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

// GroupPromotedHostname strips the pair's mode suffix: `apidev`/`apistage`/
// `apipool` → `api`. A shared environment has one runtime per app, not a pair — and it is
// the name a stage's or production's runtime has (measured 2026-09-17: a
// deploy naming `appdev` asked a stage whose runtime is `app`).
func GroupPromotedHostname(hostname string) string {
	for _, suffix := range []string{"-dev", "-stage", "-pool", "stage", "dev", "pool"} {
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
