package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestLaunchSetupNameP5_NoDefaultPipelineConstant pins the plan §P5
// grep verification: the `defaultPipelineZeropsYamlSetup` constant
// MUST NOT exist anywhere under internal/. The constant was deleted as
// part of the launch-side cleanup; its presence would mean the
// convention is creeping back in through a forgotten reference.
func TestLaunchSetupNameP5_NoDefaultPipelineConstant(t *testing.T) {
	t.Parallel()
	root := repoRootFromHere(t)
	internal := filepath.Join(root, "internal")

	var hits []string
	walkErr := filepath.Walk(internal, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip this enforcement test itself (it references the
		// deleted constant by name to assert the negative).
		if strings.HasSuffix(path, "launch_setup_name_p5_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "defaultPipelineZeropsYamlSetup") {
			hits = append(hits, strings.TrimPrefix(path, root+"/"))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	if len(hits) > 0 {
		t.Errorf("defaultPipelineZeropsYamlSetup must be deleted (plan §P5) — found references at:\n  %s",
			strings.Join(hits, "\n  "))
	}
}

// resolveLaunchSetupName cascade table — pins precedence per
// resolvedLaunchRuntime.SetupName doc + the deferred legacy "prod"
// tail (plan §P5 partial).
func TestResolveLaunchSetupName_CascadePrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		promotable       LaunchPromotableInput
		workflowOverride string
		meta             *workflow.ServiceMeta
		want             string
		wantProvenance   string
	}{
		{
			name:             "per-promotable override wins over all",
			promotable:       LaunchPromotableInput{Hostname: "app", ProdSetupNameOverride: "custom"},
			workflowOverride: "workflow-level",
			meta:             &workflow.ServiceMeta{StageSetupName: "stage-name", PrimarySetupName: "primary-name"},
			want:             "custom",
			wantProvenance:   setupProvenanceOverride,
		},
		{
			name:             "workflow-level override beats meta",
			promotable:       LaunchPromotableInput{Hostname: "app"},
			workflowOverride: "workflow-level",
			meta:             &workflow.ServiceMeta{StageSetupName: "stage-name"},
			want:             "workflow-level",
			wantProvenance:   setupProvenanceOverride,
		},
		{
			name:           "meta.StageSetupName beats meta.PrimarySetupName",
			promotable:     LaunchPromotableInput{Hostname: "app"},
			meta:           &workflow.ServiceMeta{StageSetupName: "stage-name", PrimarySetupName: "primary-name"},
			want:           "stage-name",
			wantProvenance: setupProvenanceStageSetup,
		},
		{
			name:           "meta.ProdSetupName is the recorded-prod source",
			promotable:     LaunchPromotableInput{Hostname: "app"},
			meta:           &workflow.ServiceMeta{ProdSetupName: "prod", StageSetupName: "stage-name"},
			want:           "prod",
			wantProvenance: setupProvenanceRecordedProd,
		},
		{
			name:           "meta.PrimarySetupName when stage empty",
			promotable:     LaunchPromotableInput{Hostname: "app"},
			meta:           &workflow.ServiceMeta{PrimarySetupName: "primary-name"},
			want:           "primary-name",
			wantProvenance: setupProvenanceDevPromoted,
		},
		{
			name:           "deferred prod fallback when meta empty + no override",
			promotable:     LaunchPromotableInput{Hostname: "app"},
			meta:           &workflow.ServiceMeta{},
			want:           "prod",
			wantProvenance: setupProvenanceDefault,
		},
		{
			name:           "deferred prod fallback when meta nil",
			promotable:     LaunchPromotableInput{Hostname: "app"},
			meta:           nil,
			want:           "prod",
			wantProvenance: setupProvenanceDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, provenance := resolveLaunchSetupName(tt.promotable, tt.workflowOverride, tt.meta)
			if got != tt.want {
				t.Errorf("resolveLaunchSetupName: got %q, want %q", got, tt.want)
			}
			if provenance != tt.wantProvenance {
				t.Errorf("provenance: got %q, want %q", provenance, tt.wantProvenance)
			}
		})
	}
}

// launchTargetSetupName cascade (sister helper for the source-control
// gate + pipelineCheckInputs construction). Same precedence as
// resolveLaunchSetupName but takes WorkflowInput directly.
func TestLaunchTargetSetupName_CascadePrecedence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "apistage",
		StageSetupName:   "stage-from-meta",
		PrimarySetupName: "primary-from-meta",
	}); err != nil {
		t.Fatal(err)
	}

	t.Run("override beats meta", func(t *testing.T) {
		t.Parallel()
		got := launchTargetSetupName(stateDir, "apidev",
			WorkflowInput{ProdSetupNameOverride: "explicit"})
		if got != "explicit" {
			t.Errorf("got %q, want explicit", got)
		}
	})
	t.Run("meta.StageSetupName when no override", func(t *testing.T) {
		t.Parallel()
		got := launchTargetSetupName(stateDir, "apidev", WorkflowInput{})
		if got != "stage-from-meta" {
			t.Errorf("got %q, want stage-from-meta", got)
		}
	})
	t.Run("prod fallback when meta absent", func(t *testing.T) {
		t.Parallel()
		got := launchTargetSetupName(stateDir, "non-existent-host", WorkflowInput{})
		if got != "prod" {
			t.Errorf("got %q, want prod (deferred legacy default)", got)
		}
	})
}

// repoRootFromHere walks up from the current test file's working
// directory to the repo root (looks for go.mod). Used by the
// constant-grep test to scan internal/ from a deterministic root.
func repoRootFromHere(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod up the tree)")
		}
		dir = parent
	}
}
