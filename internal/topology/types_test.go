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

// TestIsPushSource pins the predicate truth table for every Mode value.
// IsPushSource is the load-bearing dispatcher used by handleGitPush
// validation and atom-rendering filters; silent regressions corrupt
// push-target resolution across pair scenarios. Six rows cover the closed
// Mode set so a future Mode addition that forgets to extend the predicate
// (or the predicate that drifts from the Mode set) fails at test time.
func TestIsPushSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode Mode
		want bool
		why  string
	}{
		{ModeStandard, true, "dev half of standard pair — source of push"},
		{ModeSimple, true, "single container service — source of push"},
		{ModeLocalStage, true, "local CWD paired with Zerops stage — source of push"},
		{ModeLocalOnly, true, "local CWD without Zerops link — source of push"},
		{ModeDev, false, "legacy dev-only mode — invalid combo with push-git"},
		{ModeStage, false, "build target half of standard pair — not source"},
	}
	for _, c := range cases {
		t.Run(string(c.mode), func(t *testing.T) {
			t.Parallel()
			if got := IsPushSource(c.mode); got != c.want {
				t.Errorf("IsPushSource(%q) = %v, want %v (%s)", c.mode, got, c.want, c.why)
			}
		})
	}
}

// TestCloseDeployModeValues pins the closed enum set so a typo silently
// introducing a new value (e.g. "auto-close" vs "auto") fails at test time
// rather than rendering against atoms with stale axis filters.
func TestCloseDeployModeValues(t *testing.T) {
	t.Parallel()
	set := map[CloseDeployMode]struct{}{
		CloseModeUnset:   {},
		CloseModeAuto:    {},
		CloseModeGitPush: {},
		CloseModeManual:  {},
	}
	if len(set) != 4 {
		t.Fatalf("CloseDeployMode constants must be 4 distinct values, got %d", len(set))
	}
	for _, want := range []CloseDeployMode{"unset", "auto", "git-push", "manual"} {
		if _, ok := set[want]; !ok {
			t.Errorf("CloseDeployMode missing canonical value %q", want)
		}
	}
}

// TestGitPushStateValues pins the closed enum set for the per-pair
// git-push capability dimension.
func TestGitPushStateValues(t *testing.T) {
	t.Parallel()
	set := map[GitPushState]struct{}{
		GitPushUnconfigured: {},
		GitPushConfigured:   {},
		GitPushBroken:       {},
		GitPushUnknown:      {},
	}
	if len(set) != 4 {
		t.Fatalf("GitPushState constants must be 4 distinct values, got %d", len(set))
	}
	for _, want := range []GitPushState{"unconfigured", "configured", "broken", "unknown"} {
		if _, ok := set[want]; !ok {
			t.Errorf("GitPushState missing canonical value %q", want)
		}
	}
}

// TestBuildIntegrationValues pins the closed enum set for the per-pair
// ZCP-managed CI integration dimension. Three values: none / webhook /
// actions. Future additions (gitlab, bitbucket, jenkins) must extend the
// enum AND this test in the same change.
func TestBuildIntegrationValues(t *testing.T) {
	t.Parallel()
	set := map[BuildIntegration]struct{}{
		BuildIntegrationNone:    {},
		BuildIntegrationWebhook: {},
		BuildIntegrationActions: {},
	}
	if len(set) != 3 {
		t.Fatalf("BuildIntegration constants must be 3 distinct values, got %d", len(set))
	}
	for _, want := range []BuildIntegration{"none", "webhook", "actions"} {
		if _, ok := set[want]; !ok {
			t.Errorf("BuildIntegration missing canonical value %q", want)
		}
	}
}
