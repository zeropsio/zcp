//go:build e2e

// Tests for: e2e — live verification that ops.ProjectActivity detects a real
// in-flight buildFromGit deploy on the platform (the recipe-first-deploy race),
// and that the <1s fast-fail (FAILED build + frozen WAITING_TO_BUILD) is
// correctly NOT busy so recovery is never gated.
//
// Run: ZCP_API_KEY=<eval-token> go test ./e2e/ -tags e2e -run InFlight -v
package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const inflightRecipeRepo = "https://github.com/zeropsio/recipe-nodejs-hello-world"

func inflightImportYAML(hostname, repo string) string {
	return inflightImportYAMLOpts(hostname, repo, false)
}

// inflightImportYAMLOpts builds the probe import YAML; subdomain=true enqueues a
// stack.enableSubdomainAccess process alongside the build (the multi-live-op case
// the wait-drain test exercises).
func inflightImportYAMLOpts(hostname, repo string, subdomain bool) string {
	return fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    enableSubdomainAccess: %t
    minContainers: 1
    maxContainers: 1
    buildFromGit: %s
    zeropsSetup: helloworld
`, hostname, subdomain, repo)
}

// importInflightProbe provisions a buildFromGit runtime and returns its service
// ID. Registers cleanup.
func importInflightProbe(t *testing.T, h *e2eHarness, ctx context.Context, hostname, repo string) string {
	t.Helper()
	return importInflightProbeYAML(t, h, ctx, hostname, inflightImportYAML(hostname, repo))
}

// importInflightProbeYAML imports the given YAML and returns the new service ID.
// Registers cleanup.
func importInflightProbeYAML(t *testing.T, h *e2eHarness, ctx context.Context, hostname, yaml string) string {
	t.Helper()
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cleanupServices(cctx, h.client, h.projectID, hostname)
	})
	res, err := h.client.ImportServices(ctx, h.projectID, yaml)
	if err != nil {
		t.Fatalf("import %s: %v", hostname, err)
	}
	for _, ss := range res.ServiceStacks {
		if ss.Error != nil {
			t.Fatalf("import %s service error: %s — %s", hostname, ss.Error.Code, ss.Error.Message)
		}
		if ss.Name == hostname {
			return ss.ID
		}
	}
	t.Fatalf("no service id resolved for %s (result: %+v)", hostname, res.ServiceStacks)
	return ""
}

// TestInFlightActivity_LiveBuildTimeline provisions a real buildFromGit runtime,
// proves ProjectActivity reports it BUSY (build/deploy + a live processId) while
// it reads READY_TO_DEPLOY, verifies the process/target id cross-reference, waits
// for it to settle to ACTIVE + not-busy, then deletes it and asserts it is gone.
func TestInFlightActivity_LiveBuildTimeline(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	hostname := "zcpifl" + randomSuffix()
	serviceID := importInflightProbe(t, h, ctx, hostname, inflightRecipeRepo)
	idToHost := map[string]string{serviceID: hostname}

	// 0) The DIRECT list surfaces the just-imported service at-creation (the fix:
	// discover no longer goes blind during the import while the ES search lags).
	var directSaw bool
	for i := 0; i < 15 && !directSaw; i++ {
		svcs, derr := h.client.ListServicesDirect(ctx, h.projectID)
		if derr != nil {
			t.Fatalf("ListServicesDirect: %v", derr)
		}
		for _, s := range svcs {
			if s.ID == serviceID {
				directSaw = true
				t.Logf("ListServicesDirect surfaced %s (status=%s) after ~%ds", hostname, s.Status, i)
			}
		}
		if directSaw {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if !directSaw {
		t.Fatal("ListServicesDirect never surfaced the just-imported service (the lag-free read regressed)")
	}

	// 1) BUSY during the build, while the service reads READY_TO_DEPLOY.
	var busy ops.LiveOp
	var sawBusy, sawReadyWhileBusy bool
	for i := 0; i < 30 && !sawBusy; i++ {
		act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost)
		if err != nil {
			t.Fatalf("ProjectActivity: %v", err)
		}
		if op, ok := buildOrDeployOp(act[hostname]); ok {
			busy, sawBusy = op, true
			svc, _ := h.client.GetService(ctx, serviceID)
			if svc != nil && svc.Status == platform.ServiceStatusReadyToDeploy {
				sawReadyWhileBusy = true
			}
			t.Logf("busy: action=%s status=%s processId=%s svcStatus=%v", op.Action, op.Status, op.ProcessID, svc.Status)
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !sawBusy {
		t.Fatal("ProjectActivity never reported the buildFromGit service busy during its first deploy")
	}
	if busy.ProcessID == "" {
		t.Error("busy verdict must carry a processId (loop-safety / cancel-escape)")
	}
	if busy.Action != "build" && busy.Action != "deploy" {
		t.Errorf("busy action = %q, want build|deploy", busy.Action)
	}
	if !sawReadyWhileBusy {
		t.Logf("note: did not catch READY_TO_DEPLOY while busy (build moved fast); the busy detection itself held")
	}

	// 2) ID cross-reference via the DIRECT process source (the one ProjectActivity
	// uses): the live stack.build process references the target serviceID, and any
	// OTHER ref (the ephemeral build container) is a distinct id — so target-id
	// matching is unambiguous. (The ES SearchProcesses would still be empty here —
	// detection is at-creation, faster than the ES index.)
	procs, err := h.client.GetProjectProcessesDirect(ctx, h.projectID)
	if err != nil {
		t.Fatalf("GetProjectProcessesDirect: %v", err)
	}
	var foundTargetInBuild bool
	for _, p := range procs {
		if p.ActionName != "stack.build" {
			continue
		}
		refsTarget := false
		for _, ref := range p.ServiceStacks {
			if ref.ID == serviceID {
				refsTarget = true
			}
		}
		if !refsTarget {
			continue
		}
		foundTargetInBuild = true
		for _, ref := range p.ServiceStacks {
			if ref.ID != serviceID && ref.ID == "" {
				t.Error("build process carried an empty serviceStack id")
			}
		}
	}
	if !foundTargetInBuild {
		t.Error("no stack.build process referenced the target serviceID")
	}

	// 3) Settle to ACTIVE + not-busy (the build completes; activity clears).
	var settled bool
	for i := 0; i < 90 && !settled; i++ {
		act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost)
		if err != nil {
			t.Fatalf("ProjectActivity (settle): %v", err)
		}
		svc, _ := h.client.GetService(ctx, serviceID)
		if len(act[hostname]) == 0 && svc != nil && svc.Status == platform.ServiceStatusActive {
			settled = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !settled {
		t.Fatal("buildFromGit service never settled to ACTIVE + not-busy")
	}

	// 4) DELETE + assert gone.
	proc, err := h.client.DeleteService(ctx, serviceID)
	if err != nil {
		t.Fatalf("delete %s: %v", hostname, err)
	}
	if proc != nil {
		waitForProcessDirect(ctx, h.client, proc.ID)
	}
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services after delete: %v", err)
	}
	for _, s := range services {
		if s.ID == serviceID {
			t.Errorf("service %s (%s) still present after delete", hostname, serviceID)
		}
	}
}

// TestInFlightActivity_FastFailNotBusy provisions a buildFromGit runtime whose
// clone-preflight fast-fails (the .git-suffix trap), then proves the FAILED
// build + frozen WAITING_TO_BUILD appVersion is NOT busy — so adopt + corrective
// deploy after a failure are never gated (the loop trap, neutralized).
func TestInFlightActivity_FastFailNotBusy(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	hostname := "zcpifl" + randomSuffix()
	// Trailing .git => Zerops buildFromGit clone-preflight FAILS in <1s.
	serviceID := importInflightProbe(t, h, ctx, hostname, inflightRecipeRepo+".git")
	idToHost := map[string]string{serviceID: hostname}

	var sawFailedBuild bool
	for i := 0; i < 30 && !sawFailedBuild; i++ {
		procs, err := h.client.GetProjectProcessesDirect(ctx, h.projectID)
		if err != nil {
			t.Fatalf("GetProjectProcessesDirect: %v", err)
		}
		for _, p := range procs {
			if p.ActionName != "stack.build" {
				continue
			}
			for _, ref := range p.ServiceStacks {
				if ref.ID == serviceID && p.Status == platform.ProcessStatusFailed {
					sawFailedBuild = true
				}
			}
		}
		if sawFailedBuild {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if !sawFailedBuild {
		t.Fatal("expected a FAILED stack.build process for the .git-suffix fast-fail")
	}

	// The crux: a FAILED build (appVersion frozen at WAITING_TO_BUILD) is NOT
	// busy — recovery stays open.
	act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost)
	if err != nil {
		t.Fatalf("ProjectActivity: %v", err)
	}
	if live := act[hostname]; len(live) > 0 {
		t.Errorf("a FAILED build must NOT be busy (recovery would be gated); got %+v", live)
	}
}

// buildOrDeployOp returns the first build/deploy op in a service's live-op list
// (the readiness-gating operation, distinct from a transient stack.create).
func buildOrDeployOp(live []ops.LiveOp) (ops.LiveOp, bool) {
	for _, o := range live {
		if o.Action == "build" || o.Action == "deploy" {
			return o, true
		}
	}
	return ops.LiveOp{}, false
}

// TestInFlightWait_DrainsServiceToSettled provisions a real buildFromGit runtime
// WITH subdomain access (so a build AND a queued stack.enableSubdomainAccess run
// at once — the live-verified multi-op shape), then proves ops.WaitServiceSettled
// blocks until the service has NO live process: it drains the build and the
// queued subdomain toggle, reports the build FINISHED, and the service reads
// ACTIVE with empty activity afterward.
func TestInFlightWait_DrainsServiceToSettled(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	hostname := "zcpifw" + randomSuffix()
	serviceID := importInflightProbeYAML(t, h, ctx, hostname,
		inflightImportYAMLOpts(hostname, inflightRecipeRepo, true))
	idToHost := map[string]string{serviceID: hostname}

	// Confirm the multi-op shape exists at least once: a build live while a
	// subdomain-enable is queued. Best-effort (the build can outrun the poll) —
	// the wait below is the real assertion.
	for i := 0; i < 20; i++ {
		act, _ := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost)
		if len(act[hostname]) >= 2 {
			t.Logf("multi-op observed: %+v", act[hostname])
			break
		}
		time.Sleep(1 * time.Second)
	}

	res, err := ops.WaitServiceSettled(ctx, h.client, h.projectID, hostname, nil)
	if err != nil {
		t.Fatalf("WaitServiceSettled: %v", err)
	}
	if !res.Settled || res.TimedOut {
		t.Fatalf("wait must settle a real build; got %+v", res)
	}
	var sawBuildFinished bool
	for _, p := range res.Processes {
		t.Logf("waited: action=%s status=%s id=%s", p.Action, p.Status, p.ProcessID)
		if p.Action == "stack.build" && p.Status == platform.ProcessStatusFinished {
			sawBuildFinished = true
		}
	}
	if !sawBuildFinished {
		t.Errorf("expected a FINISHED stack.build among the waited processes; got %+v", res.Processes)
	}

	// After settle: no live process, service ACTIVE.
	act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost)
	if err != nil {
		t.Fatalf("ProjectActivity post-settle: %v", err)
	}
	if len(act[hostname]) != 0 {
		t.Errorf("activity must be empty after settle; got %+v", act[hostname])
	}
	svc, _ := h.client.GetService(ctx, serviceID)
	if svc == nil || svc.Status != platform.ServiceStatusActive {
		t.Errorf("service must be ACTIVE after settle; got %+v", svc)
	}
}
