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

// witness: internal/tools/standup_report.go | each dev copy's Preview is up → this Mate's preview is up
func TestStandupReport_SRTerms_SeparatesPreviewAndStage(t *testing.T) {
	t.Parallel()
	stage := workflow.MateTierRuntime{Hostname: "webstage", Setup: "web"}
	deployed := &standupDeploy{Status: standupDeployed, URL: "https://stage.example"}
	pair := &standupPair{pair: workflow.MateTierPair{Dev: workflow.MateTierRuntime{Hostname: "webdev"}, Stages: []workflow.MateTierRuntime{stage}},
		devDeploy: &standupDeploy{Status: standupDeployed}, stages: []*standupStage{{rt: stage, deploy: deployed}}}
	for _, tc := range []struct {
		name string
		text string
		want []string
	}{
		{"classic ready", buildStandupResponse(standupSource{}, []*standupPair{pair}, nil, pooledOutcome{}).Next, []string{"each part's stage run main", "each dev copy's Preview is up"}},
		{"stages queued", standupDevelopmentNext, []string{"each part's stage from main", "Preview of each dev copy"}},
		{"pooled built", standupPooledNext(pooledOutcome{verdict: mate.StandupVerdict{Headline: mate.HeadlineDone}}), []string{"each part's stage is set up", "dev copy's Preview"}},
		{"pooled stage not built", standupPooledNext(pooledOutcome{verdict: mate.StandupVerdict{Headline: mate.HeadlineReady}}), []string{"a stage is not built", "Preview is its dev copy's running dev server"}},
		{"pooled failed", standupPooledNext(pooledOutcome{verdict: mate.StandupVerdict{Headline: mate.HeadlineFailed}}), []string{"verdict row names its state"}},
		{"stage service", standupStageService(pair, pair.stages[0]).Next, []string{"part's stage running main", "https://stage.example"}},
		{"pooled plan", pooledPlanStoodLine(mate.HeadlineReady, nil, nil), []string{"stage that is not built", "Preview is its dev copy's running dev server"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range tc.want {
				if !strings.Contains(tc.text, want) {
					t.Errorf("report %q lacks %q", tc.text, want)
				}
			}
			for _, wrong := range []string{"this Mate's preview", "part has no preview", "preview of main"} {
				if strings.Contains(tc.text, wrong) {
					t.Errorf("report %q contains %q", tc.text, wrong)
				}
			}
		})
	}
}
