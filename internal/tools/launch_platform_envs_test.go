package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestClassifyPlatformEnv_TableDriven pins the closed exact-key
// table. Membership matters: any drift (rename, removal, addition)
// changes auto-classification behavior across every launch.
func TestClassifyPlatformEnv_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key        string
		wantOK     bool
		wantAction string
		wantBucket topology.SecretClassification
	}{
		// Platform-injected subdomain envs — infrastructure.
		{"zeropsSubdomainHost", true, platformEnvActionClassify, topology.SecretClassInfrastructure},
		{"zeropsSubdomainString", true, platformEnvActionClassify, topology.SecretClassInfrastructure},

		// Project-level settings — drop.
		{"envIsolation", true, platformEnvActionDrop, ""},
		{"sshIsolation", true, platformEnvActionDrop, ""},

		// ZCP control-plane envs — drop.
		{"ZCP_API_KEY", true, platformEnvActionDrop, ""},
		{"ZCP_AGENT_TYPE", true, platformEnvActionDrop, ""},
		{"ZCP_BASE_HOST", true, platformEnvActionDrop, ""},
		{"ZCP_BUILTINS_DIR", true, platformEnvActionDrop, ""},
		{"ZCP_PROJECT_DIR", true, platformEnvActionDrop, ""},

		// Out-of-table keys fall through.
		{"APP_KEY", false, "", ""},
		{"DB_PASSWORD", false, "", ""},
		{"", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			t.Parallel()
			got, ok := classifyPlatformEnv(tc.key)
			if ok != tc.wantOK {
				t.Fatalf("classifyPlatformEnv(%q): got ok=%v want %v", tc.key, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Action != tc.wantAction {
				t.Errorf("Action: got %q want %q", got.Action, tc.wantAction)
			}
			if got.Bucket != tc.wantBucket {
				t.Errorf("Bucket: got %q want %q", got.Bucket, tc.wantBucket)
			}
		})
	}
}

// TestClassifyPlatformEnv_UserPrefixedKeysNotDropped pins the
// exact-match semantics: keys merely sharing a prefix with the
// blanket-rejected entries (e.g. user-defined ZCP_CUSTOM) MUST NOT
// auto-class. Drift here would silently break user apps using the
// prefix legitimately — that's the failure mode codex flagged.
func TestClassifyPlatformEnv_UserPrefixedKeysNotDropped(t *testing.T) {
	t.Parallel()
	for _, k := range []string{
		"ZCP_CUSTOM_USER_THING",
		"ZCP_NOT_REAL_PREFIX",
		"ZCP_",            // bare prefix
		"zeropsSubdomain", // partial prefix
		"zeropsOther",     // shares zerops* but not in table
	} {
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			if _, ok := classifyPlatformEnv(k); ok {
				t.Errorf("classifyPlatformEnv(%q): ok=true (would auto-drop a user key); table must match by exact key only", k)
			}
		})
	}
}

// TestNeedsClassifyPromptForLaunch_AutoHandledEnvsSatisfied pins F3:
// the prompt fires only when the agent has work to do. Auto-handled
// envs satisfy themselves without user-supplied classification.
func TestNeedsClassifyPromptForLaunch_AutoHandledEnvsSatisfied(t *testing.T) {
	t.Parallel()
	envs := []ops.ProjectEnvVar{
		{Key: "APP_KEY"}, // user-needed
		{Key: "zeropsSubdomainHost"},
		{Key: "ZCP_API_KEY"},
		{Key: "envIsolation"},
	}

	// User has classified APP_KEY; the three platform envs auto-handle.
	classifications := map[string]string{"APP_KEY": "plain-config"}
	if needsClassifyPromptForLaunch(classifications, envs) {
		t.Errorf("expected prompt suppressed when only platform envs are unclassified")
	}

	// Drop user's classification — prompt should fire again.
	if !needsClassifyPromptForLaunch(map[string]string{}, envs) {
		t.Errorf("expected prompt to fire when user-relevant APP_KEY is missing")
	}
}

// TestFilterUserClassificationEnvs_HidesPlatformKeys pins the prompt-
// row filter: the rows the agent sees contain only envs that need
// user judgment. Source-side raw slice is untouched (caller's view).
func TestFilterUserClassificationEnvs_HidesPlatformKeys(t *testing.T) {
	t.Parallel()
	raw := []ops.ProjectEnvVar{
		{Key: "APP_KEY"},
		{Key: "zeropsSubdomainHost"},
		{Key: "DB_PASSWORD"},
		{Key: "ZCP_API_KEY"},
	}
	got := filterUserClassificationEnvs(raw)
	if len(got) != 2 {
		t.Fatalf("filtered envs: got %d entries want 2 (only user-needed keys)\n%+v", len(got), got)
	}
	wantKeys := map[string]bool{"APP_KEY": true, "DB_PASSWORD": true}
	for _, env := range got {
		if !wantKeys[env.Key] {
			t.Errorf("unexpected key in filtered output: %q", env.Key)
		}
	}
	// Source slice must be unchanged length — defense against caller
	// mutation.
	if len(raw) != 4 {
		t.Errorf("filter mutated source slice length: got %d want 4", len(raw))
	}
}

