package ops

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/platform"
)

// A plan is several tasks the person puts on the board in one write, each
// naming by its key the tasks of the same plan it needs (docs/spec-mate.md
// §6.11): HQ holds a task until every task it needs is met.
const (
	BoardPlanMin = 2
	BoardPlanMax = 20
	BoardKeyMax  = 40
)

// boardKey is a plan-local key: 1–BoardKeyMax characters of a-z, 0-9 and -.
var boardKey = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// BoardPlanTask is one task of a plan: Key names it within the plan, Needs
// the keys of the plan's tasks it builds on, never null on the wire.
type BoardPlanTask struct {
	Key   string   `json:"key"`
	Title string   `json:"title"`
	Brief string   `json:"brief"`
	Needs []string `json:"needs"`
}

// NewBoardPlan checks a plan and writes nothing: BoardPlanMin–BoardPlanMax
// tasks, each with its own key, a title and brief as NewBoardProposal takes
// them, and needs naming other keys of the plan, never in a circle. Every
// refusal names the task and key it is about.
func NewBoardPlan(tasks []BoardPlanTask) (BoardProposal, error) {
	shape := fmt.Sprintf("tasks: %d–%d tasks in the order they land, each {key, title, brief, needs}: key %d characters at most of a-z, 0-9 and -, needs the keys of this plan's tasks it builds on.",
		BoardPlanMin, BoardPlanMax, BoardKeyMax)
	switch {
	case len(tasks) < BoardPlanMin:
		return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("A plan holds at least %d tasks", BoardPlanMin), "Propose a single task with title and brief instead of tasks.")
	case len(tasks) > BoardPlanMax:
		return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("A plan holds at most %d tasks", BoardPlanMax), shape)
	}
	plan := make([]BoardPlanTask, 0, len(tasks))
	keys := make(map[string]bool, len(tasks))
	for i, task := range tasks {
		key := strings.TrimSpace(task.Key)
		switch {
		case !boardKey.MatchString(key):
			return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Task %d's key %q is not 1–%d characters of a-z, 0-9 and -", i+1, task.Key, BoardKeyMax), shape)
		case keys[key]:
			return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("Two tasks have the key %q", key), "Give each task of the plan its own key.")
		}
		keys[key] = true
		one, err := NewBoardProposal(task.Title, task.Brief)
		if err != nil {
			var pe *platform.PlatformError
			if errors.As(err, &pe) {
				return BoardProposal{}, platform.NewPlatformError(pe.Code, fmt.Sprintf("Task %q: %s", key, pe.Message), pe.Suggestion)
			}
			return BoardProposal{}, fmt.Errorf("task %q: %w", key, err)
		}
		plan = append(plan, BoardPlanTask{Key: key, Title: one.Title, Brief: one.Brief})
	}
	for i, task := range tasks {
		needs := []string{}
		for _, need := range task.Needs {
			need = strings.TrimSpace(need)
			switch {
			case !keys[need]:
				return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
					fmt.Sprintf("Task %q needs %q, which is no task of this plan", plan[i].Key, need),
					"needs names only the keys of this plan's tasks.")
			case !slices.Contains(needs, need):
				needs = append(needs, need)
			}
		}
		plan[i].Needs = needs
	}
	if cycle := planCycle(plan); cycle != nil {
		return BoardProposal{}, platform.NewPlatformError(platform.ErrInvalidParameter,
			"The plan's needs go in a circle: "+strings.Join(cycle, " → "),
			"A task needs only tasks that land before it: drop one need of that circle.")
	}
	return BoardProposal{Plan: plan}, nil
}

// planCycle is the first circle of needs in plan, as the keys walked from a
// task back to itself (a self-need is [a a]), nil when the needs hold none.
// Each need names a key of plan.
func planCycle(plan []BoardPlanTask) []string {
	needs := make(map[string][]string, len(plan))
	for _, task := range plan {
		needs[task.Key] = task.Needs
	}
	const (
		unseen = iota
		walking
		done
	)
	mark := make(map[string]int, len(plan))
	var path []string
	var walk func(key string) []string
	walk = func(key string) []string {
		mark[key] = walking
		path = append(path, key)
		for _, need := range needs[key] {
			switch mark[need] {
			case walking:
				for i, k := range path {
					if k == need {
						return append(append([]string{}, path[i:]...), need)
					}
				}
			case unseen:
				if cycle := walk(need); cycle != nil {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		mark[key] = done
		return nil
	}
	for _, task := range plan {
		if mark[task.Key] == unseen {
			if cycle := walk(task.Key); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}
