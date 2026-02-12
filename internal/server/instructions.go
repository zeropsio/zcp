package server

// Instructions is the ultra-minimal MCP instructions message.
// Deliberately minimal (~40-50 tokens). zerops_context provides full platform knowledge on-demand.
const Instructions = `ZCP provides tools for managing Zerops PaaS infrastructure: services, deployment, configuration, and debugging. Call zerops_context to load platform knowledge when working with Zerops. Read zerops://docs/{path} resources for detailed documentation on specific topics.`
