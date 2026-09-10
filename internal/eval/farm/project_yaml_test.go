package farm

import (
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

func apiKeyDescriptor() RunDescriptor {
	return RunDescriptor{
		BatchID:         "batch-2026-09-10",
		RunID:           "run-abc123",
		ScenarioID:      "recipe-first-deploy",
		EvaluatorSHA256: "eval-sha-aaaa",
		CandidateSHA256: "cand-sha-bbbb",
		ScenariosDigest: "scenarios-sha-ffff",
		Sink: Sink{
			URL:    "https://s3.prg1.zerops.app",
			Bucket: "zcp-farm",
			Key:    "sink-key-cccc",
			Secret: "sink-secret-dddd",
		},
		Credentials: []Credential{{Mode: CredentialAPIKey, Value: "sk-ant-farm-key"}},
	}
}

func oauthDescriptor() RunDescriptor {
	d := apiKeyDescriptor()
	d.RunID = "run-oauth456"
	d.Credentials = []Credential{{Mode: CredentialOAuthToken, Value: "oauth-token-value"}}
	return d
}

func launchDescriptor() RunDescriptor {
	d := apiKeyDescriptor()
	d.RunID = "run-launch789"
	d.LaunchKey = "launch-key-eeee"
	return d
}

// TestImportYAML_Descriptor_MatchesGolden pins the byte-stable render for
// each credential/launch variant against a hand-written golden (never
// dumped from the generator's own first run — spec-eval-farm.md §2.1's
// verified shape, FM-12's env names).
func TestImportYAML_Descriptor_MatchesGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		desc   RunDescriptor
		golden string
	}{
		{"api-key", apiKeyDescriptor(), "testdata/project_yaml/api_key.golden.yaml"},
		{"oauth-token", oauthDescriptor(), "testdata/project_yaml/oauth_token.golden.yaml"},
		{"launch-key", launchDescriptor(), "testdata/project_yaml/launch_key.golden.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ImportYAML: %v", err)
			}
			want, err := os.ReadFile(tc.golden)
			if err != nil {
				t.Fatalf("read golden %s: %v", tc.golden, err)
			}
			if string(got) != string(want) {
				t.Errorf("ImportYAML(%s) mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}
		})
	}
}

// TestImportYAML_BothOrNoCredential_Rejected pins §2.4 "never both in one
// project": exactly one credential kind is required.
func TestImportYAML_BothOrNoCredential_Rejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		credentials []Credential
	}{
		{"neither", nil},
		{"both", []Credential{
			{Mode: CredentialAPIKey, Value: "sk-ant-x"},
			{Mode: CredentialOAuthToken, Value: "oauth-y"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := apiKeyDescriptor()
			d.Credentials = tc.credentials
			_, err := ImportYAML(d)
			if err == nil {
				t.Fatalf("ImportYAML with %s credentials: want error, got nil", tc.name)
			}
		})
	}
}

// TestImportYAML_NeverCarriesAccountKey_AndHostnameHasNoHyphen pins FM-15
// (the generator has no field for the account-wide key, so it cannot leak
// into the output) and the fixed hostname literal "zcp" (hostnames may not
// contain hyphens, verified live — "zcp-farm-<runId>" cannot be the
// hostname).
func TestImportYAML_NeverCarriesAccountKey_AndHostnameHasNoHyphen(t *testing.T) {
	t.Parallel()
	d := apiKeyDescriptor()
	got, err := ImportYAML(d)
	if err != nil {
		t.Fatalf("ImportYAML: %v", err)
	}
	out := string(got)

	if !strings.Contains(out, "hostname: zcp\n") {
		t.Errorf("expected literal hostname %q, got:\n%s", "zcp", out)
	}
	if strings.Contains(out, "hostname: zcp-farm") {
		t.Errorf("hostname must not be the hyphenated project name:\n%s", out)
	}

	// The descriptor's field set (RunDescriptor above) has no account-wide
	// key field at all, so this is a structural guarantee, not a string
	// scrub — assert the generator never emits any of the platform's
	// account-key-shaped env names, which would only be possible via a
	// call site that bypasses RunDescriptor entirely.
	for _, forbidden := range []string{"ZCP_ACCOUNT_KEY", "ACCOUNT_API_KEY"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("output must never carry an account-wide key env, found %q", forbidden)
		}
	}
}

// TestImportYAML_ScenariosDigest_EmittedAsEnv pins the sixth FM-12 row
// (docs/spec-eval-farm.md §2.2, ZCP_FARM_SCENARIOS_DIGEST) — the wrapper
// reads this env to locate scenarios/<digest>/ in the bucket (§1.1).
// Independent oracle: the env name is copied verbatim from the spec's FM-12
// table, not derived from this package's own output.
func TestImportYAML_ScenariosDigest_EmittedAsEnv(t *testing.T) {
	t.Parallel()
	d := apiKeyDescriptor()
	got, err := ImportYAML(d)
	if err != nil {
		t.Fatalf("ImportYAML: %v", err)
	}
	out := string(got)
	const want = `ZCP_FARM_SCENARIOS_DIGEST: "scenarios-sha-ffff"`
	if !strings.Contains(out, want) {
		t.Errorf("ImportYAML output missing %q, got:\n%s", want, out)
	}
}

// TestImportYAML_ValidatesAgainstImportSchema validates the generated YAML
// against the embedded import-yml JSON Schema (internal/schema), reachable
// offline.
func TestImportYAML_ValidatesAgainstImportSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		desc RunDescriptor
	}{
		{"api-key", apiKeyDescriptor()},
		{"oauth-token", oauthDescriptor()},
		{"launch-key", launchDescriptor()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ImportYAML: %v", err)
			}
			if errs := schema.ValidateImportYAML(string(got)); len(errs) > 0 {
				t.Errorf("ValidateImportYAML found %d error(s):", len(errs))
				for _, e := range errs {
					t.Errorf("  %s", e.Error())
				}
			}
		})
	}
}
