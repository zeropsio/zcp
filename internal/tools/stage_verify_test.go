package tools

import (
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/deploy_ssh.go | if len(builds) == 0 { → if len(builds) != 0 {
func TestStageWriter_SVFTargets_Push_OrdersEveryBuild(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		builds []topology.StageRef
		want   []string
	}{
		{"no builds", nil, []string{"dev"}},
		{"one build", []topology.StageRef{{Hostname: "stage"}}, []string{"stage"}},
		{"two builds", []topology.StageRef{{Hostname: "web"}, {Hostname: "api"}}, []string{"web", "api"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deployBuildHosts("dev", tc.builds); !slices.Equal(got, tc.want) {
				t.Errorf("hosts = %v, want %v", got, tc.want)
			}
		})
	}
}

// witness: internal/tools/verify.go | if before != after { → if before == after {
func TestStageVerify_SVFIdentity_StableVersionAndCaller(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, before, after, named string
		last                       workflow.DeployAttempt
		relation                   stageRelation
		commit                     string
	}{
		{"same deploy", "v1", "v1", "abcdef0", workflow.DeployAttempt{AppVersionID: "v1", SHA: "abcdef012345"}, stageSame, "abcdef012345"},
		{"other deploy", "v1", "v1", "abcdef0", workflow.DeployAttempt{AppVersionID: "v2", SHA: "1234567"}, stageOther, "abcdef0"},
		{"no last deploy", "v1", "v1", "abcdef0", workflow.DeployAttempt{}, stageNoLast, "abcdef0"},
		{"dirty deploy", "v1", "v1", "abcdef0", workflow.DeployAttempt{AppVersionID: "v1", SHA: "abcdef0", Dirty: true}, stageSame, ""},
		{"no commit", "v1", "v1", "", workflow.DeployAttempt{AppVersionID: "v1"}, stageSame, ""},
		{"moved", "v1", "v2", "abcdef0", workflow.DeployAttempt{AppVersionID: "v1"}, stageMoved, "abcdef0"},
		{"unreadable before", "", "v1", "", workflow.DeployAttempt{}, stageUnknown, ""},
		{"unreadable after", "v1", "", "", workflow.DeployAttempt{}, stageUnknown, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			relation := stageVersionRelation(tt.before, tt.after, tt.last.AppVersionID)
			if relation != tt.relation {
				t.Errorf("relation = %v, want %v", relation, tt.relation)
			}
			if got := stageCheckedCommit(tt.named, tt.last, relation); got != tt.commit {
				t.Errorf("checked commit = %q, want %q", got, tt.commit)
			}
		})
	}
}

// witness: internal/tools/verify.go | 0123456789abcdefABCDEF → 0123456789abcdeABCDEF
func TestStageCommit_SVFCommit_OnlyCleanNames(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, want string }{
		{"main abcdef0", "abcdef0"},
		{"zcp 0123456789ab", "0123456789ab"},
		{"main ABCDEF0", "ABCDEF0"},
		{"main abcdef0-dirty", ""},
		{"main abcdef", ""},
		{"main abcdefg", ""},
		{"abcdef0", ""},
		{"main abcdef0 extra", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stageVersionCommit(tt.name); got != tt.want {
				t.Errorf("stageVersionCommit(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
