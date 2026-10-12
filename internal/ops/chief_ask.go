package ops

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProposalChiefMessage is the one proposal a project's Mate makes outside
// its project: asking the person's Chief (docs/spec-mate.md §2.11).
const ProposalChiefMessage = "chief.message"

// ChiefAskNote is what an ask-the-Chief proposal says about itself.
const ChiefAskNote = "Nothing has been sent. The person approves or declines it on the card."

// chiefSummaryWords bounds how much of the ask the card's summary quotes.
const chiefSummaryWords = 60

// AskChiefInput is what a Mate asks its person's Chief.
type AskChiefInput struct {
	Words           string
	AttachmentTitle string
	AttachmentText  string
	Reason          string
}

// ChiefMessageParams are chief.message's params.
type ChiefMessageParams struct {
	Words      string         `json:"words"`
	Attachment *OrgAttachment `json:"attachment,omitempty"`
}

// ProposeAskChief returns asking the person's Chief as a proposal from this
// project (projectID, projectName). It sends nothing: the person approves or
// declines it on a card, and on approval their client hands it to their
// Chief.
func ProposeAskChief(projectID, projectName string, newID func() string, in AskChiefInput) (*OrgProposalResult, error) {
	words := strings.TrimSpace(in.Words)
	if words == "" {
		return nil, invalid("chief.message needs words", "Say what you need from outside this project")
	}
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return nil, invalid("a proposal needs a reason", "Say in one sentence why, for the person deciding")
	}
	attachment, err := orgAttachment(in.AttachmentTitle, in.AttachmentText)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ChiefMessageParams{Words: words, Attachment: attachment})
	if err != nil {
		return nil, fmt.Errorf("encode proposal params: %w", err)
	}
	return &OrgProposalResult{
		Proposal: OrgProposal{
			ID:          newID(),
			Kind:        ProposalChiefMessage,
			Summary:     "Ask your Chief: " + quoteForSummary(words, chiefSummaryWords),
			Reason:      reason,
			ProjectID:   projectID,
			ProjectName: projectName,
			Params:      raw,
		},
		Note: ChiefAskNote,
	}, nil
}

// quoteForSummary is words on one line, cut to limit characters.
func quoteForSummary(words string, limit int) string {
	line := []rune(strings.Join(strings.Fields(words), " "))
	if len(line) <= limit {
		return string(line)
	}
	return strings.TrimRight(string(line[:limit]), " ") + "…"
}
