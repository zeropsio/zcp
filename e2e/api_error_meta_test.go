//go:build e2e

// Tests for: plans/api-validation-plumbing.md — end-to-end verification
// that structured apiMeta from the live Zerops API reaches the LLM-
// facing MCP response / PlatformError surface. Before this plan, every
// 4xx error collapsed to "Invalid parameter provided." with no field.
// These tests hit the real API and assert field-level detail survives
// the whole mapAPIError → PlatformError → convertError → MCP JSON chain.
//
// Run: go test ./e2e/ -tags e2e -run TestE2E_APIErrorMeta -v -timeout 120s

package e2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// apiMetaHarness builds a client wired to the live API with the test
// project resolved via auth.Resolve (same path newLocalHarness uses).
type apiMetaHarness struct {
	client    *platform.ZeropsClient
	projectID string
}

func newAPIMetaHarness(t *testing.T) *apiMetaHarness {
	t.Helper()
	token := os.Getenv("ZCP_API_KEY")
	if token == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}
	apiHost := os.Getenv("ZCP_API_HOST")
	if apiHost == "" {
		apiHost = "api.app-prg1.zerops.io"
	}
	client, err := platform.NewZeropsClient(token, apiHost)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := auth.Resolve(ctx, client)
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}
	return &apiMetaHarness{client: client, projectID: info.ProjectID}
}

// TestE2E_APIErrorMeta_ImportReturnsFieldDetail probes the commit-level
// import endpoint with YAMLs that trigger each error shape observed in
// plans/api-validation-plumbing.md §1.2. Every case must produce a
// *PlatformError with APIMeta populated and the expected field path in
// the decoded metadata. No services are created — the API rejects all
// cases at schema validation before any state changes.
func TestE2E_APIErrorMeta_ImportReturnsFieldDetail(t *testing.T) {
	h := newAPIMetaHarness(t)
	host := func(suffix string) string {
		return "metaprobe" + suffix
	}

	cases := []struct {
		name        string
		yaml        string
		wantAPICode string
		wantField   string // a key that must appear in meta.metadata
	}{
		{
			name: "object_storage_mode_rejected",
			yaml: "services:\n  - hostname: " + host("a") +
				"\n    type: object-storage\n    mode: NON_HA\n    objectStorageSize: 1\n    objectStoragePolicy: private\n",
			wantAPICode: "projectImportInvalidParameter",
			wantField:   host("a") + ".mode",
		},
		{
			name: "postgres_missing_mode",
			yaml: "services:\n  - hostname: " + host("b") + "\n    type: postgresql@18\n",
			wantAPICode: "projectImportMissingParameter",
			wantField:   "parameter",
		},
		{
			name: "unknown_type_version",
			yaml: "services:\n  - hostname: " + host("c") +
				"\n    type: nodejs@99\n    mode: NON_HA\n",
			wantAPICode: "serviceStackTypeNotFound",
			wantField:   "serviceStackTypeVersion",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.client.ImportServices(ctx, h.projectID, tc.yaml)
			if err == nil {
				t.Fatalf("expected API rejection, import succeeded (would need cleanup)")
			}
			pe, ok := err.(*platform.PlatformError)
			if !ok {
				t.Fatalf("expected *PlatformError, got %T: %v", err, err)
			}
			if pe.APICode != tc.wantAPICode {
				t.Errorf("APICode = %q, want %q", pe.APICode, tc.wantAPICode)
			}
			if len(pe.APIMeta) == 0 {
				t.Fatalf("APIMeta empty — F#7 regression (server sent meta, ZCP dropped it)")
			}
			found := false
			for _, item := range pe.APIMeta {
				if _, ok := item.Metadata[tc.wantField]; ok {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("field %q absent from apiMeta metadata: %+v", tc.wantField, pe.APIMeta)
			}
			// Suggestion should point the LLM at apiMeta when meta is populated
			// (regression guard on W1's suggestion rewrite).
			if !strings.Contains(strings.ToLower(pe.Suggestion), "apimeta") {
				t.Errorf("Suggestion = %q, want it to mention apiMeta", pe.Suggestion)
			}
		})
	}
}

