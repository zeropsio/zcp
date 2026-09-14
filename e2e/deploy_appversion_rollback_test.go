//go:build e2e

// Tests for: e2e — GF-8 rollback (docs/spec-workflows.md §8 R2 generalised,
// §12.6 GF-8) against a REAL buildFromGit service: ops.ReactivateAppVersion
// re-activates a recorded BACKUP appVersion in place, no rebuild.
//
// Mirrors the platform-verifier's live spec (2026-09-14):
//  1. import buildFromGit svc → first build activates (appVersion A, ACTIVE)
//  2. second build (self-deploy zcli push, an observable marker) → A flips
//     to BACKUP, the new build (B) activates
//  3. ReactivateAppVersion(A) → process actionName stack.deploy.backup, no
//     stack.build against this service since T0, active==A, B==BACKUP,
//     subdomain body == A's original content (never B's marker)
//  4. RedeployAppVersion(active id) errors appVersionInvalidStatus
//
// NOT LIVE-VERIFIED in the authoring session (brief C-rollback stop
// condition, docs/spec-eval-farm.md §4.5): needs one live pass in ASSEMBLE
// before this file is trusted (in particular the self-deploy-via-SSH
// second-build step and subdomain propagation timing).
//
// Run: ZCP_API_KEY=<eval-token> go test ./e2e/ -tags e2e -run TestE2E_DeployAppVersionRollback -v -timeout 900s
package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const rollbackRecipeRepo = "https://github.com/zeropsio/recipe-nodejs-hello-world"

