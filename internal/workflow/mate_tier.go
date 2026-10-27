package workflow

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// A Mate's tier is the recipe repository's `0 — AI Agent/import.yaml`: the
// whole project a Mate of the application is, every runtime a dev/stage pair
// built from one of the application's repositories in HQ, beside the managed
// services they share and whatever the platform builds from a public
// repository. A new Mate's browser imports it with the dev halves empty and
// the stage halves waiting for their first deploy; zcp's stand-up reads the
// same file to know which pairs to adopt, wire and deploy. This file is that
// read — pure, no I/O.

// ErrMateTierUnreadable is a tier that is not a services document at all.
var ErrMateTierUnreadable = errors.New("the AI Agent tier does not read as an import")

// ErrMateTierNoPairs is a tier that reads but names no dev/stage pair built
// from the application's repositories — nothing a Mate could stand up.
var ErrMateTierNoPairs = errors.New("the AI Agent tier names no dev/stage pair built from the application's repositories")

// MateTierRuntime is one half of a pair as the tier declares it.
type MateTierRuntime struct {
	Hostname string
	Type     string
	// Setup is the zerops.yaml setup the half builds with: its zeropsSetup,
	// or its hostname when it names none — the platform's own default.
	Setup    string
	Priority int
	// Envs are its own service variables as the tier writes them — its
	// vault, or the deprecated envVariables and envSecrets — which a build
	// lifts as ${RUNTIME_X}.
	Envs map[string]string
}

// MateTierPair is a dev/stage pair built from one of the application's
// repositories: one dev half and a stage per part (docs/spec-mate.md
// §2.8a.4) — for a pair zcp already records as variant B also the further
// dev services that share its code volume (§2.8a.4a).
type MateTierPair struct {
	// Repository is the halves' buildFromGit, canonical
	// (topology.CanonicalRepoURL).
	Repository string
	// RepoName is the repository's name in the application — what HQ is
	// asked for.
	RepoName string
	Dev      MateTierRuntime
	// Stages are the pair's stages, never empty: the one named like the dev
	// half first, then the tier's order.
	Stages []MateTierRuntime
	// CodeVolume is the tier's local-storage service a recorded variant-B
	// pair keeps its repository on, "" for any other pair.
	CodeVolume string
	// DevServices are a recorded variant-B pair's further dev services, in
	// the tier's order.
	DevServices []MateTierRuntime
}

// FirstStage is the pair's first stage.
func (p MateTierPair) FirstStage() MateTierRuntime {
	return p.Stages[0]
}

// Priority is the pair's place in the deploy order: the highest of its
// services' import priorities, so one pair is never split across two waves.
func (p MateTierPair) Priority() int {
	priority := p.Dev.Priority
	for _, rt := range append(slices.Clone(p.Stages), p.DevServices...) {
		priority = max(priority, rt.Priority)
	}
	return priority
}

// MateTierSkipReason says why a service of the tier is no pair to stand up.
type MateTierSkipReason string

const (
	// MateTierSkipManaged is a managed service: the platform created it.
	MateTierSkipManaged MateTierSkipReason = "managed"
	// MateTierSkipPlatformBuild builds from a repository outside the Mate's
	// HQ — a public one, since the platform clones only those — so the
	// platform built it at import.
	MateTierSkipPlatformBuild MateTierSkipReason = "platform-build"
	// MateTierSkipNoRepository is a runtime that names no repository.
	MateTierSkipNoRepository MateTierSkipReason = "no-repository"
	// MateTierSkipForeign builds from the Mate's HQ, but from another
	// application's repository, which HQ neither gives nor joins it.
	MateTierSkipForeign MateTierSkipReason = "foreign-repository"
	// MateTierSkipUnpaired builds from an application repository and has no
	// partner: no stage half, no dev half, or more than one candidate for
	// either.
	MateTierSkipUnpaired MateTierSkipReason = "unpaired"
)

// MateTierSkip is a service of the tier the stand-up leaves as it is.
type MateTierSkip struct {
	Hostname string
	Type     string
	Reason   MateTierSkipReason
	// Source is the repository it builds from, when it names one.
	Source string
}