// TestE2E_APIErrorMeta_ValidateZeropsYaml probes the pre-deploy yaml
// validator with a known-bad runtime version. Structured meta must
// come back with build.base / run.base entries; ErrInvalidZeropsYml
// must be the final Code so deploy handlers can distinguish user-
// fixable YAML errors from transport / 5xx failures.
func TestE2E_APIErrorMeta_ValidateZeropsYaml(t *testing.T) {
	h := newAPIMetaHarness(t)

	// Resolve a real stack-type ID from any existing nodejs service in
	// the project. The validator endpoint empirically tolerates a
	// non-existent id when the YAML itself is malformed enough to
	// reject, but a real id exercises the same code path the deploy
	// handlers take in production.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	services, err := h.client.ListServices(ctx, h.projectID)
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	var typeID string
	for _, s := range services {
		if strings.HasPrefix(s.ServiceStackTypeInfo.ServiceStackTypeVersionName, "nodejs@") {
			typeID = s.ServiceStackTypeInfo.ServiceStackTypeID
			break
		}
	}
	if typeID == "" {
		// Use any service's type ID — the endpoint doesn't strictly
		// validate the id against the version, and we're testing error
		// plumbing not catalog matching.
		for _, s := range services {
			if tid := s.ServiceStackTypeInfo.ServiceStackTypeID; tid != "" {
				typeID = tid
				break
			}
		}
	}
	if typeID == "" {
		t.Skip("no existing services in project — cannot resolve stack type id")
	}

	badYaml := `zerops:
  - setup: probe
    build:
      base: nodejs@99
      buildCommands:
        - echo ok
      deployFiles: ./
    run:
      base: nodejs@99
      start: node server.js
`
	err = h.client.ValidateZeropsYaml(ctx, platform.ValidateZeropsYamlInput{
		ServiceStackTypeID:      typeID,
		ServiceStackTypeVersion: "nodejs@22",
		ServiceStackName:        "probe",
		ZeropsYaml:              badYaml,
		Operation:               platform.ValidationOperationBuildAndDeploy,
	})
	if err == nil {
		t.Fatal("expected validation error, got nil — API catalog diverged?")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidZeropsYml {
		t.Errorf("Code = %q, want %q (deploy handlers distinguish by Code)", pe.Code, platform.ErrInvalidZeropsYml)
	}
	if len(pe.APIMeta) == 0 {
		t.Fatalf("APIMeta empty on known-bad yaml — validator wiring broken")
	}
	// The bad-base metadata should name one of build.base / run.base.
	var foundField string
	for _, item := range pe.APIMeta {
		for field := range item.Metadata {
			if strings.Contains(field, ".base") {
				foundField = field
				break
			}
		}
		if foundField != "" {
			break
		}
	}
	if foundField == "" {
		t.Errorf("no .base field in apiMeta for bad runtime version: %+v", pe.APIMeta)
	}
}

// TestE2E_APIErrorMeta_TransportFailure_NotReclassified proves the
// reclassification in ValidateZeropsYaml does NOT turn transport failures
// into ErrInvalidZeropsYml. Deploy handlers distinguish "your YAML is
// wrong" from "Zerops is unreachable" by Code; mis-classifying a network
// error as a YAML error would mislead the LLM into editing a fine YAML.
//
// The zerops-yaml-validation endpoint is public (security: [] in the
// OpenAPI spec), so bogus tokens alone don't produce auth errors —
// we force a DNS-level failure by pointing at a non-existent host.
func TestE2E_APIErrorMeta_TransportFailure_NotReclassified(t *testing.T) {
	if os.Getenv("ZCP_API_KEY") == "" {
		t.Skip("ZCP_API_KEY not set — skipping E2E test")
	}
	bad, err := platform.NewZeropsClient("any-token", "nonexistent-host-for-e2e.invalid")
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = bad.ValidateZeropsYaml(ctx, platform.ValidateZeropsYamlInput{
		ServiceStackTypeID:      "placeholder-id",
		ServiceStackTypeVersion: "nodejs@22",
		ServiceStackName:        "x",
		ZeropsYaml:              "zerops:\n  - setup: x\n",
		Operation:               platform.ValidationOperationBuildAndDeploy,
	})
	if err == nil {
		t.Fatal("expected transport failure, got nil (DNS resolved?)")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code == platform.ErrInvalidZeropsYml {
		t.Errorf("transport error mis-classified as ErrInvalidZeropsYml: %+v", pe)
	}
	// Transport failures should be NETWORK_ERROR / API_ERROR / API_TIMEOUT,
	// not anything zerops-yaml-specific.
	if pe.Code != platform.ErrNetworkError && pe.Code != platform.ErrAPIError && pe.Code != platform.ErrAPITimeout {
		t.Logf("transport error code = %q — not in the expected set but still not ErrInvalidZeropsYml", pe.Code)
	}
}
