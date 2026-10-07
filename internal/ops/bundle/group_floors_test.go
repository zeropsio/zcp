package bundle

import (
	"maps"
	"testing"
)

// TestBuildGroupRecipe_SearchEnginesGetAResourceFloor: a search engine's
// reindex spikes memory faster than vertical autoscaling reacts (a Meilisearch
// stood up from a group recipe at 1 GB ran out of memory reindexing), so the
// recipe writes a search engine with at least 2 GB and a 0.5 GB free-memory
// buffer, on every tier. A floor is the max of the source and the floor: it
// never lowers a value, raises maxRam only when it would fall below minRam,
// and leaves every service that is not a search engine as it runs.
func TestBuildGroupRecipe_SearchEnginesGetAResourceFloor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		typ     string
		scaling *Scaling
		want    map[string]string
	}{
		{
			name:    "meilisearch below the floor",
			typ:     "meilisearch:single@1.44",
			scaling: &Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 8, MinRAM: 1, MaxRAM: 48, MinFreeRAMGB: 0.25},
			want:    map[string]string{"cpuMode": "SHARED", "minCpu": "1", "maxCpu": "8", "minRam": "2", "maxRam": "48", "minFreeRamGB": "0.5"},
		},
		{
			name:    "never lowers a value above the floor",
			typ:     "meilisearch@1.20",
			scaling: &Scaling{CPUMode: "SHARED", MinRAM: 4, MaxRAM: 16, MinFreeRAMGB: 1, MinFreeRAMPercent: 10},
			want:    map[string]string{"cpuMode": "SHARED", "minRam": "4", "maxRam": "16", "minFreeRamGB": "1", "minFreeRamPercent": "10"},
		},
		{
			name:    "maxRam raised only to meet minRam",
			typ:     "elasticsearch@8.16",
			scaling: &Scaling{CPUMode: "SHARED", MinRAM: 0.5, MaxRAM: 1.5},
			want:    map[string]string{"cpuMode": "SHARED", "minRam": "2", "maxRam": "2", "minFreeRamGB": "0.5"},
		},
		{
			name:    "typesense has room to reindex on every tier",
			typ:     "typesense@27.1",
			scaling: &Scaling{CPUMode: "SHARED", MinRAM: 1, MaxRAM: 4, MinFreeRAMGB: 0.25},
			want:    map[string]string{"cpuMode": "SHARED", "minRam": "2", "maxRam": "4", "minFreeRamGB": "0.5"},
		},
		{
			name:    "a search engine whose scale could not be read: nothing written",
			typ:     "typesense@27.1",
			scaling: nil,
			want:    map[string]string{},
		},
		{
			name:    "not a search engine: untouched",
			typ:     "mariadb@10.6",
			scaling: &Scaling{CPUMode: "SHARED", MinRAM: 0.25, MaxRAM: 2, MinFreeRAMGB: 0.25},
			want:    map[string]string{"cpuMode": "SHARED", "minRam": "0.25", "maxRam": "2", "minFreeRamGB": "0.25"},
		},
		{
			name:    "not a search engine, scale unread: nothing written",
			typ:     "nats@2.10",
			scaling: nil,
			want:    map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.ManagedServices = append(in.ManagedServices, ManagedServiceEntry{Hostname: "svc", Type: tt.typ, Scaling: tt.scaling})
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range []string{"AI Agent", "Stage", "Small Production"} {
				got := scalarMap(mappingValue(serviceNode(t, tierBody(t, layout.Tiers, tier), "svc"), "verticalAutoscaling"))
				if got == nil {
					got = map[string]string{}
				}
				if !maps.Equal(got, tt.want) {
					t.Errorf("%s: verticalAutoscaling = %v, want %v", tier, got, tt.want)
				}
			}
		})
	}
}

// TestProjectScaling_CarriesTheFreeMemoryBuffer: the buffer a service keeps
// free before it scales up is part of how it runs, so an export and a recipe
// carry it — all but the platform's own default (64 MB), which would only
// write noise into every service.
func TestProjectScaling_CarriesTheFreeMemoryBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		scaling *Scaling
		want    map[string]any
	}{
		{"a set buffer", &Scaling{MinRAM: 1, MinFreeRAMGB: 0.5, MinFreeRAMPercent: 5}, map[string]any{"minRam": 1.0, "minFreeRamGB": 0.5, "minFreeRamPercent": 5.0}},
		{"the platform default", &Scaling{MinRAM: 1, MinFreeRAMGB: 0.0625}, map[string]any{"minRam": 1.0}},
		{"none", &Scaling{MinRAM: 1}, map[string]any{"minRam": 1.0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entry := map[string]any{}
			projectScaling(entry, tt.scaling)
			got, _ := entry["verticalAutoscaling"].(map[string]any)
			if !maps.Equal(got, tt.want) {
				t.Errorf("verticalAutoscaling = %v, want %v", got, tt.want)
			}
		})
	}
}
