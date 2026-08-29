package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/schema"
)

func TestSchemaCheck_StrictMissingAuthentication_ReturnsFailure(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(true, &out,
		func(string) (schema.ActiveVersionsProvider, error) { return nil, errors.New("no authentication") },
		func(string, schema.ActiveVersionsProvider) (schema.DriftReport, error) {
			t.Fatal("checker must not run without authentication")
			return schema.DriftReport{}, nil
		},
	)
	if code != 1 {
		t.Errorf("strict missing-auth exit = %d, want 1", code)
	}
	if !strings.Contains(out.String(), "ERROR") || strings.Contains(out.String(), "SKIP") {
		t.Errorf("strict missing-auth output = %q, want conclusive ERROR without SKIP", out.String())
	}
}

func testActiveVersionsProvider(context.Context) ([]string, error) {
	return []string{"local-storage:single@1"}, nil
}

func TestSchemaCheck_StrictUpstreamFailure_ReturnsFailure(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(true, &out,
		func(string) (schema.ActiveVersionsProvider, error) { return testActiveVersionsProvider, nil },
		func(string, schema.ActiveVersionsProvider) (schema.DriftReport, error) {
			return schema.DriftReport{}, errors.New("upstream unavailable")
		},
	)
	if code != 1 || !strings.Contains(out.String(), "ERROR") || strings.Contains(out.String(), "SKIP") {
		t.Errorf("strict upstream result = code %d output %q, want 1/ERROR", code, out.String())
	}
}

func TestSchemaCheck_NonStrictMissingAuthentication_RemainsSkipSuccess(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	code := runSchemaCheckWith(false, &out,
		func(string) (schema.ActiveVersionsProvider, error) { return nil, errors.New("no authentication") },
		func(string, schema.ActiveVersionsProvider) (schema.DriftReport, error) {
			t.Fatal("checker must not run without authentication")
			return schema.DriftReport{}, nil
		},
	)
	if code != 0 || !strings.Contains(out.String(), "SKIP") {
		t.Errorf("non-strict missing-auth result = code %d output %q, want 0/SKIP", code, out.String())
	}
}

func TestSchemaCheck_Drift_ReturnsTwoInBothModes(t *testing.T) {
	t.Parallel()
	for _, strict := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-strict", true: "strict"}[strict], func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			code := runSchemaCheckWith(strict, &out,
				func(string) (schema.ActiveVersionsProvider, error) { return testActiveVersionsProvider, nil },
				func(string, schema.ActiveVersionsProvider) (schema.DriftReport, error) {
					return schema.DriftReport{ImportDrift: true}, nil
				},
			)
			if code != 2 || !strings.Contains(out.String(), "DRIFT") {
				t.Errorf("drift result = code %d output %q, want 2/DRIFT", code, out.String())
			}
		})
	}
}

func TestSchemaDriftWorkflow_MainOnlyUsesEnvironmentSecretAndStrictMode(t *testing.T) {
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
		"deployment: false",
		"ZCP_API_KEY: ${{ secrets.ZEROPS_SCHEMA_CHECK_TOKEN }}",
		`test -n "$ZCP_API_KEY"`,
		"go run ./cmd/zcp schema check --strict",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("schema drift workflow missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "pull_request:") || strings.Contains(s, "continue-on-error") {
		t.Errorf("secret-bearing strict workflow must not run on/soft-fail pull requests:\n%s", s)
	}
}
