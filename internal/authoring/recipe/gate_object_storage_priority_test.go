package recipe

import "testing"

func TestGateRequireObjectStoragePriority_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		plan *Plan
		want int
	}{
		{
			name: "storage_priority_with_sibling_priority_passes",
			plan: &Plan{Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", Priority: 10},
				{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage", Priority: 10},
			}},
			want: 0,
		},
		{
			name: "storage_missing_priority_with_sibling_priority_blocks",
			plan: &Plan{Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", Priority: 10},
				{Kind: ServiceKindManaged, Hostname: "cache", Type: "valkey@7", Priority: 10},
				{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage"},
			}},
			want: 1,
		},
		{
			name: "priority_unused_entirely_skips",
			plan: &Plan{Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18"},
				{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage"},
			}},
			want: 0,
		},
		{
			name: "storage_priority_5_passes",
			plan: &Plan{Services: []Service{
				{Kind: ServiceKindManaged, Hostname: "db", Type: "postgresql@18", Priority: 10},
				{Kind: ServiceKindStorage, Hostname: "storage", Type: "object-storage", Priority: 5},
			}},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs := gateRequireObjectStoragePriority(GateContext{Plan: tc.plan})
			if len(vs) != tc.want {
				t.Fatalf("len(violations)=%d, want %d: %+v", len(vs), tc.want, vs)
			}
		})
	}
}

func TestEnvGates_ObjectStoragePriorityGateRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(EnvGates(), "object-storage-priority") {
		t.Fatalf("EnvGates missing object-storage-priority")
	}
}
