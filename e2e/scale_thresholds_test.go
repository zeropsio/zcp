//go:build e2e

// Tests for: e2e — verify MinFreeResource scaling threshold params against real Zerops API.
//
// Uses existing services in the project (no import needed).
// Tests SetAutoscaling with MinFreeResource fields, then verifies via GetService.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_ScaleThresholds -timeout 120s

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestE2E_ScaleThresholds(t *testing.T) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		t.Skipf("no credentials available: %v", err)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}

	// Find a USER-category service to test with.
	services, err := client.ListServices(ctx, authInfo.ProjectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}

	var testSvc *platform.ServiceStack
	for i := range services {
		if services[i].ServiceStackTypeInfo.ServiceStackTypeCategoryName == "USER" {
			testSvc = &services[i]
			break
		}
	}
	if testSvc == nil {
		t.Skip("no USER-category service found")
	}

	// Get full service detail to capture current autoscaling (for restore).
	fullSvc, err := client.GetService(ctx, testSvc.ID)
	if err != nil {
		t.Fatalf("GetService: %v", err)
	}

	t.Logf("Test service: %s (type=%s, mode=%s, id=%s)",
		testSvc.Name,
		testSvc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		fullSvc.Mode,
		testSvc.ID)

	// --- Sub-test 1: Read current MinFreeResource defaults ---
	t.Run("ReadDefaults", func(t *testing.T) {
		if fullSvc.CurrentAutoscaling == nil {
			t.Fatal("CurrentAutoscaling is nil")
		}
		a := fullSvc.CurrentAutoscaling
		t.Logf("CurrentAutoscaling: cpu=%d-%d ram=%.3f-%.1f disk=%.0f-%.0f",
			a.MinCPU, a.MaxCPU, a.MinRAM, a.MaxRAM, a.MinDisk, a.MaxDisk)
		t.Logf("Thresholds: minFreeRamGB=%.4f minFreeRamPercent=%.1f minFreeCpuCores=%.4f minFreeCpuPercent=%.1f swapEnabled=%v",
			a.MinFreeRAMGB, a.MinFreeRAMPercent, a.MinFreeCPUCores, a.MinFreeCPUPercent, a.SwapEnabled)
	})

	// --- Sub-test 2: Set MinFreeResource RAM thresholds ---
	t.Run("SetRAMThresholds", func(t *testing.T) {
		ramGB := 0.25
		ramPct := 15.0
		params := platform.AutoscalingParams{
			ServiceMode:           fullSvc.Mode,
			VerticalMinFreeRAMGB:  &ramGB,
			VerticalMinFreeRAMPct: &ramPct,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			t.Fatalf("SetAutoscaling RAM thresholds: %v", err)
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}

		// Verify via GetService.
		updated, err := client.GetService(ctx, testSvc.ID)
		if err != nil {
			t.Fatalf("GetService after RAM threshold: %v", err)
		}

		// Check CustomAutoscaling (user overrides).
		if updated.CustomAutoscaling == nil {
			t.Fatal("CustomAutoscaling is nil after setting thresholds")
		}
		t.Logf("Custom after RAM set: minFreeRamGB=%.4f minFreeRamPercent=%.1f",
			updated.CustomAutoscaling.MinFreeRAMGB, updated.CustomAutoscaling.MinFreeRAMPercent)

		// Check CurrentAutoscaling (effective values).
		if updated.CurrentAutoscaling == nil {
			t.Fatal("CurrentAutoscaling is nil after setting thresholds")
		}
		ca := updated.CurrentAutoscaling
		t.Logf("Current after RAM set: minFreeRamGB=%.4f minFreeRamPercent=%.1f",
			ca.MinFreeRAMGB, ca.MinFreeRAMPercent)

		if ca.MinFreeRAMGB < 0.24 || ca.MinFreeRAMGB > 0.26 {
			t.Errorf("MinFreeRAMGB = %.4f, want ~0.25", ca.MinFreeRAMGB)
		}
		if ca.MinFreeRAMPercent < 14.9 || ca.MinFreeRAMPercent > 15.1 {
			t.Errorf("MinFreeRAMPercent = %.1f, want ~15.0", ca.MinFreeRAMPercent)
		}
	})

	// --- Sub-test 3: Set MinFreeResource CPU thresholds (DEDICATED mode) ---
	t.Run("SetCPUThresholds", func(t *testing.T) {
		// Switch to DEDICATED first (CPU thresholds only work in DEDICATED).
		dedicated := "DEDICATED"
		cpuCores := 0.2
		cpuPct := 10.0
		params := platform.AutoscalingParams{
			ServiceMode:            fullSvc.Mode,
			VerticalCPUMode:        &dedicated,
			VerticalMinFreeCPUCores: &cpuCores,
			VerticalMinFreeCPUPct:   &cpuPct,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			t.Fatalf("SetAutoscaling CPU thresholds: %v", err)
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}

		updated, err := client.GetService(ctx, testSvc.ID)
		if err != nil {
			t.Fatalf("GetService after CPU threshold: %v", err)
		}
		ca := updated.CurrentAutoscaling
		if ca == nil {
			t.Fatal("CurrentAutoscaling is nil")
		}
		t.Logf("Current after CPU set: minFreeCpuCores=%.4f minFreeCpuPercent=%.1f cpuMode=%s",
			ca.MinFreeCPUCores, ca.MinFreeCPUPercent, ca.CPUMode)

		if ca.MinFreeCPUCores < 0.19 || ca.MinFreeCPUCores > 0.21 {
			t.Errorf("MinFreeCPUCores = %.4f, want ~0.2", ca.MinFreeCPUCores)
		}
		if ca.MinFreeCPUPercent < 9.9 || ca.MinFreeCPUPercent > 10.1 {
			t.Errorf("MinFreeCPUPercent = %.1f, want ~10.0", ca.MinFreeCPUPercent)
		}
	})

	// --- Sub-test 4: Set CPU thresholds in SHARED mode (expect error or silent ignore) ---
	t.Run("CPUThresholdsInSharedMode", func(t *testing.T) {
		shared := "SHARED"
		cpuCores := 0.3
		params := platform.AutoscalingParams{
			ServiceMode:            fullSvc.Mode,
			VerticalCPUMode:        &shared,
			VerticalMinFreeCPUCores: &cpuCores,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			// API rejects — good, document the error.
			t.Logf("SHARED + CPU threshold: API rejected with: %v", err)
			return
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}

		// API accepted — check if value was stored or silently ignored.
		updated, err := client.GetService(ctx, testSvc.ID)
		if err != nil {
			t.Fatalf("GetService: %v", err)
		}
		ca := updated.CurrentAutoscaling
		if ca == nil {
			t.Fatal("CurrentAutoscaling is nil")
		}
		t.Logf("SHARED + CPU threshold: API accepted. cpuMode=%s minFreeCpuCores=%.4f (was it stored or ignored?)",
			ca.CPUMode, ca.MinFreeCPUCores)
	})

	// --- Sub-test 5: Both RAM thresholds at 0 (dangerous but should be allowed) ---
	t.Run("BothRAMThresholdsZero", func(t *testing.T) {
		ramGB := 0.0
		ramPct := 0.0
		params := platform.AutoscalingParams{
			ServiceMode:           fullSvc.Mode,
			VerticalMinFreeRAMGB:  &ramGB,
			VerticalMinFreeRAMPct: &ramPct,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			t.Logf("Both RAM thresholds=0: API rejected with: %v", err)
			return
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}
		t.Log("Both RAM thresholds=0: API accepted (no OOM protection)")

		updated, err := client.GetService(ctx, testSvc.ID)
		if err != nil {
			t.Fatalf("GetService: %v", err)
		}
		ca := updated.CurrentAutoscaling
		t.Logf("After zero thresholds: minFreeRamGB=%.4f minFreeRamPercent=%.1f",
			ca.MinFreeRAMGB, ca.MinFreeRAMPercent)
	})

	// --- Sub-test 6: All thresholds in one call ---
	t.Run("AllThresholdsOneCall", func(t *testing.T) {
		dedicated := "DEDICATED"
		ramGB := 0.5
		ramPct := 20.0
		cpuCores := 0.15
		cpuPct := 5.0
		params := platform.AutoscalingParams{
			ServiceMode:            fullSvc.Mode,
			VerticalCPUMode:        &dedicated,
			VerticalMinFreeRAMGB:   &ramGB,
			VerticalMinFreeRAMPct:  &ramPct,
			VerticalMinFreeCPUCores: &cpuCores,
			VerticalMinFreeCPUPct:   &cpuPct,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			t.Fatalf("SetAutoscaling all thresholds: %v", err)
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}

		updated, err := client.GetService(ctx, testSvc.ID)
		if err != nil {
			t.Fatalf("GetService: %v", err)
		}
		ca := updated.CurrentAutoscaling
		if ca == nil {
			t.Fatal("CurrentAutoscaling is nil")
		}
		t.Logf("All thresholds: ramGB=%.4f ramPct=%.1f cpuCores=%.4f cpuPct=%.1f cpuMode=%s",
			ca.MinFreeRAMGB, ca.MinFreeRAMPercent, ca.MinFreeCPUCores, ca.MinFreeCPUPercent, ca.CPUMode)
	})

	// --- Restore: reset threshold defaults (skip cpuMode — hourly rate limit) ---
	t.Run("Restore", func(t *testing.T) {
		ramGB := 0.0625
		ramPct := 0.0
		cpuCores := 0.1
		cpuPct := 0.0
		params := platform.AutoscalingParams{
			ServiceMode:             fullSvc.Mode,
			VerticalMinFreeRAMGB:    &ramGB,
			VerticalMinFreeRAMPct:   &ramPct,
			VerticalMinFreeCPUCores: &cpuCores,
			VerticalMinFreeCPUPct:   &cpuPct,
		}
		proc, err := client.SetAutoscaling(ctx, testSvc.ID, params)
		if err != nil {
			// cpuMode hourly rate limit may block the entire call after a mode change.
			t.Logf("restore skipped (likely cpuMode cooldown): %v", err)
			return
		}
		if proc != nil {
			waitForProcessDirect(ctx, client, proc.ID)
		}
		t.Log("Restored threshold defaults")
	})
}