// MateTier is what the stand-up reads from a tier: the pairs, highest
// priority first, and every other service with the reason it is not one.
type MateTier struct {
	Pairs   []MateTierPair
	Skipped []MateTierSkip
	// ProjectEnvs are the project's vault (or its deprecated envVariables),
	// as the tier writes them:
	// what a build reads through a `${NAME}`.
	ProjectEnvs map[string]string
}

// ParseMateTier reads an application's AI Agent tier. address is the Mate's
// HQ and appID the application HQ holds it in: a runtime is a pair's half
// only when it builds from a repository of that application there,
// `<address>/git/<orgID>/<appID>/<repo>` (or, in a tier written before HQ
// named the organisation, `<address>/git/<appID>/<repo>`; hqRepoOf).
//
// Pairs are formed per repository by zcp's naming convention, never by a
// setup's name — a group names its setups after the pair (`medusadev`,
// `medusaprod`). A stage is a runtime whose hostname ends in `stage`
// (IsStageHostname). A repository with one other runtime is one pair: that
// runtime is the dev half and every stage is one of its stages, a part each
// (docs/spec-mate.md §2.8a.4). Otherwise each stage pairs with the runtime
// named like it (`apistage` → `apidev` or `api`). A runtime that fits no pair
// is skipped by name, never guessed at.
//
// zcp sets up no new code volume (§2.8a.4a), so a tier's local-storage
// service is a managed service like any other. recorded are the metas zcp
// holds: a repository with several other runtimes is read as one variant-B
// pair only when one of them is the dev half of a recorded pair keeping its
// repository on the tier's only local-storage service — that runtime the dev
// half, the rest its further dev services, every stage of the repository its
// stage. A stand-up called again on such a project thus adopts the pair it
// recorded, never a second pair for one of its services.
//
// An error only when the document is not a services list, or when it names
// no pair at all (ErrMateTierNoPairs, with Skipped still filled so the caller
// can say what the tier holds instead).
func ParseMateTier(importYAML, address, appID string, recorded ...*ServiceMeta) (MateTier, error) {
	var doc recipeImportDoc
	if err := yaml.Unmarshal([]byte(importYAML), &doc); err != nil {
		return MateTier{}, fmt.Errorf("%w: %w", ErrMateTierUnreadable, err)
	}
	if len(doc.Services) == 0 {
		return MateTier{}, fmt.Errorf("%w: it declares no services", ErrMateTierUnreadable)
	}

	type candidate struct {
		svc   recipeImportService
		index int
	}
	var (
		tier    MateTier
		skipped = map[int]MateTierSkip{}
		byRepo  = map[string][]candidate{}
		order   []string
		volumes []string
	)
	for _, svc := range doc.Services {
		if topology.IsLocalStorageType(svc.Type) {
			volumes = append(volumes, strings.TrimSpace(svc.Hostname))
		}
	}
	keep := codeVolumeKept{}
	if len(volumes) == 1 {
		keep = recordedCodeVolume(recorded, volumes[0])
	}
	for i, svc := range doc.Services {
		hostname := strings.TrimSpace(svc.Hostname)
		if hostname == "" {
			return MateTier{}, fmt.Errorf("%w: service %d has no hostname", ErrMateTierUnreadable, i+1)
		}
		source := topology.CanonicalRepoURL(svc.BuildFromGit.URL)
		skip := MateTierSkip{Hostname: hostname, Type: svc.Type, Source: source}
		switch {
		case source == "" && topology.IsManagedService(svc.Type):
			skip.Reason = MateTierSkipManaged
		case source == "":
			skip.Reason = MateTierSkipNoRepository
		default:
			app, _, onHQ := hqRepoOf(source, address)
			if !onHQ {
				skip.Reason = MateTierSkipPlatformBuild
				break
			}
			if app != appID {
				skip.Reason = MateTierSkipForeign
				break
			}
			if _, seen := byRepo[source]; !seen {
				order = append(order, source)
			}
			byRepo[source] = append(byRepo[source], candidate{svc: svc, index: i})
			continue
		}
		skipped[i] = skip
	}

	for _, repo := range order {
		members := byRepo[repo]
		_, name, _ := hqRepoOf(repo, address)
		runtimes := make([]recipeImportService, len(members))
		for i, m := range members {
			runtimes[i] = m.svc
		}
		pairs, unpaired := pairRepositoryRuntimes(runtimes, keep)
		for _, p := range pairs {
			pair := MateTierPair{Repository: repo, RepoName: name, Dev: mateTierRuntime(p.dev)}
			for _, stage := range p.stages {
				pair.Stages = append(pair.Stages, mateTierRuntime(stage))
			}
			if len(p.devServices) > 0 {
				pair.CodeVolume = keep.volume
			}
			for _, ds := range p.devServices {
				pair.DevServices = append(pair.DevServices, mateTierRuntime(ds))
			}
			tier.Pairs = append(tier.Pairs, pair)
		}
		for _, m := range members {
			if unpaired[m.svc.Hostname] {
				skipped[m.index] = MateTierSkip{Hostname: m.svc.Hostname, Type: m.svc.Type, Reason: MateTierSkipUnpaired, Source: repo}
			}
		}
	}

	indexes := make([]int, 0, len(skipped))
	for i := range skipped {
		indexes = append(indexes, i)
	}
	sort.Ints(indexes)
	for _, i := range indexes {
		tier.Skipped = append(tier.Skipped, skipped[i])
	}
	sort.SliceStable(tier.Pairs, func(i, j int) bool {
		if tier.Pairs[i].Priority() != tier.Pairs[j].Priority() {
			return tier.Pairs[i].Priority() > tier.Pairs[j].Priority()
		}
		return tier.Pairs[i].Dev.Hostname < tier.Pairs[j].Dev.Hostname
	})

	if len(tier.Pairs) == 0 {
		held := make([]string, 0, len(tier.Skipped))
		for _, s := range tier.Skipped {
			held = append(held, s.Hostname+" ("+string(s.Reason)+")")
		}
		return tier, fmt.Errorf("%w; it holds %s", ErrMateTierNoPairs, strings.Join(held, ", "))
	}
	tier.ProjectEnvs = vaultAndLegacyEnvs(doc.Project.Vault, doc.Project.EnvVariables)
	return tier, nil
}