func TestE2E_DeployAppVersionRollback(t *testing.T) {
	requireSSH(t, "zcp")
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	hostname := "zcprbk" + randomSuffix()
	serviceID := importInflightProbeYAML(t, h, ctx, hostname, inflightImportYAMLOpts(hostname, rollbackRecipeRepo, true))

	// --- Step 1: first (GIT) build activates as appVersion A. ---
	logStep(t, 1, "wait for %s's first buildFromGit build to activate", hostname)
	if err := waitForServiceActive(ctx, h.client, h.projectID, hostname, 5*time.Minute); err != nil {
		t.Fatalf("first build never activated: %v", err)
	}
	svc, err := h.client.GetService(ctx, serviceID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID == "" {
		t.Fatalf("service %s has no ActiveAppVersion after first build: %+v", hostname, svc)
	}
	appVersionA := svc.ActiveAppVersion.ID
	t.Logf("appVersion A = %s", appVersionA)

	subdomainURL := resolveSubdomainURL(t, ctx, h, svc)
	markerBodyA := mustHTTPBody(t, subdomainURL, 90*time.Second)

	// --- Step 2: second build with an observable marker — self-deploy
	// (zcli push from inside the container), which flips A to BACKUP and
	// activates a new appVersion B. ---
	logStep(t, 2, "second build on %s with an observable marker (self-deploy)", hostname)
	t0 := time.Now()
	if out, sshErr := sshExec(t, hostname, "cd /var/www && echo '<!-- e2e-rollback-marker-B -->' >> index.html"); sshErr != nil {
		t.Fatalf("write marker B: %s (%v)", out, sshErr)
	}
	if out, sshErr := sshExec(t, hostname, "cd /var/www && zcli push --workingDir /var/www 2>&1"); sshErr != nil {
		t.Fatalf("zcli push (second build): %s (%v)", out, sshErr)
	}
	if err := waitForServiceActive(ctx, h.client, h.projectID, hostname, 5*time.Minute); err != nil {
		t.Fatalf("second build never activated: %v", err)
	}

	svc, err = h.client.GetService(ctx, serviceID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID == "" || svc.ActiveAppVersion.ID == appVersionA {
		t.Fatalf("no new ACTIVE appVersion after the second build (still %+v)", svc.ActiveAppVersion)
	}
	appVersionB := svc.ActiveAppVersion.ID
	t.Logf("appVersion B = %s", appVersionB)

	if status := appVersionStatus(t, ctx, h.client, serviceID, appVersionA); status != platform.BuildStatusBackup {
		t.Fatalf("appVersion A status after second build = %s, want %s", status, platform.BuildStatusBackup)
	}

	// --- Step 3: roll back to A. ---
	logStep(t, 3, "ReactivateAppVersion(%s) — roll %s back to appVersion A", appVersionA, hostname)
	result, err := ops.ReactivateAppVersion(ctx, h.client, h.projectID, hostname, appVersionA)
	if err != nil {
		t.Fatalf("ReactivateAppVersion(A): %v", err)
	}
	if result.Status != platform.BuildStatusDeployed {
		t.Fatalf("rollback result.Status = %s, want %s", result.Status, platform.BuildStatusDeployed)
	}

	// No stack.build process against this service created after T0 — a
	// rollback is never a rebuild.
	procs, err := h.client.GetProjectProcessesDirect(ctx, h.projectID)
	if err != nil {
		t.Fatalf("GetProjectProcessesDirect: %v", err)
	}
	for _, p := range procs {
		if p.ActionName != "stack.build" || !processTargets(p, serviceID) {
			continue
		}
		if created, perr := time.Parse(time.RFC3339Nano, p.Created); perr == nil && created.After(t0) {
			t.Errorf("found a stack.build process against %s created after rollback started: %+v", hostname, p)
		}
	}

	// active==A, B==BACKUP.
	svc, err = h.client.GetService(ctx, serviceID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}
	if svc.ActiveAppVersion == nil || svc.ActiveAppVersion.ID != appVersionA {
		t.Errorf("active appVersion after rollback = %v, want %s", svc.ActiveAppVersion, appVersionA)
	}
	if status := appVersionStatus(t, ctx, h.client, serviceID, appVersionB); status != platform.BuildStatusBackup {
		t.Errorf("appVersion B status after rollback = %s, want %s", status, platform.BuildStatusBackup)
	}

	// subdomain body == A's original content, never B's marker.
	bodyAfterRollback := mustHTTPBody(t, subdomainURL, 90*time.Second)
	if bodyAfterRollback != markerBodyA {
		t.Errorf("body after rollback (%d bytes) != marker A body (%d bytes) — the re-activated artifact isn't what's being served", len(bodyAfterRollback), len(markerBodyA))
	}
	if strings.Contains(bodyAfterRollback, "e2e-rollback-marker-B") {
		t.Error("body after rollback still carries marker B")
	}

	// --- Step 4: RedeployAppVersion against the now-ACTIVE id 400s. ---
	logStep(t, 4, "RedeployAppVersion(%s) — now ACTIVE, expect appVersionInvalidStatus", appVersionA)
	if _, err := h.client.RedeployAppVersion(ctx, appVersionA, "", ""); err == nil {
		t.Error("RedeployAppVersion against the now-ACTIVE id: expected an error, got nil")
	} else if !strings.Contains(err.Error(), "appVersionInvalidStatus") {
		t.Errorf("err = %v, want it to name appVersionInvalidStatus", err)
	}

	// --- Teardown ---
	cleanupServices(ctx, h.client, h.projectID, hostname)
}

// resolveSubdomainURL reconstructs the subdomain URL the same way
// ops.attachSubdomainUrlsToResult / prod_subdomain_diag_test.go do: project
// SubdomainHost prefix + apiHost-derived region domain + the service's
// preferred HTTP port. No env values (P-LP-5 shape), so it works before any
// zerops_subdomain call has run against this service.
func resolveSubdomainURL(t *testing.T, ctx context.Context, h *e2eHarness, svc *platform.ServiceStack) string {
	t.Helper()
	proj, err := h.client.GetProject(ctx, h.projectID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	port, ok := ops.PreferredHTTPPort(svc.Ports)
	if !ok {
		t.Fatalf("no HTTP port on %s: %+v", svc.Name, svc.Ports)
	}
	domain := ops.SubdomainDomainFromAPIHost(h.authInfo.APIHost)
	url := ops.BuildSubdomainURL(svc.Name, proj.SubdomainHost+"."+domain, port.Port)
	if url == "" {
		t.Fatalf("BuildSubdomainURL produced an empty URL for %s (subdomainHost=%q domain=%q port=%d)", svc.Name, proj.SubdomainHost, domain, port.Port)
	}
	return url
}

// mustHTTPBody GETs url, retrying until the deadline (the subdomain route
// takes up to ~1.3s to propagate after activation — CLAUDE.md's L7
// propagation note — and a rollback's "UPGRADING" window needs its own
// settle time), and returns the response body as a string.
func mustHTTPBody(t *testing.T, url string, deadline time.Duration) string {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	end := time.Now().Add(deadline)
	var lastErr error
	for time.Now().Before(end) {
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			time.Sleep(3 * time.Second)
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			time.Sleep(3 * time.Second)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			return string(body)
		}
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("GET %s never returned 200 within %s: %v", url, deadline, lastErr)
	return ""
}

// appVersionStatus reads the live status of a specific appVersion via the
// DIRECT (non-ES) app-version list — never SearchAppVersions, which lags.
func appVersionStatus(t *testing.T, ctx context.Context, client platform.Client, serviceID, appVersionID string) string {
	t.Helper()
	versions, err := client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		t.Fatalf("ListServiceAppVersions: %v", err)
	}
	for _, v := range versions {
		if v.ID == appVersionID {
			return v.Status
		}
	}
	t.Fatalf("appVersion %s not found in %s's history", appVersionID, serviceID)
	return ""
}

// processTargets reports whether p references serviceID (docs/spec-
// workflows.md §3.5's ProcessTargets shape).
func processTargets(p platform.Process, serviceID string) bool {
	for _, ref := range p.ServiceStacks {
		if ref.ID == serviceID {
			return true
		}
	}
	return false
}
