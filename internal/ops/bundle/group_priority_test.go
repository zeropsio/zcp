package bundle

import (
	"maps"
	"strings"
	"testing"
)

// The platform creates a project's services a priority group at a time, and
// each group waits until the one before it is created AND deployed (measured
// 2026-09-29 on two Mates: data at 10, the backend pair at 6, the storefront
// pair at 5 ran as three serial waves). So a priority is a dependency order:
// a runtime another runtime references comes up before it.
func TestGroupPriorities_DependencyOrder(t *testing.T) {
	t.Parallel()
	app := func(key string, sources ...string) groupApp {
		return groupApp{key: key, hostnames: []string{key}, sources: sources}
	}
	tests := []struct {
		name        string
		apps        []groupApp
		projectEnvs []ProjectEnvVar
		want        map[string]int
		wantWarning string
	}{
		{
			name: "nothing references anything — every runtime sits in the lowest rank",
			apps: []groupApp{app("api"), app("worker")},
			want: map[string]int{"api": 1, "worker": 1},
		},
		{
			name: "a storefront references the backend — the backend outranks it",
			apps: []groupApp{app("api"), app("store", "http://${api_hostname}:${api_port}")},
			want: map[string]int{"api": 2, "store": 1},
		},
		{
			name: "a chain — each runtime before whatever references it",
			apps: []groupApp{app("web", "${api_zeropsSubdomain}"), app("api", "${auth_hostname}"), app("auth")},
			want: map[string]int{"auth": 3, "api": 2, "web": 1},
		},
		{
			name: "a diamond — the longest chain above a runtime decides",
			apps: []groupApp{app("web", "${api_hostname}", "${auth_hostname}"), app("api", "${auth_hostname}"), app("auth")},
			want: map[string]int{"auth": 3, "api": 2, "web": 1},
		},
		{
			name: "a reference through a project variable counts",
			apps: []groupApp{app("api"), app("store", "${MEDUSA_URL}")},
			projectEnvs: []ProjectEnvVar{
				{Key: "MEDUSA_URL", Value: "${INTERNAL_URL}"},
				{Key: "INTERNAL_URL", Value: "http://${api_hostname}:9000"},
			},
			want: map[string]int{"api": 2, "store": 1},
		},
		{
			name:        "project variables that reference each other end the walk",
			apps:        []groupApp{app("api"), app("store", "${A}")},
			projectEnvs: []ProjectEnvVar{{Key: "A", Value: "${B}"}, {Key: "B", Value: "${A}"}},
			want:        map[string]int{"api": 1, "store": 1},
		},
		{
			name: "any hostname of a pair names the pair",
			apps: []groupApp{
				{key: "apidev", hostnames: []string{"apidev", "apistage", "api"}},
				{key: "storedev", hostnames: []string{"storedev", "storestage", "store"}, sources: []string{"${apistage_zeropsSubdomain}"}},
			},
			want: map[string]int{"apidev": 2, "storedev": 1},
		},
		{
			name: "a runtime that references itself depends on nothing",
			apps: []groupApp{app("api", "${api_zeropsSubdomain}")},
			want: map[string]int{"api": 1},
		},
		{
			name: "a managed service is no runtime dependency — it is created first anyway",
			apps: []groupApp{app("api", "${db_connectionString}", "${redis_hostname}")},
			want: map[string]int{"api": 1},
		},
		{
			name: "the longest hostname a reference opens with is the one it names",
			apps: []groupApp{app("api"), app("api-v2"), app("store", "${api_v2_hostname}")},
			want: map[string]int{"api": 1, "api-v2": 2, "store": 1},
		},
		{
			name:        "two runtimes that reference each other share a rank, and it is said",
			apps:        []groupApp{app("a", "${b_hostname}"), app("b", "${a_hostname}"), app("web", "${a_hostname}")},
			want:        map[string]int{"a": 2, "b": 2, "web": 1},
			wantWarning: `"a" and "b" reference each other`,
		},
		{
			name: "a chain longer than the runtime ranks stops below the managed services",
			apps: []groupApp{
				app("r0", "${r1_x}"), app("r1", "${r2_x}"), app("r2", "${r3_x}"), app("r3", "${r4_x}"), app("r4", "${r5_x}"),
				app("r5", "${r6_x}"), app("r6", "${r7_x}"), app("r7", "${r8_x}"), app("r8", "${r9_x}"), app("r9", "${r10_x}"), app("r10"),
			},
			want: map[string]int{
				"r0": 1, "r1": 2, "r2": 3, "r3": 4, "r4": 5, "r5": 6, "r6": 7, "r7": 8, "r8": 9, "r9": 9, "r10": 9,
			},
			wantWarning: "priority 9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, warnings := groupPriorities(tt.apps, tt.projectEnvs)
			if !maps.Equal(got, tt.want) {
				t.Errorf("priorities = %v, want %v", got, tt.want)
			}
			if tt.wantWarning != "" && !warningsContain(warnings, tt.wantWarning) {
				t.Errorf("warnings %v are missing %q", warnings, tt.wantWarning)
			}
			if tt.wantWarning == "" && len(warnings) > 0 {
				t.Errorf("unexpected warnings %v", warnings)
			}
			for _, p := range got {
				if p >= managedPriority {
					t.Errorf("a runtime at %d would be created with the managed services", p)
				}
			}
		})
	}
}

