package bundle

import (
	"maps"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/recipe"
)

// The project's core package is carried as it runs, and only a value the
// import knows is.
func TestBuildGroupRecipe_CorePackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, live, want string
	}{
		{"serious core", "SERIOUS", "SERIOUS"},
		{"light core", "LIGHT", "LIGHT"},
		{"unread — the platform's default", "", ""},
		{"a mode the import does not take", "LEGACY", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.CorePackage = tt.live
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				project := mappingValue(tierMapping(t, tier.ImportYAML), "project")
				got := ""
				if node := mappingValue(project, "corePackage"); node != nil {
					got = node.Value
				}
				if got != tt.want {
					t.Errorf("%s: corePackage = %q, want %q", tier.Title, got, tt.want)
				}
			}
		})
	}
}

// An object storage is created the size and with the access policy it runs
// with: the medusa group's storage lost `public-read` and its size, and the
// storefront's product images stopped loading (2026-09-24).
func TestBuildGroupRecipe_ObjectStorageAsItRuns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry ManagedServiceEntry
		want  map[string]string
	}{
		{
			name:  "a named policy and its size",
			entry: ManagedServiceEntry{Hostname: "storage", Type: "object-storage", QuotaGBytes: 5, ObjectStoragePolicy: "public-read"},
			want:  map[string]string{"objectStorageSize": "5", "objectStoragePolicy": "public-read"},
		},
		{
			name: "a custom policy carries its document",
			entry: ManagedServiceEntry{Hostname: "storage", Type: "object-storage", QuotaGBytes: 2, ObjectStoragePolicy: "custom",
				ObjectStorageRawPolicy: `{"Version":"2012-10-17","Statement":[]}`},
			want: map[string]string{"objectStorageSize": "2", "objectStoragePolicy": "custom", "objectStorageRawPolicy": `{"Version":"2012-10-17","Statement":[]}`},
		},
		{
			name:  "nothing read — the import's minimum and the platform's default policy",
			entry: ManagedServiceEntry{Hostname: "storage", Type: "object-storage"},
			want:  map[string]string{"objectStorageSize": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.ManagedServices = append(in.ManagedServices, tt.entry)
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				storage := serviceNode(t, tier.ImportYAML, "storage")
				got := map[string]string{}
				for _, key := range []string{"objectStorageSize", "objectStoragePolicy", "objectStorageRawPolicy"} {
					if node := mappingValue(storage, key); node != nil {
						got[key] = node.Value
					}
				}
				if !maps.Equal(got, tt.want) {
					t.Errorf("%s: storage = %v, want %v", tier.Title, got, tt.want)
				}
				for _, absent := range []string{"mode", "verticalAutoscaling"} {
					if mappingValue(storage, absent) != nil {
						t.Errorf("%s: an object storage carries %q, which the import refuses", tier.Title, absent)
					}
				}
			}
		})
	}
}

// A managed service scales the way it runs. Small Production is the one
// tier that decides a database's scale instead — the production profile — so
// a profile-bearing service takes that profile there and no dev-sized bounds
// beside it; a service with no profile keeps its own bounds on every tier.
func TestBuildGroupRecipe_ManagedScalingAsItRuns(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ManagedServices = []ManagedServiceEntry{
		{Hostname: "db", Type: "postgresql@16", Profile: "oltp-hobby", Scaling: &Scaling{CPUMode: "SHARED", MinCPU: 1, MaxCPU: 3, MinRAM: 0.5, MaxRAM: 4, MinDisk: 1, MaxDisk: 20, MinContainers: 1, MaxContainers: 1}},
		{Hostname: "search", Type: "meilisearch@1.20", Scaling: &Scaling{CPUMode: "SHARED", MinRAM: 0.25, MaxRAM: 2}},
	}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	dbLive := map[string]string{"cpuMode": "SHARED", "minCpu": "1", "maxCpu": "3", "minRam": "0.5", "maxRam": "4", "minDisk": "1", "maxDisk": "20"}
	searchLive := map[string]string{"cpuMode": "SHARED", "minRam": "0.25", "maxRam": "2"}
	tests := []struct {
		tier        string
		dbProfile   string
		dbScaling   map[string]string
		searchScale map[string]string
	}{
		{"AI Agent", "oltp-hobby", dbLive, searchLive},
		{"Stage", "oltp-hobby", dbLive, searchLive},
		{"Small Production", "oltp-staging", nil, searchLive},
	}
	for _, tt := range tests {
		t.Run(tt.tier, func(t *testing.T) {
			t.Parallel()
			body := tierBody(t, layout.Tiers, tt.tier)
			db := serviceNode(t, body, "db")
			if got := mappingValue(db, "profile").Value; got != tt.dbProfile {
				t.Errorf("db profile = %q, want %q", got, tt.dbProfile)
			}
			if got := scalarMap(mappingValue(db, "verticalAutoscaling")); !maps.Equal(got, tt.dbScaling) {
				t.Errorf("db verticalAutoscaling = %v, want %v", got, tt.dbScaling)
			}
			search := serviceNode(t, body, "search")
			if got := scalarMap(mappingValue(search, "verticalAutoscaling")); !maps.Equal(got, tt.searchScale) {
				t.Errorf("search verticalAutoscaling = %v, want %v", got, tt.searchScale)
			}
			for _, managed := range []string{"db", "search"} {
				for _, absent := range []string{"minContainers", "maxContainers"} {
					if mappingValue(serviceNode(t, body, managed), absent) != nil {
						t.Errorf("%s carries %s: a managed service's containers are its mode's", managed, absent)
					}
				}
			}
		})
	}
}

