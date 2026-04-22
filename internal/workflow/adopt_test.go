package workflow

import (
	"sort"
	"testing"
)

// Phase B.4: the hostname-suffix pairing heuristic was deleted. Every runtime
// now becomes its own dev-mode target; managed services become shared EXISTS
// dependencies; control-plane types are filtered out. Tests reflect the new
// one-runtime-one-target contract.
func TestInferServicePairing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []AdoptCandidate
		wantCount  int
		wantHosts  []string // dev-mode target hostnames in any order
	}{
		{
			name: "two hostname-suffixed runtimes become two dev targets (no pairing)",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16"},
			},
			wantCount: 2,
			wantHosts: []string{"appdev", "appstage"},
		},
		{
			name: "single service dev mode",
			candidates: []AdoptCandidate{
				{Hostname: "api", Type: "go@1"},
			},
			wantCount: 1,
			wantHosts: []string{"api"},
		},
		{
			name: "only managed services yields no targets",
			candidates: []AdoptCandidate{
				{Hostname: "db", Type: "postgresql@16"},
				{Hostname: "cache", Type: "valkey@7.2"},
			},
			wantCount: 0,
		},
		{
			name: "skip zcp type",
			candidates: []AdoptCandidate{
				{Hostname: "zcp", Type: "zcp@1"},
				{Hostname: "appdev", Type: "nodejs@22"},
			},
			wantCount: 1,
			wantHosts: []string{"appdev"},
		},
		{
			name: "four runtimes, no pairing — four independent dev targets",
			candidates: []AdoptCandidate{
				{Hostname: "webdev", Type: "nodejs@22"},
				{Hostname: "webstage", Type: "nodejs@22"},
				{Hostname: "apidev", Type: "go@1"},
				{Hostname: "apistage", Type: "go@1"},
			},
			wantCount: 4,
			wantHosts: []string{"webdev", "webstage", "apidev", "apistage"},
		},
		{
			name: "hostname dev alone",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
			},
			wantCount: 1,
			wantHosts: []string{"appdev"},
		},
		{
			name: "hostname is literally dev",
			candidates: []AdoptCandidate{
				{Hostname: "dev", Type: "nodejs@22"},
			},
			wantCount: 1,
			wantHosts: []string{"dev"},
		},
		{
			name: "webstage alone",
			candidates: []AdoptCandidate{
				{Hostname: "webstage", Type: "nodejs@22"},
			},
			wantCount: 1,
			wantHosts: []string{"webstage"},
		},
		{
			name: "managed services become dependencies on every target",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16"},
				{Hostname: "cache", Type: "valkey@7.2"},
			},
			wantCount: 2,
			wantHosts: []string{"appdev", "appstage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targets := InferServicePairing(tt.candidates, nil)

			if len(targets) != tt.wantCount {
				t.Fatalf("target count: want %d, got %d", tt.wantCount, len(targets))
			}
			if tt.wantCount == 0 {
				return
			}

			gotHosts := make([]string, 0, len(targets))
			for _, target := range targets {
				gotHosts = append(gotHosts, target.Runtime.DevHostname)
				if target.Runtime.EffectiveMode() != PlanModeDev {
					t.Errorf("%s: want PlanModeDev, got %s",
						target.Runtime.DevHostname, target.Runtime.EffectiveMode())
				}
				if target.Runtime.StageHostname() != "" {
					t.Errorf("%s: dev-mode target must not synthesize a stage hostname, got %q",
						target.Runtime.DevHostname, target.Runtime.StageHostname())
				}
				if !target.Runtime.IsExisting {
					t.Errorf("%s: IsExisting should be true", target.Runtime.DevHostname)
				}
			}

			sort.Strings(gotHosts)
			wantSorted := append([]string(nil), tt.wantHosts...)
			sort.Strings(wantSorted)
			for i := range wantSorted {
				if gotHosts[i] != wantSorted[i] {
					t.Errorf("hosts: want %v, got %v", wantSorted, gotHosts)
					break
				}
			}

			// Verify managed services appear as shared dependencies.
			if tt.name == "managed services become dependencies on every target" {
				for _, target := range targets {
					if len(target.Dependencies) != 2 {
						t.Fatalf("dependencies on %s: want 2, got %d",
							target.Runtime.DevHostname, len(target.Dependencies))
					}
					for _, dep := range target.Dependencies {
						if dep.Resolution != "EXISTS" {
							t.Errorf("%s dep %s: want EXISTS, got %s",
								target.Runtime.DevHostname, dep.Hostname, dep.Resolution)
						}
					}
				}
			}
		})
	}
}

// liveManaged (from API) must override the static prefix list. A new Zerops
// managed category should be recognized even if the static list doesn't list
// it yet — otherwise every new type ships misclassification until
// managed_types.go is bumped.
func TestInferServicePairing_LiveManagedOverridesStatic(t *testing.T) {
	t.Parallel()

	// "futurecache@1" isn't in the static managedServicePrefixes list.
	candidates := []AdoptCandidate{
		{Hostname: "appdev", Type: "nodejs@22"},
		{Hostname: "cache", Type: "futurecache@1"},
	}

	// Without liveManaged: static list fails, "cache" is treated as runtime.
	staticTargets := InferServicePairing(candidates, nil)
	if len(staticTargets) != 2 {
		t.Fatalf("static fallback: want 2 runtime targets, got %d", len(staticTargets))
	}

	// With liveManaged: "futurecache" is known managed, becomes a dependency.
	liveManaged := map[string]bool{"futurecache": true}
	liveTargets := InferServicePairing(candidates, liveManaged)
	if len(liveTargets) != 1 {
		t.Fatalf("live managed: want 1 runtime target (appdev), got %d", len(liveTargets))
	}
	if liveTargets[0].Runtime.DevHostname != "appdev" {
		t.Errorf("runtime hostname: want appdev, got %s", liveTargets[0].Runtime.DevHostname)
	}
	if len(liveTargets[0].Dependencies) != 1 || liveTargets[0].Dependencies[0].Hostname != "cache" {
		t.Errorf("want 1 dep hostname=cache, got %+v", liveTargets[0].Dependencies)
	}
}

func TestIsControlPlaneType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"zcp@1", true},
		{"ZCP@2", true},
		{"nodejs@22", false},
		{"postgresql@16", false},
	}
	for _, tt := range tests {
		if got := isControlPlaneType(tt.input); got != tt.want {
			t.Errorf("isControlPlaneType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
