// Tests for: ops/pool_preview_ports.go — a raise of a dev pool's
// maxContainers past the preview ports the pool opens is refused before
// anything is sent; a scale that does not raise it on a pool already past
// them runs and says so; and a full pool's offer to raise the maximum says it
// is the person's call and that every new container needs its preview port.
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

// TestScale_PoolPastThePreviewPorts_NonRaiseScalesAndSaysSo: a pool can be
// past its preview ports already — raised before the raise was checked, or
// outside zcp (Kara's hqpool: maxContainers 4 over ports 3001-3003, and a
// lease in container 4 had no preview with nothing saying why). A scale of it
// that does not raise the maximum — the same maximum again, a lowering that
// stays past the ports, a scale that leaves the maximum alone — is no raise
// to refuse: it is sent, and its notice leads with how many containers
// against how many preview ports, which containers have none, and both
// fixes: declare the ports in the pool setup's run.ports and deploy the pool,
// or keep maxContainers at the port count. A maximum within the ports, a pool
// zcp leases nothing from and ports zcp cannot read as one block say nothing.
func TestScale_PoolPastThePreviewPorts_NonRaiseScalesAndSaysSo(t *testing.T) {
	t.Parallel()
	twoBlocks := append(httpPorts(3001, 3002), httpPorts(4001, 4002)...)
	plainLast := append(httpPorts(3001, 3002), platform.Port{Port: 3003, Protocol: "tcp", Scheme: "tcp"})

	tests := []struct {
		name     string
		curMax   int32
		params   ScaleParams
		ports    []platform.Port
		noRecord bool
		want     []string // empty: the notice says nothing about preview ports
	}{
		{
			name: "the same maximum again, one container past the ports: named, with both fixes", curMax: 4, params: ScaleParams{MaxContainers: scaleIntPtr(4)}, ports: httpPorts(3001, 3003),
			want: []string{"Pool 'devpool'", "maxContainers 4", "3 preview ports (3001-3003)", "Container 4 has no preview port",
				"does not open 3004", "a lease landing there has no preview", "run.ports", "`- port: 3004` with `httpSupport: true`",
				`zerops_deploy`, `targetService="devpool"`, "keep maxContainers at 3 or below", `zerops_scale serviceHostname="devpool" maxContainers=3`},
		},
		{
			name: "the same maximum again, several containers past the ports: every one and its port is named", curMax: 5, params: ScaleParams{MaxContainers: scaleIntPtr(5)}, ports: httpPorts(3001, 3003),
			want: []string{"maxContainers 5", "Containers 4 and 5 have no preview port", "does not open 3004 or 3005", "a lease landing in one of them",
				"declare ports 3004 and 3005", "each as `- port: <n>` with `httpSupport: true`", "keep maxContainers at 3 or below"},
		},
		{
			name: "a lowering that stays past the ports says so", curMax: 5, params: ScaleParams{MaxContainers: scaleIntPtr(4)}, ports: httpPorts(3001, 3003),
			want: []string{"maxContainers 4", "Container 4 has no preview port"},
		},
		{
			name: "a scale that leaves the maximum alone says so", curMax: 4, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: httpPorts(3001, 3003),
			want: []string{"maxContainers 4", "Container 4 has no preview port"},
		},
		{
			name: "a port declared without httpSupport opens no preview", curMax: 3, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: plainLast,
			want: []string{"2 preview ports (3001-3002)", "Container 3", "3003", "keep maxContainers at 2 or below"},
		},
		{
			name: "a single preview port", curMax: 2, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: httpPorts(3001, 3001),
			want: []string{"only 1 preview port (3001)", "Container 2", "3002", "keep maxContainers at 1 or below"},
		},
		{
			name: "a lowering to the ports says nothing", curMax: 4, params: ScaleParams{MaxContainers: scaleIntPtr(3)}, ports: httpPorts(3001, 3003),
		},
		{
			name: "a pool within its ports says nothing", curMax: 3, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: httpPorts(3001, 3003),
		},
		{
			name: "a pool zcp leases nothing from is not one zcp knows serves previews", curMax: 4, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: httpPorts(3001, 3003),
			noRecord: true,
		},
		{
			name: "two blocks of the same length: zcp cannot tell the preview ports", curMax: 3, params: ScaleParams{MinRAM: scaleFloatPtr(2)}, ports: twoBlocks,
		},
		{
			name: "a pool declaring no port has no preview to count", curMax: 4, params: ScaleParams{MinRAM: scaleFloatPtr(2)},
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
			swarm := &fakeSwarm{containers: containersAt(leasedAt(1), 1, 2, 3, 4, 5)}
			deps := LeaseDeps{Client: client, ProjectID: "p1", StateDir: stateDir,
				Swarm: func(string, string) SwarmAPI { return swarm },
				Env:   func(string) (string, bool) { return "", false }}
			guard := WithPoolShrinkGuard(deps, func(PoolShrink) error { return nil })

			res, err := Scale(context.Background(), client, "p1", testPoolHost, tt.params, guard)
			if err != nil {
				t.Fatalf("Scale: %v — a scale that does not raise the maximum is never refused for preview ports", err)
			}
			if client.sent == nil {
				t.Fatal("nothing was sent to the platform")
			}
			if len(tt.want) == 0 {
				if strings.Contains(res.Notice, "preview port") {
					t.Errorf("notice speaks of preview ports where zcp knows of no shortfall: %q", res.Notice)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(res.Notice, want) {
					t.Errorf("notice lacks %q:\n%s", want, res.Notice)
				}
			}
			if !strings.HasPrefix(res.Notice, "Pool 'devpool': maxContainers") {
				t.Errorf("the preview-port shortfall does not lead the notice:\n%s", res.Notice)
			}
		})
	}
}
