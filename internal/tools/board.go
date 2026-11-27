package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// BoardProposeInput is zerops_board_propose's input.
type BoardProposeInput struct {
	Title string `json:"title" jsonschema:"The task in one line, at most 120 characters, as the board lists it."`
	Brief string `json:"brief" jsonschema:"The whole task, at most 8000 characters, for whoever takes it: they have not seen this conversation. Every member of the organization reads the board: no secret, value or excerpt of this conversation."`
}

type boardProposed struct {
	Board ops.BoardProposal `json:"board"`
}

type boardProposeAnswer struct {
	Proposed    boardProposed `json:"proposed"`
	NextActions string        `json:"nextActions"`
}

// boardTask is one open task as zerops_board_list tells it: With is who has
// it and From where it came from, each absent when HQ says nothing of it.
type boardTask struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
	With  string `json:"with,omitempty"`
	From  string `json:"from,omitempty"`
}

type boardListAnswer struct {
	Tasks []boardTask `json:"tasks"`
	// More counts the open tasks past boardListMax, left out of Tasks.
	More        int    `json:"more,omitempty"`
	NextActions string `json:"nextActions"`
}

// boardListMax bounds the tasks one list tells the agent: a board past it
// holds more than an agent compares a proposal against.
const boardListMax = 100

// RegisterBoard registers zerops_board_propose and zerops_board_list: a Mate's
// agent proposes, for its application's board, unasked work it found outside
// every open change, after reading the board's open tasks
// (docs/spec-mate.md §6.11). The proposal writes nothing: Mate draws a card
// and the person's own client puts the task on the board. carriesBoard
// reports whether the installed Mate draws that card (mate.CarriesBoard).
func RegisterBoard(srv *mcp.Server, httpClient hq.Doer, rt runtime.Info, carriesBoard func() bool) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_board_propose",
		Description: "Propose a task for your application's board: work nobody asked for that you found outside every open change — another part, another repository, an unrelated bug. List the board first (zerops_board_list). Mate shows the person a card; their Put on board writes it. Writes nothing. Never propose work the person asked for: do it now.",
		Annotations: &mcp.ToolAnnotations{Title: "Propose a task for the board", ReadOnlyHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in BoardProposeInput) (*mcp.CallToolResult, any, error) {
		proposal, err := ops.NewBoardProposal(in.Title, in.Brief)
		if err != nil {
			return convertError(err), nil, nil
		}
		if refusal := noBoard(rt, "nothing was proposed"); refusal != "" {
			return observeFailure(refusal), nil, nil
		}
		// A Mate older than the card shows the call as a plain row: nobody
		// would see the proposal, let alone put it on the board.
		if !carriesBoard() {
			return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"This Mate cannot show a board card yet: it comes with a Mate update, so nothing was proposed",
				"Tell the person in one sentence what you found and that it is outside this work; never start it yourself.")), nil, nil
		}
		return jsonResult(boardProposeAnswer{
			Proposed: boardProposed{Board: proposal},
			NextActions: "Nothing was written: Mate shows the task in a card above, where the person can edit the brief and put it on the board. " +
				"Say one sentence — that you proposed it for the board, in the card above — and never start that work yourself, on no branch. " +
				"Then go on with what the person asked for.",
		}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_board_list",
		Description: "List the open tasks on your application's board — each one's id, title, state, who has it and where it came from — before proposing one with zerops_board_propose, so you never propose a task the board already holds.",
		Annotations: &mcp.ToolAnnotations{Title: "List the board's open tasks", ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		if refusal := noBoard(rt, "nothing was listed"); refusal != "" {
			return observeFailure(refusal), nil, nil
		}
		client, ok := openHQ(httpClient, rt)
		if !ok {
			return observeFailure("HQ could not be addressed, so the board could not be read."), nil, nil
		}
		tasks, err := client.Once().Bounded(3*time.Second, 10*time.Second).OpenTasks(ctx)
		if err != nil {
			return observeFailure(boardListRefusal(err)), nil, nil
		}
		return jsonResult(boardListOf(tasks, client.ProjectID())), nil, nil
	})
}

