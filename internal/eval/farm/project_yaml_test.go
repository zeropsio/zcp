package farm

import (
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

func oauthDescriptor() RunDescriptor {
	return RunDescriptor{
		BatchID:         "batch-2026-09-10",
		RunID:           "run-oauth456",
		ScenarioID:      "recipe-first-deploy",
		EvaluatorSHA256: "eval-sha-aaaa",
		CandidateSHA256: "cand-sha-bbbb",
		ScenariosDigest: "scenarios-sha-ffff",
		WrapperSHA256:   "wrapper-sha-9999",
		Sink: Sink{
			URL:    "https://s3.prg1.zerops.app",
			Bucket: "zcp-farm",
			Key:    "sink-key-cccc",
			Secret: "sink-secret-dddd",
		},
		OAuthToken: "oauth-token-value",
		RunToken:   "run-token-value",
	}
}

func launchDescriptor() RunDescriptor {
	d := oauthDescriptor()
	d.RunID = "run-launch789"
	d.LaunchKey = "launch-key-eeee"
	return d
}

// TestImportYAML_SplitProjectAndService_CarriesRunToken pins the byte-stable
// render of both halves against hand-written goldens (never dumped from the
// generator's own first run — spec-eval-farm.md §2.1's verified two-step
// shape, FM-12's env names): ProjectImportYAML carries the project block
// only with an empty services list, ServiceImportYAML carries the zcp
// service with ZCP_API_KEY set to the minted RunToken.
func TestImportYAML_SplitProjectAndService_CarriesRunToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		desc          RunDescriptor
		projectGolden string
		serviceGolden string
	}{
		{"oauth-token", oauthDescriptor(), "testdata/project_yaml/oauth_token.project.golden.yaml", "testdata/project_yaml/oauth_token.service.golden.yaml"},
		{"launch-key", launchDescriptor(), "testdata/project_yaml/launch_key.project.golden.yaml", "testdata/project_yaml/launch_key.service.golden.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotProject, err := ProjectImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ProjectImportYAML: %v", err)
			}
			wantProject, err := os.ReadFile(tc.projectGolden)
			if err != nil {
				t.Fatalf("read golden %s: %v", tc.projectGolden, err)
			}
			if string(gotProject) != string(wantProject) {
				t.Errorf("ProjectImportYAML(%s) mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.name, gotProject, wantProject)
			}
			if strings.Contains(string(gotProject), "hostname:") {
				t.Errorf("ProjectImportYAML(%s) must carry no services (services: []):\n%s", tc.name, gotProject)
			}

			gotService, err := ServiceImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ServiceImportYAML: %v", err)
			}
			wantService, err := os.ReadFile(tc.serviceGolden)
			if err != nil {
				t.Fatalf("read golden %s: %v", tc.serviceGolden, err)
			}
			if string(gotService) != string(wantService) {
				t.Errorf("ServiceImportYAML(%s) mismatch\n--- got ---\n%s\n--- want ---\n%s", tc.name, gotService, wantService)
			}
			if !strings.Contains(string(gotService), `ZCP_API_KEY: "run-token-value"`) {
				t.Errorf("ServiceImportYAML(%s) must carry the minted RunToken as ZCP_API_KEY:\n%s", tc.name, gotService)
			}
		})
	}
}

// TestProjectImportYAML_MissingRunID_Rejected pins the one field the
// project-creation half requires: RunID (it derives the project name).
func TestProjectImportYAML_MissingRunID_Rejected(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	d.RunID = ""
	if _, err := ProjectImportYAML(d); err == nil {
		t.Fatal("ProjectImportYAML with empty RunID: want error, got nil")
	}
}

// TestServiceImportYAML_MissingOAuthToken_Rejected pins §2.4/FM-16 (spec
// commit 79ced2cc): the agent credential is CLAUDE_CODE_OAUTH_TOKEN only —
// there is no api-key mode and no fallback.
func TestServiceImportYAML_MissingOAuthToken_Rejected(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	d.OAuthToken = ""
	if _, err := ServiceImportYAML(d); err == nil {
		t.Fatal("ServiceImportYAML with empty OAuthToken: want error, got nil")
	}
}

// TestServiceImportYAML_MissingWrapperSHA_Rejected pins R5 (LAND review):
// the init line cannot verify a wrapper it has no pinned digest for, so
// ServiceImportYAML refuses to render without one — the same discipline as
// OAuthToken/RunToken above.
func TestServiceImportYAML_MissingWrapperSHA_Rejected(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	d.WrapperSHA256 = ""
	if _, err := ServiceImportYAML(d); err == nil {
		t.Fatal("ServiceImportYAML with empty WrapperSHA256: want error, got nil")
	}
}

// TestServiceImportYAML_MissingRunToken_Rejected pins that the service half
// cannot render without the project-scoped token the controller mints after
// the project shell exists — a run project must never boot without its own
// scoped ZCP_API_KEY.
func TestServiceImportYAML_MissingRunToken_Rejected(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	d.RunToken = ""
	if _, err := ServiceImportYAML(d); err == nil {
		t.Fatal("ServiceImportYAML with empty RunToken: want error, got nil")
	}
}

