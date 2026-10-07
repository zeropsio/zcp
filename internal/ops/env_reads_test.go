package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func keySet(keys ...string) map[string]bool {
	out := map[string]bool{}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func testProject(shared map[string]bool, hosts ...string) ProjectEnvScope {
	stacks := make([]platform.ServiceStack, 0, len(hosts))
	for _, h := range hosts {
		stacks = append(stacks, platform.ServiceStack{Name: h})
	}
	return ProjectEnvScope{Shared: shared, Hosts: NewEnvRefClassifier(stacks)}
}

// TestServiceEnvScopeReads_MeasuredCases pins the resolution measured live
// 2026-10-07: renames and chains resolve, own beats Shared, `KEY: ${KEY}`
// is a literal and poisons every entry referencing KEY, an unresolved name
// stays literal, platform keys resolve.
func TestServiceEnvScopeReads_MeasuredCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries map[string]string
		own     map[string]bool
		shared  map[string]bool
		want    []VaultKey
	}{
		{
			name:    "rename reads the Shared value",
			entries: map[string]string{"R": "${SHARED}"},
			shared:  keySet("SHARED"),
			want:    []VaultKey{{Key: "SHARED"}},
		},
		{
			name:    "own value",
			entries: map[string]string{"APP_KEY": "${APP_KEY_SECRET}"},
			own:     keySet("APP_KEY_SECRET"),
			want:    []VaultKey{{Service: "app", Key: "APP_KEY_SECRET"}},
		},
		{
			name:    "own value beats Shared of the same name",
			entries: map[string]string{"A": "${K}"},
			own:     keySet("K"),
			shared:  keySet("K"),
			want:    []VaultKey{{Service: "app", Key: "K"}},
		},
		{
			name:    "chain through another entry",
			entries: map[string]string{"R": "${SHARED}", "R2": "${R}-x"},
			shared:  keySet("SHARED"),
			want:    []VaultKey{{Key: "SHARED"}},
		},
		{
			name:    "self-shadow reads nothing and poisons its referrers",
			entries: map[string]string{"KEY": "${KEY}", "X": "prefix-${KEY}"},
			shared:  keySet("KEY"),
			want:    nil,
		},
		{
			name:    "an entry of the key's name shadows the vault value",
			entries: map[string]string{"K": "${K_SECRET}", "B": "${K}"},
			shared:  keySet("K", "K_SECRET"),
			want:    []VaultKey{{Key: "K_SECRET"}},
		},
		{
			name:    "another service's value",
			entries: map[string]string{"DATABASE_URL": "${db_connectionString}"},
			want:    []VaultKey{{Service: "db", Key: "connectionString"}},
		},
		{
			name:    "literal reads nothing",
			entries: map[string]string{"NODE_ENV": "production"},
			want:    nil,
		},
		{
			name:    "unresolved reads nothing",
			entries: map[string]string{"A": "${NOPE}"},
			want:    nil,
		},
		{
			name:    "platform key reads the service's own",
			entries: map[string]string{"APP_HOST": "${hostname}"},
			want:    []VaultKey{{Service: "app", Key: "hostname"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := ServiceEnvScope{Hostname: "app", Entries: tt.entries, Own: tt.own}
			got := svc.Reads(testProject(tt.shared, "app", "db"))
			if len(got) != len(tt.want) {
				t.Fatalf("reads = %v, want %v", got, tt.want)
			}
			for _, w := range tt.want {
				if !got[w] {
					t.Errorf("reads = %v, missing %v", got, w)
				}
			}
		})
	}
}

// TestServiceEnvScopeRefs_Kinds pins the classification the deploy
// preflight reads: which references reach the app as literal text.
func TestServiceEnvScopeRefs_Kinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		hostKeys map[string]map[string]bool
		want     EnvRefKind
	}{
		{"Shared", "${SHARED}", nil, EnvRefShared},
		{"own", "${OWN}", nil, EnvRefOwn},
		{"entry", "${OTHER}", nil, EnvRefEntry},
		{"self", "${A}", nil, EnvRefSelf},
		{"platform key", "${zeropsSubdomain}", nil, EnvRefOwn},
		{"host with unknown keys", "${db_password}", nil, EnvRefHost},
		{"host with the key", "${db_password}", map[string]map[string]bool{"db": keySet("password")}, EnvRefHost},
		{"host without the key", "${db_pasword}", map[string]map[string]bool{"db": keySet("password")}, EnvRefUnresolved},
		{"host platform key", "${db_hostname}", map[string]map[string]bool{"db": keySet("password")}, EnvRefHost},
		{"nothing", "${NOPE}", nil, EnvRefUnresolved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			project := testProject(keySet("SHARED"), "app", "db")
			project.HostKeys = tt.hostKeys
			svc := ServiceEnvScope{
				Hostname: "app",
				Entries:  map[string]string{"A": tt.value, "OTHER": "x"},
				Own:      keySet("OWN"),
			}
			refs := svc.Refs(project)
			if len(refs) != 1 {
				t.Fatalf("refs = %v, want one", refs)
			}
			if refs[0].Kind != tt.want {
				t.Errorf("kind = %v, want %v", refs[0].Kind, tt.want)
			}
		})
	}
}