// A type the platform ships no HA variant of stays single-node on the tier
// that promotes the rest: `meilisearch:ha` does not exist, and one
// fabricated type fails the whole import.
func TestBuildGroupRecipe_HAIncapableStaysSingle(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.ManagedServices = append(in.ManagedServices, ManagedServiceEntry{Hostname: "search", Type: "meilisearch@1.20"})
	in.HAIncapable = []string{"search"}
	layout, warnings, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	production := tierBody(t, layout.Tiers, "Small Production")
	if got := mappingValue(serviceNode(t, production, "search"), "type").Value; strings.Contains(got, ":ha") {
		t.Errorf("search type = %q on Small Production — no such type", got)
	}
	if got := mappingValue(serviceNode(t, production, "db"), "type").Value; got != "postgresql:ha@18" {
		t.Errorf("db type = %q, want the HA promotion kept", got)
	}
	if !warningsContain(warnings, `"search"`) {
		t.Errorf("warnings %v do not say search stays single-node", warnings)
	}
}

// A runtime the project builds from a public repository and no Gitea pair —
// a utility such as mailpit — is written as the project runs it on every
// tier: its own hostname, its public build, its own scale, no tier's
// transform. It takes its place in the dependency order like any runtime.
func TestBuildGroupRecipe_PublicBuildUtility(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Runtimes[0].ZeropsYAMLBody = "zerops:\n  - setup: api\n    run:\n      envVariables:\n        SMTP_HOST: ${mailpit_hostname}\n"
	in.Utilities = []GroupUtility{{
		Hostname: "mailpit", ServiceType: "alpine@3.21",
		BuildFromGit:     "https://github.com/zerops-recipe-apps/mailpit-app.git",
		SubdomainEnabled: true,
		Scaling:          &Scaling{MinContainers: 1, MaxContainers: 1, CPUMode: "SHARED", MinRAM: 0.25},
		ServiceEnvs:      []ProjectEnvVar{{Key: "MP_UI_AUTH", Value: "admin:live-mailpit-pass"}},
	}}
	layout, _, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		t.Run(tier.Title, func(t *testing.T) {
			t.Parallel()
			mailpit := serviceNode(t, tier.ImportYAML, "mailpit")
			want := map[string]string{
				"hostname": "mailpit", "type": "alpine@3.21", "priority": "2",
				"buildFromGit": "https://github.com/zerops-recipe-apps/mailpit-app", "enableSubdomainAccess": "true",
				"minContainers": "1", "maxContainers": "1",
			}
			got := map[string]string{}
			for _, key := range mappingKeys(mailpit) {
				if node := mappingValue(mailpit, key); node.Kind == yaml.ScalarNode {
					got[key] = node.Value
				}
			}
			if !maps.Equal(got, want) {
				t.Errorf("mailpit = %v, want %v", got, want)
			}
			if got := scalarMap(mappingValue(mailpit, "verticalAutoscaling")); !maps.Equal(got, map[string]string{"cpuMode": "SHARED", "minRam": "0.25"}) {
				t.Errorf("mailpit verticalAutoscaling = %v, want its own", got)
			}
			if got := scalarMap(mappingValue(mailpit, "envSecrets")); got["MP_UI_AUTH"] == "admin:live-mailpit-pass" {
				t.Errorf("mailpit's credential reached the recipe")
			}
			if strings.Contains(tier.ImportYAML, "live-mailpit-pass") {
				t.Errorf("mailpit's credential reached the recipe")
			}
		})
	}
}

