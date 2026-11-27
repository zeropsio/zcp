package tools

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// witness: internal/tools/workflow.go | return handleDevelopBriefing(ctx, engine, client, projectID, input, rt, sshDeployer) → if result := activeWorkflowStartError(engine, input.Workflow); result != nil { return result, nil, nil }; return handleDevelopBriefing(ctx, engine, client, projectID, input, rt, sshDeployer)
func TestDevelopStart_WD15_EditBeforeBootstrapGate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		scope      string
		activeHere bool
		wantError  bool
	}{
		{"other conversation's interrupted bootstrap", "dev", false, false},
		{"own active bootstrap and edit", "dev", true, false},
		{"own active bootstrap and deploy", "stage", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
				Hostname: "dev", Mode: topology.ModeStandard, Stages: []topology.StageRef{{Hostname: "stage"}},
				BootstrappedAt: "2026-01-01T00:00:00Z", HQ: &workflow.HQRepoRef{AppID: "app", Repo: "dev"},
			}); err != nil {
				t.Fatalf("write pair: %v", err)
			}
			engine := workflow.NewEngine(dir, workflow.EnvContainer, nil)
			id := "interrupted"
			if tt.activeHere {
				started, err := engine.BootstrapStartWithRoute("project", "bootstrap", workflow.BootstrapRouteClassic, "")
				if err != nil {
					t.Fatalf("start bootstrap: %v", err)
				}
				id = started.SessionID
			} else {
				state := &workflow.WorkflowState{SessionID: id, ProjectID: "project", Workflow: workflow.WorkflowBootstrap, Bootstrap: &workflow.BootstrapState{Active: true, Steps: []workflow.BootstrapStep{{Name: "discover", Status: "in_progress"}}}}
				if err := workflow.SaveSessionState(dir, id, state); err != nil {
					t.Fatalf("save interrupted bootstrap: %v", err)
				}
				if err := workflow.RegisterSession(dir, workflow.SessionEntry{SessionID: id, Workflow: workflow.WorkflowBootstrap, ProjectID: "project"}); err != nil {
					t.Fatalf("register interrupted bootstrap: %v", err)
				}
				engine = workflow.NewEngine(dir, workflow.EnvContainer, nil)
			}
			before, err := workflow.LoadSessionByID(dir, id)
			if err != nil {
				t.Fatalf("load bootstrap: %v", err)
			}
			client := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "dev-id", Name: "dev", Status: platform.ServiceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}, ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "code", Source: "CLI", Built: true}},
				{ID: "stage-id", Name: "stage", Status: platform.ServiceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			}).WithProject(&platform.Project{ID: "project"})
			rt := runtime.Info{InContainer: true, MateEnabled: true, HQ: &runtime.HQLink{Origin: "https://example.invalid", ProjectID: "project", Key: "test"}}
			if !tt.activeHere {
				status, _, err := handleWorkflowAction(context.Background(), "project", engine, client, nil, nil, nil, WorkflowInput{Action: "status"}, dir, "", nil, nil, rt, "", nil)
				if err != nil || status == nil || status.IsError || strings.Contains(extractText(status), id) || strings.Contains(extractText(status), "session-active") {
					t.Fatalf("other conversation's status = %#v, %v", status, err)
				}
			}
			result, _, err := handleStart(context.Background(), "project", engine, client, nil, WorkflowInput{Workflow: workflowDevelop, Scope: []string{tt.scope}}, rt, nil)
			if err != nil || result == nil || result.IsError != tt.wantError {
				t.Fatalf("start = %#v, %v; want error %v", result, err, tt.wantError)
			}
			if tt.wantError && !strings.Contains(extractText(result), string(platform.ErrWorkflowActive)) {
				t.Fatalf("deploy refusal = %q", extractText(result))
			}
			if !tt.wantError && !strings.Contains(extractText(result), "develop-edit") {
				t.Fatalf("edit briefing = %q", extractText(result))
			}
			after, err := workflow.LoadSessionByID(dir, id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("bootstrap changed: before %#v, after %#v, error %v", before, after, err)
			}
			if !tt.activeHere {
				original := workflow.NewEngine(dir, workflow.EnvContainer, nil)
				if resumed, err := original.Resume(id); err != nil || resumed.Bootstrap == nil || !resumed.Bootstrap.Active {
					t.Fatalf("resume original bootstrap = %#v, %v", resumed, err)
				}
				status, _, err := handleWorkflowAction(context.Background(), "project", original, client, nil, nil, nil, WorkflowInput{Action: "status"}, dir, "", nil, nil, rt, "", nil)
				if err != nil || status == nil || status.IsError || !strings.Contains(extractText(status), id) || !strings.Contains(extractText(status), "session-active") {
					t.Fatalf("resumed conversation's status = %#v, %v", status, err)
				}
			}
		})
	}
}

