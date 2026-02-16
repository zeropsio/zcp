package server

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/runtime"
)

const baseInstructions = `ZCP manages Zerops PaaS infrastructure. For multi-step operations (creating services, deploying, debugging), start with zerops_workflow — it includes live service versions and step-by-step guidance. Call zerops_knowledge before any operation that requires generating configuration (import.yml, zerops.yml), setting up or bootstrapping services, deploying code, or debugging issues. This loads platform rules, YAML schema, runtime-specific config, and troubleshooting patterns. Skip zerops_knowledge for direct single-step commands (restart, delete, logs, discover) where the user gives explicit parameters. Use zerops_discover to check current state.`

// BuildInstructions returns the MCP instructions message injected into the system prompt.
// When running inside a Zerops service, it includes the service name for context.
func BuildInstructions(rt runtime.Info) string {
	if rt.ServiceName == "" {
		return baseInstructions
	}
	return fmt.Sprintf(
		"You are running inside the Zerops service '%s'. You manage services in the same project.\n\n%s",
		rt.ServiceName, baseInstructions,
	)
}
