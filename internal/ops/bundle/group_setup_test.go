package bundle

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// witness: internal/ops/bundle/group.go | why = groupUndeclaredStageSetup(r, setups) →
func TestGroupSetup_GSUndeclared_WithholdsTiersUntilDeclared(t *testing.T) {
	t.Parallel()
	const declared = `zerops:
  - setup: apppool
  - setup: app
    run:
      base: nodejs@22
`
	const variant = `  - setup: app-preview
    extends: app
    run:
      envVariables:
        FRAME_ANCESTORS: "'self' https://mate.zerops.io https://app-123.prg1.zerops.app"
`
	tests := []struct {
		name          string
		body          string
		stageSetup    string
		devPool       bool
		secondRuntime bool
		secondStage   bool
		wantError     []string
		forbidError   []string
		wantWarning   string
		wantAgent     string
		wantGroup     string
	}{
		{
			name: "pool waits for a variant on a change", body: declared, stageSetup: "app-preview", devPool: true,
			wantError: []string{`runtime "apppool"`, `stage "appstage"`, `setup "app-preview"`,
				"declared: apppool, app", "AI Agent", "Stage", "Small Production", "main's tiers stay",
				"pass composes again", "main's zerops.yaml", "merge"},
			forbidError: []string{"is not recorded", "set-default-setup"},
		},
		{
			name: "declared variant composes", body: declared + variant, stageSetup: "app-preview", devPool: true,
			wantAgent: "app-preview", wantGroup: "app",
		},
		{
			name: "another resolved runtime does not make a partial recipe", body: declared, stageSetup: "app-preview",
			devPool: true, secondRuntime: true, wantError: []string{`stage "appstage"`, `setup "app-preview"`, "Small Production"},
		},
		{
			name: "unread yaml leaves the recorded setup unverified", stageSetup: "app-preview", devPool: true,
			wantWarning: "unverified", wantAgent: "app-preview", wantGroup: "app-preview",
		},
		{
			name: "unparseable yaml leaves the recorded setup unverified", body: "zerops: [", stageSetup: "app-preview", devPool: true,
			wantWarning: "unverified", wantAgent: "app-preview", wantGroup: "app-preview",
		},
		{
			name: "dev service waits for its own file", body: declared, stageSetup: "app-preview",
			wantError:   []string{`runtime "appdev"`, `stage "appstage"`, `setup "app-preview"`, "once its zerops.yaml declares the setup"},
			forbidError: []string{"main's zerops.yaml", "merge", "is not recorded", "set-default-setup"},
		},
		{
			name: "second stage is checked", body: declared, stageSetup: "app", devPool: true, secondStage: true,
			wantError: []string{`stage "workerstage"`, `setup "worker-preview"`, "declared: apppool, app"},
		},
		{
			name: "unrecorded stage is resolved from the file", body: declared, devPool: true,
			wantAgent: "app", wantGroup: "app",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dev := "appdev"
			serviceType := "nodejs@22"
			if tt.devPool {
				dev = "apppool"
				serviceType = "swarm@1"
			}
			runtimes := []GroupRuntime{{
				DevHostname: dev, DevPool: tt.devPool, ServiceType: serviceType,
				RepoURL: "https://git.example.org/app.git", SetupName: "apppool", ZeropsYAMLBody: tt.body,
				Stages: []GroupStage{{Hostname: "appstage", SetupName: tt.stageSetup, ServiceType: "nodejs@22"}},
			}}
			if tt.secondStage {
				runtimes[0].Stages = append(runtimes[0].Stages, GroupStage{Hostname: "workerstage", SetupName: "worker-preview", ServiceType: "nodejs@22"})
			}
			if tt.secondRuntime {
				runtimes = append(runtimes, GroupRuntime{
					DevHostname: "workerpool", DevPool: true, ServiceType: "swarm@1", RepoURL: "https://git.example.org/worker.git",
					SetupName: "workerpool", ZeropsYAMLBody: "zerops:\n  - setup: workerpool\n  - setup: worker\n",
					Stages: []GroupStage{{Hostname: "workerstage", SetupName: "worker", ServiceType: "nodejs@22"}},
				})
			}
			layout, warnings, err := BuildGroupRecipe(GroupRecipeInputs{Name: "app", Runtimes: runtimes})
			if len(tt.wantError) > 0 {
				if err == nil || len(layout.Tiers) != 0 || layout.Name != "" {
					t.Fatalf("layout name = %q, tiers = %d, error = %v; want no layout and an error", layout.Name, len(layout.Tiers), err)
				}
				for _, part := range tt.wantError {
					if !strings.Contains(err.Error(), part) {
						t.Errorf("error %q does not name %q", err, part)
					}
				}
				for _, part := range tt.forbidError {
					if strings.Contains(err.Error(), part) {
						t.Errorf("error %q must not say %q", err, part)
					}
				}
				return
			}
			if err != nil || len(layout.Tiers) != 3 {
				t.Fatalf("tiers = %d, error = %v; want three tiers", len(layout.Tiers), err)
			}
			if tt.wantWarning != "" && !strings.Contains(strings.Join(warnings, "\n"), tt.wantWarning) {
				t.Errorf("warnings = %v, want %q", warnings, tt.wantWarning)
			}
			want := map[string]struct{ host, setup string }{
				"AI Agent": {"appstage", tt.wantAgent}, "Stage": {"app", tt.wantGroup}, "Small Production": {"app", tt.wantGroup},
			}
			for _, tier := range layout.Tiers {
				entry, ok := want[tier.Title]
				if !ok {
					t.Errorf("unexpected tier %q", tier.Title)
					continue
				}
				var doc struct {
					Services []struct {
						Hostname    string `yaml:"hostname"`
						ZeropsSetup string `yaml:"zeropsSetup"`
					} `yaml:"services"`
				}
				if err := yaml.Unmarshal([]byte(tier.ImportYAML), &doc); err != nil {
					t.Fatalf("parse %s tier: %v", tier.Title, err)
				}
				found := false
				for _, service := range doc.Services {
					if service.Hostname == entry.host {
						found = true
						if service.ZeropsSetup != entry.setup {
							t.Errorf("%s %s setup = %q, want %q", tier.Title, entry.host, service.ZeropsSetup, entry.setup)
						}
					}
				}
				if !found {
					t.Errorf("%s tier lacks %s", tier.Title, entry.host)
				}
				delete(want, tier.Title)
			}
			if len(want) > 0 {
				t.Errorf("missing tiers: %v", want)
			}
		})
	}
}
