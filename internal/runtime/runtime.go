// Package runtime detects whether ZCP is running inside a Zerops container.
// The Info struct is resolved once at startup and passed down as a value parameter.
package runtime

import (
	"os"
	"strings"
)

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

	// Z3Enabled is the single source of truth for the ZCP_Z3_ENABLED gate:
	// when false, NOTHING z3-shaped happens — no bundle is downloaded or
	// installed, no zerops@z3 unit is registered, nginx publishes no /z3/
	// location and leaves port 3773 reachable through code-server's own
	// proxy, and the container's readiness marker is not written. `zcp init`
	// converges the OTHER direction too: a container whose flag was turned
	// off stops and removes the unit it finds. Read once here so the init
	// step (internal/init) and the nginx renderer cannot drift.
	// Spec: docs/spec-z3.md §2.
	Z3Enabled bool
}

// Detect reads Zerops container env vars and returns runtime info.
// The serviceId env var is injected by Zerops into every container — its
// presence is the definitive signal for container detection. The
// ZCP_AUTHORING and ZCP_Z3_ENABLED gates are read regardless of container
// detection (authoring runs in both envs; z3 is acted on only in a container,
// but one read here keeps the value with a single home).
func Detect() Info {
	authoring := os.Getenv("ZCP_AUTHORING") == "1"
	z3Enabled := envEnabled(os.Getenv("ZCP_Z3_ENABLED"))
	serviceID := os.Getenv("serviceId")
	if serviceID == "" {
		return Info{Authoring: authoring, Z3Enabled: z3Enabled}
	}
	return Info{
		InContainer: true,
		ServiceName: os.Getenv("hostname"),
		ServiceID:   serviceID,
		ProjectID:   os.Getenv("projectId"),
		Authoring:   authoring,
		Z3Enabled:   z3Enabled,
	}
}

// HomeDir returns the container service user's home directory: $HOME, or
// /home/zerops when HOME is unset or "/" — both common in Zerops
// initCommands, which run before anything sets a login shell's environment.
// Every path ZCP writes under the service user's home resolves through here,
// so a test that redirects HOME never touches the real one.
func HomeDir() string {
	home := os.Getenv("HOME")
	if home == "" || home == "/" {
		return "/home/zerops"
	}
	return home
}

// envEnabled reads an operator-typed on/off service env: "1" or "true",
// case-insensitive, surrounding space tolerated. Deliberately more forgiving
// than the ZCP_AUTHORING gate's exact "1" — ZCP_AUTHORING is set by a
// maintainer's shell profile, while ZCP_Z3_ENABLED is typed into a service's
// env in the Zerops GUI, where "true" is the form a person reaches for first
// and a silently ignored value is indistinguishable from a broken feature.
func envEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true":
		return true
	default:
		return false
	}
}