// A runtime's references are read from its pair's zerops.yaml — the build's
// variables as well as the run's, since a storefront's BUILD bakes the
// backend's publishable key — and from both halves' own variables.
func TestBuildGroupRecipe_PriorityFollowsReferences(t *testing.T) {
	t.Parallel()
	const backendYAML = "zerops:\n  - setup: medusadev\n    run:\n      start: zsc noop\n  - setup: medusaprod\n    run:\n      envVariables:\n        DATABASE_URL: ${db_connectionString}\n"
	const storefrontYAML = "zerops:\n  - setup: nextstoredev\n    run:\n      start: zsc noop\n  - setup: nextstoreprod\n    build:\n      envVariables:\n        NEXT_PUBLIC_MEDUSA_PUBLISHABLE_KEY: ${medusa_CHANNEL_PUBLISHABLE_KEY}\n    run:\n      start: yarn start\n"
	in := GroupRecipeInputs{
		Name: "acme", MateProjectName: "acme-mate-1",
		Runtimes: []GroupRuntime{
			{DevHostname: "medusadev", StageHostname: "medusastage", ServiceType: "nodejs@22", RepoURL: "https://git.example.com/acme/medusadev", SetupName: "medusadev", ZeropsYAMLBody: backendYAML},
			{DevHostname: "nextstoredev", StageHostname: "nextstorestage", ServiceType: "nodejs@22", RepoURL: "https://git.example.com/acme/nextstoredev", SetupName: "nextstoredev", ZeropsYAMLBody: storefrontYAML},
			{DevHostname: "workerdev", StageHostname: "workerstage", ServiceType: "nodejs@22", RepoURL: "https://git.example.com/acme/workerdev", SetupName: "workerdev",
				ZeropsYAMLBody: "zerops:\n  - setup: workerdev\n    run:\n      start: zsc noop\n  - setup: workerprod\n    run:\n      start: node worker.js\n"},
		},
		ManagedServices: []ManagedServiceEntry{{Hostname: "db", Type: "postgresql@16"}},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	files := groupFiles(t, layout)
	tests := []struct {
		dir  string
		want map[string]int
	}{
		{"0 — AI Agent", map[string]int{"db": 10, "medusadev": 2, "medusastage": 2, "nextstoredev": 1, "nextstorestage": 1, "workerdev": 1, "workerstage": 1}},
		{"3 — Stage", map[string]int{"db": 10, "medusa": 2, "nextstore": 1, "worker": 1}},
		{"4 — Small Production", map[string]int{"db": 10, "medusa": 2, "nextstore": 1, "worker": 1}},
	}
	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			t.Parallel()
			services := serviceEntries(t, tierDoc(t, files, tt.dir))
			got := map[string]int{}
			for host, entry := range services {
				p, _ := entry["priority"].(int)
				got[host] = p
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("priorities = %v, want %v", got, tt.want)
			}
			// The file lists the services in the order they are created.
			body := files[tt.dir+"/import.yaml"]
			if at, before := strings.Index(body, "hostname: db"), strings.Index(body, "hostname: medusa"); at < 0 || before < 0 || at > before {
				t.Errorf("the managed services are not listed first")
			}
			if strings.Index(body, "hostname: medusa") > strings.Index(body, "hostname: nextstore") {
				t.Errorf("the backend is not listed before the storefront that references it")
			}
		})
	}
}

