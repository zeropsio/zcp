// Tests for: ops/env.go — env set and delete operations.
package ops

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// countingProjectEnvMock wraps platform.Mock and tracks CreateProjectEnv calls.
// Optionally fails on a specific call number (1-indexed).
type countingProjectEnvMock struct {
	platform.Client
	calls   []projectEnvCall
	failOn  int // 0 = never fail, N = fail on Nth call (1-indexed)
	failErr error
}

type projectEnvCall struct {
	Key   string
	Value string
}

func (m *countingProjectEnvMock) CreateProjectEnv(_ context.Context, _ string, key, value string, _ bool) (*platform.Process, error) {
	m.calls = append(m.calls, projectEnvCall{Key: key, Value: value})
	if m.failOn > 0 && len(m.calls) == m.failOn {
		return nil, m.failErr
	}
	return &platform.Process{
		ID:         fmt.Sprintf("proc-projenvset-%d", len(m.calls)),
		ActionName: "envSet",
		Status:     "PENDING",
	}, nil
}

func TestEnvSet_Service(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	result, err := EnvSet(context.Background(), mock, "proj-1", "api", false, []string{"PORT=3000", "HOST=0.0.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestEnvSet_Project(t *testing.T) {
	t.Parallel()

	mock := &countingProjectEnvMock{Client: platform.NewMock()}

	result, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{"A=1", "B=2", "C=3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}

	// Verify all 3 variables were sent to the API with correct key/value pairs.
	if len(mock.calls) != 3 {
		t.Fatalf("CreateProjectEnv calls = %d, want 3", len(mock.calls))
	}
	wantCalls := []projectEnvCall{
		{Key: "A", Value: "1"},
		{Key: "B", Value: "2"},
		{Key: "C", Value: "3"},
	}
	for i, want := range wantCalls {
		if mock.calls[i] != want {
			t.Errorf("call[%d] = %+v, want %+v", i, mock.calls[i], want)
		}
	}
}

func TestEnvSet_Project_PreprocessorExpansion(t *testing.T) {
	t.Parallel()

	// Values containing <@...> syntax must be expanded through zParser
	// before being sent to the API. The platform stores literal strings;
	// the preprocessor is a zcp-side concern that gives workspace setup
	// byte-for-byte parity with the deliverable's import.yaml.
	mock := &countingProjectEnvMock{Client: platform.NewMock()}

	result, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{
		"APP_KEY=<@generateRandomString(<32>)>",
		"PLAIN_VALUE=literal",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(mock.calls))
	}

	// APP_KEY: 32 raw chars, no residual <@...> syntax.
	if got := mock.calls[0].Value; len(got) != 32 {
		t.Errorf("APP_KEY length = %d, want 32 (value: %q)", len(got), got)
	}
	if strings.Contains(mock.calls[0].Value, "<@") || strings.Contains(mock.calls[0].Value, ">") {
		t.Errorf("APP_KEY still contains preprocessor syntax: %q", mock.calls[0].Value)
	}

	// PLAIN_VALUE: passes through unchanged.
	if mock.calls[1].Value != "literal" {
		t.Errorf("PLAIN_VALUE = %q, want literal", mock.calls[1].Value)
	}

	// Stored mirrors API calls — both keys marked Replaced=false (new entries).
	if len(result.Stored) != 2 {
		t.Fatalf("want 2 stored entries, got %d", len(result.Stored))
	}
	if result.Stored[0].Key != "APP_KEY" || result.Stored[0].Replaced {
		t.Errorf("Stored[0] = %+v, want {APP_KEY, new}", result.Stored[0])
	}
	if result.Stored[1].Value != "literal" {
		t.Errorf("Stored[1].Value = %q, want literal", result.Stored[1].Value)
	}
}

func TestEnvSet_Project_RejectsBase64PrefixedPreprocessor(t *testing.T) {
	t.Parallel()

	// The recurring APP_KEY footgun: agent wraps preprocessor output in
	// base64: because the framework's key:generate command outputs that
	// shape. Platform stores "base64:{32chars}", framework decodes, gets
	// ~24 bytes, fixed-length cipher rejects. Caught at zcp layer — no
	// API call should be made.
	mock := &countingProjectEnvMock{Client: platform.NewMock()}

	_, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{
		"APP_KEY=base64:<@generateRandomString(<32>)>",
	})
	if err == nil {
		t.Fatal("expected error for base64:-prefixed preprocessor expression")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error missing base64 context: %v", err)
	}
	if len(mock.calls) != 0 {
		t.Errorf("rejection should prevent API calls, got %d calls", len(mock.calls))
	}
}

func TestEnvSet_Project_AllowsLiteralBase64Value(t *testing.T) {
	t.Parallel()

	// A caller passing a pre-encoded literal (no <@...> inside) is fine —
	// they actually did the base64 encoding themselves. This case must
	// pass through unchanged, distinct from the preprocessor-wrapping
	// footgun above.
	mock := &countingProjectEnvMock{Client: platform.NewMock()}

	_, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{
		"APP_KEY=base64:QWxhZGRpbjpPcGVuU2VzYW1lQWxhZGRpbjpPcGVu",
	})
	if err != nil {
		t.Fatalf("literal base64 value should pass through: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(mock.calls))
	}
}

func TestEnvSet_Project_UpsertExistingKey(t *testing.T) {
	t.Parallel()

	// Calling EnvSet with an already-existing project env must UPDATE it
	// (delete + create), not fail with projectEnvDuplicateKey. Agents
	// iterating on a recipe used to hit this error repeatedly.
	base := platform.NewMock().WithProjectEnv([]platform.EnvVar{
		{ID: "env-old-1", Key: "APP_KEY", Content: "old-value"},
	})
	mock := &countingProjectEnvMock{Client: base}

	result, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{
		"APP_KEY=new-literal-value",
	})
	if err != nil {
		t.Fatalf("unexpected error on upsert: %v", err)
	}
	if len(mock.calls) != 1 {
		t.Fatalf("want 1 CreateProjectEnv call, got %d", len(mock.calls))
	}
	if mock.calls[0].Value != "new-literal-value" {
		t.Errorf("created value = %q, want new-literal-value", mock.calls[0].Value)
	}
	if len(result.Stored) != 1 || !result.Stored[0].Replaced {
		t.Errorf("Stored = %+v, want [{APP_KEY, replaced=true}]", result.Stored)
	}
}

