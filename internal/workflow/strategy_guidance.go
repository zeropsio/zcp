package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// BuildStrategyGuidance returns strategy-specific guidance synthesised via
// the canonical Synthesize pipeline so placeholder substitution and
// unknown-placeholder rejection apply. Phase 2 (C4) of the pipeline-repair
// plan: pre-fix this function read raw `atom.Body` directly, bypassing
// substitution and emitting literal `{hostname}` to the LLM. Now it
// constructs a StateEnvelope from the strategies map + environment and
// hands it to Synthesize.
//
// Environment must be supplied so atoms scoped to `environments:
// [container]` or `[local]` can match the caller's actual surface —
// strategy guidance differs (push-dev container uses zerops_deploy via
// SSH; push-dev local uses zcli push). Render order is deterministic:
// Synthesize sorts by (priority, id).
//
// Snapshots carry `Deployed: true` because strategy guidance is only
// surfaced post-first-deploy (the strategy decision happens at the
// `develop-strategy-review` atom branch, which gates on
// `deployStates: [deployed]`). Pre-first-deploy strategy is fixed at
// the default self-deploy mechanism per spec invariant D2a.
func BuildStrategyGuidance(env Environment, strategies map[string]topology.DeployStrategy) (string, error) {
	envelope := strategyGuidanceEnvelope(env, strategies)
	corpus, err := LoadAtomCorpus()
	if err != nil {
		return "", fmt.Errorf("load atom corpus: %w", err)
	}
	// Filter to the strategies-axis subset before synthesizing — atoms
	// without a `strategies:` axis declared are general develop guidance
	// (Plan/Status overview wording), not strategy-specific. The atom
	// pipeline still owns substitution and unknown-placeholder rejection;
	// this function just narrows the corpus to the strategy-specific
	// slice the caller asked for.
	selected := make(map[topology.DeployStrategy]struct{}, len(strategies))
	for _, s := range strategies {
		selected[s] = struct{}{}
	}
	subset := make([]KnowledgeAtom, 0, len(corpus))
	for _, atom := range corpus {
		if !phaseInSet(PhaseDevelopActive, atom.Axes.Phases) {
			continue
		}
		if len(atom.Axes.Strategies) == 0 {
			continue
		}
		for _, s := range atom.Axes.Strategies {
			if _, ok := selected[s]; ok {
				subset = append(subset, atom)
				break
			}
		}
	}
	if len(subset) == 0 {
		return "", nil
	}
	sort.SliceStable(subset, func(i, j int) bool {
		if subset[i].Priority != subset[j].Priority {
			return subset[i].Priority < subset[j].Priority
		}
		return subset[i].ID < subset[j].ID
	})
	matches, err := Synthesize(envelope, subset)
	if err != nil {
		return "", fmt.Errorf("synthesize strategy guidance: %w", err)
	}
	return strings.Join(BodiesOf(matches), "\n\n---\n\n"), nil
}

// strategyGuidanceEnvelope constructs the StateEnvelope used for
// BuildStrategyGuidance synthesis: one ServiceSnapshot per hostname with
// the requested Strategy and Deployed=true. Environment surfaces from
// the caller so container/local atoms route correctly.
func strategyGuidanceEnvelope(env Environment, strategies map[string]topology.DeployStrategy) StateEnvelope {
	hosts := make([]string, 0, len(strategies))
	for h := range strategies {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	snaps := make([]ServiceSnapshot, 0, len(hosts))
	for _, h := range hosts {
		snaps = append(snaps, ServiceSnapshot{
			Hostname:     h,
			Bootstrapped: true,
			Deployed:     true,
			Strategy:     strategies[h],
		})
	}
	return StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: env,
		Services:    snaps,
	}
}