// The order a recipe is written in is designed from what each runtime's build
// reads: a service read at build time outranks its readers, and services with
// no build-time read between them share a priority — a runtime read only once
// it runs waits for nothing.
func TestBuildGroupRecipe_PriorityFollowsBuildReads(t *testing.T) {
	t.Parallel()
	pair := func(name, prodYAML string) GroupRuntime {
		return GroupRuntime{
			DevHostname: name + "dev", StageHostname: name + "stage", ServiceType: "nodejs@22",
			RepoURL: "https://git.example.com/acme/" + name + "dev", SetupName: name + "dev",
			ZeropsYAMLBody: "zerops:\n  - setup: " + name + "dev\n    run:\n      start: zsc noop\n  - setup: " + name + "prod\n" + prodYAML,
		}
	}
	tests := []struct {
		name        string
		runtimes    []GroupRuntime
		projectEnvs []ProjectEnvVar
		want        map[string]int
	}{
		{
			name: "a storefront's build reads the backend: the backend outranks it",
			runtimes: []GroupRuntime{
				pair("api", "    run:\n      start: node api.js\n"),
				pair("store", "    build:\n      envVariables:\n        API: https://${apistage_zeropsSubdomain}\n    run:\n      start: node store.js\n"),
			},
			want: map[string]int{"apidev": 2, "apistage": 2, "storedev": 1, "storestage": 1},
		},
		{
			name: "a storefront that reads the backend only once it runs: one priority",
			runtimes: []GroupRuntime{
				pair("api", "    run:\n      start: node api.js\n"),
				pair("store", "    run:\n      envVariables:\n        API: https://${apistage_zeropsSubdomain}\n      start: node store.js\n"),
			},
			want: map[string]int{"apidev": 1, "apistage": 1, "storedev": 1, "storestage": 1},
		},
		{
			name: "a build that lifts a runtime variable reading the backend",
			runtimes: []GroupRuntime{
				pair("api", "    run:\n      start: node api.js\n"),
				pair("store", "    build:\n      envVariables:\n        API: ${RUNTIME_API_URL}\n    run:\n      envVariables:\n        API_URL: ${api_zeropsSubdomain}\n      start: node store.js\n"),
			},
			want: map[string]int{"apidev": 2, "apistage": 2, "storedev": 1, "storestage": 1},
		},
		{
			name: "a build that reads the backend through a project variable",
			runtimes: []GroupRuntime{
				pair("api", "    run:\n      start: node api.js\n"),
				pair("store", "    build:\n      envVariables:\n        API: ${API_URL}\n    run:\n      start: node store.js\n"),
			},
			projectEnvs: []ProjectEnvVar{{Key: "API_URL", Value: "https://${apistage_zeropsSubdomain}"}},
			want:        map[string]int{"apidev": 2, "apistage": 2, "storedev": 1, "storestage": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			layout, _, err := BuildGroupRecipe(GroupRecipeInputs{
				Name: "acme", MateProjectName: "acme-mate-1", Runtimes: tt.runtimes, ProjectEnvs: tt.projectEnvs,
				ManagedServices: []ManagedServiceEntry{{Hostname: "db", Type: "postgresql@16"}},
			})
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			services := serviceEntries(t, tierDoc(t, groupFiles(t, layout), "0 — AI Agent"))
			got := map[string]int{}
			for host, entry := range services {
				if host == "db" {
					continue
				}
				p, _ := entry["priority"].(int)
				got[host] = p
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("priorities = %v, want %v", got, tt.want)
			}
		})
	}
}
