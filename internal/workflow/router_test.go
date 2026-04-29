package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestRoute_EmptyProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		input          RouterInput
		wantTop        string
		wantMinOffered int
	}{
		{
			name:           "no services, no metas, no sessions",
			input:          RouterInput{},
			wantTop:        "bootstrap",
			wantMinOffered: 3, // bootstrap + recipe + scale
		},
		{
			name: "active bootstrap session offers resume",
			input: RouterInput{
				ActiveSessions: []SessionEntry{{Workflow: "bootstrap", SessionID: "abc123"}},
			},
			wantTop: "bootstrap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected at least one offering")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			if tt.wantMinOffered > 0 && len(offerings) < tt.wantMinOffered {
				t.Errorf("count = %d, want >= %d", len(offerings), tt.wantMinOffered)
			}
		})
	}
}

func TestRoute_AllBootstrapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "git-push close-mode",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeGitPush,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "develop",
		},
		{
			name: "auto close-mode",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeAuto,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "develop",
		},
		{
			name: "manual close-mode — develop still offered",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{{
					Hostname: "appdev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeManual,
				}},
				LiveServices: []string{"appdev"},
			},
			wantTop: "develop", // Deploy always offered — close-mode is informational, not a gate
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected at least one offering")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			// Should always have bootstrap available.
			hasBootstrap := false
			for _, o := range offerings {
				if o.Workflow == "bootstrap" {
					hasBootstrap = true
				}
			}
			if !hasBootstrap {
				t.Error("expected bootstrap in offerings")
			}
		})
	}
}

func TestRoute_UnmanagedRuntimes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "only unmanaged runtimes — adoption",
			input: RouterInput{
				UnmanagedRuntimes: []string{"appdev", "appstage"},
			},
			wantTop: "bootstrap",
		},
		{
			name: "mix bootstrapped and unmanaged — adoption first",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{{
					Hostname: "apidev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeAuto,
				}},
				LiveServices:      []string{"apidev", "appdev"},
				UnmanagedRuntimes: []string{"appdev"},
			},
			wantTop: "bootstrap",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected offerings")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
			// Adoption hint should mention the unmanaged hostnames.
			if !strings.Contains(offerings[0].Hint, "adopt") {
				t.Errorf("hint = %q, want to contain 'adopt'", offerings[0].Hint)
			}
		})
	}
}

func TestRoute_UnmanagedWithStrategy(t *testing.T) {
	t.Parallel()
	// When both unmanaged runtimes and bootstrapped services exist,
	// adoption is p1 but develop should also appear.
	input := RouterInput{
		ServiceMetas: []*ServiceMeta{{
			Hostname: "apidev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeAuto,
		}},
		LiveServices:      []string{"apidev", "appdev"},
		UnmanagedRuntimes: []string{"appdev"},
	}
	offerings := Route(input)
	hasDeploy := false
	for _, o := range offerings {
		if o.Workflow == "develop" {
			hasDeploy = true
		}
	}
	if !hasDeploy {
		t.Error("expected develop offering alongside adoption")
	}
}

func TestRoute_AlwaysIncludesUtilities(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input RouterInput
	}{
		{"empty", RouterInput{}},
		{"bootstrapped", RouterInput{
			ServiceMetas: []*ServiceMeta{{Hostname: "a", BootstrappedAt: "2026-01-01"}},
			LiveServices: []string{"a"},
		}},
		{"unmanaged", RouterInput{UnmanagedRuntimes: []string{"x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			hasScale := false
			for _, o := range offerings {
				if o.Workflow == "scale" {
					hasScale = true
				}
				if o.Workflow == "debug" || o.Workflow == "configure" {
					t.Errorf("removed workflow %q should not appear in offerings", o.Workflow)
				}
			}
			if !hasScale {
				t.Error("missing utility workflow \"scale\"")
			}

			hasRecipe := false
			for _, o := range offerings {
				if o.Workflow == "recipe" {
					hasRecipe = true
					if o.Priority != 4 {
						t.Errorf("recipe priority = %d, want 4", o.Priority)
					}
				}
			}
			if !hasRecipe {
				t.Error("missing utility workflow \"recipe\"")
			}
		})
	}
}

