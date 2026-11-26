package ops

import (
	"strings"
	"testing"
)

// witness: internal/ops/git_brokered_relay.go | if brokeredGitReadOnly(args.Subcommand) { → if !brokeredGitReadOnly(args.Subcommand) {
// witness: internal/ops/git_brokered_relay.go | "-c", "gc.auto=0", → "-c", "gc.auto=1",
// witness: internal/ops/git_brokered_relay.go | "-c", "maintenance.auto=false", → "-c", "maintenance.auto=true",
// witness: internal/ops/git_relayed_script.go | {"gc.autoDetach", "false"}, → {"gc.autoDetach", "true"},
// witness: internal/ops/git_relayed_script.go | {"maintenance.autoDetach", "false"}, → {"maintenance.autoDetach", "true"},
func TestGitRelay_GRNoAuto_ReadOnlySkipsAutomaticMaintenance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		command  string
		rest     []string
		readOnly bool
	}{
		{command: "log", readOnly: true},
		{command: "show", readOnly: true},
		{command: "grep", readOnly: true},
		{command: "diff", readOnly: true},
		{command: "status", readOnly: true},
		{command: "rev-parse", readOnly: true},
		{command: "ls-files", readOnly: true},
		{command: "blame", readOnly: true},
		{command: "cat-file", readOnly: true},
		{command: "check-ignore", readOnly: true},
		{command: "describe", readOnly: true},
		{command: "diff-tree", readOnly: true},
		{command: "for-each-ref", readOnly: true},
		{command: "ls-tree", readOnly: true},
		{command: "merge-base", readOnly: true},
		{command: "rev-list", readOnly: true},
		{command: "show-ref", readOnly: true},
		{command: "annotate", readOnly: true},
		{command: "check-attr", readOnly: true},
		{command: "check-mailmap", readOnly: true},
		{command: "check-ref-format", readOnly: true},
		{command: "count-objects", readOnly: true},
		{command: "diff-files", readOnly: true},
		{command: "diff-index", readOnly: true},
		{command: "ls-remote", readOnly: true},
		{command: "name-rev", readOnly: true},
		{command: "range-diff", readOnly: true},
		{command: "shortlog", readOnly: true},
		{command: "show-branch", readOnly: true},
		{command: "verify-commit", readOnly: true},
		{command: "verify-tag", readOnly: true},
		{command: "whatchanged", readOnly: true},
		{command: "commit"},
		{command: "fetch"},
		{command: "pull"},
		{command: "push"},
		{command: "merge"},
		{command: "rebase"},
		{command: "cherry-pick"},
		{command: "am"},
		{command: "stash"},
		{command: "add"},
		{command: "tag", rest: []string{"-a", "v1"}},
		{command: "hash-object", rest: []string{"-w", "file"}},
		{command: "custom-alias"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			t.Parallel()
			args := BrokeredGitArgs{
				Config:     []string{"gc.auto=1", "maintenance.auto=true", "gc.autoDetach=true", "maintenance.autoDetach=true"},
				Subcommand: tc.command,
				Rest:       tc.rest,
			}
			command := buildRelayedGitCommand("/var/www", args, "testnonce", false, "https")
			fragment := "'-c' 'http.proxy=' '-c' 'gc.autoDetach=false' '-c' 'maintenance.autoDetach=false' "
			if tc.readOnly {
				fragment = "'-c' 'http.proxy=' '-c' 'gc.auto=0' '-c' 'maintenance.auto=false' "
			}
			fragment += ShellQuote(tc.command)
			quoted := ShellQuote(fragment)
			if !strings.Contains(command, quoted[1:len(quoted)-1]) {
				t.Errorf("relayed git %s lacks final configuration %s", tc.command, fragment)
			}
		})
	}
	t.Run("script writing objects", func(t *testing.T) {
		t.Parallel()
		command := buildRelayedScriptCommand("git fetch origin", "testnonce", []string{"https"})
		fragment := "GIT_CONFIG_KEY_3='gc.autoDetach' GIT_CONFIG_VALUE_3='false' GIT_CONFIG_KEY_4='maintenance.autoDetach' GIT_CONFIG_VALUE_4='false'"
		quoted := ShellQuote(fragment)
		if !strings.Contains(command, quoted[1:len(quoted)-1]) {
			t.Error("relayed script does not keep maintenance in the foreground")
		}
	})
}
