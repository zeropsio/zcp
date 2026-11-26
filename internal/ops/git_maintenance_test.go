package ops

import (
	"strings"
	"testing"
)

// witness: internal/ops/git_brokered_relay.go | "-c", "gc.auto=0", → "-c", "gc.auto=1",
// witness: internal/ops/git_brokered_relay.go | "-c", "maintenance.auto=false", → "-c", "maintenance.auto=true",
// witness: internal/ops/git_relayed_script.go | {"gc.auto", "0"}, → {"gc.auto", "1"},
// witness: internal/ops/git_relayed_script.go | {"maintenance.auto", "false"}, → {"maintenance.auto", "true"},
func TestGitRelay_GRNoAuto_AutomaticMaintenance_Disabled(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		command  string
		fragment string
	}{
		{
			name: "git call overrides caller config",
			command: buildRelayedGitCommand("/var/www", BrokeredGitArgs{
				Config:     []string{"gc.auto=1", "maintenance.auto=true"},
				Subcommand: "log",
			}, "testnonce", false, "https"),
			fragment: "'-c' 'gc.auto=0' '-c' 'maintenance.auto=false' 'log'",
		},
		{
			name:     "zcp script configures every git",
			command:  buildRelayedScriptCommand("git fetch origin", "testnonce", []string{"https"}),
			fragment: "GIT_CONFIG_KEY_3='gc.auto' GIT_CONFIG_VALUE_3='0' GIT_CONFIG_KEY_4='maintenance.auto' GIT_CONFIG_VALUE_4='false'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			quoted := ShellQuote(tc.fragment)
			if !strings.Contains(tc.command, quoted[1:len(quoted)-1]) {
				t.Errorf("relayed command does not carry %q", tc.fragment)
			}
		})
	}
}