// repositoryPair is one pair of a repository's runtimes.
type repositoryPair struct {
	dev         recipeImportService
	devServices []recipeImportService
	stages      []recipeImportService
}

// codeVolumeKept is the variant-B pair a tier keeps: the local-storage
// service a recorded pair keeps its repository on and that pair's dev half,
// both "" when the tier keeps none.
type codeVolumeKept struct {
	volume, dev string
}

// recordedCodeVolume is the recorded variant-B pair keeping its repository on
// volume — never a pair already moved onto a dev pool.
func recordedCodeVolume(recorded []*ServiceMeta, volume string) codeVolumeKept {
	for _, m := range recorded {
		if m != nil && m.CodeVolume != nil && !m.DevIsPool() && m.CodeVolume.Hostname == volume {
			return codeVolumeKept{volume: volume, dev: m.Hostname}
		}
	}
	return codeVolumeKept{}
}

// pairRepositoryRuntimes pairs the runtimes one repository builds (see
// ParseMateTier): every stage (IsStageHostname) belongs to the only other
// runtime; with several others, one of them the dev half of the variant-B
// pair the tier keeps, to that pair of all of them; else each stage takes the
// runtime named like it (`apistage` → `apidev` or `api`). unpaired names
// every runtime left over.
func pairRepositoryRuntimes(runtimes []recipeImportService, keep codeVolumeKept) (pairs []repositoryPair, unpaired map[string]bool) {
	unpaired = map[string]bool{}
	var stages, others []recipeImportService
	for _, r := range runtimes {
		if IsStageHostname(r.Hostname) {
			stages = append(stages, r)
		} else {
			others = append(others, r)
		}
	}
	devAt := slices.IndexFunc(others, func(r recipeImportService) bool { return keep.dev != "" && r.Hostname == keep.dev })
	switch {
	case len(stages) > 0 && len(others) == 1:
		return []repositoryPair{{dev: others[0], stages: stagesOf(others[0], stages)}}, unpaired
	case len(stages) > 0 && len(others) > 1 && devAt >= 0:
		dev := others[devAt]
		devServices := slices.Delete(slices.Clone(others), devAt, devAt+1)
		return []repositoryPair{{dev: dev, devServices: devServices, stages: stagesOf(dev, stages)}}, unpaired
	}
	taken := map[string]bool{}
	for _, stage := range stages {
		stem := strings.TrimSuffix(stage.Hostname, stageHostnameSuffix)
		var partner *recipeImportService
		for i := range others {
			h := others[i].Hostname
			if !taken[h] && (h == stem+devHostnameSuffix || h == stem) {
				partner = &others[i]
				break
			}
		}
		if partner == nil {
			unpaired[stage.Hostname] = true
			continue
		}
		taken[partner.Hostname] = true
		pairs = append(pairs, repositoryPair{dev: *partner, stages: []recipeImportService{stage}})
	}
	for _, other := range others {
		if !taken[other.Hostname] {
			unpaired[other.Hostname] = true
		}
	}
	return pairs, unpaired
}

