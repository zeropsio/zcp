package workflow

import (
	"testing"
)

func TestInferServicePairing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []AdoptCandidate
		wantCount  int
		wantModes  map[string]string // hostname → expected mode
		wantStage  map[string]string // hostname → expected StageHostname
	}{
		{
			name: "standard pair appdev+appstage",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16"},
			},
			wantCount: 1,
			wantModes: map[string]string{"appdev": PlanModeStandard},
			wantStage: map[string]string{"appdev": "appstage"},
		},
		{
			name: "single service dev mode",
			candidates: []AdoptCandidate{
				{Hostname: "api", Type: "go@1"},
			},
			wantCount: 1,
			wantModes: map[string]string{"api": PlanModeDev},
			wantStage: map[string]string{"api": ""},
		},
		{
			name: "skip managed services",
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
			wantModes: map[string]string{"appdev": PlanModeDev},
		},
		{
			name: "multiple pairs",
			candidates: []AdoptCandidate{
				{Hostname: "webdev", Type: "nodejs@22"},
				{Hostname: "webstage", Type: "nodejs@22"},
				{Hostname: "apidev", Type: "go@1"},
				{Hostname: "apistage", Type: "go@1"},
			},
			wantCount: 2,
			wantModes: map[string]string{
				"webdev": PlanModeStandard,
				"apidev": PlanModeStandard,
			},
		},
		{
			name: "hostname dev alone no stage",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
			},
			wantCount: 1,
			wantModes: map[string]string{"appdev": PlanModeDev},
		},
		{
			name: "hostname is literally dev",
			candidates: []AdoptCandidate{
				{Hostname: "dev", Type: "nodejs@22"},
			},
			wantCount: 1,
			wantModes: map[string]string{"dev": PlanModeDev},
		},
		{
			name: "webstage alone no pair",
			candidates: []AdoptCandidate{
				{Hostname: "webstage", Type: "nodejs@22"},
			},
			wantCount: 1,
			wantModes: map[string]string{"webstage": PlanModeDev},
		},
		{
			name: "stage before dev in API order",
			candidates: []AdoptCandidate{
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16"},
			},
			wantCount: 1,
			wantModes: map[string]string{"appdev": PlanModeStandard},
			wantStage: map[string]string{"appdev": "appstage"},
		},
		{
			name: "stage before dev with extra runtime",
			candidates: []AdoptCandidate{
				{Hostname: "workerstage", Type: "php-nginx@8.4"},
				{Hostname: "appstage", Type: "php-nginx@8.4"},
				{Hostname: "appdev", Type: "php-nginx@8.4"},
			},
			wantCount: 2,
			wantModes: map[string]string{
				"appdev":      PlanModeStandard,
				"workerstage": PlanModeDev,
			},
			wantStage: map[string]string{
				"appdev":      "appstage",
				"workerstage": "",
			},
		},
		{
			name: "managed services become dependencies",
			candidates: []AdoptCandidate{
				{Hostname: "appdev", Type: "bun@1.2"},
				{Hostname: "appstage", Type: "bun@1.2"},
				{Hostname: "db", Type: "postgresql@16"},
				{Hostname: "cache", Type: "valkey@7.2"},
			},
			wantCount: 1,
			wantModes: map[string]string{"appdev": PlanModeStandard},
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

			for _, target := range targets {
				hostname := target.Runtime.DevHostname
				if wantMode, ok := tt.wantModes[hostname]; ok {
					if target.Runtime.EffectiveMode() != wantMode {
						t.Errorf("%s mode: want %s, got %s", hostname, wantMode, target.Runtime.EffectiveMode())
					}
				}
				if wantStage, ok := tt.wantStage[hostname]; ok {
					gotStage := target.Runtime.StageHostname()
					if gotStage != wantStage {
						t.Errorf("%s stage: want %q, got %q", hostname, wantStage, gotStage)
					}
				}
				if !target.Runtime.IsExisting {
					t.Errorf("%s: IsExisting should be true", hostname)
				}
			}

			// Verify managed services appear as dependencies.
			if tt.name == "managed services become dependencies" {
				target := targets[0]
				if len(target.Dependencies) != 2 {
					t.Fatalf("dependencies: want 2, got %d", len(target.Dependencies))
				}
				for _, dep := range target.Dependencies {
					if dep.Resolution != "EXISTS" {
						t.Errorf("dependency %s resolution: want EXISTS, got %s", dep.Hostname, dep.Resolution)
					}
				}
			}
		})
	}
}

// F4 regression: liveManaged (from API) must override the static prefix list.
// A new Zerops managed category should be recognized even if the static list
// doesn't list it yet — otherwise every new type ships misclassification
// until managed_types.go is bumped.
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
