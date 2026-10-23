package topology

import "strings"

// Type bases of the service kinds that are NOT app runtimes although the
// platform category cannot tell them apart from one: Swarm pools report
// `USER` like any runtime, a Zerops Box reports `STANDARD` like a database.
// Classification therefore keys on the type string (spec-workflows.md §8 PS-1).
const (
	basePool    = "swarm"
	basePoolVM  = "swarm-vm"
	baseSandbox = "zbox"
)

// typeBase returns the lower-cased base name of a service type identifier:
// a known OS prefix and the `@version` suffix are stripped, so
// `ubuntu/swarm-vm@1` and `SWARM-VM@1` both yield `swarm-vm`. The base is
// compared whole — `swarm-vm` is never split into `swarm` + `-vm`.
func typeBase(serviceType string) string {
	base := stripKnownOSPrefix(strings.ToLower(strings.TrimSpace(serviceType)))
	if i := strings.IndexByte(base, '@'); i >= 0 {
		base = base[:i]
	}
	return base
}

// IsPoolType reports whether serviceType names a Zerops Swarm pool
// (`swarm@1`, `swarm-vm@1`): a service whose orchestrator hands out leased
// containers, not an app runtime. Case-insensitive; a known OS prefix is
// ignored. Spec: spec-workflows.md §8 PS-1.
func IsPoolType(serviceType string) bool {
	switch typeBase(serviceType) {
	case basePool, basePoolVM:
		return true
	}
	return false
}

// IsSandboxType reports whether serviceType names a Zerops Box
// (`ubuntu/zbox@1`, `zbox@1`): a service that hosts sandboxes, not an app
// runtime. Case-insensitive; a known OS prefix is ignored. Spec:
// spec-workflows.md §8 PS-1.
func IsSandboxType(serviceType string) bool {
	return typeBase(serviceType) == baseSandbox
}

// IsPoolOrSandboxType reports a Swarm pool or Zerops Box — the two service
// kinds that are neither an app runtime nor a managed dependency, so adoption
// surfaces skip them wherever they filter runtimes (spec-workflows.md §8 PS-2).
func IsPoolOrSandboxType(serviceType string) bool {
	return IsPoolType(serviceType) || IsSandboxType(serviceType)
}
