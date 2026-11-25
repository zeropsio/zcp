package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/standup_report.go | if containers == 0 && pools > 0 { → if containers != 0 && pools > 0 {
func TestStandupReport_SRPooled_SaysTheRowsNotThePairs(t *testing.T) {
	t.Parallel()
	pool := &standupPair{pair: workflow.MateTierPair{DevPool: true,
		Dev:    workflow.MateTierRuntime{Hostname: "hellopool", Setup: "hellopool"},
		Stages: []workflow.MateTierRuntime{{Hostname: "hellostage", Setup: "hello"}},
	}}
	stage := workflow.MateTierRuntime{Hostname: "webstage", Setup: "web"}
	container := &standupPair{pair: workflow.MateTierPair{Dev: workflow.MateTierRuntime{Hostname: "webdev"}, Stages: []workflow.MateTierRuntime{stage}},
		devDeploy: &standupDeploy{Status: standupDeployed}, stages: []*standupStage{{rt: stage, deploy: &standupDeploy{Status: standupDeployed}}}}
	pooled := pooledOutcome{verdict: mate.StandupVerdict{Headline: mate.HeadlineFailed, Rows: []mate.StandupRow{
		{Hostname: "hellopool", Kind: mate.RowPool, State: mate.RowFailed, Class: platform.ClassRefused, Cause: "pool build.base refused", ProcessID: "process-1", AppVersionID: "version-1"},
		{Hostname: "hellostage", Kind: mate.RowStage, State: mate.RowFailed, Class: platform.ClassRefused, Cause: "stage build.base refused"},
	}}, main: map[string]string{"z/repo": "1234567890123", "a/repo": "2345678901234"}, built: map[string]bool{"a/repo": true}}
	for _, tc := range []struct {
		name   string
		pairs  []*standupPair
		want   []string
		absent string
	}{
		{"pooled", []*standupPair{pool}, []string{"verdict is failed", "hellopool failed (refused), process process-1, app version version-1: pool build.base refused", "hellostage failed (refused): stage build.base refused", "a/repo was built at 234567890123", "z/repo's main is at 123456789012"}, "pairs stand"},
		{"mixed", []*standupPair{pool, container}, []string{"1 of 1 pairs stand"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			message := buildStandupResponse(standupSource{}, tc.pairs, nil, pooled).Message
			for _, want := range tc.want {
				if !strings.Contains(message, want) {
					t.Errorf("message %q lacks %q", message, want)
				}
			}
			if tc.absent != "" && strings.Contains(message, tc.absent) {
				t.Errorf("message %q contains %q", message, tc.absent)
			}
			if tc.name == "pooled" && strings.Index(message, "a/repo") > strings.Index(message, "z/repo") {
				t.Errorf("repositories not sorted: %q", message)
			}
		})
	}
}
