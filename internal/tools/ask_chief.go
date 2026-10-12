package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
)

// AskChiefInput is zerops_propose_ask_chief's input.
type AskChiefInput struct {
	Words           string `json:"words"                     jsonschema:"What you need from outside this project, in the person's terms."`
	AttachmentTitle string `json:"attachmentTitle,omitempty" jsonschema:"A title for an attached excerpt, e.g. 'my failing test'."`
	AttachmentText  string `json:"attachmentText,omitempty"  jsonschema:"The excerpt itself (log lines, an error); the lines that matter."`
	Reason          string `json:"reason"                    jsonschema:"Why this needs the Chief, in one sentence, for the person deciding."`
}

// RegisterAskChief registers zerops_propose_ask_chief: a project's Mate asks
// its person's Chief for something outside the project (docs/spec-mate.md
// §2.11). Like org mode's act tools it sends nothing; the person approves or
// declines the card. newID mints a proposal's id.
func RegisterAskChief(srv *mcp.Server, projectID, projectName string, newID func() string) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_propose_ask_chief",
		Description: "Propose asking the person's Chief — their Mate for the whole organization — for something outside this project: another project's logs, a production error, the organization's state. Sends nothing: the person approves or declines it on a card, and the Chief's answer comes back to this conversation.",
		Annotations: orgPropose("Ask your Chief"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in AskChiefInput) (*mcp.CallToolResult, any, error) {
		return orgAnswer(ops.ProposeAskChief(projectID, projectName, newID, ops.AskChiefInput{
			Words: in.Words, AttachmentTitle: in.AttachmentTitle, AttachmentText: in.AttachmentText, Reason: in.Reason,
		}))
	})
}
