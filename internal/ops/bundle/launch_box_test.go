// Tests for: launch bundle — the Zerops Box a launched runtime reads, and the
// closure check that refuses a launch whose runtime reads a service the
// production bundle does not carry.
package bundle

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"gopkg.in/yaml.v3"
)

// noolaLaunchInputs is Noola's runner launched to production: its stage
// builds runnerprod, which reads the broker and the project's Box; the
// source project also runs the runner's dev service and its stage, the
// Mate's mail catcher and zcp itself.
func noolaLaunchInputs(body string) LaunchBundleInputs {
	return LaunchBundleInputs{
		SourceProjectID:   "src-proj",
		TargetProjectName: "noola-prod",
		Variant:           VariantLaunchNew,
		Runtimes: []LaunchRuntimeInput{{
			ProdHostname:   "runner",
			ServiceType:    "alpine/go@1",
			SetupName:      "runnerprod",
			RepoURL:        "https://github.com/fxck/noola.git",
			GitCommitSHA:   "abc123",
			ZeropsYAMLBody: body,
		}},
		ManagedServices: []ManagedServiceEntry{
			{Hostname: "broker", Type: "nats@2.12", Mode: "NON_HA"},
			{Hostname: "box", Type: "ubuntu/zbox@1"},
		},
		ProjectHostnames: []string{"box", "broker", "mailpit", "runnerdev", "runnerstage", "zcp"},
	}
}

// natsReads is a setup that reads the broker alone.
var natsReads = map[string]string{"NATS_PORT": "${broker_port}"}

// launchServices is the services of a launch bundle's import, by hostname.
func launchServices(t *testing.T, b *LaunchBundle) map[string]map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(b.ImportYAML), &doc); err != nil {
		t.Fatalf("launch import does not parse: %v\n%s", err, b.ImportYAML)
	}
	return serviceEntries(t, doc)
}

