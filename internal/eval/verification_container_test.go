package eval

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// httpBody builds an io.ReadCloser http.Response.Body from a literal
// string, for container-side oracle tests that fake ops.HTTPDoer.
func httpBody(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// errNotConnected is the sentinel dial-refused transport error used by
// container-side oracle tests to simulate an unreachable service.
var errNotConnected = errors.New("dial tcp: connection refused")

// TestInternalLiveness_ServiceAnswersWithMarker_Passed pins the O2'
// internalLiveness happy path (docs/spec-eval-farm.md §4.1 FM-61): a GET
// over the project network returning 2xx with the declared marker in the
// body passes the row.
func TestInternalLiveness_ServiceAnswersWithMarker_Passed(t *testing.T) {
	t.Parallel()
	probe := &InternalLivenessProbe{Service: "worker", Port: 8080, Path: "/healthz", Marker: "ok"}
	doer := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "http://worker:8080/healthz" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		return &http.Response{StatusCode: http.StatusOK, Body: httpBody("status: ok"), Header: make(http.Header)}, nil
	})
	rows := evaluateInternalLivenessRows(context.Background(), probe, "/work", nil, doer, time.Now())
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Result != CheckPassed {
		t.Errorf("row = %+v, want passed", got)
	}
	if got.ID != "internal_liveness/worker" {
		t.Errorf("ID = %q, want internal_liveness/worker", got.ID)
	}
	if got.Source != "internal-liveness" {
		t.Errorf("Source = %q, want internal-liveness", got.Source)
	}
}

// TestInternalLiveness_DialRefused_FailedNotBlocked pins FM-61: an
// unreachable service is `failed`, never `blocked` — unlike the
// subdomain-facing O2 liveness row, a transport error here proves the
// service is genuinely unreachable over the project network, not merely
// unevaluated.
func TestInternalLiveness_DialRefused_FailedNotBlocked(t *testing.T) {
	t.Parallel()
	probe := &InternalLivenessProbe{Service: "worker", Port: 8080, Path: "/healthz", Marker: "ok"}
	doer := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errNotConnected
	})
	rows := evaluateInternalLivenessRows(context.Background(), probe, "/work", nil, doer, time.Now())
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Result != CheckFailed {
		t.Errorf("row = %+v, want failed", got)
	}
	wantMsg := "GET http://worker:8080/healthz: dial tcp: connection refused"
	if got.Message != wantMsg {
		t.Errorf("Message = %q, want %q", got.Message, wantMsg)
	}
}

// TestInternalLiveness_MarkerMissing_Failed pins FM-61: a 2xx response
// whose body does not contain the declared marker fails the row.
func TestInternalLiveness_MarkerMissing_Failed(t *testing.T) {
	t.Parallel()
	probe := &InternalLivenessProbe{Service: "worker", Port: 8080, Path: "/healthz", Marker: "ok"}
	doer := httpDoerFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: httpBody("status: degraded"), Header: make(http.Header)}, nil
	})
	rows := evaluateInternalLivenessRows(context.Background(), probe, "/work", nil, doer, time.Now())
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.Result != CheckFailed {
		t.Errorf("row = %+v, want failed", got)
	}
	if strings.Contains(got.Observed, "degraded") == false {
		t.Errorf("Observed = %q, want it to carry the response body", got.Observed)
	}
}

// TestContainerCheck_ExactAndRegex_Rows pins O10 (docs/spec-eval-farm.md
// §4.1 FM-61): exact (expect) and regex (match) stdout comparison, plus a
// non-zero exit, each yield one row per entry.
func TestContainerCheck_ExactAndRegex_Rows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		entry      ContainerCheckEntry
		stdout     string
		execErr    error
		wantResult CheckResult
	}{
		{
			name:       "exact match pass",
			entry:      ContainerCheckEntry{Service: "appdev", Cmd: "printenv FEATURE_X", Expect: "on"},
			stdout:     "on\n",
			wantResult: CheckPassed,
		},
		{
			name:       "exact mismatch fail",
			entry:      ContainerCheckEntry{Service: "appdev", Cmd: "printenv FEATURE_X", Expect: "on"},
			stdout:     "off\n",
			wantResult: CheckFailed,
		},
		{
			name:       "regex pass",
			entry:      ContainerCheckEntry{Service: "appdev", Cmd: "printenv FEATURE_X", Match: "^on"},
			stdout:     "on\n",
			wantResult: CheckPassed,
		},
		{
			name:       "non-zero exit fail",
			entry:      ContainerCheckEntry{Service: "appdev", Cmd: "false", Expect: "on"},
			stdout:     "",
			execErr:    errors.New("ssh appdev: exit status 1"),
			wantResult: CheckFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			execSSH := func(_ context.Context, hostname, cmd string) ([]byte, error) {
				if hostname != tc.entry.Service || cmd != tc.entry.Cmd {
					t.Fatalf("execSSH called with (%q, %q), want (%q, %q)", hostname, cmd, tc.entry.Service, tc.entry.Cmd)
				}
				return []byte(tc.stdout), tc.execErr
			}
			rows := evaluateContainerCheckRows(context.Background(), []ContainerCheckEntry{tc.entry}, "/work", execSSH, nil, time.Now())
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			got := rows[0]
			if got.Result != tc.wantResult {
				t.Errorf("row = %+v, want %s", got, tc.wantResult)
			}
			wantID := "container_check/appdev/1"
			if got.ID != wantID {
				t.Errorf("ID = %q, want %q", got.ID, wantID)
			}
		})
	}
}

