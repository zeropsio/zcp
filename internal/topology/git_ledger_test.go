package topology_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestDeployTagName_ProjectTargetAppVersion_ReturnsNamespacedTagName(t *testing.T) {
	got := topology.DeployTagName("proj-1", "appstage", "av-1")
	want := "zcp/deploy/proj-1/appstage/av-1"
	if got != want {
		t.Errorf("DeployTagName(...) = %q, want %q", got, want)
	}
}

func TestDeployTagPrefix_ProjectTarget_ReturnsPrefixNoTrailingSlash(t *testing.T) {
	got := topology.DeployTagPrefix("proj-1", "appstage")
	want := "zcp/deploy/proj-1/appstage"
	if got != want {
		t.Errorf("DeployTagPrefix(...) = %q, want %q", got, want)
	}
	if got[len(got)-1] == '/' {
		t.Errorf("DeployTagPrefix(...) = %q, must not carry a trailing slash", got)
	}
}

func TestLedgerEntry_JSONRoundTrip_PreservesFields(t *testing.T) {
	entry := topology.LedgerEntry{
		SHA:          "abc123",
		AppVersionID: "av-1",
		Target:       "appstage",
		Project:      "proj-1",
		At:           "2026-09-14T12:00:00Z",
		Dirty:        true,
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

func TestLedgerEntry_JSONRoundTrip_DirtyOmittedWhenFalse(t *testing.T) {
	entry := topology.LedgerEntry{SHA: "abc123", Target: "appstage"}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); strings.Contains(got, `"dirty"`) {
		t.Errorf("marshaled = %s, want no \"dirty\" key when Dirty is false (omitempty)", got)
	}
}
