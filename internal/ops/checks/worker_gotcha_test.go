package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

// workerReadme assembles a knowledge-base fragment with the given
// stem/body pairs so the predicates have structured content to walk.
func workerReadme(entries []struct{ stem, body string }) string {
	var sb strings.Builder
	sb.WriteString("# Worker\n\n")
	sb.WriteString("<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n")
	sb.WriteString("## Gotchas\n\n")
	for _, e := range entries {
		sb.WriteString("- **")
		sb.WriteString(e.stem)
		sb.WriteString("** — ")
		sb.WriteString(e.body)
		sb.WriteString("\n")
	}
	sb.WriteString("\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->\n")
	return sb.String()
}

func workerTarget() workflow.RecipeTarget {
	return workflow.RecipeTarget{
		Hostname: "worker",
		IsWorker: true,
	}
}

func sharedCodebaseWorker() workflow.RecipeTarget {
	t := workerTarget()
	t.SharesCodebaseWith = "api"
	return t
}

func TestCheckWorkerQueueGroupGotcha_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		readme     string
		target     workflow.RecipeTarget
		wantStatus string
	}{
		{
			name:       "non-worker returns nil",
			readme:     workerReadme([]struct{ stem, body string }{{"a", "b"}}),
			target:     workflow.RecipeTarget{Hostname: "api"},
			wantStatus: "",
		},
		{
			name:       "shared-codebase worker returns nil",
			readme:     workerReadme([]struct{ stem, body string }{{"a", "b"}}),
			target:     sharedCodebaseWorker(),
			wantStatus: "",
		},
		{
			name:       "queue-group token present passes",
			readme:     workerReadme([]struct{ stem, body string }{{"NATS queue group", "prevents double-process under minContainers"}}),
			target:     workerTarget(),
			wantStatus: "pass",
		},
		{
			name:       "per-replica phrasing passes",
			readme:     workerReadme([]struct{ stem, body string }{{"Subscription fan-out", "every replica processes per replica if no queue group"}}),
			target:     workerTarget(),
			wantStatus: "pass",
		},
		{
			name:       "no queue-group topic fails",
			readme:     workerReadme([]struct{ stem, body string }{{"SIGTERM drain", "drain in-flight on shutdown"}}),
			target:     workerTarget(),
			wantStatus: "fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckWorkerQueueGroupGotcha(context.Background(), "worker", tt.readme, tt.target)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "worker_worker_queue_group_gotcha")
			if check == nil {
				t.Fatalf("expected check, got %+v", shim)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q (detail: %s)", check.Status, tt.wantStatus, check.Detail)
			}
		})
	}
}

func TestCheckWorkerShutdownGotcha_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		readme     string
		target     workflow.RecipeTarget
		wantStatus string
	}{
		{
			name:       "SIGTERM drain passes",
			readme:     workerReadme([]struct{ stem, body string }{{"SIGTERM drain", "call nc.drain then exit"}}),
			target:     workerTarget(),
			wantStatus: "pass",
		},
		{
			name:       "rolling deploy phrase passes",
			readme:     workerReadme([]struct{ stem, body string }{{"Rolling deploy", "losing messages without graceful shutdown"}}),
			target:     workerTarget(),
			wantStatus: "pass",
		},
		{
			name:       "only queue-group topic fails (shutdown absent)",
			readme:     workerReadme([]struct{ stem, body string }{{"Queue group", "processed twice"}}),
			target:     workerTarget(),
			wantStatus: "fail",
		},
		{
			name:       "non-worker returns nil",
			readme:     workerReadme([]struct{ stem, body string }{{"a", "b"}}),
			target:     workflow.RecipeTarget{Hostname: "api"},
			wantStatus: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckWorkerShutdownGotcha(context.Background(), "worker", tt.readme, tt.target)
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			if tt.wantStatus == "" {
				if len(shim) != 0 {
					t.Errorf("expected nil, got %+v", shim)
				}
				return
			}
			check := findCheck(shim, "worker_worker_shutdown_gotcha")
			if check == nil {
				t.Fatalf("expected check, got %+v", shim)
			}
			if check.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", check.Status, tt.wantStatus)
			}
		})
	}
}
