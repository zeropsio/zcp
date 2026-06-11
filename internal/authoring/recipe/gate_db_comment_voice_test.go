package recipe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanTradeoffLeadComments_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "postgres_restore_small_prod_tradeoff_blocks",
			body: "# Single-instance NON_HA Postgres — restoring from snapshot means downtime, the small-prod tradeoff.\n- hostname: db\n  type: postgresql@18\n  mode: NON_HA\n",
			want: true,
		},
		{
			name: "postgres_restore_rehearsal_grade_tradeoff_blocks",
			body: "# Single-instance NON_HA Postgres — restoring from snapshot means downtime, the rehearsal-grade tradeoff.\n- hostname: db\n  type: postgresql@18\n  mode: NON_HA\n",
			want: true,
		},
		{
			name: "cache_tolerate_restart_blocks",
			body: "# Single-instance NON_HA cache — tolerate a brief restart window during deploys.\n- hostname: cache\n  type: valkey@7\n  mode: NON_HA\n",
			want: true,
		},
		{
			name: "cde_role_context_lead_passes",
			body: "# Same single-instance NON_HA Postgres as tier 0 — sized for CDE iteration; snapshots cover restoration if your scratch data matters.\n- hostname: db\n  type: postgresql@18\n  mode: NON_HA\n",
			want: false,
		},
		{
			name: "postgres_role_lead_passes",
			body: "# Single-instance NON_HA Postgres — used by the api codebase to store items + uploads.\n- hostname: db\n  type: postgresql@18\n  mode: NON_HA\n",
			want: false,
		},
		{
			name: "valkey_role_lead_passes",
			body: "# Single-node Valkey — sized for production cache traffic; bump verticalAutoscaling.minRam when monitoring shows the eviction rate climbing.\n- hostname: cache\n  type: valkey@7\n  mode: NON_HA\n",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := len(scanTradeoffLeadComments(tc.body)) > 0
			if got != tc.want {
				t.Fatalf("scanTradeoffLeadComments blocked=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestEnvGates_TradeoffLeadGateRegistered(t *testing.T) {
	t.Parallel()
	if !gateRegistered(EnvGates(), "db-comment-tradeoff-lead") {
		t.Fatalf("EnvGates missing db-comment-tradeoff-lead")
	}
}

func TestGateForbidTradeoffLeadOnDbComment_TierFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	tier := Tiers()[3]
	dir := filepath.Join(root, tier.Folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Single-instance NON_HA Postgres — restoring from snapshot means downtime, the rehearsal-grade tradeoff.\n- hostname: db\n  type: postgresql@18\n"
	if err := os.WriteFile(filepath.Join(dir, "import.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	vs := gateForbidTradeoffLeadOnDbComment(GateContext{OutputRoot: root})
	if len(vs) != 1 || vs[0].Code != "managed-service-comment-tradeoff-lead" {
		t.Fatalf("violations = %+v, want one managed-service-comment-tradeoff-lead", vs)
	}
}

func gateRegistered(gates []Gate, name string) bool {
	for _, gate := range gates {
		if gate.Name == name {
			return true
		}
	}
	return false
}
