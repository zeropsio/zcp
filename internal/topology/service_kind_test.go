package topology

import "testing"

func TestIsPoolType_TypeStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"swarm@1", true},
		{"swarm-vm@1", true},
		{"SWARM@1", true},
		{"Swarm-VM@1", true},
		{"ubuntu/swarm@1", true},
		{"alpine/swarm-vm@1", true},
		{"nodejs@22", false},
		{"ubuntu/python@3.12", false},
		{"postgresql@16", false},
		{"zcp@1", false},
		{"static", false},
		{"mailpit", false},
		{"ubuntu/zbox@1", false},
		{"swarmish@1", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := IsPoolType(tt.in); got != tt.want {
				t.Errorf("IsPoolType(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsSandboxType_TypeStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"ubuntu/zbox@1", true},
		{"zbox@1", true},
		{"UBUNTU/ZBOX@1", true},
		{"nodejs@22", false},
		{"ubuntu/python@3.12", false},
		{"postgresql@16", false},
		{"zcp@1", false},
		{"static", false},
		{"mailpit", false},
		{"swarm@1", false},
		{"swarm-vm@1", false},
		{"zboxx@1", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := IsSandboxType(tt.in); got != tt.want {
				t.Errorf("IsSandboxType(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsRuntimeType_PoolAndSandbox_False(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"swarm@1", false},
		{"swarm-vm@1", false},
		{"ubuntu/zbox@1", false},
		{"zbox@1", false},
		{"nodejs@22", true},
		{"ubuntu/python@3.12", true},
		{"static", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := IsRuntimeType(tt.in); got != tt.want {
				t.Errorf("IsRuntimeType(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsPoolVMType_TypeStrings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"swarm-vm@1", true},
		{"Swarm-VM@1", true},
		{"alpine/swarm-vm@1", true},
		{"swarm@1", false},
		{"ubuntu/swarm@1", false},
		{"nodejs@22", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := IsPoolVMType(tt.in); got != tt.want {
				t.Errorf("IsPoolVMType(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestBoxLocationAllowed pins where a Zerops Box can be created: only in a
// project in region prg1 (owner-stated platform fact, 2026-10-24), which the
// project record reports as the location id `eu-central`, named
// "EU Central (prg1)" (live-verified 2026-10-24; ny1 reads `us-east-1`,
// "US East (ny1)"). Spec: spec-workflows.md §8 PS-16.
func TestBoxLocationAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		{"eu-central", true},
		{" EU-Central ", true},
		{"us-east-1", false},
		{"us-west-1", false},
		{"prg1", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			if got := BoxLocationAllowed(tt.in); got != tt.want {
				t.Errorf("BoxLocationAllowed(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestBoxRegions pins the region names a refusal names: prg1 alone today.
func TestBoxRegions(t *testing.T) {
	t.Parallel()
	got := BoxRegions()
	if len(got) != 1 || got[0] != "prg1" {
		t.Errorf("BoxRegions() = %v, want [prg1]", got)
	}
}
