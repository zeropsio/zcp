// Tests for: internal/tools/workflow_checks_worker_correctness.go
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

const v16WorkerReadmeNoCorrectnessGotchas = `# Worker

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **` + "`DEPLOY_FAILED`" + ` with no logs when ` + "`httpSupport: true`" + ` is missing** — the platform requires ` + "`httpSupport: true`" + ` on the health-check port. Without it, the readiness probe never fires.
- **` + "`run.envVariables`" + ` not available until first deploy** — before the first deploy, env vars resolve to empty strings.
- **` + "`typeorm_metadata`" + ` advisory lock contention** — if the worker's initCommands also run migrate.ts, two containers race for the lock.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

const workerReadmeWithCorrectness = `# Worker

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **NATS queue group mandatory under minContainers > 1** — without ` + "`queue: 'workers'`" + ` in the subscribe call every container processes every message, so a 2-replica worker runs every job twice.
- **SIGTERM drain for in-flight jobs** — Zerops sends SIGTERM during rolling deploys; the handler must call nc.drain() and await completion before process.exit(0) or messages are lost.
- **` + "`zsc execOnce`" + ` burn on failed seed** — if a seed crashes mid-run the appVersionId key is consumed; touch any source file to rotate.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

const workerReadmeQueueGroupOnly = `# Worker

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **NATS queue group mandatory under minContainers > 1** — without queue group option, double-process across replicas.
- **` + "`zsc execOnce`" + ` burn on failed seed** — consumed key.
- **` + "`typeorm_metadata`" + ` advisory lock contention** — race on migration startup.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

const workerReadmeShutdownOnly = `# Worker

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **SIGTERM drain prevents in-flight message loss** — call nc.drain() on graceful shutdown before exit.
- **` + "`zsc execOnce`" + ` burn on failed seed** — consumed key.
- **` + "`typeorm_metadata`" + ` advisory lock contention** — race on migration startup.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

func TestCheckWorkerProductionCorrectness_V16RegressionCaught(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}
	checks := checkWorkerProductionCorrectness(t.Context(), "worker", v16WorkerReadmeNoCorrectnessGotchas, target)

	var queueFail, shutdownFail bool
	for _, c := range checks {
		if c.Name == "worker_worker_queue_group_gotcha" && c.Status == statusFail {
			queueFail = true
		}
		if c.Name == "worker_worker_shutdown_gotcha" && c.Status == statusFail {
			shutdownFail = true
		}
	}
	if !queueFail {
		t.Errorf("expected queue-group gotcha to fail on v16 worker README, got checks: %+v", checks)
	}
	if !shutdownFail {
		t.Errorf("expected shutdown gotcha to fail on v16 worker README, got checks: %+v", checks)
	}
}

func TestCheckWorkerProductionCorrectness_BothCovered(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}
	checks := checkWorkerProductionCorrectness(t.Context(), "worker", workerReadmeWithCorrectness, target)

	var pass bool
	for _, c := range checks {
		if c.Status == statusFail {
			t.Errorf("expected all checks to pass, got fail: %s — %s", c.Name, c.Detail)
		}
		if c.Name == "worker_worker_production_correctness" && c.Status == statusPass {
			pass = true
		}
	}
	if !pass {
		t.Errorf("expected worker_worker_production_correctness = pass, got checks: %+v", checks)
	}
}

func TestCheckWorkerProductionCorrectness_PartialCoverage(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}

	// Only queue group covered — shutdown missing
	checks := checkWorkerProductionCorrectness(t.Context(), "worker", workerReadmeQueueGroupOnly, target)
	var queueOK, shutdownFail bool
	for _, c := range checks {
		if c.Name == "worker_worker_queue_group_gotcha" && c.Status != statusFail {
			queueOK = true
		}
		if c.Name == "worker_worker_shutdown_gotcha" && c.Status == statusFail {
			shutdownFail = true
		}
	}
	if queueOK && !shutdownFail {
		// queue group check doesn't emit a pass entry when it alone
		// passes (only the combined pass). Check there's NO queue fail
		// and there IS a shutdown fail.
		for _, c := range checks {
			if c.Name == "worker_worker_queue_group_gotcha" && c.Status == statusFail {
				t.Errorf("queue-group should pass on queue-only fixture")
			}
		}
	}
	if !shutdownFail {
		t.Errorf("expected shutdown to fail on queue-only fixture, got checks: %+v", checks)
	}

	// Only shutdown covered — queue group missing
	checks = checkWorkerProductionCorrectness(t.Context(), "worker", workerReadmeShutdownOnly, target)
	var queueFailFound bool
	for _, c := range checks {
		if c.Name == "worker_worker_queue_group_gotcha" && c.Status == statusFail {
			queueFailFound = true
		}
	}
	if !queueFailFound {
		t.Errorf("expected queue-group to fail on shutdown-only fixture, got checks: %+v", checks)
	}
}