// writeServiceMetaFixture writes a service-meta JSON fixture at
// <workDir>/.zcp/state/services/<hostname>.json — the on-disk shape the
// meta family reads (workflow.ServiceMeta's path, internal/workflow/service_meta.go).
func writeServiceMetaFixture(t *testing.T, workDir, hostname, jsonBody string) {
	t.Helper()
	dir := filepath.Join(workDir, ".zcp", "state", "services")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, hostname+".json")
	if err := os.WriteFile(path, []byte(jsonBody), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestMeta_FieldMatchesAndAbsent_Rows pins O11 (docs/spec-eval-farm.md §4.1
// FM-61): a field read from the candidate's
// .zcp/state/services/<hostname>.json, matched/mismatched/absent, and a
// missing meta file, each yield one row per entry.
func TestMeta_FieldMatchesAndAbsent_Rows(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		hostname   string
		fixture    string // empty = no fixture file written
		field      string
		expect     string
		wantResult CheckResult
	}{
		{
			name:       "match pass",
			hostname:   "appdev",
			fixture:    `{"closeDeployMode":"manual"}`,
			field:      "closeDeployMode",
			expect:     "manual",
			wantResult: CheckPassed,
		},
		{
			name:       "mismatch fail",
			hostname:   "appdev",
			fixture:    `{"closeDeployMode":"auto"}`,
			field:      "closeDeployMode",
			expect:     "manual",
			wantResult: CheckFailed,
		},
		{
			name:       "file missing fail",
			hostname:   "ghost",
			fixture:    "", // no fixture written
			field:      "closeDeployMode",
			expect:     "manual",
			wantResult: CheckFailed,
		},
		{
			name:       "field absent fail",
			hostname:   "appdev",
			fixture:    `{"mode":"dev-stage"}`,
			field:      "closeDeployMode",
			expect:     "manual",
			wantResult: CheckFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			workDir := t.TempDir()
			if tc.fixture != "" {
				writeServiceMetaFixture(t, workDir, tc.hostname, tc.fixture)
			}
			entry := MetaCheckEntry{Hostname: tc.hostname, Field: tc.field, Expect: tc.expect}
			rows := evaluateMetaRows(context.Background(), []MetaCheckEntry{entry}, workDir, nil, nil, time.Now())
			if len(rows) != 1 {
				t.Fatalf("rows = %d, want 1", len(rows))
			}
			got := rows[0]
			if got.Result != tc.wantResult {
				t.Errorf("row = %+v, want %s", got, tc.wantResult)
			}
			wantID := "meta/" + tc.hostname + "/" + tc.field
			if got.ID != wantID {
				t.Errorf("ID = %q, want %q", got.ID, wantID)
			}
			if tc.name == "field absent fail" {
				wantMsg := "field closeDeployMode absent"
				if got.Message != wantMsg {
					t.Errorf("Message = %q, want %q", got.Message, wantMsg)
				}
			}
		})
	}
}

// validImportYAMLFixture is a minimal, schema-valid import.yaml (mirrors
// internal/schema/validate_jsonschema_test.go's validImportYAML fixture —
// an independent literal, not a call into the implementation being tested).
const validImportYAMLFixture = `project:
  name: demo
services:
  - hostname: appdev
    type: alpine/nodejs@22
    mode: NON_HA
    buildFromGit: https://github.com/example/demo.git
    zeropsSetup: appdev
`

// invalidImportYAMLFixture adds a field the import.yaml schema does not
// declare (additionalProperties: false) under the service entry.
const invalidImportYAMLFixture = `project:
  name: demo
services:
  - hostname: appdev
    type: alpine/nodejs@22
    mode: NON_HA
    buildFromGit: https://github.com/example/demo.git
    zeropsSetup: appdev
    bogusField: yes
`

// TestSchemaValid_ImportYAML_ValidAndInvalid_Rows pins O12
// (docs/spec-eval-farm.md §4.1 FM-61): a valid artifact passes, an unknown
// field fails, and a missing artifact fails.
func TestSchemaValid_ImportYAML_ValidAndInvalid_Rows(t *testing.T) {
	t.Parallel()
	t.Run("valid import yaml passes", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "import.yaml"), []byte(validImportYAMLFixture), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		rows := evaluateSchemaValidRow(context.Background(), &SchemaValidCheck{Artifact: "import.yaml"}, workDir, nil, nil, time.Now())
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		if rows[0].Result != CheckPassed {
			t.Errorf("row = %+v, want passed", rows[0])
		}
		if rows[0].ID != "schema_valid/import.yaml" {
			t.Errorf("ID = %q, want schema_valid/import.yaml", rows[0].ID)
		}
	})
	t.Run("unknown field fails", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "import.yaml"), []byte(invalidImportYAMLFixture), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		rows := evaluateSchemaValidRow(context.Background(), &SchemaValidCheck{Artifact: "import.yaml"}, workDir, nil, nil, time.Now())
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		if rows[0].Result != CheckFailed {
			t.Errorf("row = %+v, want failed", rows[0])
		}
	})
	t.Run("missing artifact fails", func(t *testing.T) {
		t.Parallel()
		workDir := t.TempDir()
		rows := evaluateSchemaValidRow(context.Background(), &SchemaValidCheck{Artifact: "import.yaml"}, workDir, nil, nil, time.Now())
		if len(rows) != 1 {
			t.Fatalf("rows = %d, want 1", len(rows))
		}
		if rows[0].Result != CheckFailed {
			t.Errorf("row = %+v, want failed", rows[0])
		}
	})
}
