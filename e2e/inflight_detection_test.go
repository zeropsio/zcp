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
	return fmt.Sprintf(`services:
  - hostname: %s
    type: nodejs@22
    enableSubdomainAccess: false
    minContainers: 1
    maxContainers: 1
    buildFromGit: %s
    zeropsSetup: helloworld
`, hostname, repo)
}

// importInflightProbe provisions a buildFromGit runtime and returns its service
// ID. Registers cleanup.
func importInflightProbe(t *testing.T, h *e2eHarness, ctx context.Context, hostname, repo string) string {
	t.Helper()
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cleanupServices(cctx, h.client, h.projectID, hostname)
	})
	res, err := h.client.ImportServices(ctx, h.projectID, inflightImportYAML(hostname, repo))
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

	// 1) BUSY during the build, while the service reads READY_TO_DEPLOY.
	var busy ops.ServiceActivity
	var sawBusy, sawReadyWhileBusy bool
	for i := 0; i < 30 && !sawBusy; i++ {
		act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost, 100)
		if err != nil {
			t.Fatalf("ProjectActivity: %v", err)
		}
		if a, ok := act[hostname]; ok {
			busy, sawBusy = a, true
			svc, _ := h.client.GetService(ctx, serviceID)
			if svc != nil && svc.Status == platform.ServiceStatusReadyToDeploy {
				sawReadyWhileBusy = true
			}
			t.Logf("busy: action=%s status=%s processId=%s svcStatus=%v", a.Action, a.Status, a.ProcessID, svc.Status)
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

	// 2) ID cross-reference: the live stack.build process references the target
	// serviceID, and any OTHER ref (the ephemeral build container) is a distinct
	// id — so target-id matching is unambiguous.
	procs, err := h.client.SearchProcesses(ctx, h.projectID, 100)
	if err != nil {
		t.Fatalf("SearchProcesses: %v", err)
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
		act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost, 100)
		if err != nil {
			t.Fatalf("ProjectActivity (settle): %v", err)
		}
		svc, _ := h.client.GetService(ctx, serviceID)
		if _, stillBusy := act[hostname]; !stillBusy && svc != nil && svc.Status == platform.ServiceStatusActive {
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
		procs, err := h.client.SearchProcesses(ctx, h.projectID, 100)
		if err != nil {
			t.Fatalf("SearchProcesses: %v", err)
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
	act, err := ops.ProjectActivity(ctx, h.client, h.projectID, idToHost, 100)
	if err != nil {
		t.Fatalf("ProjectActivity: %v", err)
	}
	if a, busy := act[hostname]; busy {
		t.Errorf("a FAILED build must NOT be busy (recovery would be gated); got %+v", a)
	}
}
