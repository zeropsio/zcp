package eval

import (
	"encoding/json"
	"strings"
)

// launchKeyArg is the zerops_workflow argument that carries the manual-path
// launch token (docs/spec-workflows.md P-LP-14).
const launchKeyArg = "launchKey"

// isWorkflowTool reports whether a tool name (bare or MCP-prefixed, e.g.
// mcp__zerops__zerops_workflow) is zerops_workflow.
func isWorkflowTool(name string) bool {
	return name == "zerops_workflow" || strings.HasSuffix(name, "__zerops_workflow")
}

// scrubAllowedLaunchKeyPlaces removes from the JSONL transcript the two
// places the manual launch path legitimately holds the token: the launchKey
// argument of zerops_workflow calls, and the one Bash call that reads
// $ZCP_E2E_LAUNCH_KEY (its result). Every other place — a second read, other
// tool arguments or results, assistant text — stays in the returned text, so
// the digest scan fails on it. Lines that are not JSON are kept verbatim.
func scrubAllowedLaunchKeyPlaces(transcriptText, launchTokenSHA256 string) string {
	readCalls := map[string]bool{}
	readAllowed := true
	lines := strings.Split(transcriptText, "\n")
	for i, line := range lines {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		workflowCalls := map[string]bool{}
		blocks := contentBlocks(event)
		for _, block := range blocks {
			switch block["type"] {
			case "tool_use":
				id, _ := block["id"].(string)
				name, _ := block["name"].(string)
				input, _ := block["input"].(map[string]any)
				if isWorkflowTool(name) {
					workflowCalls[id] = true
					delete(input, launchKeyArg)
				}
				if command, _ := input["command"].(string); name == "Bash" && strings.Contains(command, LaunchKeyEnvVar) {
					readCalls[id] = true
				}
			case "tool_result":
				id, _ := block["tool_use_id"].(string)
				if !readAllowed || !readCalls[id] || !textHoldsLaunchToken(block["content"], launchTokenSHA256) {
					continue
				}
				readAllowed = false
				block["content"] = ""
				if result, ok := event["tool_use_result"].(map[string]any); ok {
					delete(result, "stdout")
				}
			}
		}
		if wire, ok := event["wire_tool_inputs"].(map[string]any); ok {
			for id := range workflowCalls {
				if input, ok := wire[id].(map[string]any); ok {
					delete(input, launchKeyArg)
				}
			}
		}
		if scrubbed, err := json.Marshal(event); err == nil {
			lines[i] = string(scrubbed)
		}
	}
	return strings.Join(lines, "\n")
}

// contentBlocks returns the message.content blocks of a stream-json event.
func contentBlocks(event map[string]any) []map[string]any {
	message, _ := event["message"].(map[string]any)
	raw, _ := message["content"].([]any)
	blocks := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if block, ok := item.(map[string]any); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// textHoldsLaunchToken reports whether a tool_result content (string or text
// parts) contains the token, by digest.
func textHoldsLaunchToken(content any, launchTokenSHA256 string) bool {
	data, err := json.Marshal(content)
	if err != nil {
		return false
	}
	for _, tok := range launchTokenTokens(string(data)) {
		if sha256Hex(tok) == launchTokenSHA256 {
			return true
		}
	}
	return false
}
