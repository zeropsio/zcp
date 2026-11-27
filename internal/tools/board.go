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
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// BoardProposeInput is zerops_board_propose's input: one task (Title and
// Brief) or a plan (Tasks), never both.
type BoardProposeInput struct {
	Title string               `json:"title,omitempty" jsonschema:"One task: the task in one line, at most 120 characters, as the board lists it."`
	Brief string               `json:"brief,omitempty" jsonschema:"One task: the whole task, at most 8000 characters, for whoever takes it: they have not seen this conversation. Every member of the organization reads the board: no secret, value or excerpt of this conversation."`
	Tasks []BoardPlanTaskInput `json:"tasks,omitempty" jsonschema:"A plan, instead of title and brief: 2-20 tasks in the order they must land, for work whose steps build on each other's changes."`
}

// BoardPlanTaskInput is one task of a proposed plan.
type BoardPlanTaskInput struct {
	Key   string   `json:"key"             jsonschema:"The task's key within this plan: 1-40 characters of a-z, 0-9 and -."`
	Title string   `json:"title"           jsonschema:"The task in one line, at most 120 characters."`
	Brief string   `json:"brief"           jsonschema:"The whole task, at most 8000 characters, self-contained for whoever takes it: they have seen neither this conversation nor the plan's other briefs. No secret, value or excerpt of this conversation."`
	Needs []string `json:"needs,omitempty" jsonschema:"The keys of this plan's tasks whose changes this one builds on: it waits until they are done and merged."`
}

type boardProposed struct {
	Board ops.BoardProposal `json:"board"`
}

type boardProposeAnswer struct {
	Proposed    boardProposed `json:"proposed"`
	NextActions string        `json:"nextActions"`
}

// boardTask is one open task as zerops_board_list tells it: With is who has
// it, From where it came from and Needs the tasks it waits on, each absent
// when HQ says nothing of it.
type boardTask struct {
	ID    string      `json:"id"`
	Title string      `json:"title"`
	State string      `json:"state"`
	With  string      `json:"with,omitempty"`
	From  string      `json:"from,omitempty"`
	Needs []boardNeed `json:"needs,omitempty"`
}

// boardNeed is a task another one needs: its title when HQ or the list names
// it, and HQ's state of the need (waiting, met or broken).
type boardNeed struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
	State string `json:"state"`
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
// and the person's own client puts the task, or the plan's tasks, on the
// board. installed reads the installed Mate's version, which says whether it
// draws that card (mate.CarriesBoard, mate.CarriesBoardPlan).
func RegisterBoard(srv *mcp.Server, httpClient hq.Doer, rt runtime.Info, installed func() string) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "zerops_board_propose",
		Description: "Propose a task for your application's board: unasked work found outside every open change — another part or repository, an unrelated bug — or work the person asks to put there. Steps building on each other: one plan with needs. List the board first (zerops_board_list). Mate shows a card; Put on board writes it. Other asked work: do it now.",
		Annotations: &mcp.ToolAnnotations{Title: "Propose a task for the board", ReadOnlyHint: true, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in BoardProposeInput) (*mcp.CallToolResult, any, error) {
		proposal, err := boardProposal(in)
		if err != nil {
			return convertError(err), nil, nil
		}
		if refusal := noBoard(rt, "nothing was proposed"); refusal != "" {
			return observeFailure(refusal), nil, nil
		}
		// A Mate older than the card shows the call as a plain row: nobody
		// would see the proposal, let alone put it on the board.
		version := installed()
		if !mate.CarriesBoard(version) {
			return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"This Mate cannot show a board card yet: it comes with a Mate update, so nothing was proposed",
				"Tell the person in one sentence what you found and that it is outside this work; never start it yourself.")), nil, nil
		}
		if proposal.Plan == nil {
			return jsonResult(boardProposeAnswer{
				Proposed: boardProposed{Board: proposal},
				NextActions: "Nothing was written: Mate shows the task in a card in this conversation, where the person can edit the brief and put it on the board. " +
					"Say one sentence — that you proposed it for the board, in its card — and never start that work yourself, on no branch. " +
					"Then go on with what the person asked for.",
			}), nil, nil
		}
		// A Mate older than the plan card draws one task's card at most.
		if !mate.CarriesBoardPlan(version) {
			return convertError(platform.NewPlatformError(platform.ErrPrerequisiteMissing,
				"This Mate cannot show a plan card yet: it comes with a Mate update, so nothing was proposed",
				"Propose the plan's tasks one by one, each with title and brief, in the order they must land, and say in each brief which of them it builds on.")), nil, nil
		}
		return jsonResult(boardProposeAnswer{
			Proposed: boardProposed{Board: proposal},
			NextActions: "Nothing was written: Mate shows the plan in one card in this conversation — its tasks, their order and what each needs — where the person can put them on the board together. " +
				"Say one sentence — that you proposed the plan for the board, in its card — and start none of its tasks yourself, on no branch. " +
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

// boardProposal checks what the agent proposes: one task, or a plan of
// several, never both.
func boardProposal(in BoardProposeInput) (ops.BoardProposal, error) {
	if len(in.Tasks) == 0 {
		return ops.NewBoardProposal(in.Title, in.Brief)
	}
	if strings.TrimSpace(in.Title) != "" || strings.TrimSpace(in.Brief) != "" {
		return ops.BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			"A proposal is one task or a plan, never both",
			"Pass title and brief for one task, or tasks for a plan whose steps build on each other.")
	}
	tasks := make([]ops.BoardPlanTask, 0, len(in.Tasks))
	for _, task := range in.Tasks {
		tasks = append(tasks, ops.BoardPlanTask{Key: task.Key, Title: task.Title, Brief: task.Brief, Needs: task.Needs})
	}
	return ops.NewBoardPlan(tasks)
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
	titles := make(map[string]string, len(tasks))
	for _, t := range tasks {
		titles[t.ID] = t.Title
	}
	for i, t := range tasks {
		if i == boardListMax {
			answer.More = len(tasks) - boardListMax
			break
		}
		answer.Tasks = append(answer.Tasks, boardTask{ID: t.ID, Title: t.Title, State: t.State, With: taskHolder(t, own), From: taskFrom(t, own), Needs: taskNeeds(t, titles)})
	}
	switch {
	case len(tasks) == 0:
		answer.NextActions = "The board has no open task: propose what you found with zerops_board_propose."
	default:
		answer.NextActions = "These are the board's open tasks. Propose only work none of them already covers; when one does, tell the person in one sentence that it is on the board, and propose nothing."
	}
	return answer
}

// taskNeeds is what a task needs, each named by its title: HQ's own, else
// the title of the open task of that id; nil for a task that needs nothing.
func taskNeeds(t hq.Task, titles map[string]string) []boardNeed {
	if len(t.Needs) == 0 {
		return nil
	}
	needs := make([]boardNeed, 0, len(t.Needs))
	for _, need := range t.Needs {
		title := need.Title
		if title == "" {
			title = titles[need.TaskID]
		}
		needs = append(needs, boardNeed{ID: need.TaskID, Title: title, State: need.State})
	}
	return needs
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
