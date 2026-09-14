package topology_test

import (
	"encoding/json"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestEnvRefName_Hostname_ReturnsEnvNamespacedRef(t *testing.T) {
	got := topology.EnvRefName("appstage")
	want := "refs/zcp/env/appstage"
	if got != want {
		t.Errorf("EnvRefName(%q) = %q, want %q", "appstage", got, want)
	}
}

func TestLedgerEntry_JSONRoundTrip_PreservesFields(t *testing.T) {
	entry := topology.LedgerEntry{
		SHA:          "abc123",
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded topology.LedgerEntry
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded != entry {
		t.Errorf("round-trip = %+v, want %+v", decoded, entry)
	}
}
