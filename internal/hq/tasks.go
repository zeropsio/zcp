package hq

import (
	"context"
	"fmt"
	"net/http"
)

// An application's board is HQ's task record for it (docs/spec-mate.md §6.11):
// HQ keeps every task, who put it there and who has it. A Mate only reads the
// open ones, to propose no task the board already holds; a task is put on the
// board by a person's own client, never by zcp.

// Kinds of who has a task, as HQ names them.
const (
	TakerPool   = "pool"
	TakerPerson = "person"
)

// SourceConversation is the kind of a task a person put on the board from a
// conversation in a Mate.
const SourceConversation = "conversation"

// TaskMate is the Mate a task is with, as HQ names it now.
type TaskMate struct {
	ProjectID string `json:"projectId"`
	Name      string `json:"name"`
}

// TaskTaker is who has a task: an automated Mate of the pool, or a person
// who took it into their own Mate.
type TaskTaker struct {
	Kind          string `json:"kind"`
	MateProjectID string `json:"mateProjectId"`
}

// TaskSource is where a task came from; zcp reads only its kind and, for a
// conversation, the Mate it was put from.
type TaskSource struct {
	Kind          string `json:"kind"`
	MateProjectID string `json:"mateProjectId,omitempty"`
}

// Task is one open task of the Mate's application as `GET /api/mate/tasks`
// lists it; zcp reads only what it tells the agent.
type Task struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	State  string      `json:"state"`
	Label  string      `json:"label"`
	Mate   *TaskMate   `json:"mate"`
	Taker  *TaskTaker  `json:"taker"`
	Source *TaskSource `json:"source"`
}

// OpenTasks lists the open tasks of the application HQ holds this Mate in.
// HQ's refusals come back as *RefusedError, carrying its code and reason.
func (c Client) OpenTasks(ctx context.Context) ([]Task, error) {
	var answer struct {
		Tasks []Task `json:"tasks"`
	}
	if err := c.call.json(ctx, http.MethodGet, "/api/mate/tasks", c.authorization(), nil, &answer); err != nil {
		return nil, fmt.Errorf("list the board's tasks: %w", err)
	}
	return answer.Tasks, nil
}