// A utility's build names its repository and nothing else: a query or a
// fragment is dropped, and a URL that carried a user carried a credential —
// the repository is private, so the tiers name no build for it at all and
// say its source is set by hand. The platform builds a named setup only from
// a repository, so the setup goes with it.
func TestBuildGroupRecipe_UtilityBuildCarriesNoCredential(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, live, wantBuild, wantSetup string
		wantPrivate                      bool
	}{
		{name: "a public repository", live: "https://github.com/zerops-recipe-apps/mailpit-app.git",
			wantBuild: "https://github.com/zerops-recipe-apps/mailpit-app", wantSetup: "mailpit"},
		{name: "a query and a fragment are dropped", live: "https://github.com/acme/tool.git?ref=main#readme",
			wantBuild: "https://github.com/acme/tool", wantSetup: "mailpit"},
		{name: "a user and a password: a private repository", live: "https://x-access-token:" + fakeGitHubToken + "@github.com/acme/private-tool", wantPrivate: true},
		{name: "a token for the user: a private repository", live: "https://" + fakeGitHubToken + "@github.com/acme/private-tool", wantPrivate: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := groupInputsFixture()
			in.MateProjectName = "acme-juno"
			in.Utilities = []GroupUtility{{Hostname: "mailpit", ServiceType: "alpine@3.21", BuildFromGit: tt.live, SetupName: "mailpit"}}
			layout, _, err := BuildGroupRecipe(in)
			if err != nil {
				t.Fatalf("BuildGroupRecipe: %v", err)
			}
			for _, tier := range layout.Tiers {
				mailpit := serviceNode(t, tier.ImportYAML, "mailpit")
				got := map[string]string{}
				for _, key := range []string{"buildFromGit", "zeropsSetup"} {
					if node := mappingValue(mailpit, key); node != nil {
						got[key] = node.Value
					}
				}
				want := map[string]string{"buildFromGit": tt.wantBuild, "zeropsSetup": tt.wantSetup}
				if tt.wantPrivate {
					want = map[string]string{}
				}
				if !maps.Equal(got, want) {
					t.Errorf("%s: mailpit = %v, want %v", tier.Title, got, want)
				}
				comment := strings.ToLower(mailpit.HeadComment)
				says := strings.Contains(comment, "private repository") && strings.Contains(comment, "acme-juno") && strings.Contains(comment, "by hand")
				if says != tt.wantPrivate {
					t.Errorf("%s: mailpit's comment is %q; want one saying it builds from a private repository in acme-juno and its source is set by hand: %v",
						tier.Title, mailpit.HeadComment, tt.wantPrivate)
				}
			}
			for path, body := range groupFiles(t, layout) {
				if strings.Contains(body, fakeGitHubToken) || strings.Contains(body, "?ref=") || strings.Contains(body, "#readme") {
					t.Errorf("%s carries more of the build URL than its repository", path)
				}
			}
		})
	}
}

// A utility named like a group environment's runtime would give that tier
// the hostname twice, which the import refuses: it stays on the Mate's tier
// only, and the composer says why.
func TestBuildGroupRecipe_UtilityCollidingWithAPromotedName(t *testing.T) {
	t.Parallel()
	in := groupInputsFixture()
	in.Utilities = []GroupUtility{{Hostname: "api", ServiceType: "alpine@3.21", BuildFromGit: "https://github.com/acme/api-docs"}}
	layout, warnings, err := BuildGroupRecipe(in)
	if err != nil {
		t.Fatalf("BuildGroupRecipe: %v", err)
	}
	for _, tier := range layout.Tiers {
		services := mappingValue(tierMapping(t, tier.ImportYAML), "services")
		seen := map[string]int{}
		for _, item := range services.Content {
			seen[mappingValue(item, "hostname").Value]++
		}
		for host, n := range seen {
			if n > 1 {
				t.Errorf("%s: hostname %q written %d times", tier.Title, host, n)
			}
		}
	}
	if !warningsContain(warnings, `"api"`) {
		t.Errorf("warnings %v do not name the collision", warnings)
	}
}

// serviceNode is a tier's service with the given hostname.
func serviceNode(t *testing.T, body, hostname string) *yaml.Node {
	t.Helper()
	services := mappingValue(tierMapping(t, body), "services")
	for _, item := range services.Content {
		if node := mappingValue(item, "hostname"); node != nil && node.Value == hostname {
			return item
		}
	}
	t.Fatalf("no service %q in:\n%s", hostname, body)
	return nil
}

// tierBody is the import.yaml of the tier with the given title.
func tierBody(t *testing.T, tiers []recipe.Tier, title string) string {
	t.Helper()
	for _, tier := range tiers {
		if tier.Title == title {
			return tier.ImportYAML
		}
	}
	t.Fatalf("no tier %q", title)
	return ""
}
