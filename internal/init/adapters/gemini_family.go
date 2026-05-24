package adapters

// geminiFamilyMCPServerKey is the canonical MCP server name written into
// the mcpServers object for Gemini CLI / Antigravity. Matches the key
// every atom references via mcp__zerops__* and the value Codex /
// Claude / our own templates use — TestMCPServerNameCanonical pins it
// cross-package.
const geminiFamilyMCPServerKey = "zerops"

// geminiFamilyMCPEntry builds the MCPServerConfig contents that Gemini
// CLI v0.39+ and Antigravity v1.0+ both accept. The schema lives in
// Gemini CLI's MCPServerConfig class (chunk-WFCK2Z32.js); Antigravity
// is a Gemini fork (product=antigravity) and accepts the same shape at
// its dedicated mcp_config.json path.
//
// Shape rationale:
//
//   - command + args — stdio transport (default), matches Claude /
//     Codex / mcp-config.json template.
//   - trust=true — bypasses tool-call confirmation prompts. Without it
//     every mcp__zerops__* call gates on an interactive y/N, which
//     defeats the headless container UX.
//   - description — surfaces in the agent's MCP listing so the operator
//     can see what `zerops` is.
//   - NO env field — Gemini's MCP spawn uses
//     `env: { ...process.env, ...envMap }` (permissive parent-env
//     spread). serviceId / hostname / projectId / ZCP_API_KEY all flow
//     through from the calling shell without enumeration. This is the
//     OPPOSITE of Codex's restrictive env_vars list (codex.go fixes
//     bug 2026-05-24 where env_vars missing runtime-detection vars
//     stripped them and zcp serve saw InContainer=false). Both models
//     are correct for their host CLI; neither is portable.
func geminiFamilyMCPEntry() map[string]any {
	return map[string]any{
		"command":     "zcp",
		"args":        []any{"serve"},
		"trust":       true,
		"description": "Zerops platform MCP server",
	}
}
