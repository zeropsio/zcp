//go:build e2e

// Tests for: SDK autoscaling workaround — verifies GetService returns
// non-empty autoscaling data despite zerops-go SDK v1.0.16 JSON tag mismatch.
//
// Run: go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_ScalingData -timeout 60s

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestE2E_ScalingData(t *testing.T) {
	creds, err := auth.ResolveCredentials()
	if err != nil {
		t.Skipf("no credentials available: %v", err)
	}

	client, err := platform.NewZeropsClient(creds.Token, creds.APIHost)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authInfo, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}

	services, err := client.ListServices(ctx, authInfo.ProjectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}

	// Find a runtime service (not shared-storage/object-storage) to test scaling.
	tests := []struct {
		name     string
		hostname string
		id       string
	}{}
	for _, svc := range services {
		cat := svc.ServiceStackTypeInfo.ServiceStackTypeCategoryName
		if cat == "USER" {
			tests = append(tests, struct {
				name     string
				hostname string
				id       string
			}{
				name:     svc.Name + "_" + svc.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				hostname: svc.Name,
				id:       svc.ID,
			})
		}
	}
	if len(tests) == 0 {
		t.Skip("no USER-category services found in project")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := client.GetService(ctx, tt.id)
			if err != nil {
				t.Fatalf("GetService(%s): %v", tt.hostname, err)
			}

			if svc.CurrentAutoscaling == nil {
				t.Fatalf("CurrentAutoscaling is nil for %s — SDK workaround not applied", tt.hostname)
			}

			a := svc.CurrentAutoscaling
			if a.CPUMode == "" {
				t.Errorf("CPUMode is empty for %s", tt.hostname)
			}
			if a.MinCPU == 0 {
				t.Errorf("MinCPU is 0 for %s", tt.hostname)
			}
			if a.MaxCPU == 0 {
				t.Errorf("MaxCPU is 0 for %s", tt.hostname)
			}
			if a.MinRAM == 0 {
				t.Errorf("MinRAM is 0 for %s", tt.hostname)
			}
			if a.MaxRAM == 0 {
				t.Errorf("MaxRAM is 0 for %s", tt.hostname)
			}

			t.Logf("%s: cpuMode=%s cpu=%d-%d ram=%.1f-%.1f disk=%.0f-%.0f containers=%d-%d",
				tt.hostname, a.CPUMode, a.MinCPU, a.MaxCPU, a.MinRAM, a.MaxRAM,
				a.MinDisk, a.MaxDisk, a.HorizontalMinCount, a.HorizontalMaxCount)
		})
	}
}
