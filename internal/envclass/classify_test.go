package envclass_test

import (
	"testing"

	"github.com/zeropsio/zcp/internal/envclass"
	"github.com/zeropsio/zcp/internal/ops/inventory"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestClassifyProjectEnv_SystemDrops pins Rule 2 (§5.4): every
// project env with Type=SYSTEM drops, regardless of Editable. Live
// fixtures: zeropsSubdomain* (Editable=false), staticCdnUrl /
// apiCdnUrl / storageCdnUrl (Editable=false; CLOSES F19), envIsolation
// / sshIsolation (Editable=true).
func TestClassifyProjectEnv_SystemDrops(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  inventory.ProjectEnvVar
	}{
		{"zeropsSubdomainHost", inventory.ProjectEnvVar{Key: "zeropsSubdomainHost", Type: platform.ProjectEnvSystem, Editable: false}},
		{"zeropsSubdomainString", inventory.ProjectEnvVar{Key: "zeropsSubdomainString", Type: platform.ProjectEnvSystem, Editable: false}},
		{"staticCdnUrl", inventory.ProjectEnvVar{Key: "staticCdnUrl", Type: platform.ProjectEnvSystem, Editable: false}},
		{"apiCdnUrl", inventory.ProjectEnvVar{Key: "apiCdnUrl", Type: platform.ProjectEnvSystem, Editable: false}},
		{"storageCdnUrl", inventory.ProjectEnvVar{Key: "storageCdnUrl", Type: platform.ProjectEnvSystem, Editable: false}},
		{"envIsolation (Editable=true subset)", inventory.ProjectEnvVar{Key: "envIsolation", Type: platform.ProjectEnvSystem, Editable: true}},
		{"sshIsolation (Editable=true subset)", inventory.ProjectEnvVar{Key: "sshIsolation", Type: platform.ProjectEnvSystem, Editable: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := envclass.ClassifyProjectEnv(tc.env)
			if got.Decision != envclass.Drop {
				t.Errorf("Decision: got %v want Drop (Type=SYSTEM must always drop)", got.Decision)
			}
			if got.Bias != topology.SecretClassUnset {
				t.Errorf("Bias on Drop: got %q want unset", got.Bias)
			}
		})
	}
}

// TestClassifyProjectEnv_CDNKeys_SystemDrops is the migrated F19
// reproducer (formerly internal/ops/launch_bundle_f19_f20_f21_test.go::
// TestF19_CDNKeysLeakIntoTargetImportYaml under build tag regression_red).
// CDN URLs are server-Type=SYSTEM; classifier drops them universally
// regardless of name. F19 closes: no static table, no name-pattern
// fallback — SDK Type field is authoritative.
func TestClassifyProjectEnv_CDNKeys_SystemDrops(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"staticCdnUrl", "apiCdnUrl", "storageCdnUrl"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			got := envclass.ClassifyProjectEnv(inventory.ProjectEnvVar{
				Key:  key,
				Type: platform.ProjectEnvSystem,
			})
			if got.Decision != envclass.Drop {
				t.Errorf("CDN key %q with Type=SYSTEM: got Decision %v want Drop", key, got.Decision)
			}
		})
	}
}

// TestClassifyProjectEnv_UserPattern_BiasAutoSecret pins Rule 3
// bias (§5.4): USER-typed keys matching the credential suffix
// pattern get AutoSecret. Case-insensitive; covers _KEY, _SECRET,
// _TOKEN, _PASS, APP_KEY shapes.
func TestClassifyProjectEnv_UserPattern_BiasAutoSecret(t *testing.T) {
	t.Parallel()
	cases := []string{
		"APP_KEY", "JWT_SECRET", "DB_PASS",
		"GITHUB_TOKEN", "session_pass", "OpenAI_API_KEY",
		"DATABASE_PASS", "STRIPE_SECRET", "MY_AUTH_TOKEN",
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			got := envclass.ClassifyProjectEnv(inventory.ProjectEnvVar{
				Key:  key,
				Type: platform.ProjectEnvUser,
			})
			if got.Decision != envclass.PromptUser {
				t.Errorf("Decision: got %v want PromptUser", got.Decision)
			}
			if got.Bias != topology.SecretClassAutoSecret {
				t.Errorf("Bias: got %q want %q", got.Bias, topology.SecretClassAutoSecret)
			}
		})
	}
}

