package topology

import "testing"

func TestIsManagedService(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		serviceType string
		want        bool
	}{
		{"postgresql", "postgresql@16", true},
		{"valkey", "valkey@7.2", true},
		{"object_storage_hyphen", "object-storage@1", true},
		{"shared_storage_hyphen", "shared-storage@1", true},
		{"object_storage_bare", "object-storage", true},
		{"shared_storage_bare", "shared-storage", true},
		{"nats", "nats@2.10", true},
		{"clickhouse", "clickhouse@24.3", true},
		{"qdrant", "qdrant@1.12", true},
		{"typesense", "typesense@27.1", true},
		{"runtime_bun", "bun@1.2", false},
		{"runtime_nodejs", "nodejs@22", false},
		{"runtime_go", "go@1", false},
		{"runtime_php", "php-nginx@8.4", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsManagedService(tt.serviceType); got != tt.want {
				t.Errorf("IsManagedService(%q) = %v, want %v", tt.serviceType, got, tt.want)
			}
		})
	}
}

func TestIsRuntimeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		svcType string
		want    bool
	}{
		{"php-nginx@8.4", true},
		{"nodejs@22", true},
		{"go@1", true},
		{"bun@1.2", true},
		{"python@3.12", true},
		{"nginx@1.22", true},
		{"static", true},
		{"rust@stable", true},
		{"docker@26.1", true},
		{"ubuntu@24.04", true},
		// managed and utility are NOT runtime
		{"postgresql@17", false},
		{"mariadb@10.6", false},
		{"valkey@7.2", false},
		{"meilisearch@1.20", false},
		{"object-storage", false},
		{"shared-storage", false},
		{"nats@2.12", false},
		{"kafka@3.9", false},
		{"mailpit", false},
	}

	for _, tt := range tests {
		t.Run(tt.svcType, func(t *testing.T) {
			t.Parallel()
			got := IsRuntimeType(tt.svcType)
			if got != tt.want {
				t.Errorf("IsRuntimeType(%q) = %v, want %v", tt.svcType, got, tt.want)
			}
		})
	}
}

func TestServiceTypeCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		serviceType   string
		supportsMode  bool
		supportsScale bool
		isObjStorage  bool
		isUtility     bool
	}{
		{"runtime", "php-nginx@8.4", false, true, false, false},
		{"postgresql", "postgresql@16", true, true, false, false},
		{"valkey", "valkey@7.2", true, true, false, false},
		{"meilisearch", "meilisearch@1", true, true, false, false},
		{"object_storage", "object-storage", false, false, true, false},
		{"shared_storage", "shared-storage", true, false, false, false},
		{"mailpit", "mailpit", false, true, false, true},
		{"nodejs", "nodejs@22", false, true, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ServiceSupportsMode(tt.serviceType); got != tt.supportsMode {
				t.Errorf("ServiceSupportsMode(%q) = %v, want %v", tt.serviceType, got, tt.supportsMode)
			}
			if got := ServiceSupportsAutoscaling(tt.serviceType); got != tt.supportsScale {
				t.Errorf("ServiceSupportsAutoscaling(%q) = %v, want %v", tt.serviceType, got, tt.supportsScale)
			}
			if got := IsObjectStorageType(tt.serviceType); got != tt.isObjStorage {
				t.Errorf("IsObjectStorageType(%q) = %v, want %v", tt.serviceType, got, tt.isObjStorage)
			}
			if got := IsUtilityType(tt.serviceType); got != tt.isUtility {
				t.Errorf("IsUtilityType(%q) = %v, want %v", tt.serviceType, got, tt.isUtility)
			}
		})
	}
}

// TestPlanModeAliases pins the alias contract — PlanMode* values are
// exact equals to their Mode counterparts, so callers carry one
// vocabulary even when the field name flavors differ.
func TestPlanModeAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		alias Mode
		want  Mode
	}{
		{PlanModeStandard, ModeStandard},
		{PlanModeDev, ModeDev},
		{PlanModeSimple, ModeSimple},
		{PlanModeLocalStage, ModeLocalStage},
		{PlanModeLocalOnly, ModeLocalOnly},
	}
	for _, c := range cases {
		if c.alias != c.want {
			t.Errorf("alias %q != target %q", c.alias, c.want)
		}
	}
}

// TestDeployRoleAliases pins the deploy-role alias contract.
func TestDeployRoleAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		alias Mode
		want  Mode
	}{
		{DeployRoleDev, ModeDev},
		{DeployRoleStage, ModeStage},
		{DeployRoleSimple, ModeSimple},
	}
	for _, c := range cases {
		if c.alias != c.want {
			t.Errorf("alias %q != target %q", c.alias, c.want)
		}
	}
}
