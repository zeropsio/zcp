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
func InferServicePairing(candidates []AdoptCandidate) []BootstrapTarget {
	// Separate runtimes from managed services.
	var runtimes []AdoptCandidate
	var managed []AdoptCandidate
	hostnames := make(map[string]string) // hostname → type
	for _, c := range candidates {
		if isControlPlaneType(c.Type) {
			continue
		}
		hostnames[c.Hostname] = c.Type
		if IsManagedService(c.Type) {
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

	// Detect dev/stage pairs. A pair exists when both {base}dev and {base}stage
	// are present in the runtime list (base must be non-empty).
	paired := make(map[string]bool) // stage hostnames already claimed
	var targets []BootstrapTarget

	for _, r := range runtimes {
		if paired[r.Hostname] {
			continue // already claimed as stage of another runtime
		}

		base, isDev := strings.CutSuffix(r.Hostname, "dev")
		if isDev && base != "" {
			stageHostname := base + "stage"
			if _, stageExists := hostnames[stageHostname]; stageExists && !IsManagedService(hostnames[stageHostname]) {
				// Standard pair found.
				paired[stageHostname] = true
				target := BootstrapTarget{
					Runtime: RuntimeTarget{
						DevHostname:   r.Hostname,
						Type:          r.Type,
						IsExisting:    true,
						BootstrapMode: PlanModeStandard,
					},
					Dependencies: deps,
				}
				// ExplicitStage only needed when auto-derive won't work.
				// StageHostname() auto-derives from *dev suffix, which works here.
				targets = append(targets, target)
				continue
			}
		}

		// No pair found — dev mode (standalone).
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
