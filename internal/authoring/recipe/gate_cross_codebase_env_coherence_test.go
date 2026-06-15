package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pinned by run-32 Step 2 — detect cross-codebase env-var coherence
// violations at scaffold complete-phase. The test set covers (a) the
// real captured run-32 mismatches, (b) coherent-input regression to
// ensure no false positives, and (c) a synthetic edge case that
// exercises composite-value passthrough.

func TestFindCrossCodebaseEnvMismatches_Run32CapturedMismatches(t *testing.T) {
	t.Parallel()
	apidevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PASSWORD: ${db_password}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
`)
	workerdevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PASS: ${db_password}
        S3_KEY: ${storage_accessKeyId}
        S3_SECRET: ${storage_secretAccessKey}
`)
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev":    apidevYaml,
		"workerdev": workerdevYaml,
	})
	wantSources := map[string]struct{ first, second string }{
		"${db_password}":             {"apidev=DB_PASSWORD", "workerdev=DB_PASS"},
		"${storage_accessKeyId}":     {"apidev=S3_ACCESS_KEY_ID", "workerdev=S3_KEY"},
		"${storage_secretAccessKey}": {"apidev=S3_SECRET_ACCESS_KEY", "workerdev=S3_SECRET"},
	}
	if len(got) != len(wantSources) {
		t.Fatalf("expected %d mismatches; got %d:\n%v", len(wantSources), len(got), got)
	}
	for _, m := range got {
		exp, ok := wantSources[m.Source]
		if !ok {
			t.Errorf("unexpected source flagged: %q", m.Source)
			continue
		}
		if len(m.Pairs) != 2 {
			t.Errorf("source %s: expected 2 pairs; got %d", m.Source, len(m.Pairs))
			continue
		}
		got0 := m.Pairs[0].Codebase + "=" + m.Pairs[0].Key
		got1 := m.Pairs[1].Codebase + "=" + m.Pairs[1].Key
		if got0 != exp.first || got1 != exp.second {
			t.Errorf("source %s: pairs [%q %q]; want [%q %q]", m.Source, got0, got1, exp.first, exp.second)
		}
	}
}

func TestFindCrossCodebaseEnvMismatches_CoherentInput_NoFalsePositive(t *testing.T) {
	t.Parallel()
	apidevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PASSWORD: ${db_password}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
`)
	workerdevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PASSWORD: ${db_password}
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
`)
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev":    apidevYaml,
		"workerdev": workerdevYaml,
	})
	if len(got) != 0 {
		t.Errorf("coherent input should produce zero mismatches; got %v", got)
	}
}

func TestFindCrossCodebaseEnvMismatches_SingleCodebase_Empty(t *testing.T) {
	t.Parallel()
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev": []byte(`zerops:
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
`),
	})
	if len(got) != 0 {
		t.Errorf("single codebase cannot disagree; got %v", got)
	}
}

func TestFindCrossCodebaseEnvMismatches_CompositeValuesIgnored(t *testing.T) {
	t.Parallel()
	// `${storage_apiUrl}/${storage_bucketName}` is a composite URL —
	// computed value, not an alias. Coherence rule does not apply.
	apidevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        S3_ENDPOINT: ${storage_apiUrl}/${storage_bucketName}
`)
	workerdevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
`)
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev":    apidevYaml,
		"workerdev": workerdevYaml,
	})
	if len(got) != 0 {
		t.Errorf("composite values should not flag; got %v", got)
	}
}

// Run-32 codex review fix — intra-codebase aliasing of the same
// source under multiple keys must surface in the union, not be hidden
// by first-key-wins. Captured shape: codebase A has BOTH DB_PASSWORD
// and DB_AUTH bound to ${db_password}; codebase B has DB_PASSWORD
// only. Pre-fix the gate reported no mismatch (A first-seen=DB_PASSWORD
// matches B); post-fix both keys union together and DB_AUTH surfaces
// alongside.
func TestFindCrossCodebaseEnvMismatches_IntraCodebaseAliasingUnioned(t *testing.T) {
	t.Parallel()
	apidevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_PASSWORD: ${db_password}
        DB_AUTH: ${db_password}
`)
	workerdevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        DB_PASSWORD: ${db_password}