func TestCheckWorkerProductionCorrectness_NonWorkerSkipped(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "api", Type: "nodejs@22", IsWorker: false}
	checks := checkWorkerProductionCorrectness(t.Context(), "api", v16WorkerReadmeNoCorrectnessGotchas, target)
	if len(checks) != 0 {
		t.Errorf("expected no checks for non-worker target, got %d", len(checks))
	}
}

func TestCheckWorkerProductionCorrectness_SharedCodebaseSkipped(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{
		Hostname: "worker", Type: "php-nginx@8.4",
		IsWorker: true, SharesCodebaseWith: "app",
	}
	checks := checkWorkerProductionCorrectness(t.Context(), "worker", v16WorkerReadmeNoCorrectnessGotchas, target)
	if len(checks) != 0 {
		t.Errorf("expected shared-codebase worker to skip check, got %d checks", len(checks))
	}
}

// TestCheckWorkerDrainCodeBlock verifies the v18 regression: worker
// READMEs must carry a fenced code block showing the SIGTERM → drain →
// exit call sequence. v7's worker README had this as IG #3 with a
// full typescript diff. v18's worker README referenced drain/close in
// prose inside a gotcha but shipped zero code examples for the drain
// sequence — a reader copy-pasting the gotcha has no reference code.
func TestCheckWorkerDrainCodeBlock(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}

	// v18 workerdev regression: prose-only drain mention, no code.
	v18Regression := `# Worker

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding ` + "`zerops.yaml`" + `

` + "```yaml" + `
zerops:
  - setup: prod
` + "```" + `

### 2. Pass NATS credentials as separate options

` + "```typescript" + `
const app = await NestFactory.createMicroservice(AppModule, {});
` + "```" + `

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **NATS queue group required under minContainers > 1** — under horizontal scaling, every replica processes every message.
- **Graceful shutdown on SIGTERM prevents in-flight loss** — catch SIGTERM, call app.close() which triggers OnModuleDestroy, drain the NATS connection via nc.drain(), then exit. Without this, rolling deploys silently lose jobs.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

	// v7 worker-style: drain sequence in a fenced typescript block.
	v7WithDrainCode := `# Worker

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding ` + "`zerops.yaml`" + `

` + "```yaml" + `
zerops:
  - setup: prod
` + "```" + `

### 3. Drain on SIGTERM

` + "```typescript" + `
const stop = async (signal: string) => {
  await nc.drain();
  await dataSource.destroy();
  process.exit(0);
};
process.on('SIGTERM', () => void stop('SIGTERM'));
` + "```" + `

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- **NATS queue group mandatory under minContainers > 1** — every replica processes every message otherwise.
- **Graceful shutdown on SIGTERM** — drain in-flight messages before exit.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`

	tests := []struct {
		name    string
		readme  string
		target  workflow.RecipeTarget
		want    string // "pass", "fail", or "" for skipped
		wantSub string // substring in detail
	}{
		{"v18 prose-only drain fails", v18Regression, target, "fail", "drain"},
		{"v7 drain code block passes", v7WithDrainCode, target, "pass", ""},
		{
			"shared-codebase worker skipped",
			v18Regression,
			workflow.RecipeTarget{Hostname: "worker", IsWorker: true, SharesCodebaseWith: "app"},
			"",
			"",
		},
		{
			"non-worker skipped",
			v18Regression,
			workflow.RecipeTarget{Hostname: "api", IsWorker: false},
			"",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := checkWorkerDrainCodeBlock("worker", tt.readme, tt.target)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("expected no checks (skipped), got: %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("checks len = %d, want 1: %+v", len(got), got)
			}
			if got[0].Name != "worker_drain_code_block" {
				t.Errorf("check name = %q", got[0].Name)
			}
			if got[0].Status != tt.want {
				t.Errorf("status = %q, want %q; detail: %s", got[0].Status, tt.want, got[0].Detail)
			}
			if tt.wantSub != "" && !strings.Contains(got[0].Detail, tt.wantSub) {
				t.Errorf("detail %q missing expected substring %q", got[0].Detail, tt.wantSub)
			}
		})
	}
}

func TestCheckWorkerProductionCorrectness_FailMessagesAreActionable(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}
	checks := checkWorkerProductionCorrectness(t.Context(), "worker", v16WorkerReadmeNoCorrectnessGotchas, target)
	for _, c := range checks {
		if c.Status != statusFail {
			continue
		}
		if c.Name == "worker_worker_queue_group_gotcha" {
			if !strings.Contains(c.Detail, "minContainers") || !strings.Contains(c.Detail, "queue group") {
				t.Errorf("queue-group fail message missing actionable terms: %s", c.Detail)
			}
		}
		if c.Name == "worker_worker_shutdown_gotcha" {
			if !strings.Contains(c.Detail, "SIGTERM") || !strings.Contains(c.Detail, "drain") {
				t.Errorf("shutdown fail message missing actionable terms: %s", c.Detail)
			}
		}
	}
}
