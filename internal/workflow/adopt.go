package workflow

import (
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
	// AdoptCandidate represents a live service for auto-adoption.
)

type AdoptCandidate struct {
	Hostname string
	Type     string
}

// isControlPlaneType returns true for ZCP's own service type.
func isControlPlaneType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "zcp")
}

// InferServicePairing builds BootstrapTargets from live services for adoption.
// Filters out managed and control-plane types; every remaining runtime
// becomes its own target with `BootstrapMode: PlanModeDev`. Managed
// services become EXISTS-resolution dependencies shared across targets.
//
// Why no pairing: earlier revisions inferred `{base}dev` + `{base}stage`
// pairs from hostname suffixes. Service names are now arbitrary strings,
// so the heuristic silently misclassified repos with non-conforming
// names (e.g. `frontend-app` + `frontend-app-prod`) and overrode the
// author's intent when they wanted `appdev`+`appstage` adopted as two
// independent services. Users who want a dev/stage pair adopted as
// PlanModeStandard write that into the bootstrap plan explicitly
// (BootstrapMode=standard, ExplicitStage=<hostname>).
//
// liveManaged: base names of managed types from the live API catalog
// (knowledge.ManagedBaseNames). When non-empty it overrides the static
// prefix list so new Zerops managed categories are classified correctly
// without requiring a managed_types.go bump. Pass nil to use the static
// fallback.
func InferServicePairing(candidates []AdoptCandidate, liveManaged map[string]bool) []BootstrapTarget {
	var runtimes []AdoptCandidate
	var managed []AdoptCandidate
	for _, c := range candidates {
		if isControlPlaneType(c.Type) {
			continue
		}
		if isManagedTypeWithLive(c.Type, liveManaged) {
			managed = append(managed, c)
			continue
		}
		runtimes = append(runtimes, c)
	}

	if len(runtimes) == 0 {
		return nil
	}

	// Shared dependencies from managed services.
	deps := make([]Dependency, len(managed))
	for i, m := range managed {
		deps[i] = Dependency{
			Hostname:   m.Hostname,
			Type:       m.Type,
			Resolution: "EXISTS",
		}
	}

	targets := make([]BootstrapTarget, 0, len(runtimes))
	for _, r := range runtimes {
		targets = append(targets, BootstrapTarget{
			Runtime: RuntimeTarget{
				DevHostname:   r.Hostname,
				Type:          r.Type,
				IsExisting:    true,
				BootstrapMode: topology.PlanModeDev,
			},
			Dependencies: deps,
		})
	}
	return targets
}
