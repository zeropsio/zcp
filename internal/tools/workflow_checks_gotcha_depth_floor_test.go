package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// gotcha_depth_floor enforces a per-role minimum gotcha count so the
// quality checks (causal-anchor, content-reality) don't incentivize
// deletion-to-pass. v8.78 rolled back predecessor-floor for the right
// reason (standalone recipes shouldn't fail on predecessor overlap),
// but left ZERO upward pressure on content. v21 landed exactly at the
// service_coverage floor (6/4/5 gotchas); this replaces the void.

func kbOfN(n int) string {
	b := strings.Builder{}
	b.WriteString("### Gotchas\n")
	for i := range n {
		b.WriteString("- **Gotcha ")
		b.WriteRune(rune('A' + i))
		b.WriteString("** — body.\n")
	}
	return b.String()
}

func TestGotchaDepthFloor_ApiRole_RequiresFive(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor(kbOfN(4), "api", "apidev"); checks[0].Status != statusFail {
		t.Fatalf("4 gotchas for api role must fail: %+v", checks)
	}
	if checks := checkGotchaDepthFloor(kbOfN(5), "api", "apidev"); checks[0].Status != statusPass {
		t.Fatalf("5 gotchas for api role must pass: %+v", checks)
	}
}

func TestGotchaDepthFloor_FrontendRole_RequiresThree(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor(kbOfN(2), "frontend", "appdev"); checks[0].Status != statusFail {
		t.Fatalf("2 gotchas for frontend role must fail: %+v", checks)
	}
	if checks := checkGotchaDepthFloor(kbOfN(3), "frontend", "appdev"); checks[0].Status != statusPass {
		t.Fatalf("3 gotchas for frontend role must pass: %+v", checks)
	}
}

func TestGotchaDepthFloor_WorkerRole_RequiresFour(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor(kbOfN(3), "worker", "workerdev"); checks[0].Status != statusFail {
		t.Fatalf("3 gotchas for worker role must fail: %+v", checks)
	}
	if checks := checkGotchaDepthFloor(kbOfN(4), "worker", "workerdev"); checks[0].Status != statusPass {
		t.Fatalf("4 gotchas for worker role must pass: %+v", checks)
	}
}

func TestGotchaDepthFloor_FullstackRole_RequiresFive(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor(kbOfN(4), "fullstack", "app"); checks[0].Status != statusFail {
		t.Fatalf("4 gotchas for fullstack must fail: %+v", checks)
	}
	if checks := checkGotchaDepthFloor(kbOfN(5), "fullstack", "app"); checks[0].Status != statusPass {
		t.Fatalf("5 gotchas for fullstack must pass: %+v", checks)
	}
}

func TestGotchaDepthFloor_UnknownRole_Skips(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor(kbOfN(2), "", "apidev"); len(checks) != 0 {
		t.Fatalf("unknown role must no-op; got %+v", checks)
	}
	if checks := checkGotchaDepthFloor(kbOfN(2), "mystery", "apidev"); len(checks) != 0 {
		t.Fatalf("unknown role must no-op; got %+v", checks)
	}
}

func TestGotchaDepthFloor_FailDetailNamesCount(t *testing.T) {
	t.Parallel()
	checks := checkGotchaDepthFloor(kbOfN(4), "api", "apidev")
	if checks[0].Status != statusFail {
		t.Fatalf("expected fail: %+v", checks)
	}
	detail := checks[0].Detail
	for _, w := range []string{"has 4", "at least 5", "apidev", "api"} {
		if !strings.Contains(detail, w) {
			t.Errorf("detail must contain %q: %s", w, detail)
		}
	}
}

func TestGotchaDepthFloor_EmptyContent_Skips(t *testing.T) {
	t.Parallel()
	if checks := checkGotchaDepthFloor("", "api", "apidev"); len(checks) != 0 {
		t.Fatalf("empty content should no-op; got %+v", checks)
	}
}

// TestCodebaseRole_Mapping — end-to-end: given the plan shape that
// produces each role, the helper returns the expected classification.
func TestCodebaseRole_Mapping(t *testing.T) {
	t.Parallel()
	dualRuntime := &workflow.RecipePlan{
		Targets: []workflow.RecipeTarget{
			{Hostname: "apidev", Type: "nodejs@22", Role: workflow.RecipeRoleAPI},
			{Hostname: "appdev", Type: "static", DevBase: "nodejs@22", Role: workflow.RecipeRoleApp},
			{Hostname: "workerdev", Type: "nodejs@22", IsWorker: true},
		},
	}
	cases := []struct {
		host string
		want string
	}{
		{"apidev", "api"},
		{"appdev", "frontend"},
		{"workerdev", "worker"},
		{"nosuch", ""},
	}
	for _, c := range cases {
		if got := workflow.CodebaseRole(dualRuntime, c.host); got != c.want {
			t.Errorf("CodebaseRole(%q) = %q; want %q", c.host, got, c.want)
		}
	}

	// Single-codebase fullstack: RecipeRoleApp with NO api peer → fullstack.
	fullstack := &workflow.RecipePlan{
		Targets: []workflow.RecipeTarget{
			{Hostname: "app", Type: "php@8.3", Role: workflow.RecipeRoleApp},
		},
	}
	if got := workflow.CodebaseRole(fullstack, "app"); got != "fullstack" {
		t.Errorf("single-codebase CodebaseRole = %q; want fullstack", got)
	}
}
