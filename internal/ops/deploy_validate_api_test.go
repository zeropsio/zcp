// Tests for: deploy_validate_api.go — pre-deploy API validation helpers.
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// stackFor returns a minimal ServiceStack with ID + type fields populated so
// RunPreDeployValidation / ValidatePreDeployContent can extract the
// ServiceStackTypeID + TypeVersionName the Zerops validator requires.
// Signature takes a hostname even though current callers only pass
// "appdev" — future tests will vary it, and the diff churn isn't worth a
// rename cycle.
//
//nolint:unparam
func stackFor(hostname string) *platform.ServiceStack {
	return &platform.ServiceStack{
		ID:   "svc-" + hostname,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeID:          "ssti-nodejs22",
			ServiceStackTypeVersionName: "nodejs@22",
		},
	}
}

func TestRunPreDeployValidation_CallsClient(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	yaml := "zerops:\n  - setup: appdev\n    run:\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	mock := platform.NewMock()

	if err := RunPreDeployValidation(context.Background(), mock, stackFor("appdev"), "appdev", dir); err != nil {
		t.Fatalf("expected nil on success, got %v", err)
	}
	if len(mock.CapturedValidateZeropsYaml) != 1 {
		t.Fatalf("expected 1 captured validation call, got %d", len(mock.CapturedValidateZeropsYaml))
	}
	got := mock.CapturedValidateZeropsYaml[0]
	if got.ServiceStackTypeID != "ssti-nodejs22" {
		t.Errorf("ServiceStackTypeID = %q, want ssti-nodejs22", got.ServiceStackTypeID)
	}
	if got.ServiceStackTypeVersion != "nodejs@22" {
		t.Errorf("ServiceStackTypeVersion = %q, want nodejs@22", got.ServiceStackTypeVersion)
	}
	if got.ServiceStackName != "appdev" {
		t.Errorf("ServiceStackName = %q, want appdev", got.ServiceStackName)
	}
	if got.Operation != platform.ValidationOperationBuildAndDeploy {
		t.Errorf("Operation = %q, want %q", got.Operation, platform.ValidationOperationBuildAndDeploy)
	}
	if got.ZeropsYaml != yaml {
		t.Errorf("ZeropsYaml mismatch\ngot:  %q\nwant: %q", got.ZeropsYaml, yaml)
	}
}

func TestRunPreDeployValidation_PropagatesValidationError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("zerops: []\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	// Simulate a structured validation rejection that's already been
	// reclassified from APICode=zeropsYamlInvalidParameter to
	// Code=ErrInvalidZeropsYml (what platform.ValidateZeropsYaml returns
	// in production).
	want := platform.NewPlatformError(
		platform.ErrInvalidZeropsYml,
		"Invalid parameter provided.",
		"see apiMeta for each field's failure reason",
	)
	want.APICode = "zeropsYamlInvalidParameter"
	want.APIMeta = []platform.APIMetaItem{
		{
			Code:  "zeropsYamlInvalidParameter",
			Error: "Invalid parameter provided.",
			Metadata: map[string][]string{
				"build.base": {"unknown base nodejs@99"},
			},
		},
	}
	mock := platform.NewMock().WithError("ValidateZeropsYaml", want)

	err := RunPreDeployValidation(context.Background(), mock, stackFor("appdev"), "appdev", dir)
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrInvalidZeropsYml {
		t.Errorf("Code = %q, want %q", pe.Code, platform.ErrInvalidZeropsYml)
	}
	if len(pe.APIMeta) != 1 {
		t.Fatalf("APIMeta lost during propagation (got %d items)", len(pe.APIMeta))
	}
	if pe.APIMeta[0].Metadata["build.base"][0] != "unknown base nodejs@99" {
		t.Errorf("meta content lost: %+v", pe.APIMeta)
	}
}

func TestRunPreDeployValidation_PropagatesTransportError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("zerops: []\n"), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	// Transport failure: the Client returns a network error. Deploy must
	// abort — no fallback "proceed on transport error" path per plan §2.
	transportErr := platform.NewPlatformError(
		platform.ErrNetworkError,
		"connection refused",
		"Check API host and network",
	)
	mock := platform.NewMock().WithError("ValidateZeropsYaml", transportErr)

	err := RunPreDeployValidation(context.Background(), mock, stackFor("appdev"), "appdev", dir)
	if !errors.Is(err, transportErr) {
		t.Fatalf("expected transport error propagated, got %v", err)
	}
}

func TestRunPreDeployValidation_SkipWhenNoYaml(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // empty dir
	mock := platform.NewMock()
	if err := RunPreDeployValidation(context.Background(), mock, stackFor("appdev"), "appdev", dir); err != nil {
		t.Errorf("expected nil (skip) when no yaml present, got %v", err)
	}
	if len(mock.CapturedValidateZeropsYaml) != 0 {
		t.Errorf("validator called with no yaml on disk — should skip, got %d calls", len(mock.CapturedValidateZeropsYaml))
	}
}

func TestRunPreDeployValidation_SkipWhenTargetNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	if err := RunPreDeployValidation(context.Background(), mock, nil, "appdev", "/does/not/matter"); err != nil {
		t.Errorf("nil target must skip without error, got %v", err)
	}
	if len(mock.CapturedValidateZeropsYaml) != 0 {
		t.Errorf("validator called with nil target")
	}
}

func TestValidatePreDeployContent_SkipsEmptyContent(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	if err := ValidatePreDeployContent(context.Background(), mock, stackFor("appdev"), "appdev", ""); err != nil {
		t.Errorf("empty content must skip without error, got %v", err)
	}
	if len(mock.CapturedValidateZeropsYaml) != 0 {
		t.Errorf("validator called with empty content")
	}
}

func TestValidatePreDeployContent_DefaultsSetupToHostname(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	// setupName="" should default to target.Name ("appdev"). The Zerops
	// validator matches serviceStackName against the setup: key in the yaml,
	// and the default platform convention is hostname = setup name.
	err := ValidatePreDeployContent(context.Background(), mock, stackFor("appdev"), "", "zerops:\n  - setup: appdev\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mock.CapturedValidateZeropsYaml) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.CapturedValidateZeropsYaml))
	}
	if got := mock.CapturedValidateZeropsYaml[0].ServiceStackName; got != "appdev" {
		t.Errorf("ServiceStackName = %q, want appdev (default to hostname)", got)
	}
}
