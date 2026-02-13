package server

// Instructions is the MCP instructions message injected into the system prompt.
// Self-sufficient: each tool response includes live versions and validation,
// so the LLM can enter at any point and still produce correct output.
const Instructions = `ZCP manages Zerops PaaS infrastructure. For multi-step operations (creating services, deploying, debugging), start with zerops_workflow — it includes live service versions and step-by-step guidance. Call zerops_knowledge before generating YAML for runtime-specific rules and version validation. Use zerops_discover to check current state.`
