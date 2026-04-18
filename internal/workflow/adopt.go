package workflow

import "strings"

// AdoptCandidate represents a live service for auto-adoption.
type AdoptCandidate struct {
	Hostname string
	Type     string
}

// isControlPlaneType returns true for ZCP's own service type.
func isControlPlaneType(serviceType string) bool {
	return strings.HasPrefix(strings.ToLower(serviceType), "zcp")
}

// InferServicePairing builds BootstrapTargets from live services for adoption.
// It filters out managed and control-plane types, infers dev/stage pairing
// from hostname conventions, and sets ExplicitStage when auto-derive won't work.
// Managed services are returned as dependencies with resolution EXISTS.
//
// liveManaged: base names of managed types from the live API catalog
// (knowledge.ManagedBaseNames). When non-empty it overrides the static prefix
// list so new Zerops managed categories are classified correctly without
// requiring a managed_types.go bump. Pass nil to use the static fallback.
func InferServicePairing(candidates []AdoptCandidate, liveManaged map[string]bool) []BootstrapTarget {
	// Separate runtimes from managed services.
	var runtimes []AdoptCandidate
	var managed []AdoptCandidate
	hostnames := make(map[string]string) // hostname → type
	for _, c := range candidates {
		if isControlPlaneType(c.Type) {
			continue
		}
		hostnames[c.Hostname] = c.Type
		if isManagedTypeWithLive(c.Type, liveManaged) {
			managed = append(managed, c)
			continue
		}
		runtimes = append(runtimes, c)
	}

	if len(runtimes) == 0 {
		return nil
	}

	// Build shared dependencies from managed services.
	deps := make([]Dependency, len(managed))
	for i, m := range managed {
		deps[i] = Dependency{
			Hostname:   m.Hostname,
			Type:       m.Type,
			Resolution: "EXISTS",
		}
	}

	// Pass 1: identify all dev/stage pairs. A pair exists when both {base}dev
	// and {base}stage are present in the runtime list (base must be non-empty).
	// This must happen before target creation so API ordering doesn't matter.
	paired := make(map[string]bool) // stage hostnames claimed by a dev service
	for _, r := range runtimes {
		base, isDev := strings.CutSuffix(r.Hostname, "dev")
		if isDev && base != "" {
			stageHostname := base + "stage"
			if _, stageExists := hostnames[stageHostname]; stageExists && !isManagedTypeWithLive(hostnames[stageHostname], liveManaged) {
				paired[stageHostname] = true
			}
		}
	}

	// Pass 2: create targets, skipping hostnames already claimed as stage.
	var targets []BootstrapTarget
	for _, r := range runtimes {
		if paired[r.Hostname] {
			continue
		}

		base, isDev := strings.CutSuffix(r.Hostname, "dev")
		if isDev && base != "" && paired[base+"stage"] {
			targets = append(targets, BootstrapTarget{
				Runtime: RuntimeTarget{
					DevHostname:   r.Hostname,
					Type:          r.Type,
					IsExisting:    true,
					BootstrapMode: PlanModeStandard,
				},
				Dependencies: deps,
			})
			continue
		}

		targets = append(targets, BootstrapTarget{
			Runtime: RuntimeTarget{
				DevHostname:   r.Hostname,
				Type:          r.Type,
				IsExisting:    true,
				BootstrapMode: PlanModeDev,
			},
			Dependencies: deps,
		})
	}

	return targets
}
