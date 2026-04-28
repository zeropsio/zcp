package recipe

import "sort"

// ServiceModeDelta is one per-service mode change between two tiers.
// Computed by TierServiceModeDelta after applying the same NON_HA
// downgrade logic the yaml emitter applies (yaml_emitter.go:325) so the
// delta reflects the mode that actually ships at each tier, not the
// raw Tier.ServiceMode field.
type ServiceModeDelta struct {
	Service string // hostname, e.g. "db" / "cache" / "search"
	From    string // "NON_HA" or "HA"
	To      string
}

// TierServiceModeDelta returns per-service mode changes between two
// tiers, applying the yaml-emitter's downgrade rule (§5.3 of run-16-
// readiness): managed-service families that don't support HA stay
// NON_HA even when the tier's whole-tier ServiceMode is HA.
//
// Only managed services contribute deltas — runtime services don't have
// a per-service mode field. plan must be non-nil; the result is sorted
// by hostname so the order is stable across calls.
//
// Run-16 Tranche 2 commit 3 — used by emittedFactsForCodebase to seed
// per-service tier_decision facts. tiers.go::Diff handles whole-tier
// scalars (RunsDevContainer, RuntimeMinContainers, CPUMode, …);
// TierServiceModeDelta handles the per-service half Diff cannot
// express (the "Postgres NON_HA at tier 4 vs HA at tier 5" shape, where
// only some managed services flip while others stay NON_HA across the
// jump).
func TierServiceModeDelta(fromTier, toTier Tier, plan *Plan) []ServiceModeDelta {
	if plan == nil {
		return nil
	}
	resolveServiceMode := func(t Tier, svc Service) string {
		mode := t.ServiceMode
		if mode == modeHA && !svc.SupportsHA && !managedServiceSupportsHA(svc.Type) {
			return modeNonHA
		}
		return mode
	}

	var deltas []ServiceModeDelta
	for _, svc := range plan.Services {
		if svc.Kind != ServiceKindManaged {
			continue
		}
		from := resolveServiceMode(fromTier, svc)
		to := resolveServiceMode(toTier, svc)
		if from != to {
			deltas = append(deltas, ServiceModeDelta{Service: svc.Hostname, From: from, To: to})
		}
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i].Service < deltas[j].Service })
	return deltas
}