// The launch used to leave every Zerops Box out with a warning, and Noola's
// production came up with a runner whose setup reads its Box at
// `http://box:8080` with `${box_ZBOX_WORKSPACE_API_KEY}` and no Box to read:
// `box` was imported by hand. A launch carries a Box exactly where a runtime
// it launches reads it — the setup the launch builds for the runtime (with
// those of the setups it extends, build and run variables alike), or the
// runtime's own variables as the bundle writes them — by the group recipe's
// rule (PS-3). A Box read only by a setup the launch does not build, or by
// nothing, stays out with the warning it always had. Spec: spec-workflows.md
// §10.3 P-LP-19.
func TestBuildLaunch_Box_CarriedWhereALaunchedRuntimeReadsIt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		body            string
		serviceEnvs     []ProjectEnvVar
		classifications map[string]topology.SecretClassification
		wantCarried     bool
	}{
		{
			name: "Noola's runner: by address and by reference",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: boxReads},
				setupVars{name: "runnerprod", vars: boxReads}),
			wantCarried: true,
		},
		{
			name: "by reference alone",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerprod", vars: map[string]string{"BOX_API_KEY": "${box_ZBOX_WORKSPACE_API_KEY}"}}),
			wantCarried: true,
		},
		{
			name: "an address on the project network under its internal name",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerprod", vars: map[string]string{"SANDBOX_INGRESS": "http://box.zerops:8081"}}),
			wantCarried: true,
		},
		{
			name: "a host and port alone",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerprod", vars: map[string]string{"BOX_ADDR": "box:8080"}}),
			wantCarried: true,
		},
		{
			name: "the setup the launched one extends reads it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerbase", vars: boxReads},
				setupVars{name: "runnerprod", extends: "runnerbase", vars: natsReads}),
			wantCarried: true,
		},
		{
			name: "its build variables read it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerprod", vars: natsReads, build: map[string]string{"BOX_KEY": "${box_ZBOX_WORKSPACE_API_KEY}"}}),
			wantCarried: true,
		},
		{
			name:            "its own variable, written as it runs",
			body:            zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: natsReads}),
			serviceEnvs:     []ProjectEnvVar{{Key: "BOX_API_URL", Value: "http://box:8080"}},
			classifications: map[string]topology.SecretClassification{"BOX_API_URL": topology.SecretClassPlainConfig},
			wantCarried:     true,
		},
		{
			name:        "its own variable the bundle writes as a placeholder",
			body:        zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: natsReads}),
			serviceEnvs: []ProjectEnvVar{{Key: "BOX_API_URL", Value: "http://box:8080"}},
		},
		{
			name: "only the dev setup reads it",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerdev", vars: boxReads},
				setupVars{name: "runnerprod", vars: natsReads}),
		},
		{
			name: "nothing reads it",
			body: zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: natsReads}),
		},
		{
			name: "a word, another host, a public address or a domain is no read",
			body: zeropsYAMLWithVars(
				setupVars{name: "runnerprod", vars: map[string]string{
					"NATS_PORT":    "${broker_port}",
					"SANDBOX_KIND": "box",
					"OTHER_API":    "http://boxes:8080",
					"PUBLIC_URL":   "https://box-${zeropsSubdomainHost}-8080.prg1.zerops.app",
					"DOCS_URL":     "https://box.example.com/docs",
				}}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := noolaLaunchInputs(tt.body)
			inputs.Runtimes[0].ServiceEnvs = tt.serviceEnvs
			b, err := BuildLaunch(inputs, tt.classifications)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if len(b.Errors) > 0 {
				t.Errorf("launch import fails the schema: %v\n%s", b.Errors, b.ImportYAML)
			}
			box, carried := launchServices(t, b)["box"]
			if carried != tt.wantCarried {
				t.Fatalf("Box carried = %v, want %v:\n%s", carried, tt.wantCarried, b.ImportYAML)
			}
			notCarried := `Zerops Box "box" (ubuntu/zbox@1) is not carried into the production bundle — add it to the production import yourself`
			if !tt.wantCarried {
				if !slices.Contains(b.Warnings, notCarried) {
					t.Errorf("warnings %v do not carry the existing not-carried warning", b.Warnings)
				}
				return
			}
			if slices.Contains(b.Warnings, notCarried) {
				t.Errorf("warnings %v say a carried Box is not carried", b.Warnings)
			}
			// Launch rules on the Box: no build (pipeline-first), no public
			// access (P-PROD-2), no mode, HA variant or scale — one VM —
			// created with the managed services, before the runtime.
			if keys := slices.Sorted(maps.Keys(box)); !slices.Equal(keys, []string{"hostname", "priority", "type"}) {
				t.Errorf("Box keys = %v, want [hostname priority type]", keys)
			}
			if box["type"] != "ubuntu/zbox@1" {
				t.Errorf("Box type = %v, want ubuntu/zbox@1 as it runs", box["type"])
			}
			if box["priority"] != managedPriority {
				t.Errorf("Box priority = %v, want %d", box["priority"], managedPriority)
			}
			if !warningsContain(b.Warnings, `Zerops Box "box" (ubuntu/zbox@1) is carried into the production bundle: runtime "runner" reads it`) {
				t.Errorf("warnings %v do not say the Box is carried and which runtime reads it", b.Warnings)
			}
			if !warningsContain(b.Warnings, "zbox setup") {
				t.Errorf("warnings %v do not say the carried Box needs its setup finished", b.Warnings)
			}
		})
	}
}

// A Swarm pool is never carried: one nothing launched reads stays out with
// the warning it always had, and one a launched runtime reads refuses the
// launch (P-LP-19's closure) — it is never emitted either way.
func TestBuildLaunch_Pool_NeverCarried(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		vars      map[string]string
		wantError bool
	}{
		{name: "nothing reads it", vars: natsReads},
		{name: "the launched setup reads it", vars: map[string]string{"NATS_PORT": "${broker_port}", "POOL_TOKEN": "${fleet_API_TOKEN}"}, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := noolaLaunchInputs(zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: tt.vars}))
			inputs.ManagedServices = append(inputs.ManagedServices, ManagedServiceEntry{Hostname: "fleet", Type: "swarm@1"})
			b, err := BuildLaunch(inputs, nil)
			if tt.wantError {
				if err == nil {
					t.Fatalf("launch composed, want it refused:\n%s", b.ImportYAML)
				}
				for _, want := range []string{`runtime "runner"`, "run.envVariables.POOL_TOKEN", `"fleet"`, "Swarm pool"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("refusal does not say %s: %v", want, err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if _, ok := launchServices(t, b)["fleet"]; ok {
				t.Errorf("the pool is emitted:\n%s", b.ImportYAML)
			}
			want := `Swarm pool "fleet" (swarm@1) is not carried into the production bundle — add it to the production import yourself`
			if !slices.Contains(b.Warnings, want) {
				t.Errorf("warnings %v do not carry %q", b.Warnings, want)
			}
		})
	}
}