func TestEnvSet_Project_PreprocessorSyntaxError(t *testing.T) {
	t.Parallel()

	// Invalid preprocessor syntax must fail at expansion time, BEFORE any
	// API call is made — the agent gets a clear error rather than storing
	// garbage in the platform.
	mock := &countingProjectEnvMock{Client: platform.NewMock()}

	_, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{
		"APP_KEY=<@thisFunctionDoesNotExist(<32>)>",
	})
	if err == nil {
		t.Fatal("expected preprocessor error for unknown function")
	}
	if len(mock.calls) != 0 {
		t.Errorf("expansion failure should prevent API calls, got %d calls", len(mock.calls))
	}
}

func TestEnvSet_Project_PartialFailure(t *testing.T) {
	t.Parallel()

	// Mock: CreateProjectEnv fails on 2nd call out of 3.
	// Expected: error returned, but 1st variable was already set (1 successful call).
	mock := &countingProjectEnvMock{
		Client:  platform.NewMock(),
		failOn:  2,
		failErr: fmt.Errorf("API timeout"),
	}

	_, err := EnvSet(context.Background(), mock, "proj-1", "", true, []string{"A=1", "B=2", "C=3"})
	if err == nil {
		t.Fatal("expected error for partial failure")
	}

	// Verify: 2 calls made (1st succeeded, 2nd failed, 3rd never reached).
	if len(mock.calls) != 2 {
		t.Fatalf("CreateProjectEnv calls = %d, want 2 (1 success + 1 failure)", len(mock.calls))
	}

	// 1st call should have correct key/value (it succeeded).
	if mock.calls[0].Key != "A" || mock.calls[0].Value != "1" {
		t.Errorf("call[0] = %+v, want {A 1}", mock.calls[0])
	}

	// 2nd call is the one that failed.
	if mock.calls[1].Key != "B" || mock.calls[1].Value != "2" {
		t.Errorf("call[1] = %+v, want {B 2}", mock.calls[1])
	}
}

func TestEnvSet_InvalidFormat(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		})

	_, err := EnvSet(context.Background(), mock, "proj-1", "api", false, []string{"NOEQUALS"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidEnvFormat {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidEnvFormat, pe.Code)
	}
}

func TestEnvDelete_Service_Found(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "DB_HOST", Content: "localhost"},
		})

	result, err := EnvDelete(context.Background(), mock, "proj-1", "api", false, []string{"DB_HOST"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}

func TestEnvDelete_Service_NotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "api", ProjectID: "proj-1"},
		}).
		WithServiceEnv("svc-1", []platform.EnvVar{
			{ID: "e1", Key: "DB_HOST", Content: "localhost"},
		})

	_, err := EnvDelete(context.Background(), mock, "proj-1", "api", false, []string{"MISSING"})
	if err == nil {
		t.Fatal("expected error for missing env key")
	}
}

func TestEnvDelete_Project(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithProjectEnv([]platform.EnvVar{
			{ID: "pe1", Key: "GLOBAL_KEY", Content: "val"},
		})

	result, err := EnvDelete(context.Background(), mock, "proj-1", "", true, []string{"GLOBAL_KEY"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Process == nil {
		t.Fatal("expected non-nil process")
	}
}
