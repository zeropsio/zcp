// Tests for bootstrap response Kind discriminator. Two distinct response
// shapes are returned by `zerops_workflow action="start" workflow="bootstrap"`:
//
//   - kind="route-menu" — first call (no `route` argument). Returns ranked
//     route options; NO session is open. Agent picks one and re-calls start
//     with route=<chosen>.
//   - kind="session-active" — second call (route committed). Session is
//     live; SessionID populated; the discover step is in flight.
//
// Pinning the field separately from existing route/step tests so a future
// refactor that drops the field gets caught explicitly. Without this
// discriminator, agents had to infer the response phase from field presence
// (RouteOptions vs SessionID), which eval retros showed agents getting wrong
// (5+ scenarios across runs flagged "I almost moved on thinking the first
// call succeeded").

package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/runtime"
)

func TestBootstrapDiscover_KindIsRouteMenu(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	resp, err := eng.BootstrapDiscover("proj-1", "node todo app", nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BootstrapDiscover: %v", err)
	}
	if resp.Kind != BootstrapKindRouteMenu {
		t.Errorf("discovery Kind = %q, want %q", resp.Kind, BootstrapKindRouteMenu)
	}
	// No session yet — discovery is read-only.
	if got := resp.RouteOptions; len(got) == 0 {
		t.Error("discovery must return at least one route option")
	}
}

func TestBootstrapStart_KindIsSessionActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	resp, err := eng.BootstrapStart("proj-1", "test")
	if err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	if resp.Kind != BootstrapKindSessionActive {
		t.Errorf("session-active Kind = %q, want %q", resp.Kind, BootstrapKindSessionActive)
	}
	if resp.SessionID == "" {
		t.Error("session-active response must carry SessionID")
	}
}

func TestBootstrapComplete_KindIsSessionActive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	if _, err := eng.BootstrapStart("proj-1", "test"); err != nil {
		t.Fatalf("BootstrapStart: %v", err)
	}
	resp, err := eng.BootstrapComplete(context.Background(), "discover", "FRESH project, plan submitted", nil)
	if err != nil {
		t.Fatalf("BootstrapComplete: %v", err)
	}
	if resp.Kind != BootstrapKindSessionActive {
		t.Errorf("complete Kind = %q, want %q", resp.Kind, BootstrapKindSessionActive)
	}
}

func TestBootstrapDiscoveryMessage_AnnouncesTwoCallPattern(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	eng := NewEngine(dir, EnvLocal, nil)

	resp, err := eng.BootstrapDiscover("proj-1", "node todo app", nil, runtime.Info{})
	if err != nil {
		t.Fatalf("BootstrapDiscover: %v", err)
	}
	// The discovery message must explicitly state the two-call shape so
	// agents that read prose (not just fields) immediately understand the
	// phase. Eval friction: agents missed this when message lead was the
	// generic "Bootstrap route discovery — pick one".
	for _, want := range []string{`kind="route-menu"`, `NO session is open yet`, `kind="session-active"`} {
		if !strings.Contains(resp.Message, want) {
			t.Errorf("discovery message missing %q hint; got:\n%s", want, resp.Message)
		}
	}
}
