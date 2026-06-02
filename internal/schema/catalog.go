package schema

import (
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// The schema-derived catalog. These methods answer the client-side
// "what does this Zerops instance accept?" questions from the parsed
// *Schemas (live-from-host with embedded floor) — the single source that
// replaces the deleted stack-types API / StackTypeCache.
//
// Existence checks are EQUIVALENCE-aware (topology.TypesAreEquivalent): an
// authored bare form (`nodejs@22`, `php@8.4`) matches a catalog that has moved
// to the composite form (`alpine/nodejs@22`), and vice versa — so the answer is
// identical whether the schema was seeded from the bare-carrying embedded floor
// or a composite-only live fetch. Plain set membership (BuildBaseSet etc.) is
// form-sensitive and must NOT be used for authored-value validation; these
// methods are the correct surface.

// HasServiceType reports whether the import schema accepts serviceType,
// matching composite and bare spellings equivalently.
func (s *Schemas) HasServiceType(serviceType string) bool {
	return s.ImportYml != nil && matchesAnyEquivalent(serviceType, s.ImportYml.ServiceTypes)
}

// HasRunBase reports whether the zerops.yml schema accepts the run.base value,
// composite/bare-tolerant.
func (s *Schemas) HasRunBase(base string) bool {
	return s.ZeropsYml != nil && matchesAnyEquivalent(base, s.ZeropsYml.RunBases)
}

// HasBuildBase reports whether the zerops.yml schema accepts the build.base
// value, composite/bare-tolerant.
func (s *Schemas) HasBuildBase(base string) bool {
	return s.ZeropsYml != nil && matchesAnyEquivalent(base, s.ZeropsYml.BuildBases)
}

// ManagedBaseNames returns the set of managed-service base names the schema
// accepts, keyed by the symmetric topology.CanonicalBaseName (storage spellings
// normalized). Replaces the API-Category-filtered knowledge.ManagedBaseNames:
// the schema owns type EXISTENCE, topology owns the managed CLASSIFICATION.
func (s *Schemas) ManagedBaseNames() map[string]bool {
	out := make(map[string]bool)
	if s.ImportYml == nil {
		return out
	}
	for _, t := range s.ImportYml.ServiceTypes {
		if topology.IsManagedService(t) {
			out[topology.CanonicalBaseName(t)] = true
		}
	}
	return out
}

// matchesAnyEquivalent reports whether candidate is equivalent to any value in
// set under the composite/bare BC semantic. Linear scan — these are
// authoring/pre-check paths, never latency-critical (deploy/import validate via
// the platform API directly).
func matchesAnyEquivalent(candidate string, set []string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	// A `:mode` decoration is only valid on a mode-capable base (managed DBs +
	// shared-storage). Reject a mode carried by a mode-incapable base — runtime
	// (`nodejs:ha@22`) or object-storage (`object-storage:ha`) — so the bare-form
	// equivalence below can't strip the mode and falsely match the real base.
	// (Done here, not in the topology equivalence primitive, to avoid coupling
	// the BC matcher to classification.)
	if strings.Contains(candidate, ":") && !topology.ServiceSupportsMode(candidate) {
		return false
	}
	for _, v := range set {
		if topology.TypesAreEquivalent(candidate, v) {
			return true
		}
	}
	return false
}
