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
				{ServiceID: "abc123", Hostname: "appstage"},
			},
			branch: "main",
			wantParts: []string{
				"name: Deploy to Zerops",
				"branches: [main]",
				"zeropsio/actions@main",
				"service-id: abc123",
				"# appstage",
			},
		},
		{
			name: "MultipleServices",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage"},
				{ServiceID: "def456", Hostname: "apistage"},
			},
			branch: "main",
			wantParts: []string{
				"service-id: abc123",
				"service-id: def456",
				"# appstage",
				"# apistage",
			},
		},
		{
			name: "CustomBranch",
			targets: []cicdTarget{
				{ServiceID: "abc123", Hostname: "appstage"},
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
				{ServiceID: "abc123", Hostname: "appstage"},
			},
			branch: "",
			wantParts: []string{
				"branches: [main]",
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

func TestBuildCICDTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T) string
		services  map[string]string // hostname → serviceID
		wantCount int
		wantHost  string
		wantID    string
	}{
		{
			name: "StageTarget",
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
			services:  map[string]string{"appstage": "svc-abc123"},
			wantCount: 1,
			wantHost:  "appstage",
			wantID:    "svc-abc123",
		},
		{
			name: "DirectTarget",
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
			services:  map[string]string{"apidev": "svc-def456"},
			wantCount: 1,
			wantHost:  "apidev",
			wantID:    "svc-def456",
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
			services:  map[string]string{}, // no service IDs available
			wantCount: 1,
			wantHost:  "appstage",
			wantID:    "", // empty = placeholder
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
			}
		})
	}
}
