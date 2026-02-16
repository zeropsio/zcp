package server

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/runtime"
)

const baseInstructions = `ZCP manages Zerops PaaS infrastructure. For multi-step operations (creating services, deploying, debugging), start with zerops_workflow — it includes live service versions and step-by-step guidance. Call zerops_knowledge before generating YAML for runtime-specific rules and version validation. Use zerops_discover to check current state.`

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
