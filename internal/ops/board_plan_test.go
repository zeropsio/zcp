package ops

import (
	"fmt"
	"strings"
	"testing"
)

// planOf is a plan of tasks given as "key:need,need", each with a title and
// brief of its own.
func planOf(specs ...string) []BoardPlanTask {
	plan := make([]BoardPlanTask, 0, len(specs))
	for _, spec := range specs {
		key, needs, _ := strings.Cut(spec, ":")
		task := BoardPlanTask{Key: key, Title: "Step " + key, Brief: "Do step " + key + "."}
		if needs != "" {
			task.Needs = strings.Split(needs, ",")
		}
		plan = append(plan, task)
	}
	return plan
}

// witness: internal/ops/board_plan.go | case len(tasks) > BoardPlanMax: → case len(tasks) > BoardPlanMax+1:
func TestBoardPlan_PLCheck_ChecksEveryTaskAndNamesWhatItRefuses(t *testing.T) {
	t.Parallel()
	twenty := make([]string, 0, 21)
	for i := range 21 {
		twenty = append(twenty, fmt.Sprintf("s%d", i))
	}
	for _, tc := range []struct {
		name  string
		tasks []BoardPlanTask
		want  string // the plan as key<needs, or the refusal's words
	}{
		{name: "two in order", tasks: planOf("schema", "api:schema"), want: "schema<[] api<[schema]"},
		{name: "trimmed, repeats dropped", tasks: []BoardPlanTask{
			{Key: " schema ", Title: " Add the column ", Brief: " Add it. "},
			{Key: "api", Title: "Serve it", Brief: "Serve it.", Needs: []string{"schema", " schema"}},
		}, want: "schema<[] api<[schema]"},
		{name: "twenty", tasks: planOf(twenty[:20]...), want: "s0<[]"},
		{name: "one task", tasks: planOf("schema"), want: "at least 2 tasks"},
		{name: "twenty-one", tasks: planOf(twenty...), want: "at most 20 tasks"},
		{name: "a key in capitals", tasks: planOf("Schema", "api"), want: `Task 1's key "Schema"`},
		{name: "a key of 41 characters", tasks: planOf(strings.Repeat("k", 41), "api"), want: "is not 1–40 characters"},
		{name: "an empty key", tasks: planOf("", "api"), want: `Task 1's key ""`},
		{name: "a repeated key", tasks: planOf("api", "api"), want: `Two tasks have the key "api"`},
		{name: "an unknown need", tasks: planOf("schema", "api:scheme"), want: `Task "api" needs "scheme", which is no task of this plan`},
		{name: "a title over two lines", tasks: []BoardPlanTask{{Key: "a", Title: "x\ny", Brief: "b"}, {Key: "b", Title: "t", Brief: "b"}}, want: `Task "a": The title is one line`},
		{name: "no brief", tasks: []BoardPlanTask{{Key: "a", Title: "t", Brief: "b"}, {Key: "b", Title: "t", Brief: " "}}, want: `Task "b": A proposal needs its brief`},
		{name: "a circle", tasks: planOf("a:b", "b:a"), want: "in a circle: a → b → a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			proposal, err := NewBoardPlan(tc.tasks)
			got := ""
			if err != nil {
				got = err.Error()
			}
			for _, task := range proposal.Plan {
				got += fmt.Sprintf(" %s<%v", task.Key, task.Needs)
				if task.Needs == nil || strings.TrimSpace(task.Title) != task.Title || strings.TrimSpace(task.Brief) != task.Brief {
					t.Errorf("task %q = %+v, want trimmed with needs a list", task.Key, task)
				}
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("NewBoardPlan = %q, want %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

// witness: internal/ops/board_plan.go | return append(append([]string{}, path[i:]...), need) → return append(append([]string{}, path[i+1:]...), need)
func TestPlanCycle_PLCycle_NamesTheCircleOfNeeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		plan []BoardPlanTask
		want string
	}{
		{name: "no needs", plan: planOf("a", "b"), want: "[]"},
		{name: "a chain", plan: planOf("a", "b:a", "c:b"), want: "[]"},
		{name: "a diamond", plan: planOf("a", "b:a", "c:a", "d:b,c"), want: "[]"},
		{name: "needs on later tasks", plan: planOf("d:b,c", "b:a", "c:a", "a"), want: "[]"},
		{name: "a task needing itself", plan: planOf("a", "b:b"), want: "[b b]"},
		{name: "two needing each other", plan: planOf("a:b", "b:a"), want: "[a b a]"},
		{name: "a circle behind a chain", plan: planOf("a:b", "b:c", "c:d", "d:b"), want: "[b c d b]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fmt.Sprint(planCycle(tc.plan)); got != tc.want {
				t.Errorf("planCycle = %s, want %s", got, tc.want)
			}
		})
	}
}