// TestServiceImportYAML_NeverCarriesAccountKey_AndHostnameHasNoHyphen pins
// FM-15 (the generator has no field for the account-wide key, so it cannot
// leak into the output) and the fixed hostname literal "zcp" (hostnames may
// not contain hyphens, verified live — "zcp-farm-<runId>" cannot be the
// hostname).
func TestServiceImportYAML_NeverCarriesAccountKey_AndHostnameHasNoHyphen(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	got, err := ServiceImportYAML(d)
	if err != nil {
		t.Fatalf("ServiceImportYAML: %v", err)
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

// TestServiceImportYAML_InitLine_NoDirectEnvExpansion pins live finding L7:
// the platform pre-expands both ${VAR} and $VAR inside run.initCommands and
// exposes the expanded text in the container's own `initCommands` env
// variable — so an init line that reads the sink key/secret as $VAR/${VAR}
// leaks them in clear via `env`. Only command substitution ($(...)) survives
// unexpanded, so the init line must read every sink value that way.
func TestServiceImportYAML_InitLine_NoDirectEnvExpansion(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	got, err := ServiceImportYAML(d)
	if err != nil {
		t.Fatalf("ServiceImportYAML: %v", err)
	}
	out := string(got)

	for _, forbidden := range []string{
		"$ZCP_FARM_S3_KEY", "${ZCP_FARM_S3_KEY}",
		"$ZCP_FARM_S3_SECRET", "${ZCP_FARM_S3_SECRET}",
		"$ZCP_FARM_S3_URL", "${ZCP_FARM_S3_URL}",
		"$ZCP_FARM_S3_BUCKET", "${ZCP_FARM_S3_BUCKET}",
		"$ZCP_FARM_WRAPPER_SHA", "${ZCP_FARM_WRAPPER_SHA}",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("init line carries a directly-expanded env token %q (platform pre-expands $VAR/${VAR} in initCommands, leaking it via the container's initCommands env var), output:\n%s", forbidden, out)
		}
	}
	for _, want := range []string{
		"$(printenv ZCP_FARM_S3_KEY)",
		"$(printenv ZCP_FARM_S3_SECRET)",
		"$(printenv ZCP_FARM_S3_URL)",
		"$(printenv ZCP_FARM_S3_BUCKET)",
		"$(printenv ZCP_FARM_WRAPPER_SHA)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init line missing command-substitution read %q, output:\n%s", want, out)
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
	d := oauthDescriptor()
	got, err := ServiceImportYAML(d)
	if err != nil {
		t.Fatalf("ServiceImportYAML: %v", err)
	}
	out := string(got)
	const want = `ZCP_FARM_SCENARIOS_DIGEST: "scenarios-sha-ffff"`
	if !strings.Contains(out, want) {
		t.Errorf("ServiceImportYAML output missing %q, got:\n%s", want, out)
	}
}

// TestProjectYAML_WrapperSHA_RequiredAndVerifiedInInit pins R5 (LAND
// review): every run container holds a write-capable bucket key, so an
// init line that fetches farm/wrapper.sh unpinned and execs it hands code
// execution as `zerops` to whoever overwrote that key first. The fix pins
// WrapperSHA256 into ZCP_FARM_WRAPPER_SHA and the init line fetches the
// content-addressed key and sha256sum-verifies it before chmod+exec —
// literal assertions on the rendered init line, since that is the actual
// security boundary, not just the env var's presence.
func TestProjectYAML_WrapperSHA_RequiredAndVerifiedInInit(t *testing.T) {
	t.Parallel()
	d := oauthDescriptor()
	got, err := ServiceImportYAML(d)
	if err != nil {
		t.Fatalf("ServiceImportYAML: %v", err)
	}
	out := string(got)

	if !strings.Contains(out, `ZCP_FARM_WRAPPER_SHA: "wrapper-sha-9999"`) {
		t.Errorf("ServiceImportYAML output missing ZCP_FARM_WRAPPER_SHA env, got:\n%s", out)
	}
	for _, want := range []string{
		"farm/wrapper/$(printenv ZCP_FARM_WRAPPER_SHA).sh",
		"$(printenv ZCP_FARM_WRAPPER_SHA)  /tmp/farm-wrapper.sh",
		"sha256sum -c",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init line missing %q, output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "farm/wrapper.sh") {
		t.Errorf("init line must fetch the content-addressed key, not the unpinned farm/wrapper.sh, output:\n%s", out)
	}
}

// TestImportYAML_ValidatesAgainstImportSchema validates both generated YAML
// halves against the embedded import-yml JSON Schema (internal/schema),
// reachable offline.
func TestImportYAML_ValidatesAgainstImportSchema(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		desc RunDescriptor
	}{
		{"oauth-token", oauthDescriptor()},
		{"launch-key", launchDescriptor()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotProject, err := ProjectImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ProjectImportYAML: %v", err)
			}
			if errs := schema.ValidateImportYAML(string(gotProject)); len(errs) > 0 {
				t.Errorf("ValidateImportYAML(project) found %d error(s):", len(errs))
				for _, e := range errs {
					t.Errorf("  %s", e.Error())
				}
			}

			gotService, err := ServiceImportYAML(tc.desc)
			if err != nil {
				t.Fatalf("ServiceImportYAML: %v", err)
			}
			if errs := schema.ValidateImportYAML(string(gotService)); len(errs) > 0 {
				t.Errorf("ValidateImportYAML(service) found %d error(s):", len(errs))
				for _, e := range errs {
					t.Errorf("  %s", e.Error())
				}
			}
		})
	}
}