// TestEnvKeyReaders — the services a set or delete of one key restarts.
func TestEnvKeyReaders(t *testing.T) {
	t.Parallel()
	services := []ServiceEnvScope{
		{Hostname: "app", Entries: map[string]string{"STRIPE_KEY": "${STRIPE}", "SECRET": "${app_TOKEN}"}},
		{Hostname: "worker", Entries: map[string]string{"NODE_ENV": "production"}},
		{Hostname: "api", Entries: map[string]string{"S": "${STRIPE}", "T": "${TOKEN}"}, Own: keySet("STRIPE")},
		{Hostname: "web", Entries: map[string]string{"STRIPE": "${STRIPE}"}},
		{Hostname: "admin", Entries: map[string]string{"DB": "${db_password}", "U": "${app_TOKEN}"}},
	}
	tests := []struct {
		name   string
		target VaultKey
		want   []string
	}{
		{"Shared key read by a rename; own and self-shadow are not readers", VaultKey{Key: "STRIPE"}, []string{"app"}},
		{"own key read by its service", VaultKey{Service: "api", Key: "STRIPE"}, []string{"api"}},
		{"service key read by host references", VaultKey{Service: "app", Key: "TOKEN"}, []string{"admin", "app"}},
		{"managed key", VaultKey{Service: "db", Key: "password"}, []string{"admin"}},
		{"nothing reads it", VaultKey{Key: "UNUSED"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EnvKeyReaders(tt.target, services, testProject(nil, "app", "worker", "api", "web", "admin", "db"))
			if !slices.Equal(got, tt.want) {
				t.Errorf("readers = %v, want %v", got, tt.want)
			}
		})
	}
}

func runtimeStack(id, name, avID string) platform.ServiceStack {
	return platform.ServiceStack{
		ID: id, Name: name, Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
		ActiveAppVersion:     &platform.ActiveAppVersionDigest{ID: avID},
	}
}

// TestReadProjectEnvScopes_FromDeployedRows — the scopes come from the
// deployed run entries (app-version rows), each service's own rows and the
// Shared rows; readers fall out of them.
func TestReadProjectEnvScopes_FromDeployedRows(t *testing.T) {
	t.Parallel()
	app := runtimeStack("s-app", "app", "av-app")
	worker := runtimeStack("s-worker", "worker", "av-worker")
	fresh := runtimeStack("s-fresh", "fresh", "")
	fresh.ActiveAppVersion = nil
	db := platform.ServiceStack{ID: "s-db", Name: "db", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@17", ServiceStackTypeCategoryName: "STANDARD"}}
	services := []platform.ServiceStack{app, worker, fresh, db}
	mock := platform.NewMock().
		WithServices(services).
		WithProjectEnv([]platform.ProjectEnvVar{{ID: "p1", Key: "STRIPE", Content: "x", Type: platform.ProjectEnvUser}}).
		WithServiceEnv("s-app", []platform.ServiceEnvVar{{ID: "u1", Key: "APP_KEY_SECRET", Content: "y", Sensitive: true}}).
		WithServiceEnv("s-db", []platform.ServiceEnvVar{{ID: "u2", Key: "password", Content: "z"}}).
		WithAppVersionUserData("av-app", []platform.ServiceEnvVar{
			{Key: "STRIPE_KEY", Content: "${STRIPE}"},
			{Key: "APP_KEY", Content: "${APP_KEY_SECRET}"},
			{Key: "DB_PASS", Content: "${db_password}"},
		}).
		WithAppVersionUserData("av-worker", []platform.ServiceEnvVar{{Key: "NODE_ENV", Content: "production"}})

	scopes, err := ReadProjectEnvScopes(context.Background(), mock, "proj", services)
	if err != nil {
		t.Fatalf("ReadProjectEnvScopes: %v", err)
	}
	if len(scopes.Unread) != 0 {
		t.Errorf("unread = %v, want none", scopes.Unread)
	}
	tests := []struct {
		target VaultKey
		want   []string
	}{
		{VaultKey{Key: "STRIPE"}, []string{"app"}},
		{VaultKey{Service: "app", Key: "APP_KEY_SECRET"}, []string{"app"}},
		{VaultKey{Service: "db", Key: "password"}, []string{"app"}},
		{VaultKey{Key: "NODE_ENV"}, nil},
	}
	for _, tt := range tests {
		if got := scopes.Readers(tt.target); !slices.Equal(got, tt.want) {
			t.Errorf("Readers(%v) = %v, want %v", tt.target, got, tt.want)
		}
	}
}

// TestReadProjectEnvScopes_UnreadableService — a service whose rows fail to
// read is named, never taken as reading nothing.
func TestReadProjectEnvScopes_UnreadableService(t *testing.T) {
	t.Parallel()
	app := runtimeStack("s-app", "app", "av-app")
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{app}).
		WithError("GetAppVersionUserData", errors.New("boom"))
	scopes, err := ReadProjectEnvScopes(context.Background(), mock, "proj", []platform.ServiceStack{app})
	if err != nil {
		t.Fatalf("ReadProjectEnvScopes: %v", err)
	}
	if !slices.Equal(scopes.Unread, []string{"app"}) {
		t.Errorf("unread = %v, want [app]", scopes.Unread)
	}
}

// TestReadProjectEnvScopes_SharedReadFails — without the Shared rows no
// reference can be judged; the read fails.
func TestReadProjectEnvScopes_SharedReadFails(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("GetProjectEnv", errors.New("boom"))
	if _, err := ReadProjectEnvScopes(context.Background(), mock, "proj", nil); err == nil {
		t.Fatal("want an error when the Shared rows cannot be read")
	}
}