func TestRoute_GitPush_DevelopHintMentionsGitPush(t *testing.T) {
	t.Parallel()
	// Every meta reaches Route via parseMeta with CloseDeployMode populated.
	input := RouterInput{
		ServiceMetas: []*ServiceMeta{{
			Hostname:        "appdev",
			BootstrappedAt:  "2026-01-01",
			CloseDeployMode: topology.CloseModeGitPush,
		}},
		LiveServices: []string{"appdev"},
	}
	offerings := Route(input)
	var developHint string
	for _, o := range offerings {
		if o.Workflow == "develop" {
			developHint = o.Hint
		}
	}
	if developHint == "" {
		t.Fatal("expected develop offering")
	}
	// When git-push close-mode is dominant, the develop hint must tell the
	// agent that pushing to a git remote requires starting this workflow first.
	wantParts := []string{"git", "remote", "push"}
	for _, part := range wantParts {
		if !strings.Contains(strings.ToLower(developHint), part) {
			t.Errorf("develop hint should mention %q for git-push close-mode, got: %s", part, developHint)
		}
	}
}

func TestRoute_AutoCloseMode_DevelopHintNoGitMention(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ServiceMetas: []*ServiceMeta{{
			Hostname:        "appdev",
			BootstrappedAt:  "2026-01-01",
			CloseDeployMode: topology.CloseModeAuto,
		}},
		LiveServices: []string{"appdev"},
	}
	offerings := Route(input)
	var developHint string
	for _, o := range offerings {
		if o.Workflow == "develop" {
			developHint = o.Hint
		}
	}
	if developHint == "" {
		t.Fatal("expected develop offering")
	}
	// Auto close-mode should NOT have the git-specific hint.
	if strings.Contains(strings.ToLower(developHint), "git remote") {
		t.Errorf("auto close-mode develop hint should NOT mention git remote, got: %s", developHint)
	}
}

func TestRoute_StaleMetaFiltering(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ServiceMetas: []*ServiceMeta{
			{Hostname: "appdev", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeGitPush},
			{Hostname: "staleservice", BootstrappedAt: "2026-01-01", CloseDeployMode: topology.CloseModeAuto},
		},
		LiveServices: []string{"appdev"},
	}
	offerings := Route(input)
	if offerings[0].Workflow != "develop" {
		t.Errorf("top = %q, want develop (stale meta should be filtered, git-push dominant)", offerings[0].Workflow)
	}
}

func TestRoute_ResumeHint(t *testing.T) {
	t.Parallel()
	input := RouterInput{
		ActiveSessions: []SessionEntry{{Workflow: "bootstrap", SessionID: "abc123"}},
	}
	offerings := Route(input)
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}
	if !strings.Contains(offerings[0].Hint, "resume") {
		t.Errorf("hint = %q, want to contain 'resume'", offerings[0].Hint)
	}
}

func TestRoute_IncompleteMetas(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   RouterInput
		wantTop string
	}{
		{
			name: "incomplete meta suggests bootstrap",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{
					{Hostname: "appdev", Mode: topology.PlanModeDev},
				},
				LiveServices: []string{"appdev"},
			},
			wantTop: "bootstrap",
		},
		{
			name: "complete meta routes to develop",
			input: RouterInput{
				ServiceMetas: []*ServiceMeta{
					{Hostname: "appdev", BootstrappedAt: "2026-03-04", CloseDeployMode: topology.CloseModeAuto},
				},
				LiveServices: []string{"appdev"},
			},
			wantTop: "develop",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			offerings := Route(tt.input)
			if len(offerings) == 0 {
				t.Fatal("expected offerings")
			}
			if offerings[0].Workflow != tt.wantTop {
				t.Errorf("top = %q, want %q", offerings[0].Workflow, tt.wantTop)
			}
		})
	}
}

func TestRoute_NoReasonField(t *testing.T) {
	t.Parallel()
	// Verify FlowOffering has no Reason field — facts only, no editorial.
	offering := FlowOffering{Workflow: "develop", Priority: 1, Hint: "test"}
	_ = offering.Workflow
	_ = offering.Priority
	_ = offering.Hint
}

func TestFormatOfferings_Compact(t *testing.T) {
	t.Parallel()
	offerings := []FlowOffering{
		{Workflow: "bootstrap", Priority: 1, Hint: `zerops_workflow action="start" workflow="bootstrap"`},
		{Workflow: "scale", Priority: 5, Hint: `zerops_scale serviceHostname="..."`},
	}
	result := FormatOfferings(offerings)
	if result == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(result, "bootstrap") {
		t.Error("missing bootstrap")
	}
	lines := strings.Split(strings.TrimSpace(result), "\n")
	if len(lines) > 8 {
		t.Errorf("has %d lines, want <= 8", len(lines))
	}
}

func TestFormatOfferings_Empty(t *testing.T) {
	t.Parallel()
	result := FormatOfferings(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}
