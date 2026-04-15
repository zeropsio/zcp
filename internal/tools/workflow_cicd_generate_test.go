package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/workflow"
)

func TestGenerateGitHubActionsWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		targets   []cicdTarget
		branch    string
		wantParts []string
		wantEmpty bool
	}{
		{
			name: "SingleService",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage", Setup: "prod"},
			},
			branch: "main",
			wantParts: []string{
				"name: Deploy to Zerops",
				"branches: [main]",
				"actions/checkout@v4",
				"Install zcli",
				"zerops.io/zcli/install.sh",
				"GITHUB_PATH",
				"Deploy to appstage",
				"zcli push --serviceId abc123 --setup prod",
				"ZEROPS_TOKEN",
			},
		},
		{
			name: "MultipleServices",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage", Setup: "prod"},
				{ServiceID: "def456", Hostname: "apistage", Setup: "prod"},
			},
			branch: "main",
			wantParts: []string{
				"zcli push --serviceId abc123 --setup prod",
				"zcli push --serviceId def456 --setup prod",
				"Deploy to appstage",
				"Deploy to apistage",
			},
		},
		{
			name: "CustomBranch",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage", Setup: "prod"},
			},
			branch: "production",
			wantParts: []string{
				"branches: [production]",
			},
		},
		{
			name:      "EmptyTargets",
			targets:   nil,
			branch:    "main",
			wantEmpty: true,
		},
		{
			name: "DefaultBranch",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage", Setup: "prod"},
			},
			branch: "",
			wantParts: []string{
				"branches: [main]",
			},
		},
		{
			name: "PlaceholderServiceID",
			targets: []cicdTarget{
				{ServiceID: "", Hostname: "appstage", Setup: "prod"},
			},
			branch: "main",
			wantParts: []string{
				"zcli push --serviceId {SERVICE_ID} --setup prod",
			},
		},
		{
			name: "DevSetup",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "apidev", Setup: "dev"},
			},
			branch: "main",
			wantParts: []string{
				"zcli push --serviceId abc123 --setup dev",
				"Deploy to apidev",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := generateGitHubActionsWorkflow(tt.targets, tt.branch)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got:\n%s", got)
				}
				return
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(got, part) {
					t.Errorf("want substring %q in:\n%s", part, got)
				}
			}
		})
	}
}

func TestGenerateGitHubActionsWorkflow_SingleInstall(t *testing.T) {
	t.Parallel()
	targets := []cicdTarget{
		{ServiceID: "abc", Hostname: "appstage", Setup: "prod"},
		{ServiceID: "def", Hostname: "apistage", Setup: "prod"},
	}
	got := generateGitHubActionsWorkflow(targets, "main")
	if count := strings.Count(got, "Install zcli"); count != 1 {
		t.Errorf("want exactly 1 install step, got %d in:\n%s", count, got)
	}
}

func TestBuildCICDTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		services  map[string]string // hostname → serviceID
		wantCount int
		wantHost  string
		wantID    string
		wantSetup string
	}{
		{
			name: "StageTarget",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "appdev",
					StageHostname:  "appstage",
					Mode:           workflow.PlanModeStandard,
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			services:  map[string]string{"appstage": "svc-abc123"},
			wantCount: 1,
			wantHost:  "appstage",
			wantID:    "svc-abc123",
			wantSetup: "prod",
		},
		{
			name: "DirectTargetDevMode",
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
			services:  map[string]string{"apidev": "svc-def456"},
			wantCount: 1,
			wantHost:  "apidev",
			wantID:    "svc-def456",
			wantSetup: "dev",
		},
		{
			name: "DirectTargetSimpleMode",
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
			services:  map[string]string{"myapp": "svc-ghi789"},
			wantCount: 1,
			wantHost:  "myapp",
			wantID:    "svc-ghi789",
			wantSetup: "prod",
		},
		{
			name: "SkipNonPushGit",
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
			services:  map[string]string{"appdev": "svc-xxx"},
			wantCount: 0,
		},
		{
			name: "MissingServiceID",
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
			services:  map[string]string{},
			wantCount: 1,
			wantHost:  "appstage",
			wantID:    "",
			wantSetup: "prod",
		},
		{
			name: "EmptyModeDefaultsProd",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				writeMeta(t, dir, &workflow.ServiceMeta{
					Hostname:       "apidev",
					DeployStrategy: workflow.StrategyPushGit,
					BootstrappedAt: "2026-01-01",
				})
				return dir
			},
			services:  map[string]string{"apidev": "svc-xxx"},
			wantCount: 1,
			wantHost:  "apidev",
			wantID:    "svc-xxx",
			wantSetup: "prod",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setup(t)
			targets := buildCICDTargets(dir, tt.services)
			if len(targets) != tt.wantCount {
				t.Fatalf("want %d targets, got %d: %+v", tt.wantCount, len(targets), targets)
			}
			if tt.wantCount > 0 {
				if targets[0].Hostname != tt.wantHost {
					t.Errorf("want hostname %q, got %q", tt.wantHost, targets[0].Hostname)
				}
				if targets[0].ServiceID != tt.wantID {
					t.Errorf("want serviceID %q, got %q", tt.wantID, targets[0].ServiceID)
				}
				if targets[0].Setup != tt.wantSetup {
					t.Errorf("want setup %q, got %q", tt.wantSetup, targets[0].Setup)
				}
			}
		})
	}
}