// noBoard is why this Mate has no board to propose to or read, "" when it has
// one or its state is not heard: HQ holds it in no application. undone says
// what that left undone.
func noBoard(rt runtime.Info, undone string) string {
	doc, err := hqIdentity(rt).ReadMateState()
	if err != nil {
		return ""
	}
	state, err := doc.Heard()
	if err != nil || state.AppID != nil {
		return ""
	}
	return strings.TrimSpace("HQ holds this Mate in no application yet, so it has no board and " + undone + ". " + doc.Standing())
}

// boardListOf tells the open tasks HQ listed, at most boardListMax of them,
// as the agent reads them beside own, this Mate's project.
func boardListOf(tasks []hq.Task, own string) boardListAnswer {
	answer := boardListAnswer{Tasks: []boardTask{}}
	for i, t := range tasks {
		if i == boardListMax {
			answer.More = len(tasks) - boardListMax
			break
		}
		answer.Tasks = append(answer.Tasks, boardTask{ID: t.ID, Title: t.Title, State: t.State, With: taskHolder(t, own), From: taskFrom(t, own)})
	}
	switch {
	case len(tasks) == 0:
		answer.NextActions = "The board has no open task: propose what you found with zerops_board_propose."
	default:
		answer.NextActions = "These are the board's open tasks. Propose only work none of them already covers; when one does, tell the person in one sentence that it is on the board, and propose nothing."
	}
	return answer
}

// taskHolder is who has a task, "" for nobody: the taker HQ names, else, from
// an HQ that names none, the Mate a task it handed out is with.
func taskHolder(t hq.Task, own string) string {
	if t.Taker != nil {
		switch {
		case t.Taker.Kind == hq.TakerPerson && t.Taker.MateProjectID == own:
			return "your person, in this Mate"
		case t.Taker.Kind == hq.TakerPerson:
			return "a person, in " + taskMateName(t, t.Taker.MateProjectID)
		case t.Taker.Kind == hq.TakerPool:
			return taskMateName(t, t.Taker.MateProjectID) + ", an automated Mate"
		}
	}
	if t.State == "queued" || t.Mate == nil {
		return ""
	}
	return taskMateName(t, t.Mate.ProjectID)
}

// taskMateName names the Mate of project as the task names it.
func taskMateName(t hq.Task, project string) string {
	if t.Mate != nil && t.Mate.ProjectID == project && t.Mate.Name != "" {
		return t.Mate.Name
	}
	return "another Mate"
}

// taskFrom is where a task came from: a conversation, in this Mate or
// another, or HQ's own label of its source.
func taskFrom(t hq.Task, own string) string {
	if t.Source != nil && t.Source.Kind == hq.SourceConversation {
		if t.Source.MateProjectID == own {
			return "a conversation in this Mate"
		}
		return "a conversation in another Mate"
	}
	return t.Label
}

// boardListRefusal says why the board could not be read, in words that say
// whether asking again helps.
func boardListRefusal(err error) string {
	if hq.IsUnavailable(err) {
		return "HQ did not answer, so the board could not be read. Try again in a moment; should it still not answer, propose anyway: the person sees the board before they put a task there."
	}
	if access := accessRefusal(err); access != "" {
		return access + ", so the board could not be read. Asking again will not help until that access is back: tell the person their Mate's access to HQ is refused."
	}
	var refused *hq.RefusedError
	if errors.As(err, &refused) && refused.Status == http.StatusNotFound {
		return "This Mate's HQ keeps no board for it yet, so nothing was listed. Propose anyway: the person sees the board before they put a task there."
	}
	return fmt.Sprintf("HQ refused to list the board's tasks (%v), so nothing was listed.", err)
}
