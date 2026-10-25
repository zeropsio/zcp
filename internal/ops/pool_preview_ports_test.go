// Tests for: ops/pool_preview_ports.go — a raise of a dev pool's
// maxContainers past the preview ports the pool opens is refused before
// anything is sent, and a full pool's offer to raise the maximum says it is
// the person's call and that every new container needs its preview port.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// previewPoolClient is a project holding testPoolHost at maximum curMax,
// declaring ports.
func previewPoolClient(curMax int32, ports []platform.Port) *scaleCaptureClient {
	return &scaleCaptureClient{Mock: platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj"}).
		WithServices([]platform.ServiceStack{{
			ID: "svc-pool", Name: testPoolHost, ProjectID: "p1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"},
			CustomAutoscaling:    &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: curMax},
			Ports:                ports,
		}}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: testPoolToken, Sensitive: true}})}
}

// httpPorts are the ports from..to, each declared with httpSupport (the
// platform reports such a port with scheme http, live 2026-10-25).
func httpPorts(from, to int) []platform.Port {
	out := make([]platform.Port, 0, to-from+1)
	for p := from; p <= to; p++ {
		out = append(out, platform.Port{Port: p, Protocol: "tcp", Scheme: "http"})
	}
	return out
}

// TestScale_PoolRaise_PastThePreviewPorts_Refused: a lease of a dev pool
// answers its preview on previewPortBase + its container's stable number (1 …
// maxContainers), and a per-port subdomain routes only to a port the pool
// declares with httpSupport. So raising the maximum of a pool zcp leases from
// past the block of HTTP ports it declares would give the new containers a
// preview that answers 502 (Kara's run, 2026-10-25: hqpool declared 3001-3003,
// was raised to 4, and container 4's preview on 3004 answered 502). Such a
// raise is refused before anything is sent, naming the ports to declare, the
// deploy that opens them, and that a raise is the person's call. A raise
// within the ports, a lowering, a pool zcp leases nothing from and ports zcp
// cannot read as one block all proceed.
func TestScale_PoolRaise_PastThePreviewPorts_Refused(t *testing.T) {
	t.Parallel()
	twoBlocks := append(httpPorts(3001, 3002), httpPorts(4001, 4002)...)
	plainLast := append(httpPorts(3001, 3002), platform.Port{Port: 3003, Protocol: "tcp", Scheme: "tcp"})

	tests := []struct {
		name           string
		curMax, newMax int32
		ports          []platform.Port
		noRecord       bool
		wantRefused    bool
		wantMessage    []string
		wantSuggest    []string
		banned         []string
	}{
		{
			name: "one container past the ports: refused, naming the port", curMax: 3, newMax: 4, ports: httpPorts(3001, 3003),
			wantRefused: true,
			wantMessage: []string{"Pool 'devpool'", "3001", "3003", "container 4", "3004", "502"},
			wantSuggest: []string{"ask the person", "infrastructure change", "run.ports", "3004", "httpSupport: true",
				`zerops_deploy`, `targetService="devpool"`, `zerops_scale serviceHostname="devpool" maxContainers=4`},
		},
		{
			name: "several containers past the ports: every missing port is named", curMax: 3, newMax: 5, ports: httpPorts(3001, 3003),
			wantRefused: true,
			wantMessage: []string{"3004", "3005"},
			wantSuggest: []string{"3004", "3005"},
		},
		{
			name: "a port declared without httpSupport opens no preview", curMax: 2, newMax: 3, ports: plainLast,
			wantRefused: true,
			wantMessage: []string{"3001", "3002", "3003"},
			wantSuggest: []string{"3003", "httpSupport: true"},
		},
		{
			name: "a raise within the declared ports proceeds", curMax: 3, newMax: 5, ports: httpPorts(3001, 3005),
		},
		{
			name: "a lowering never checks the ports", curMax: 4, newMax: 3, ports: httpPorts(3001, 3003),
		},
		{
			name: "the same maximum again never checks the ports", curMax: 4, newMax: 4, ports: httpPorts(3001, 3003),
		},
		{
			name: "a pool zcp leases nothing from is not one zcp knows serves previews", curMax: 3, newMax: 4, ports: httpPorts(3001, 3003),
			noRecord: true,
		},
		{
			name: "two blocks of the same length: zcp cannot tell the preview ports", curMax: 2, newMax: 3, ports: twoBlocks,
		},
		{
			name: "a pool declaring no port has no preview to break", curMax: 3, newMax: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := previewPoolClient(tt.curMax, tt.ports)
			stateDir := t.TempDir()
			if !tt.noRecord {
				recordLease(t, stateDir, testPoolHost, "svc-pool", 1, "feature")
			}
			swarm := &fakeSwarm{containers: containersAt(leasedAt(1), 1, 2, 3, 4)}
			deps := LeaseDeps{Client: client, ProjectID: "p1", StateDir: stateDir,
				Swarm: func(string, string) SwarmAPI { return swarm },
				Env:   func(string) (string, bool) { return "", false }}
			guard := WithPoolShrinkGuard(deps, func(PoolShrink) error { return nil })

			_, err := Scale(context.Background(), client, "p1", testPoolHost, ScaleParams{MaxContainers: scaleIntPtr(int(tt.newMax))}, guard)
			if !tt.wantRefused {
				if err != nil {
					t.Fatalf("Scale: %v", err)
				}
				if client.sent == nil {
					t.Fatal("nothing was sent to the platform")
				}
				return
			}
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal (*PlatformError)", err)
			}
			if pe.Code != platform.ErrPrerequisiteMissing {
				t.Errorf("code = %s, want %s", pe.Code, platform.ErrPrerequisiteMissing)
			}
			if client.sent != nil {
				t.Errorf("a refused raise was sent to the platform: %+v", client.sent)
			}
			for _, want := range tt.wantMessage {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			for _, want := range tt.wantSuggest {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			for _, banned := range tt.banned {
				if strings.Contains(pe.Message+" "+pe.Suggestion, banned) {
					t.Errorf("refusal carries %q", banned)
				}
			}
		})
	}
}

// TestLeaseAcquire_FullPool_RaiseOfferIsThePersonsCallAndNeedsPreviewPorts: a
// full pool's refusal offers raising its maxContainers while it is below the
// ceiling; that offer says a raise is an infrastructure change to agree with
// the person first, and that each new container needs its preview port in
// the pool's run.ports (Kara's run, 2026-10-25: a helper raised a full pool
// unasked, past its preview ports).
func TestLeaseAcquire_FullPool_RaiseOfferIsThePersonsCallAndNeedsPreviewPorts(t *testing.T) {
	t.Parallel()
	for _, limitsErr := range []error{nil, errors.New("limits: i/o timeout")} {
		fx := newLeaseFixture(t)
		recordLease(t, fx.stateDir, testPoolHost, "svc-pool", 1, "b1")
		fx.swarm.containers = reservedRange(3)
		fx.swarm.limits = platform.SwarmPool{MaximumContainerCount: 3}
		fx.swarm.limitsErr = limitsErr
		fx.swarm.acquireErr = allReserved(causeAtMaximum)

		_, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, "feature")
		var pe *platform.PlatformError
		if !errors.As(err, &pe) {
			t.Fatalf("err = %v, want a *PlatformError", err)
		}
		for _, want := range []string{"zerops_scale", "ask the person", "infrastructure change", "preview port", "run.ports"} {
			if !strings.Contains(pe.Suggestion, want) {
				t.Errorf("limits err %v: suggestion %q lacks %q", limitsErr, pe.Suggestion, want)
			}
		}
	}
}
