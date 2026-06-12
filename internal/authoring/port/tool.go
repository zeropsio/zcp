package port

// toolName is the gated MCP tool identity. Registered only behind
// ZCP_AUTHORING=1 (server.go composition root) — end users never see it.
const toolName = "zerops_port"
