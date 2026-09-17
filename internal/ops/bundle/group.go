package bundle

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

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
// Secrets are export's classification, unchanged: infrastructure and excluded
// entries are dropped, auto-secrets become a generator directive the target
// project expands, and anything else — INCLUDING an unclassified user-set
// service env — becomes REPLACE_ME. No value a Mate holds reaches the group
// repo verbatim by default.

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
	// StageSetupName is the stage half's block; empty falls back to SetupName.
	StageSetupName string
	// ZeropsYAMLBody is the pair's zerops.yaml, read only to check that the
	// named setups are declared in it.
	ZeropsYAMLBody string
	// ServiceEnvs is the runtime's user-set per-service env layer, emitted as
	// `envSecrets` through the same classification as export.
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
	ProjectEnvs     []ProjectEnvVar
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
// with nobody to ask.
func BuildGroupRecipe(
	inputs GroupRecipeInputs,
	classifications map[string]topology.SecretClassification,
) (recipe.Layout, []string, error) {
	if strings.TrimSpace(inputs.Name) == "" {
		return recipe.Layout{}, nil, fmt.Errorf("group recipe: Name required (the group's slug)")
	}
	if len(inputs.Runtimes) == 0 {
		return recipe.Layout{}, nil, fmt.Errorf("group recipe %q: at least one runtime required", inputs.Name)
	}
	if classifications == nil {
		classifications = map[string]topology.SecretClassification{}
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

	var warnings []string
	warnings = append(warnings, groupSetupWarnings(runtimes)...)

	layout := recipe.Layout{
		Name:  inputs.Name,
		Title: firstNonBlank(inputs.Title, inputs.Name),
		Intro: inputs.Intro,
	}
	for _, policy := range groupTiers {
		body, tierWarnings, err := composeGroupTierYAML(inputs, runtimes, managed, policy, classifications)
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
	return layout, warnings, nil
}

// composeGroupTierYAML renders one tier's whole-project import.yaml.
func composeGroupTierYAML(
	inputs GroupRecipeInputs,
	runtimes []GroupRuntime,
	managed []ManagedServiceEntry,
	policy groupTierPolicy,
	classifications map[string]topology.SecretClassification,
) (string, []string, error) {
	projectEnvs, warnings := composeProjectEnvVariables(inputs.ProjectEnvs, classifications)

	services := make([]any, 0, 2*len(runtimes)+len(managed))
	allSecrets := map[string]string{}
	for _, r := range runtimes {
		halves := []struct{ hostname, setup string }{}
		if policy.pairs {
			halves = append(halves, struct{ hostname, setup string }{r.DevHostname, r.SetupName})
			if r.StageHostname != "" {
				halves = append(halves, struct{ hostname, setup string }{
					r.StageHostname, firstNonBlank(r.StageSetupName, r.SetupName),
				})
			}
		} else {
			// A group environment runs what the pair's stage half runs: the
			// dev half's setup is the dev loop's, never a stage's.
			halves = append(halves, struct{ hostname, setup string }{
				groupPromotedHostname(r.DevHostname), firstNonBlank(r.StageSetupName, r.SetupName),
			})
		}
		for _, half := range halves {
			entry, entryWarnings := groupRuntimeEntry(r, half.hostname, half.setup, policy, classifications)
			warnings = append(warnings, entryWarnings...)
			if secrets, ok := entry["envSecrets"].(map[string]string); ok {
				maps.Copy(allSecrets, secrets)
			}
			services = append(services, entry)
		}
	}
	for _, m := range managed {
		services = append(services, managedEntryWithRules(m, policy.promoteHA, false /*keepNonHA*/))
	}

	project := map[string]any{"name": groupTierProjectName(inputs, policy)}
	if len(projectEnvs) > 0 {
		project["envVariables"] = projectEnvs
	}

	out, err := yaml.Marshal(map[string]any{"project": project, "services": services})
	if err != nil {
		return "", nil, fmt.Errorf("marshal: %w", err)
	}
	return addPreprocessorHeader(string(out), projectEnvs, allSecrets), warnings, nil
}

// groupRuntimeEntry composes one runtime's services[] entry under a policy.
func groupRuntimeEntry(
	r GroupRuntime,
	hostname, setupName string,
	policy groupTierPolicy,
	classifications map[string]topology.SecretClassification,
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

	secrets, secretWarnings := composeServiceEnvSecrets(r.ServiceEnvs, classifications)
	warnings = append(warnings, secretWarnings...)
	if len(secrets) > 0 {
		entry["envSecrets"] = secrets
	}
	return entry, warnings
}

// groupSetupWarnings reports every runtime naming a setup its zerops.yaml does
// not declare. A warning rather than an error: this composes in a reconcile,
// and a recipe missing one setup name is still worth proposing.
func groupSetupWarnings(runtimes []GroupRuntime) []string {
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
		for _, wanted := range dedupeStrings([]string{r.SetupName, r.StageSetupName}) {
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

// groupPromotedHostname strips the pair's mode suffix: `apidev`/`apistage` →
// `api`. A shared environment has one runtime per app, not a pair.
func groupPromotedHostname(hostname string) string {
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
