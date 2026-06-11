package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasPriorityJustificationBlock_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "jetstream_canonical_block_passes",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n\n  # Set higher priority for databases and storages,\n  # because the app depends on those services.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: true,
		},
		{
			name: "run38_no_block_blocks",
			body: "services:\n  - hostname: worker\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: false,
		},
		{
			name: "priority_without_dependency_reason_blocks",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n\n  # Higher priority for databases.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: false,
		},
		{
			name: "boot_order_depend_passes",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n\n  # Boot-order: runtimes depend on managed services.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: true,
		},
		{
			// Regression: run-38 tier-4 has priority:5 on the api
			// runtime AND priority:10 on managed services. The
			// justification block must apply to the first
			// priority>=10 directive, not the first priority of any
			// value (priority:5 on a runtime is unrelated to
			// boot-order semantics on managed deps).
			name: "runtime_priority_5_then_managed_priority_10_with_block_passes",
			body: "services:\n  - hostname: api\n    type: nodejs@22\n    priority: 5\n  - hostname: app\n    type: static\n\n  # Set higher priority for databases and storages,\n  # because the app depends on those services.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := hasPriorityJustificationBlock(tc.body)
			if got != tc.want {
				t.Fatalf("hasPriorityJustificationBlock=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasPriorityServices_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "priority_10", body: "priority: 10\n", want: true},
		{name: "priority_higher", body: "priority: 20\n", want: true},
		{name: "priority_absent", body: "mode: NON_HA\n", want: false},
		{name: "priority_lower", body: "priority: 5\n", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasPriorityServices(tc.body); got != tc.want {
				t.Fatalf("hasPriorityServices=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestGateRequirePriorityJustification_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "priority_with_justification_passes",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n\n  # Set higher priority for databases and storages,\n  # because the app depends on those services.\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: 0,
		},
		{
			name: "priority_without_justification_blocks",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n    priority: 10\n",
			want: 1,
		},
		{
			name: "no_priority_skips",
			body: "services:\n  - hostname: app\n    type: nodejs@22\n  - hostname: db\n    type: postgresql@18\n",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			tier := Tiers()[4]
			dir := filepath.Join(root, tier.Folder)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "import.yaml"), []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			vs := gateRequirePriorityJustification(GateContext{OutputRoot: root})
			if len(vs) != tc.want {
				t.Fatalf("len(violations)=%d, want %d: %+v", len(vs), tc.want, vs)
			}
		})
	}
}

func TestEnvGates_PriorityJustificationGateRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(EnvGates(), "priority-justification") {
		t.Fatalf("EnvGates missing priority-justification")
	}
}