// TestClassifyProjectEnv_UserOther_BiasPlainConfig pins the
// non-credential-pattern fallback bias: keys outside the credential
// suffix shape get PlainConfig bias on PromptUser. DB_PASSWORD,
// MY_AUTH (no _KEY/_SECRET/_TOKEN/_PASS/APP_KEY suffix per plan §5.4
// regex literal) intentionally fall here — pattern is exact-suffix
// match, not prefix or fuzzy. LLM/user may upgrade at classify-prompt.
func TestClassifyProjectEnv_UserOther_BiasPlainConfig(t *testing.T) {
	t.Parallel()
	cases := []string{
		"LOG_LEVEL", "APP_ENV", "PORT",
		"NODE_ENV", "FEATURE_FLAG_NEW_UI", "REGION",
		"CUSTOM_HOST", "MAX_RETRIES",
		"DB_PASSWORD", "MY_AUTH", // pattern-edge: ends with PASSWORD/AUTH, not _PASS/_KEY
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			got := envclass.ClassifyProjectEnv(inventory.ProjectEnvVar{
				Key:  key,
				Type: platform.ProjectEnvUser,
			})
			if got.Decision != envclass.PromptUser {
				t.Errorf("Decision: got %v want PromptUser", got.Decision)
			}
			if got.Bias != topology.SecretClassPlainConfig {
				t.Errorf("Bias: got %q want %q", got.Bias, topology.SecretClassPlainConfig)
			}
		})
	}
}

// TestClassifyProjectEnv_EmptyType_DefaultsToUser pins the test-fixture
// permissive default: when ProjectEnvVar is constructed without setting
// Type (zero-value `ProjectEnvType("")`), the classifier treats it as
// USER rather than failing. Real SDK responses always populate Type;
// this rule only matters for ad-hoc test constructions.
func TestClassifyProjectEnv_EmptyType_DefaultsToUser(t *testing.T) {
	t.Parallel()
	got := envclass.ClassifyProjectEnv(inventory.ProjectEnvVar{Key: "FOO_BAR"})
	if got.Decision != envclass.PromptUser {
		t.Errorf("empty Type: got Decision %v want PromptUser (test-fixture default)", got.Decision)
	}
	if got.Bias != topology.SecretClassPlainConfig {
		t.Errorf("empty Type FOO_BAR: got Bias %q want PlainConfig", got.Bias)
	}
}

// TestClassifyServiceEnv_AnyType_Drops pins Rule 1 (§5.4): every
// service env drops, regardless of UserDataTypeEnum value. Both 2026-08
// SDK enum values (USER, SYSTEM) hit the same Drop outcome.
func TestClassifyServiceEnv_AnyType_Drops(t *testing.T) {
	t.Parallel()
	cases := []platform.ServiceEnvType{
		platform.ServiceEnvUser,
		platform.ServiceEnvSystem,
		"", // zero-value default also drops (test-fixture permissive)
	}
	for _, typ := range cases {
		name := string(typ)
		if name == "" {
			name = "empty-type"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := envclass.ClassifyServiceEnv(inventory.ServiceEnvVar{
				Key:  "accessKeyId",
				Type: typ,
			})
			if got.Decision != envclass.Drop {
				t.Errorf("Type=%q: got Decision %v want Drop", typ, got.Decision)
			}
			if got.Bias != topology.SecretClassUnset {
				t.Errorf("Bias on Drop: got %q want unset", got.Bias)
			}
		})
	}
}

// TestClassifyProjectEnv_ZCPApiKey_PromptUser pins the behavioral
// shift the workflow-family redesign introduces: ZCP_API_KEY (live
// server marks it Type=USER, Sensitive=false; verified eval-zcp
// 2026-05-14) is no longer auto-dropped by a static table — the
// classifier surfaces it to PromptUser for LLM/user judgment. Atom
// guidance (Phase 6a) will tell the LLM these are control-plane
// envs to drop; classifier just reports the SDK type honestly.
func TestClassifyProjectEnv_ZCPApiKey_PromptUser(t *testing.T) {
	t.Parallel()
	got := envclass.ClassifyProjectEnv(inventory.ProjectEnvVar{
		Key:       "ZCP_API_KEY",
		Type:      platform.ProjectEnvUser,
		Sensitive: false, // server marks it false despite being a literal bearer token
	})
	if got.Decision != envclass.PromptUser {
		t.Errorf("ZCP_API_KEY: got %v want PromptUser (server says Type=USER; LLM/atom-guidance decides)", got.Decision)
	}
	if got.Bias != topology.SecretClassAutoSecret {
		t.Errorf("ZCP_API_KEY bias: got %q want AutoSecret (name ends _KEY)", got.Bias)
	}
}
