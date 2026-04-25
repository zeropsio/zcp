package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// BuildStrategyGuidance returns strategy-specific atom bodies joined with
// a `---` separator. Filters the atom corpus to phase=develop-active atoms
// whose `strategies` axis contains any of the given strategies. Output is
// deterministic (priority asc, id asc); duplicate strategies render once.
func BuildStrategyGuidance(strategies map[string]topology.DeployStrategy) (string, error) {
	corpus, err := LoadAtomCorpus()
	if err != nil {
		return "", fmt.Errorf("load atom corpus: %w", err)
	}
	selected := make(map[topology.DeployStrategy]struct{}, len(strategies))
	for _, s := range strategies {
		selected[s] = struct{}{}
	}
	var matched []KnowledgeAtom
	for _, atom := range corpus {
		if !phaseInSet(PhaseDevelopActive, atom.Axes.Phases) {
			continue
		}
		if len(atom.Axes.Strategies) == 0 {
			continue
		}
		for _, s := range atom.Axes.Strategies {
			if _, ok := selected[s]; ok {
				matched = append(matched, atom)
				break
			}
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority != matched[j].Priority {
			return matched[i].Priority < matched[j].Priority
		}
		return matched[i].ID < matched[j].ID
	})
	parts := make([]string, 0, len(matched))
	for _, atom := range matched {
		parts = append(parts, atom.Body)
	}
	return strings.Join(parts, "\n\n---\n\n"), nil
}
