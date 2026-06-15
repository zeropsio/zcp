// Package runtime detects whether ZCP is running inside a Zerops container.
// The Info struct is resolved once at startup and passed down as a value parameter.
package runtime

import "os"

// Info holds runtime environment detection results.
type Info struct {
	InContainer bool   // true when running in a Zerops container
	ServiceName string // hostname from container env (empty when not in container)
	ServiceID   string // Zerops service ID (only in container)
	ProjectID   string // Zerops project ID (only in container)

	// Authoring is the single source of truth for the ZCP_AUTHORING gate:
	// when true, the maintainer-only authoring domain (recipe v3 engine +
	// its tools) registers its MCP surface AND the emitted agent context
	// (AGENTS.md/CLAUDE.md) carries recipe-authoring guidance. Read once
	// here so the tool-registration gate (internal/server) and the agent
	// context renderer (internal/content) cannot drift. Independent of
	// InContainer — recipe authoring is also done on a local dev machine.
	// Spec: docs/spec-authoring-boundary.md.
	Authoring bool
}

// Detect reads Zerops container env vars and returns runtime info.
// The serviceId env var is injected by Zerops into every container — its
// presence is the definitive signal for container detection. The
// ZCP_AUTHORING gate is read regardless of container detection (authoring
// runs in both envs).
func Detect() Info {
	authoring := os.Getenv("ZCP_AUTHORING") == "1"
	serviceID := os.Getenv("serviceId")
	if serviceID == "" {
		return Info{Authoring: authoring}
	}
	return Info{
		InContainer: true,
		ServiceName: os.Getenv("hostname"),
		ServiceID:   serviceID,
		ProjectID:   os.Getenv("projectId"),
		Authoring:   authoring,
	}
}
