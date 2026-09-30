package workflow

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/topology"
)

// A Mate's tier is the group repo's `0 — AI Agent/import.yaml` (D13): the
// whole project a Mate of the group is, every runtime a dev/stage pair built
// from one of the group's repositories, beside the managed services they share
// and whatever the platform builds from a public repository. A new Mate's
// browser imports it with the dev halves empty and the stage halves waiting
// for their first deploy; zcp's stand-up reads the same file to know which
// pairs to adopt, wire and deploy. This file is that read — pure, no I/O.

// MateTierImportPath is where a group repo keeps the AI Agent tier, exactly as
// the recipe layout names it (internal/recipe: `<index> — <title>`).
const MateTierImportPath = "0 — AI Agent/import.yaml"

// ErrMateTierUnreadable is a tier that is not a services document at all.
var ErrMateTierUnreadable = errors.New("the AI Agent tier does not read as an import")

// ErrMateTierNoPairs is a tier that reads but names no dev/stage pair built
// from the group's repositories — nothing a Mate could stand up.
var ErrMateTierNoPairs = errors.New("the AI Agent tier names no dev/stage pair built from the group's repositories")

// MateTierRuntime is one half of a pair as the tier declares it.
type MateTierRuntime struct {
	Hostname string
	Type     string
	// Setup is the zerops.yaml setup the half builds with: its zeropsSetup,
	// or its hostname when it names none — the platform's own default.
	Setup    string
	Priority int
	// Envs are its own service variables as the tier writes them — its
	// envVariables and envSecrets — which a build lifts as ${RUNTIME_X}.
	Envs map[string]string
}

// MateTierPair is a dev/stage pair built from one of the group's repositories.
type MateTierPair struct {
	// Repository is the halves' buildFromGit, canonical
	// (topology.CanonicalRepoURL).
	Repository string
	// RepoName is the repository's name in the group's org — what the broker
	// is asked for.
	RepoName string
	Dev      MateTierRuntime
	Stage    MateTierRuntime
}

// Priority is the pair's place in the deploy order: the higher of its halves'
// import priorities, so one pair is never split across two waves.
func (p MateTierPair) Priority() int {
	return max(p.Dev.Priority, p.Stage.Priority)
}

// MateTierSkipReason says why a service of the tier is no pair to stand up.
type MateTierSkipReason string

const (
	// MateTierSkipManaged is a managed service: the platform created it.
	MateTierSkipManaged MateTierSkipReason = "managed"
	// MateTierSkipPlatformBuild builds from a repository outside the group's
	// Gitea — a public one, since the platform clones only those — so the
	// platform built it at import.
	MateTierSkipPlatformBuild MateTierSkipReason = "platform-build"
	// MateTierSkipNoRepository is a runtime that names no repository.
	MateTierSkipNoRepository MateTierSkipReason = "no-repository"
	// MateTierSkipForeign builds from the group's Gitea, but from another
	// org's repository, which the group's broker neither gives nor joins.
	MateTierSkipForeign MateTierSkipReason = "foreign-repository"
	// MateTierSkipUnpaired builds from a group repository and has no partner:
	// no stage half, no dev half, or more than one candidate for either.
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
	// ProjectEnvs are the project's envVariables, as the tier writes them:
	// what a build reads through a `${NAME}`.
	ProjectEnvs map[string]string
}

// ParseMateTier reads a group's AI Agent tier. giteaURL is the group's Gitea
// (GITEA_URL) and org the group's org on it: a runtime is a pair's half only
// when it builds from a repository of that org there.
//
// Pairs are formed per repository by zcp's naming convention, never by a
// setup's name — a group names its setups after the pair (`medusadev`,
// `medusaprod`). The stage half is the runtime whose hostname ends in `stage`
// (IsStageHostname); its dev half is the repository's other runtime, or, when
// one repository builds several pairs, the one named like it (`apistage` →
// `apidev` or `api`). A runtime that fits no pair is skipped by name, never
// guessed at.
//
// An error only when the document is not a services list, or when it names
// no pair at all (ErrMateTierNoPairs, with Skipped still filled so the caller
// can say what the tier holds instead).
func ParseMateTier(importYAML, giteaURL, org string) (MateTier, error) {
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
	)
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
		case topology.ClassifyGitHost(source, giteaURL) != topology.GitHostGitea:
			skip.Reason = MateTierSkipPlatformBuild
		default:
			repoOrg, _ := giteaRepoPath(source)
			if !strings.EqualFold(repoOrg, org) {
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
		_, name := giteaRepoPath(repo)
		runtimes := make([]recipeImportService, len(members))
		for i, m := range members {
			runtimes[i] = m.svc
		}
		pairs, unpaired := pairRepositoryRuntimes(runtimes)
		for _, p := range pairs {
			tier.Pairs = append(tier.Pairs, MateTierPair{
				Repository: repo,
				RepoName:   name,
				Dev:        mateTierRuntime(p[0]),
				Stage:      mateTierRuntime(p[1]),
			})
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
	if len(doc.Project.EnvVariables) > 0 {
		tier.ProjectEnvs = make(map[string]string, len(doc.Project.EnvVariables))
		for key, value := range doc.Project.EnvVariables {
			tier.ProjectEnvs[key] = fmt.Sprint(value)
		}
	}
	return tier, nil
}

// pairRepositoryRuntimes pairs the runtimes one repository builds. Every stage
// half (IsStageHostname) takes the other runtime when the repository builds
// exactly two, or else the one named like it (`apistage` → `apidev` or
// `api`). Each pair is [dev, stage]; unpaired names every runtime left over.
func pairRepositoryRuntimes(runtimes []recipeImportService) (pairs [][2]recipeImportService, unpaired map[string]bool) {
	unpaired = map[string]bool{}
	var stages, others []recipeImportService
	for _, r := range runtimes {
		if IsStageHostname(r.Hostname) {
			stages = append(stages, r)
		} else {
			others = append(others, r)
		}
	}
	if len(stages) == 1 && len(others) == 1 {
		return [][2]recipeImportService{{others[0], stages[0]}}, unpaired
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
		pairs = append(pairs, [2]recipeImportService{*partner, stage})
	}
	for _, other := range others {
		if !taken[other.Hostname] {
			unpaired[other.Hostname] = true
		}
	}
	return pairs, unpaired
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
// envVariables and envSecrets in one map; nil when it has none.
func serviceEnvs(svc recipeImportService) map[string]string {
	if len(svc.EnvVariables)+len(svc.EnvSecrets) == 0 {
		return nil
	}
	envs := make(map[string]string, len(svc.EnvVariables)+len(svc.EnvSecrets))
	for _, block := range []map[string]any{svc.EnvSecrets, svc.EnvVariables} {
		for key, value := range block {
			envs[key] = fmt.Sprint(value)
		}
	}
	return envs
}

// giteaRepoPath is the org and name a repository URL on Gitea names — its
// last two path segments, so a Gitea served under a path prefix reads too.
func giteaRepoPath(repoURL string) (org, name string) {
	u, err := url.Parse(topology.CanonicalRepoURL(repoURL))
	if err != nil {
		return "", ""
	}
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) < 2 {
		return "", ""
	}
	return segments[len(segments)-2], segments[len(segments)-1]
}