// witness: internal/tools/workflow_develop.go | if service.DevIsPool { → if false && service.DevIsPool {
func TestDevelopScope_WD2_EditOnlyForWiredDevCopies(t *testing.T) {
	t.Parallel()
	pool := workflow.ServiceSnapshot{Hostname: "pool", DevIsPool: true, HQRepo: "app", StageHostname: "stage"}
	classic := workflow.ServiceSnapshot{Hostname: "dev", HQRepo: "app", StageHostname: "stage", Mode: topology.ModeStandard, HasLiveCode: true, Status: platform.ServiceStatusRunning}
	stage := workflow.ServiceSnapshot{Hostname: "stage"}
	leases := []workflow.LeaseSnapshot{{Alias: "pool-1", Pool: "pool", State: ops.LeaseStateHeld}, {Alias: "pool-2", Pool: "pool", State: ops.LeaseStateHeld, Unverified: true}}
	tests := []struct {
		name  string
		env   workflow.StateEnvelope
		scope []string
		want  bool
	}{
		{"pool", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{pool, stage}, Leases: &leases}, []string{"pool"}, true},
		{"held lease", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{pool, stage}, Leases: &leases}, []string{"pool-1"}, true},
		{"unverified lease", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{pool, stage}, Leases: &leases}, []string{"pool-2"}, false},
		{"classic live code", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{classic, stage}}, []string{"dev"}, true},
		{"never deployed", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "dev", HQRepo: "app", StageHostname: "stage", Mode: topology.ModeStandard, Deployed: true, Status: platform.ServiceStatusRunning}, stage}}, []string{"dev"}, false},
		{"stopped classic", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "dev", HQRepo: "app", StageHostname: "stage", Mode: topology.ModeStandard, HasLiveCode: true, Status: "STOPPED"}, stage}}, []string{"dev"}, false},
		{"unwired", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "dev", StageHostname: "stage", Mode: topology.ModeStandard, HasLiveCode: true, Status: platform.ServiceStatusRunning}, stage}}, []string{"dev"}, false},
		{"simple", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "dev", HQRepo: "app", StageHostname: "stage", Mode: topology.ModeSimple, HasLiveCode: true, Status: platform.ServiceStatusRunning}, stage}}, []string{"dev"}, false},
		{"local stage", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "dev", HQRepo: "app", StageHostname: "stage", Mode: topology.ModeLocalStage, HasLiveCode: true, Status: platform.ServiceStatusRunning}, stage}}, []string{"dev"}, false},
		{"missing stage", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{pool}}, []string{"pool"}, false},
		{"stage", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{classic, stage}}, []string{"stage"}, false},
		{"mixed", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{classic, stage}}, []string{"dev", "stage"}, false},
		{"no scope", workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{classic, stage}}, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := developEditScope(tt.scope, tt.env); got != tt.want {
				t.Errorf("developEditScope(%v) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | if !wanted[copy.Name] && (!copy.Lease || !pools[copy.Pool]) { → if !wanted[copy.Name] {
func TestEditCopies_WD3_PoolNamesEveryHeldLease(t *testing.T) {
	t.Parallel()
	env := workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "pool", DevIsPool: true}, {Hostname: "empty", DevIsPool: true}}}
	facts := workflow.WhereFacts{Copies: []workflow.WhereCopy{
		{Name: "pool-1", Pool: "pool", Branch: "one", Lease: true, State: ops.LeaseStateHeld},
		{Name: "pool-2", Pool: "pool", Branch: "two", Lease: true, State: ops.LeaseStateHeld},
		{Name: "other-1", Pool: "other", Branch: "three", Lease: true, State: ops.LeaseStateHeld},
		{Name: "pool-3", Pool: "pool", Lease: true, State: "lost"},
	}}
	tests := []struct {
		name                  string
		scope, names, missing []string
	}{
		{"all held", []string{"pool"}, []string{"pool-1", "pool-2"}, nil},
		{"one alias", []string{"pool-2"}, []string{"pool-2"}, nil},
		{"empty pool", []string{"empty"}, nil, []string{"empty"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, missing := selectEditCopies(facts, env, tt.scope)
			names := make([]string, 0, len(got.Copies))
			for _, copy := range got.Copies {
				names = append(names, copy.Name)
			}
			if !slices.Equal(names, tt.names) || !slices.Equal(missing, tt.missing) {
				t.Errorf("copies = %v, missing = %v; want %v, %v", names, missing, tt.names, tt.missing)
			}
		})
	}
}

