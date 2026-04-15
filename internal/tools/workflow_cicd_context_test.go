package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func writeMeta(t *testing.T, dir string, meta *workflow.ServiceMeta) {
	t.Helper()
	svcDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(svcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcDir, meta.Hostname+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildCICDContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		want      string
		wantEmpty bool
	}{
		{
			name: "WithStage",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "appdev",
					StageHostname:  "appstage",
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			want: "appdev -> appstage (setup=prod)",
		},
		{
			name: "IncludesGeneratedWorkflow",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "appdev",
					StageHostname:  "appstage",
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			want: "zcli push",
		},
		{
			name: "NoStage",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "apidev",
					Mode:           workflow.PlanModeDev,
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			want: "apidev (setup=dev)",
		},
		{
			name: "NoCICDServices",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "appdev",
					DeployStrategy: workflow.StrategyPushDev,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			wantEmpty: true,
		},
		{
			name: "NoMetas",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			wantEmpty: true,
		},
		{
			name: "MixedStrategies",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "appdev",
					DeployStrategy: workflow.StrategyPushGit,
					StageHostname:  "appstage",
					BootstrappedAt: "2026-01-01",
				})
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "workerdev",
					DeployStrategy: workflow.StrategyPushDev,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			want: "appdev -> appstage",
		},
		{
			name: "EmptyStateDir",
			setup: func(_ *testing.T) string {
				return ""
			},
			wantEmpty: true,
		},
		{
			name: "SimpleMode",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "myapp",
					Mode:           workflow.PlanModeSimple,
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			want: "myapp (setup=prod)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setup(t)
			got := buildCICDContext(dir)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("want substring %q in:\n%s", tt.want, got)
			}
			if !strings.Contains(got, "## Your CI/CD Targets") {
				t.Errorf("missing header in:\n%s", got)
			}
		})
	}
}