// A launch that carries a runtime but not a service the runtime's setup
// reads starts a production app that cannot reach it. So the composer
// refuses the launch — before any token or project — and says which runtime
// builds which setup, which variable of it reads what, and why production
// lacks it. A read is the group recipe's (PS-3a): a `${host_key}` reference
// or the service as the host of an address on the project network, in the
// build or run variables of the setup the launch builds, and only a hostname
// the source project runs is a service. Spec: spec-workflows.md §10.3
// P-LP-19.
func TestBuildLaunch_Closure_ReadOfAServiceTheBundleLacks_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		prod   setupVars
		mutate func(*LaunchBundleInputs)
		want   []string
	}{
		{
			name: "the Mate's mail catcher by address",
			prod: setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}", "MAIL_URL": "smtp://mailpit:1025"}},
			want: []string{`runtime "runner" builds setup "runnerprod", whose run.envVariables.MAIL_URL names "mailpit"`},
		},
		{
			name: "its stage under the source project's hostname",
			prod: setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}", "SELF_URL": "${runnerstage_zeropsSubdomain}"}},
			want: []string{"run.envVariables.SELF_URL", `"runnerstage"`},
		},
		{
			name: "the dev service, in its build variables",
			prod: setupVars{name: "runnerprod", vars: natsReads, build: map[string]string{"DEV_API": "http://runnerdev:8080"}},
			want: []string{"build.envVariables.DEV_API", `"runnerdev"`},
		},
		{
			name: "a managed dependency the launch excludes",
			prod: setupVars{name: "runnerprod", vars: natsReads},
			mutate: func(in *LaunchBundleInputs) {
				in.ExcludeManaged = []string{"broker"}
			},
			want: []string{"run.envVariables.NATS_PORT", `"broker"`, "excluded"},
		},
		{
			name: "a Box the launch excludes",
			prod: setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}", "BOX_API_URL": "http://box:8080"}},
			mutate: func(in *LaunchBundleInputs) {
				in.ExcludeManaged = []string{"box"}
			},
			want: []string{"run.envVariables.BOX_API_URL", `"box"`, "excluded"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := noolaLaunchInputs(zeropsYAMLWithVars(tt.prod))
			if tt.mutate != nil {
				tt.mutate(&inputs)
			}
			b, err := BuildLaunch(inputs, nil)
			if err == nil {
				t.Fatalf("launch composed, want it refused (warnings %v):\n%s", b.Warnings, b.ImportYAML)
			}
			for _, want := range append([]string{"does not carry"}, tt.want...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not say %s:\n%v", want, err)
				}
			}
		})
	}
}