// TestMergePlatformAutoClassifications pins the effective-classification
// map shape: user values + auto-classified platform values, with user
// precedence on conflicts.
func TestMergePlatformAutoClassifications(t *testing.T) {
	t.Parallel()

	t.Run("empty user input", func(t *testing.T) {
		t.Parallel()
		got := mergePlatformAutoClassifications(map[string]topology.SecretClassification{})
		// Only "classify" auto-actions appear — drops are absent (by
		// design: absence excludes from bundle composer).
		if got["zeropsSubdomainHost"] != topology.SecretClassInfrastructure {
			t.Errorf("zeropsSubdomainHost: got %q want infrastructure", got["zeropsSubdomainHost"])
		}
		if got["zeropsSubdomainString"] != topology.SecretClassInfrastructure {
			t.Errorf("zeropsSubdomainString: got %q want infrastructure", got["zeropsSubdomainString"])
		}
		// Dropped platform envs MUST NOT appear.
		for _, k := range []string{"envIsolation", "sshIsolation", "ZCP_API_KEY", "ZCP_AGENT_TYPE", "ZCP_BASE_HOST"} {
			if _, present := got[k]; present {
				t.Errorf("dropped platform env %q must not appear in effective map", k)
			}
		}
	})

	t.Run("user input merged", func(t *testing.T) {
		t.Parallel()
		user := map[string]topology.SecretClassification{
			"APP_KEY":  topology.SecretClassExternalSecret,
			"LOG_FILE": topology.SecretClassPlainConfig,
		}
		got := mergePlatformAutoClassifications(user)
		if got["APP_KEY"] != topology.SecretClassExternalSecret {
			t.Errorf("APP_KEY: got %q want external-secret", got["APP_KEY"])
		}
		if got["zeropsSubdomainHost"] != topology.SecretClassInfrastructure {
			t.Errorf("zeropsSubdomainHost: got %q want infrastructure (auto-classify still applies)", got["zeropsSubdomainHost"])
		}
	})

	t.Run("user override wins", func(t *testing.T) {
		t.Parallel()
		user := map[string]topology.SecretClassification{
			"zeropsSubdomainHost": topology.SecretClassPlainConfig, // unusual but allowed
		}
		got := mergePlatformAutoClassifications(user)
		if got["zeropsSubdomainHost"] != topology.SecretClassPlainConfig {
			t.Errorf("zeropsSubdomainHost: got %q want plain-config (user override)", got["zeropsSubdomainHost"])
		}
	})
}

// TestClassifyPromptResponse_HidesPlatformEnvs is an end-to-end pin
// on the handler: feed envs including platform-known and user keys,
// assert the response carries ONLY user keys in the classifications
// row table.
func TestClassifyPromptResponse_HidesPlatformEnvs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{Key: "APP_KEY", Content: "secret-value"},
		{Key: "zeropsSubdomainHost", Content: "abc.zerops.app"},
		{Key: "ZCP_API_KEY", Content: "zcp-key-value"},
		{Key: "envIsolation", Content: "project"},
		{Key: "DB_PASSWORD", Content: "db-secret"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
		// EnvClassifications empty — prompt should fire for the
		// user-needed envs only.
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, t.TempDir(), runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	text := extractText(result)
	resp := decodeLaunchResp(t, []byte(text))

	if resp.Status != "classify-prompt" {
		t.Fatalf("status: got %q want classify-prompt\nbody:\n%s", resp.Status, text)
	}
	if len(resp.Classifications) != 2 {
		t.Fatalf("classifications rows: got %d want 2 (only APP_KEY + DB_PASSWORD)\n%+v", len(resp.Classifications), resp.Classifications)
	}
	keysSeen := map[string]bool{}
	for _, row := range resp.Classifications {
		keysSeen[row.Key] = true
	}
	if !keysSeen["APP_KEY"] || !keysSeen["DB_PASSWORD"] {
		t.Errorf("missing user-needed keys in rows: %+v", keysSeen)
	}
	for _, banned := range []string{"zeropsSubdomainHost", "ZCP_API_KEY", "envIsolation"} {
		if keysSeen[banned] {
			t.Errorf("platform env %q must not appear in classifications rows", banned)
		}
	}
	// Defense-in-depth: no env VALUES should leak into the response.
	for _, val := range []string{"secret-value", "zcp-key-value", "db-secret"} {
		if strings.Contains(text, val) {
			t.Errorf("env value %q leaked into response", val)
		}
	}
}

// TestClassifyPromptResponse_OnlyPlatformEnvs_NoPromptFires pins the
// no-loop guarantee: when EVERY source env is platform-handled, the
// workflow advances past classify-prompt without an empty loop.
func TestClassifyPromptResponse_OnlyPlatformEnvs_NoPromptFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newLaunchMockClient().WithProjectEnv([]platform.EnvVar{
		{Key: "zeropsSubdomainHost", Content: "abc.zerops.app"},
		{Key: "ZCP_API_KEY", Content: "zcp-value"},
	})

	input := WorkflowInput{
		Workflow:              workflowLaunchProduction,
		ProductionProjectName: "myapp-prod",
		TargetService:         "app",
	}
	result, _, err := handleLaunchProduction(ctx, "source-project-id", client, input, t.TempDir(), runtime.Info{}, nil)
	if err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	// With only auto-handled envs, the workflow advances to ready-to-launch.
	if resp.Status != "ready-to-launch" {
		t.Errorf("status: got %q want ready-to-launch (auto-handled envs should not trigger prompt)", resp.Status)
	}
}
