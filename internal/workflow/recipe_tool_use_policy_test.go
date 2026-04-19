// Tests for v8.90 Fix C — subagent tool-use policy in every subagent brief.
//
// v25 evidence: both the scaffold sub-agent (at 20:20:46) and the feature
// sub-agent (at 20:34:30) called `mcp__zerops__zerops_workflow` as one of
// their first tool calls. The server rejected them (misleadingly, with
// PREREQUISITE_MISSING — Fix A corrects that). v8.90 Fix C layers
// belt-and-braces prevention: every subagent brief now contains an
// explicit TOOL-USE POLICY block listing permitted and forbidden tools
// so the sub-agent reads the policy BEFORE making its first tool call.
//
// The block is terse, framework-agnostic, and identical across all four
// subagent brief blocks in recipe.md. The universal list is the source
// of truth; a dedicated test iterates every required item against every
// brief block so a future drift in one brief is caught.

package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// subagentBriefBlocks is the authoritative list of blocks that are used
// as dispatch-brief preambles for sub-agents. Every one of them must
// carry the tool-use policy block.
var subagentBriefBlocks = []string{
	"scaffold-subagent-brief",
	"dev-deploy-subagent-brief",
	"readme-with-fragments",
	"code-review-subagent",
}

// universalForbiddenTools is the byte-literal list every brief must
// contain in its tool-use policy. Changes here must land in every
// brief block simultaneously.
var universalForbiddenTools = []string{
	"mcp__zerops__zerops_workflow",
	"mcp__zerops__zerops_import",
	"mcp__zerops__zerops_env",
	"mcp__zerops__zerops_deploy",
	"mcp__zerops__zerops_subdomain",
	"mcp__zerops__zerops_mount",
	"mcp__zerops__zerops_verify",
}

// extractBlock finds a <block name="X">...</block> section in recipe.md
// and returns its body (without wrapper tags).
func extractBlock(t *testing.T, md, name string) string {
	t.Helper()
	for _, b := range ExtractBlocks(md) {
		if b.Name == name {
			return b.Body
		}
	}
	return ""
}

func TestSubagentBriefs_ContainToolUsePolicy(t *testing.T) {
	t.Parallel()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	for _, blockName := range subagentBriefBlocks {
		t.Run(blockName, func(t *testing.T) {
			t.Parallel()
			body := extractBlock(t, md, blockName)
			if body == "" {
				t.Fatalf("block %q not found in recipe.md", blockName)
			}
			wants := []string{
				"TOOL-USE POLICY",
				"Permitted tools",
				"Forbidden tools",
				"SUBAGENT_MISUSE",
			}
			for _, w := range wants {
				if !strings.Contains(body, w) {
					t.Errorf("block %q missing tool-use-policy token %q", blockName, w)
				}
			}
		})
	}
}

func TestSubagentBriefs_ListAllForbiddenTools(t *testing.T) {
	t.Parallel()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	for _, blockName := range subagentBriefBlocks {
		t.Run(blockName, func(t *testing.T) {
			t.Parallel()
			body := extractBlock(t, md, blockName)
			if body == "" {
				t.Fatalf("block %q not found", blockName)
			}
			for _, tool := range universalForbiddenTools {
				if !strings.Contains(body, tool) {
					t.Errorf("block %q must list forbidden tool %q in its tool-use policy", blockName, tool)
				}
			}
		})
	}
}

// TestSubagentBriefs_DoNotCallWorkflowTool — belt-and-braces assertion
// that the phrase instructing the sub-agent to NOT call zerops_workflow
// appears in every brief. This is the exact copy a v25-style misfire
// would have benefited from.
func TestSubagentBriefs_DoNotCallWorkflowTool(t *testing.T) {
	t.Parallel()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}

	for _, blockName := range subagentBriefBlocks {
		t.Run(blockName, func(t *testing.T) {
			t.Parallel()
			body := extractBlock(t, md, blockName)
			if body == "" {
				t.Fatalf("block %q not found", blockName)
			}
			if !strings.Contains(body, "workflow state is main-agent-only") {
				t.Errorf("block %q missing the 'workflow state is main-agent-only' rule", blockName)
			}
		})
	}
}
