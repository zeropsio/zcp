package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

func TestSchemaCheck_DoesNotResolveCredentials(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(true, &out,
		func(string) (schema.DriftReport, error) {
			return schema.DriftReport{}, nil
		},
	)
	if code != 0 || !strings.Contains(out.String(), "OK") {
		t.Errorf("credential-free strict result = code %d output %q, want 0/OK", code, out.String())
	}
}

func TestSchemaCheck_StrictUpstreamFailure_ReturnsFailure(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(true, &out,
		func(string) (schema.DriftReport, error) {
			return schema.DriftReport{}, errors.New("upstream unavailable")
		},
	)
	if code != 1 || !strings.Contains(out.String(), "ERROR") || strings.Contains(out.String(), "SKIP") {
		t.Errorf("strict upstream result = code %d output %q, want 1/ERROR", code, out.String())
	}
}

func TestSchemaCheck_NonStrictUpstreamFailure_RemainsSkipSuccess(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(false, &out,
		func(string) (schema.DriftReport, error) {
			return schema.DriftReport{}, errors.New("upstream unavailable")
		},
	)
	if code != 0 || !strings.Contains(out.String(), "SKIP") {
		t.Errorf("non-strict upstream result = code %d output %q, want 0/SKIP", code, out.String())
	}
}

func TestSchemaCommands_PublicOnlyHaveNoCredentialOrActiveStatusDependency(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("schema.go")
	if err != nil {
		t.Fatalf("read schema.go: %v", err)
	}
	for _, forbidden := range []string{
		"ResolveCredentials",
		"ActiveServiceTypeVersions",
		"ActiveVersionsProvider",
		"ApplyActiveFilter",
	} {
		if strings.Contains(string(body), forbidden) {
			t.Errorf("schema commands retain forbidden dependency %q", forbidden)
		}
	}
}

func TestSchemaCheck_Drift_ReturnsTwoInBothModes(t *testing.T) {
	t.Parallel()
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-strict", true: "strict"}[strict], func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			code := runSchemaCheckWith(strict, &out,
				func(string) (schema.DriftReport, error) {
					return schema.DriftReport{ImportDrift: true}, nil
				},
			)
			if code != 2 || !strings.Contains(out.String(), "DRIFT") {
				t.Errorf("drift result = code %d output %q, want 2/DRIFT", code, out.String())
			}
		})
	}
}

func TestSchemaDriftWorkflow_MainOnlyUsesPublicSchemaAndStrictMode(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile("../../.github/workflows/schema-drift.yml")
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"workflow_dispatch:",
		"permissions:\n  contents: read",
		"github.ref == 'refs/heads/main'",
		"name: schema-drift",
		"go run ./cmd/zcp schema check --strict",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("schema drift workflow missing %q:\n%s", want, s)
		}
	}
	for _, forbidden := range []string{
		"pull_request:",
		"continue-on-error",
		"ZCP_API_KEY",
		"ZEROPS_SCHEMA_CHECK_TOKEN",
		"secrets.",
		"environment:",
		"Require schema-check credential",
	} {
		if strings.Contains(s, forbidden) {
			t.Errorf("public schema drift workflow contains forbidden %q:\n%s", forbidden, s)
		}
	}
}
