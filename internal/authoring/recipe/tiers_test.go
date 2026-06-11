package recipe

import (
	"reflect"
	"testing"
)

func TestTiers_Count(t *testing.T) {
	t.Parallel()
	if got, want := len(Tiers()), 6; got != want {
		t.Fatalf("Tiers() count = %d, want %d", got, want)
	}
}

func TestTiers_FolderAndSuffix(t *testing.T) {
	t.Parallel()
	wantFolders := []string{
		"0 — AI Agent",
		"1 — Remote (CDE)",
		"2 — Local",
		"3 — Stage",
		"4 — Small Production",
		"5 — Highly-available Production",
	}
	wantSuffixes := []string{"agent", "remote", "local", "stage", "small-prod", "ha-prod"}
	for i, tier := range Tiers() {
		if tier.Index != i {
			t.Errorf("tier %d: Index = %d, want %d", i, tier.Index, i)
		}
		if tier.Folder != wantFolders[i] {
			t.Errorf("tier %d: Folder = %q, want %q", i, tier.Folder, wantFolders[i])
		}
		if tier.Suffix != wantSuffixes[i] {
			t.Errorf("tier %d: Suffix = %q, want %q", i, tier.Suffix, wantSuffixes[i])
		}
	}
}

func TestTierDiff_AdjacentTiers(t *testing.T) {
	t.Parallel()

	// Adjacent tier diffs encode the platform-level behavior delta between
	// tiers. Metadata fields (Index/Folder/Label/Suffix) are excluded from
	// Diff — those are addressing, not behavior. Writers compose promotion
	// prose from behavior deltas alone.
	cases := []struct {
		name    string
		from    int
		to      int
		changes []FieldChange
	}{
		{
			name: "0→1: dev container persists, no behavior delta",
			from: 0, to: 1,
			changes: nil,
		},
		{
			name: "1→2: dev slot drops when leaving cloud dev envs",
			from: 1, to: 2,
			changes: []FieldChange{
				{Field: "RunsDevContainer", From: "true", To: "false", Kind: "runtime"},
			},
		},
		{
			name: "2→3: free-ram floor appears at stage",
			from: 2, to: 3,
			changes: []FieldChange{
				{Field: "MinFreeRAMGB", From: "0", To: "0.25", Kind: "all"},
			},
		},
		{
			name: "3→4: throughput replica pair appears in production",
			from: 3, to: 4,
			changes: []FieldChange{
				{Field: "RuntimeMinContainers", From: "1", To: "2", Kind: "runtime"},
			},
		},
		{
			name: "4→5: HA mode, dedicated CPU, SERIOUS support, managed RAM bump",
			from: 4, to: 5,
			changes: []FieldChange{
				{Field: "ServiceMode", From: "NON_HA", To: "HA", Kind: "managed"},
				{Field: "CPUMode", From: "", To: "DEDICATED", Kind: "runtime"},
				{Field: "CorePackage", From: "", To: "SERIOUS", Kind: "project"},
				{Field: "MinFreeRAMGB", From: "0.25", To: "0.5", Kind: "all"},
				{Field: "ManagedMinRAM", From: "0.25", To: "1", Kind: "managed"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, _ := TierAt(tc.from)
			b, _ := TierAt(tc.to)
			diff := Diff(a, b)
			if diff.FromIndex != tc.from || diff.ToIndex != tc.to {
				t.Errorf("FromIndex/ToIndex = %d/%d, want %d/%d",
					diff.FromIndex, diff.ToIndex, tc.from, tc.to)
			}
			if !reflect.DeepEqual(diff.Changes, tc.changes) {
				t.Errorf("Changes mismatch\ngot:  %+v\nwant: %+v", diff.Changes, tc.changes)
			}
		})
	}
}

func TestTierAt_OutOfRange(t *testing.T) {
	t.Parallel()
	if _, ok := TierAt(-1); ok {
		t.Error("TierAt(-1) should return ok=false")
	}
	if _, ok := TierAt(6); ok {
		t.Error("TierAt(6) should return ok=false")
	}
}