// A launch whose runtimes read only what the bundle carries composes as
// before: the managed dependencies it promotes — by a reference that spells
// a hyphenated hostname with an underscore, HA-promoted or not — the Box it
// carries, a project variable, a name no service of the source project has
// (the runtime's own production hostname), a public address, a bare word; a
// setup the launch does not build; the runtime's own variables, which never
// refuse it; and, on an existing project, a service production already runs
// that the bundle leaves as it is (P-LP-12 skip).
func TestBuildLaunch_Closure_EveryReadCarried_Composes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		dev, prod       setupVars
		mutate          func(*LaunchBundleInputs)
		classifications map[string]topology.SecretClassification
	}{
		{
			name: "Noola's runner: the broker and the Box",
			prod: setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}", "BOX_API_URL": "http://box:8080", "BOX_API_KEY": "${box_ZBOX_WORKSPACE_API_KEY}"}},
		},
		{
			name: "a promoted managed dependency with a hyphenated hostname",
			prod: setupVars{name: "runnerprod", vars: map[string]string{"NATS_PORT": "${broker_port}", "DATABASE_URL": "${my_db_connectionString}", "DB_HOST": "my-db:5432"}},
			mutate: func(in *LaunchBundleInputs) {
				in.ManagedServices = append(in.ManagedServices, ManagedServiceEntry{Hostname: "my-db", Type: "postgresql@16", Mode: "NON_HA"})
				in.ProjectHostnames = append(in.ProjectHostnames, "my-db")
				in.KeepNonHA = []string{"broker"}
			},
		},
		{
			name: "a project variable, a name no source service has, a public address, a word",
			prod: setupVars{name: "runnerprod", vars: map[string]string{
				"NATS_PORT": "${broker_port}",
				"HOST":      "${zeropsSubdomainHost}",
				"SELF_API":  "http://runner:3000",
				"SELF_KEY":  "${runner_apiKey}",
				"MAIL_UI":   "https://mailpit-${zeropsSubdomainHost}-8025.prg1.zerops.app",
				"SMTP_HOST": "mailpit",
			}},
		},
		{
			name: "the dev setup alone reads the Mate's mail catcher",
			dev:  setupVars{name: "runnerdev", vars: map[string]string{"MAIL_URL": "smtp://mailpit:1025"}},
			prod: setupVars{name: "runnerprod", vars: natsReads},
		},
		{
			name: "the runtime's own variable names the Mate's mail catcher",
			prod: setupVars{name: "runnerprod", vars: natsReads},
			mutate: func(in *LaunchBundleInputs) {
				in.Runtimes[0].ServiceEnvs = []ProjectEnvVar{{Key: "MAIL_URL", Value: "smtp://mailpit:1025"}}
			},
			classifications: map[string]topology.SecretClassification{"MAIL_URL": topology.SecretClassPlainConfig},
		},
		{
			name: "a service the existing production project already runs",
			prod: setupVars{name: "runnerprod", vars: natsReads},
			mutate: func(in *LaunchBundleInputs) {
				in.Variant = VariantLaunchExisting
				in.ManagedServices = in.ManagedServices[1:]
				in.TargetHostnames = []string{"broker"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			setups := []setupVars{tt.prod}
			if tt.dev.name != "" {
				setups = append([]setupVars{tt.dev}, setups...)
			}
			inputs := noolaLaunchInputs(zeropsYAMLWithVars(setups...))
			if tt.mutate != nil {
				tt.mutate(&inputs)
			}
			b, err := BuildLaunch(inputs, tt.classifications)
			if err != nil {
				t.Fatalf("BuildLaunch: %v", err)
			}
			if _, ok := launchServices(t, b)["runner"]; !ok {
				t.Errorf("the runtime is not launched:\n%s", b.ImportYAML)
			}
		})
	}
}

// A Box can be created only in a project in region prg1 (PS-16), and the
// launch import is not zcp's zerops_import, which would refuse it there: a
// launch-new bundle that carries a Box into a production project elsewhere
// is refused, naming the Box and the regions. A Box the bundle does not
// carry, the default location, and an existing project — whose location the
// composer does not know — refuse nothing. Spec: spec-workflows.md §10.3
// P-LP-19.
func TestBuildLaunch_Box_OutsideItsRegion_Refused(t *testing.T) {
	t.Parallel()
	reads := zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: boxReads})
	unread := zeropsYAMLWithVars(setupVars{name: "runnerprod", vars: natsReads})
	tests := []struct {
		name      string
		body      string
		location  string
		variant   Variant
		wantError bool
	}{
		{name: "carried, the production project in ny1", body: reads, location: "us-east-1", variant: VariantLaunchNew, wantError: true},
		{name: "carried, the default location", body: reads, variant: VariantLaunchNew},
		{name: "carried, prg1 named", body: reads, location: "eu-central", variant: VariantLaunchNew},
		{name: "not carried, the production project in ny1", body: unread, location: "us-east-1", variant: VariantLaunchNew},
		{name: "carried into an existing project", body: reads, location: "us-east-1", variant: VariantLaunchExisting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inputs := noolaLaunchInputs(tt.body)
			inputs.Location = tt.location
			inputs.Variant = tt.variant
			_, err := BuildLaunch(inputs, nil)
			if !tt.wantError {
				if err != nil {
					t.Fatalf("BuildLaunch: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("launch composed, want it refused")
			}
			for _, want := range []string{`Zerops Box "box"`, "prg1", `"us-east-1"`} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal does not say %s: %v", want, err)
				}
			}
		})
	}
}