// witness: internal/tools/workflow.go | envelope.Phase = workflow.PhaseDevelopEdit → envelope.Phase = workflow.PhaseIdle
func TestEditStatus_WD4_DeployAndEditAreBothVisible(t *testing.T) {
	t.Parallel()
	deploy := workflow.Plan{Primary: workflow.NextAction{Label: "deploy"}}
	edit := workflow.Plan{Primary: workflow.NextAction{Label: "commit"}}
	tests := []struct {
		name      string
		phase     workflow.Phase
		hasEdit   bool
		guidance  []string
		next      string
		wantPhase workflow.Phase
	}{
		{"idle edit", workflow.PhaseIdle, true, []string{"edit"}, "commit", workflow.PhaseDevelopEdit},
		{"active deploy and edit", workflow.PhaseDevelopActive, true, []string{"deploy", "edit"}, "deploy", workflow.PhaseDevelopActive},
		{"deploy alone", workflow.PhaseDevelopActive, false, []string{"deploy"}, "deploy", workflow.PhaseDevelopActive},
		{"idle alone", workflow.PhaseIdle, false, []string{"deploy"}, "deploy", workflow.PhaseIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := workflow.StateEnvelope{Phase: tt.phase}
			matches, plan := addEditStatus(&env, []workflow.MatchedRender{{Body: "deploy"}}, deploy, []workflow.MatchedRender{{Body: "edit"}}, edit, tt.hasEdit)
			guidance := workflow.BodiesOf(matches)
			if !reflect.DeepEqual(guidance, tt.guidance) || plan.Primary.Label != tt.next || env.Phase != tt.wantPhase {
				t.Errorf("status = %v, next %q, phase %q; want %v, %q, %q", guidance, plan.Primary.Label, env.Phase, tt.guidance, tt.next, tt.wantPhase)
			}
			if text := workflow.RenderStatus(workflow.Response{Envelope: env}); !strings.Contains(text, "Phase: "+string(tt.wantPhase)+"\n") {
				t.Errorf("status text = %q, want phase %q", text, tt.wantPhase)
			}
		})
	}
}

