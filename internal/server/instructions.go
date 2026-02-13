package server

// Instructions is the MCP instructions message injected into the system prompt.
// Kept concise but directive. zerops_context provides full platform knowledge on-demand.
// zerops_workflow ensures knowledge-first pattern triggers for multi-step operations.
const Instructions = `ZCP provides tools for managing Zerops PaaS infrastructure: services, deployment, configuration, and debugging. Call zerops_context to load platform knowledge when working with Zerops. For multi-step operations (bootstrap, deploy, debug), call zerops_workflow first. Call zerops_knowledge before generating any YAML (import.yml or zerops.yml). Read zerops://docs/{path} resources for detailed documentation on specific topics.`
