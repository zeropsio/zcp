package ops

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/platform"
)

// BoardTitleMax and BoardBriefMax are what HQ takes of a task put on the
// application's board (`POST /api/apps/:appId/tasks`, docs/spec-mate.md
// §6.11), so every proposal zcp lets through is one the person's Put on board
// can write as it stands.
const (
	BoardTitleMax = 120
	BoardBriefMax = 8000
)

// BoardProposal is what the agent proposes for its application's board: one
// task (Title and Brief) or a plan of several (Plan). Mate draws a card with it
// in the conversation, and the person's own client puts it on the board when
// they press Put on board. Nothing is written by the proposal itself.
type BoardProposal struct {
	// Title is the task's one line on the board.
	Title string `json:"title,omitempty"`
	// Brief is the whole task, written for whoever takes it, who has not seen
	// this conversation.
	Brief string `json:"brief,omitempty"`
	// Plan is the tasks of a plan in the agent's order (NewBoardPlan).
	Plan []BoardPlanTask `json:"plan,omitempty"`
}

// NewBoardProposal checks a proposal and writes nothing: title is one line of
// at most BoardTitleMax characters, brief at most BoardBriefMax, neither
// empty.
func NewBoardProposal(title, brief string) (BoardProposal, error) {
	if err := validateRequestLine("title", title, BoardTitleMax,
		fmt.Sprintf("title: the task in one line of at most %d characters, as the board lists it.", BoardTitleMax)); err != nil {
		return BoardProposal{}, err
	}
	brief = strings.TrimSpace(brief)
	hint := fmt.Sprintf("brief: the whole task in at most %d characters, written for whoever takes it — they have not seen this conversation.", BoardBriefMax)
	switch {
	case brief == "":
		return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter, "A proposal needs its brief", hint)
	case utf8.RuneCountInString(brief) > BoardBriefMax:
		return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The brief is longer than %d characters", BoardBriefMax), hint)
	}
	return BoardProposal{Title: strings.TrimSpace(title), Brief: brief}, nil
}