// stagesOf orders a dev half's stages: the one named like it first
// (`appdev` → `appstage`), then the tier's order.
func stagesOf(dev recipeImportService, stages []recipeImportService) []recipeImportService {
	named := strings.TrimSuffix(dev.Hostname, devHostnameSuffix) + stageHostnameSuffix
	out := make([]recipeImportService, 0, len(stages))
	for _, s := range stages {
		if s.Hostname == named {
			out = append(out, s)
		}
	}
	for _, s := range stages {
		if s.Hostname != named {
			out = append(out, s)
		}
	}
	return out
}

// mateTierRuntime is one half as the stand-up needs it.
func mateTierRuntime(svc recipeImportService) MateTierRuntime {
	setup := strings.TrimSpace(svc.ZeropsSetup)
	if setup == "" {
		setup = svc.Hostname
	}
	return MateTierRuntime{Hostname: svc.Hostname, Type: svc.Type, Setup: setup, Priority: svc.Priority, Envs: serviceEnvs(svc)}
}

// serviceEnvs is a service's own variables as the tier writes them, its
// vault and the deprecated envVariables and envSecrets in one map; nil when
// it has none.
func serviceEnvs(svc recipeImportService) map[string]string {
	return vaultAndLegacyEnvs(svc.Vault, svc.EnvSecrets, svc.EnvVariables)
}

// vaultAndLegacyEnvs merges a vault with the deprecated blocks a tier written
// before vault: may still carry — the vault wins a key both hold, a later
// legacy block an earlier one. Nil when all are empty.
func vaultAndLegacyEnvs(vault importVault, legacy ...map[string]any) map[string]string {
	size := len(vault)
	for _, block := range legacy {
		size += len(block)
	}
	if size == 0 {
		return nil
	}
	envs := make(map[string]string, size)
	for _, block := range legacy {
		for key, value := range block {
			envs[key] = fmt.Sprint(value)
		}
	}
	maps.Copy(envs, vault)
	return envs
}

// hqRepoOf is the application and repository a repository URL names on the
// HQ at address — https, on HQ's own host and port — and whether it names one
// there at all. HQ serves a repository at `<address>/git/<orgID>/<appID>/<repo>`
// (hq.RepoURLAt, what zcp's group recipe writes); a tier written before HQ
// named the organisation carries `<address>/git/<appID>/<repo>`, and still
// names the same repository: the application, never the organisation, is
// what places it in HQ.
func hqRepoOf(repoURL, address string) (appID, repo string, ok bool) {
	u, err := url.Parse(topology.CanonicalRepoURL(repoURL))
	if err != nil || !strings.EqualFold(u.Scheme, "https") {
		return "", "", false
	}
	hq, err := url.Parse(strings.TrimRight(address, "/"))
	if err != nil || !strings.EqualFold(hq.Scheme, "https") || !strings.EqualFold(u.Host, hq.Host) {
		return "", "", false
	}
	rest, under := strings.CutPrefix(u.Path, hq.Path+"/git/")
	if !under {
		return "", "", false
	}
	segments := strings.Split(rest, "/")
	if slices.Contains(segments, "") {
		return "", "", false
	}
	switch len(segments) {
	case 3:
		return segments[1], segments[2], true
	case 2:
		return segments[0], segments[1], true
	}
	return "", "", false
}
