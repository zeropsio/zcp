package console

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/dataconsole/console/provider"
	"github.com/zeropsio/zcp/internal/dataconsole/console/safety"
)

// A database console that runs inside the project (a Mate's container) reaches
// the private network as it is: it never tells anyone to bring up a VPN. The
// hint stays for a console on a person's own machine.
func TestEngine_VPNHint_OnlyOutsideTheProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		opts     []EngineOption
		wantHint bool
	}{
		{"a person's own machine", nil, true},
		{"inside the project", []EngineOption{InsideProject(true)}, false},
		{"told it is outside", []EngineOption{InsideProject(false)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			engine := NewEngine(summaryHost{provider.SQLConn{Host: "db"}}, safety.NewPolicy(false, "", ""),
				map[provider.Family]Factory{provider.FamilyTabular: nil}, tc.opts...)
			if err := engine.Refresh(context.Background()); err != nil {
				t.Fatal(err)
			}
			hint := false
			for _, a := range engine.Services()[0].Actions {
				hint = hint || a.ID == provider.ActionShowVPNGate
			}
			if hint != tc.wantHint {
				t.Fatalf("VPN hint = %v, want %v", hint, tc.wantHint)
			}
		})
	}
}