// witness: internal/tools/mount.go | if service.Hostname == host && service.DevIsPool { → if false && service.Hostname == host && service.DevIsPool {
func TestEditMount_WD5_DevOrLeaseButNotPool(t *testing.T) {
	t.Parallel()
	services := []workflow.ServiceSnapshot{{Hostname: "pool", DevIsPool: true}, {Hostname: "dev"}}
	tests := []struct {
		name        string
		edit, lease bool
		host        string
		want        bool
	}{
		{"classic dev", true, false, "dev", true},
		{"held lease", true, true, "pool-1", true},
		{"pool", true, false, "pool", false},
		{"not an edit", false, false, "dev", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editMountTarget(tt.edit, tt.lease, services, tt.host); got != tt.want {
				t.Errorf("mount %s = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | editEnvelope.Phase, editEnvelope.WorkSession = workflow.PhaseDevelopEdit, nil → editEnvelope.Phase, editEnvelope.WorkSession = workflow.PhaseDevelopActive, nil
func TestEditBriefing_WD6_ResynthesizesEditAtoms(t *testing.T) {
	t.Parallel()
	corpus := []workflow.KnowledgeAtom{
		{ID: "edit", Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopEdit}}, Body: "edit guidance"},
		{ID: "deploy", Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopActive}}, Body: "deploy guidance"},
	}
	for _, phase := range []workflow.Phase{workflow.PhaseIdle, workflow.PhaseDevelopActive} {
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()
			guidance, _, err := editBriefing(context.Background(), nil, "", "", runtime.Info{}, nil, workflow.StateEnvelope{Phase: phase}, nil, corpus)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(guidance, "\n")
			if !strings.Contains(text, "edit guidance") || strings.Contains(text, "deploy guidance") {
				t.Errorf("edit briefing = %q", text)
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | editEnvelope.Services = editAtomServices(envelope, scope) → editEnvelope.Services = envelope.Services
func TestEditAtoms_WD10_UseTheEditScope(t *testing.T) {
	t.Parallel()
	leases := []workflow.LeaseSnapshot{{Alias: "pool-1", Pool: "pool", State: ops.LeaseStateHeld}}
	env := workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{
		{Hostname: "dev", RuntimeClass: topology.RuntimeDynamic},
		{Hostname: "pool", RuntimeClass: topology.RuntimePool, DevIsPool: true},
	}, Leases: &leases}
	corpus := []workflow.KnowledgeAtom{
		{ID: "common", Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopEdit}}, Body: "common guidance"},
		{ID: "classic", Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopEdit}, Runtimes: []topology.RuntimeClass{topology.RuntimeDynamic}}, Body: "classic guidance"},
		{ID: "pool", Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopEdit}, Runtimes: []topology.RuntimeClass{topology.RuntimePool}}, Body: "pool guidance"},
	}
	tests := []struct {
		name          string
		scope         []string
		classic, pool bool
	}{
		{"classic", []string{"dev"}, true, false},
		{"pool", []string{"pool"}, false, true},
		{"lease", []string{"pool-1"}, false, true},
		{"both", []string{"dev", "pool"}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guidance, _, err := editBriefing(context.Background(), nil, "", "", runtime.Info{}, nil, env, tt.scope, corpus)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Join(guidance, "\n")
			if !strings.Contains(text, "common guidance") || strings.Contains(text, "classic guidance") != tt.classic || strings.Contains(text, "pool guidance") != tt.pool {
				t.Errorf("guidance = %q, want classic %v and pool %v", text, tt.classic, tt.pool)
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | matches = workflow.ComposeUnderBudget(matches, corpus, max(budget, 0)) → matches = matches
func TestEditBudget_WD11_StartAndStatusComposeAllAtoms(t *testing.T) {
	t.Parallel()
	corpus := []workflow.KnowledgeAtom{
		{ID: "edit", Title: "Edit", Priority: 2, Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopEdit}}, Body: "Edit step.\n\n" + strings.Repeat("e", 28000)},
		{ID: "deploy", Title: "Deploy", Priority: 1, Axes: workflow.AxisVector{Phases: []workflow.Phase{workflow.PhaseDevelopActive}}, Body: "Deploy step.\n\n" + strings.Repeat("d", 14000)},
	}
	tests := []struct {
		name   string
		status bool
	}{
		{"edit start", false},
		{"status beside deploy", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := workflow.StateEnvelope{Phase: workflow.PhaseDevelopActive}
			var guidance []string
			if tt.status {
				edit, err := readEditBriefing(context.Background(), nil, "", "", runtime.Info{}, nil, env, nil, corpus)
				if err != nil {
					t.Fatal(err)
				}
				matches, err := workflow.Synthesize(env, corpus)
				if err != nil {
					t.Fatal(err)
				}
				guidance, _ = statusGuidance(&env, corpus, matches, workflow.Plan{}, edit, true)
			} else {
				var err error
				guidance, _, err = editBriefing(context.Background(), nil, "", "", runtime.Info{}, nil, env, nil, corpus)
				if err != nil {
					t.Fatal(err)
				}
			}
			text := strings.Join(guidance, "")
			if len(text) > workflow.GuidanceBudget(env) || !strings.Contains(text, "Edit step.") || (tt.status && !strings.Contains(text, "Deploy step.")) {
				t.Errorf("guidance: %d bytes, edit %v, deploy %v; budget %d", len(text), strings.Contains(text, "Edit step."), strings.Contains(text, "Deploy step."), workflow.GuidanceBudget(env))
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | budget -= len(line) + 2*(strings.Count(line, "\n")+1) → budget -= 2*(strings.Count(line, "\n")+1)
func TestEditBudget_WD12_ReservesCopyLines(t *testing.T) {
	t.Parallel()
	env := workflow.StateEnvelope{}
	body := "Edit step.\n\n" + strings.Repeat("e", 19000)
	corpus := []workflow.KnowledgeAtom{{ID: "edit", Title: "Edit", Priority: 1, Body: body}}
	tests := []struct {
		name    string
		copies  string
		demoted bool
	}{
		{"fits", "copy", false},
		{"copy lines take the remaining room", strings.Repeat("c", 6000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			guidance := composeEditGuidance(env, corpus, []workflow.MatchedRender{{AtomID: "edit", Body: body}}, []string{tt.copies})
			if len(guidance) != 2 || guidance[1] != tt.copies || (guidance[0] != body) != tt.demoted || len(strings.Join(guidance, "")) > workflow.GuidanceBudget(env) {
				t.Errorf("copy line preserved = %v, atom demoted = %v, bytes = %d", len(guidance) == 2 && guidance[1] == tt.copies, len(guidance) > 0 && guidance[0] != body, len(strings.Join(guidance, "")))
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | err := g.gatherEdit(ctx, envelope) → err := g.gather(ctx)
func TestEditReads_WD13_ReusesTheSuppliedEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, want string
		leases     []workflow.LeaseSnapshot
	}{
		{"empty pool", "acquire a lease", nil},
		{"held lease", "pool-1", []workflow.LeaseSnapshot{{Alias: "pool-1", Pool: "pool", State: ops.LeaseStateHeld, Branch: "topic"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := platform.NewMock()
			env := workflow.StateEnvelope{Services: []workflow.ServiceSnapshot{{Hostname: "pool", DevIsPool: true}}, Leases: &tt.leases}
			guidance, plan, err := editBriefing(context.Background(), client, "already-read", "", runtime.Info{MateEnabled: true}, nil, env, []string{"pool"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(guidance, "\n") + plan.Primary.Label; !strings.Contains(got, tt.want) || client.CallCount("GetProject") != 0 || client.CallCount("ListServices") != 0 {
				t.Errorf("briefing = %q; project reads = %d, service reads = %d", got, client.CallCount("GetProject"), client.CallCount("ListServices"))
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | if len(copies) == 0 && len(missing) > 0 { → if false {
func TestEditNext_WD7_NamesTheCurrentStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		copies      []workflow.WhereCopy
		missing     []string
		tool, label string
	}{
		{"empty pool", nil, []string{"pool"}, "zerops_lease", "Lease a container of pool"},
		{"commit", []workflow.WhereCopy{{Name: "pool-1", Step: &workflow.EditStep{Action: "commit", Copy: "pool-1"}}}, nil, "", "pool-1: commit in pool-1"},
		{"open change", []workflow.WhereCopy{{Name: "dev", Step: &workflow.EditStep{Action: "open change with a description"}}}, nil, "", "dev: open change with a description"},
		{"unread", nil, nil, "", "unknown: working copies were not read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := editNext(tt.copies, tt.missing).Primary
			if got.Tool != tt.tool || got.Label != tt.label {
				t.Errorf("next = %+v, want tool %q and label %q", got, tt.tool, tt.label)
			}
		})
	}
}

// witness: internal/tools/workflow.go | tracked session for a deploy → tracked session
func TestWorkflowDescription_WD8_DescribesEditInputs(t *testing.T) {
	t.Parallel()
	fields := reflect.TypeFor[WorkflowInput]()
	tests := []struct {
		field string
		words []string
	}{
		{"Scope", []string{"pool", "lease", "tracked session"}},
		{"Description", []string{"open-change", "describe-change"}},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			t.Parallel()
			field, ok := fields.FieldByName(tt.field)
			if !ok {
				t.Fatalf("missing field %s", tt.field)
			}
			for _, word := range tt.words {
				if !strings.Contains(field.Tag.Get("jsonschema"), word) {
					t.Errorf("%s description lacks %q", tt.field, word)
				}
			}
		})
	}
	if desc := workflowDescription(runtime.Info{MateEnabled: true}); !strings.Contains(desc, "tracked session for a deploy") {
		t.Errorf("description does not distinguish a deploy session: %s", desc)
	}
}

// witness: internal/tools/where.go | return path.Join(ops.DevRepoRoot, host, rel) → return path.Join(ops.DevRepoRoot, rel)
func TestEditCheckout_WD9_UsesTheMountedPath(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, host, root, want string }{
		{"dev", "appdev", "/var/www", "/var/www/appdev"},
		{"lease", "pool-1", "/var/www", "/var/www/pool-1"},
		{"volume", "appdev", "/var/www/code", "/var/www/appdev/code"},
		{"outside mount", "appdev", "/other", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := editCheckoutPath(tt.host, tt.root); got != tt.want {
				t.Errorf("checkout = %q, want %q", got, tt.want)
			}
		})
	}
}

// witness: internal/tools/workflow_develop_dev_pool.go | if mate { → if !mate {
func TestPoolOnlyBriefing_WD14_SelectsThePlacementGuidance(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		mate bool
		want string
	}{
		{"standalone", false, "develop-dev-pool"},
		{"mate", true, "develop-dev-pool-mate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := devPoolBriefingAtomID(tt.mate); got != tt.want {
				t.Errorf("pool-only guidance = %q, want %q", got, tt.want)
			}
		})
	}
}