`)
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev":    apidevYaml,
		"workerdev": workerdevYaml,
	})
	if len(got) != 1 {
		t.Fatalf("intra-codebase aliasing should surface 1 mismatch; got %d: %v", len(got), got)
	}
	m := got[0]
	if m.Source != "${db_password}" {
		t.Errorf("source = %q; want ${db_password}", m.Source)
	}
	keysByCodebase := map[string][]string{}
	for _, p := range m.Pairs {
		keysByCodebase[p.Codebase] = append(keysByCodebase[p.Codebase], p.Key)
	}
	if len(keysByCodebase["apidev"]) != 2 {
		t.Errorf("apidev should report both DB_PASSWORD and DB_AUTH; got %v", keysByCodebase["apidev"])
	}
	if len(keysByCodebase["workerdev"]) != 1 {
		t.Errorf("workerdev should report DB_PASSWORD only; got %v", keysByCodebase["workerdev"])
	}
}

// Codex review NOTICE — malformed refs like ${db_} or ${db__password}
// must be rejected as service refs (not silently treated as valid).
func TestMatchSingleServiceRef_RejectsMalformedSuffix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		value string
		want  bool
	}{
		{"${db_}", false},          // empty suffix
		{"${db__password}", false}, // suffix starts with `_`
		{"${db_password}", true},   // canonical
		{"${db_1abc}", false},      // suffix starts with digit
		{"${storage_apiUrl}", true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			t.Parallel()
			_, got := matchSingleServiceRef(tc.value)
			if got != tc.want {
				t.Errorf("matchSingleServiceRef(%q) = %v; want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestFindCrossCodebaseEnvMismatches_ProjectScopeRefIgnored(t *testing.T) {
	t.Parallel()
	// `${APP_SECRET}` is project-scope (uppercase first char), not a
	// service ref; coherence rule applies only to `${<host>_<suffix>}`.
	apidevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        JWT_SECRET: ${APP_SECRET}
`)
	workerdevYaml := []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        AUTH_KEY: ${APP_SECRET}
`)
	got := FindCrossCodebaseEnvMismatches(map[string][]byte{
		"apidev":    apidevYaml,
		"workerdev": workerdevYaml,
	})
	if len(got) != 0 {
		t.Errorf("project-scope refs should not flag; got %v", got)
	}
}

// Integration test: gate wired with real on-disk yamls returns notice-
// severity violations; nothing blocking. Pinned to ensure detection-only
// behavior holds.
func TestGateCrossCodebaseEnvCoherence_DetectionOnlySeverity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	apidevDir := filepath.Join(dir, "apidev")
	workerdevDir := filepath.Join(dir, "workerdev")
	if err := os.MkdirAll(apidevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workerdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apidevDir, "zerops.yaml"), []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerdevDir, "zerops.yaml"), []byte(`
zerops:
  - setup: prod
    run:
      envVariables:
        S3_KEY: ${storage_accessKeyId}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &Plan{
		Codebases: []Codebase{
			{Hostname: "apidev", SourceRoot: apidevDir},
			{Hostname: "workerdev", SourceRoot: workerdevDir},
		},
	}
	violations := gateCrossCodebaseEnvCoherence(GateContext{Plan: plan})
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation; got %d: %v", len(violations), violations)
	}
	v := violations[0]
	if v.Severity != SeverityNotice {
		t.Errorf("Run-32 Step 2 is detection-only; severity must be Notice, got %d", v.Severity)
	}
	if v.Code != "cross-codebase-env-coherence" {
		t.Errorf("violation Code = %q; want cross-codebase-env-coherence", v.Code)
	}
	if !strings.Contains(v.Message, "S3_ACCESS_KEY_ID") || !strings.Contains(v.Message, "S3_KEY") {
		t.Errorf("violation Message should name both keys; got %q", v.Message)
	}
}
