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
	checks := checkWorkerProductionCorrectness("worker", v16WorkerReadmeNoCorrectnessGotchas, target)

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
	checks := checkWorkerProductionCorrectness("worker", workerReadmeWithCorrectness, target)

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
	checks := checkWorkerProductionCorrectness("worker", workerReadmeQueueGroupOnly, target)
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
	checks = checkWorkerProductionCorrectness("worker", workerReadmeShutdownOnly, target)
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
	checks := checkWorkerProductionCorrectness("api", v16WorkerReadmeNoCorrectnessGotchas, target)
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
	checks := checkWorkerProductionCorrectness("worker", v16WorkerReadmeNoCorrectnessGotchas, target)
	if len(checks) != 0 {
		t.Errorf("expected shared-codebase worker to skip check, got %d checks", len(checks))
	}
}

func TestCheckWorkerProductionCorrectness_FailMessagesAreActionable(t *testing.T) {
	t.Parallel()
	target := workflow.RecipeTarget{Hostname: "worker", Type: "nodejs@22", IsWorker: true}
	checks := checkWorkerProductionCorrectness("worker", v16WorkerReadmeNoCorrectnessGotchas, target)
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
