package server

// Instructions is the ultra-minimal MCP instructions message.
// Deliberately minimal (~60 tokens). zerops_context provides full platform knowledge on-demand.
// zerops_workflow nudge ensures knowledge-first pattern triggers for multi-step operations.
const Instructions = `ZCP provides tools for managing Zerops PaaS infrastructure: services, deployment, configuration, and debugging. Call zerops_context to load platform knowledge when working with Zerops. For multi-step operations (bootstrap, deploy, debug), call zerops_workflow first. Read zerops://docs/{path} resources for detailed documentation on specific topics.`
