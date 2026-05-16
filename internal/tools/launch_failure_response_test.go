// Tests for: launch_failure_response.go — F5 fix verification.
//
// Reproduces the Karel failure shape (session 3238877f, 2026-05-13):
// CreateAndImportProject returned an apiError.Error whose top-level
// Message was the placeholder "See metadata"; the launch handler
// collapsed it through err.Error() and surfaced "CreateAndImportProject
// failed: See metadata" with category=auth. Agent and user had no
// actionable detail and entered a token-regeneration loop.
//
// Post-F5: blocker carries pe.Suggestion (expanded by
// formatAPIMetaActionable in zerops_errors.go) plus pe.APICode and
// pe.APIMeta; category is derived from pe.Code, not hardcoded auth.
package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestLaunchFailedFromPlatformError_PreservesStructuredDetail
// reproduces the Karel mutation-failure surface and asserts the
// post-F5 response carries the structured detail instead of
// collapsing to "See metadata".
func TestLaunchFailedFromPlatformError_PreservesStructuredDetail(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:       platform.ErrAPIError,
		Message:    "See metadata",
		Suggestion: "Rejected fields: 'project.name' (already exists). Fix in YAML and retry.",
		APICode:    "projectImportInvalidParameter",
		APIMeta: []platform.APIMetaItem{
			{
				Code:  "projectImportInvalidParameter",
				Error: "Invalid parameter provided.",
				Metadata: map[string][]string{
					"project.name": {"already exists"},
				},
			},
		},
	}

	result := launchFailedFromPlatformError(nil, pe,
		topology.BlockerCategoryOther,
		"create-import-failed",
		"CreateAndImportProject failed: %v")

	body := extractText(result)
	resp := decodeLaunchResp(t, []byte(body))

	if resp.Status != "failed" {
		t.Fatalf("status: got %q want failed", resp.Status)
	}
	if len(resp.Blockers) != 1 {
		t.Fatalf("expected 1 blocker, got %d", len(resp.Blockers))
	}
	b := resp.Blockers[0]

	if b.ID != "create-import-failed" {
		t.Errorf("blocker ID: got %q want create-import-failed", b.ID)
	}
	if b.Suggestion == "" {
		t.Error("blocker Suggestion must surface pe.Suggestion, got empty")
	}
	if !strings.Contains(b.Suggestion, "already exists") {
		t.Errorf("blocker Suggestion missing field-rejection detail: %q", b.Suggestion)
	}
	if b.APICode != "projectImportInvalidParameter" {
		t.Errorf("blocker APICode: got %q want projectImportInvalidParameter", b.APICode)
	}
	if len(b.APIMeta) != 1 {
		t.Fatalf("blocker APIMeta: got %d items want 1", len(b.APIMeta))
	}
	if b.APIMeta[0].Metadata["project.name"][0] != "already exists" {
		t.Errorf("APIMeta metadata content not preserved: %+v", b.APIMeta[0].Metadata)
	}
	// Category derivation: ErrAPIError without a more specific mapping
	// falls back to caller's fallbackCategory (BlockerCategoryOther here).
	if b.Category != topology.BlockerCategoryOther {
		t.Errorf("blocker Category: got %q want other (no specific code mapping)", b.Category)
	}
	// Message must NOT be the bare "See metadata" placeholder.
	if b.Message == "CreateAndImportProject failed: See metadata" {
		t.Errorf("blocker Message collapsed to placeholder (F5 regression): %q", b.Message)
	}
	// APICode should appear in the composed message so the agent has a
	// type-discriminator even when the platform Message is generic.
	if !strings.Contains(b.Message, "projectImportInvalidParameter") {
		t.Errorf("blocker Message missing APICode tag: %q", b.Message)
	}
}

// TestLaunchFailedFromPlatformError_DerivedCategoryAuthFromTokenExpired
// pins the category-mapping table — auth-class platform codes flip
// the BlockerCategory to Auth regardless of fallbackCategory.
func TestLaunchFailedFromPlatformError_DerivedCategoryAuthFromTokenExpired(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:    platform.ErrAuthTokenExpired,
		Message: "Authentication required",
	}
	result := launchFailedFromPlatformError(nil, pe,
		topology.BlockerCategoryOther, // fallback — should be overridden
		"launch-key-invalid",
		"ProjectAdminClient construction failed: %v")
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Blockers[0].Category != topology.BlockerCategoryAuth {
		t.Errorf("category: got %q want auth (derived from ErrAuthTokenExpired)", resp.Blockers[0].Category)
	}
}

// TestLaunchFailedFromPlatformError_DerivedCategorySchemaFromImportYml
// pins ErrInvalidImportYml → Schema mapping so schema-validation
// failures don't masquerade as auth/other.
func TestLaunchFailedFromPlatformError_DerivedCategorySchemaFromImportYml(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:    platform.ErrInvalidImportYml,
		Message: "yaml validation failed",
	}
	result := launchFailedFromPlatformError(nil, pe,
		topology.BlockerCategoryOther,
		"create-import-failed",
		"CreateAndImportProject failed: %v")
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Blockers[0].Category != topology.BlockerCategorySchema {
		t.Errorf("category: got %q want schema (derived from ErrInvalidImportYml)", resp.Blockers[0].Category)
	}
}

// TestLaunchFailedFromPlatformError_FallbackForUntypedError pins the
// degraded-path: when err is NOT a *PlatformError, the response still
// emits a valid failed blocker using the fallback category + verbatim
// err.Error() in the message. No structured fields, no crash.
func TestLaunchFailedFromPlatformError_FallbackForUntypedError(t *testing.T) {
	t.Parallel()

	err := errors.New("unexpected wire-level failure")
	result := launchFailedFromPlatformError(nil, err,
		topology.BlockerCategoryOther,
		"create-import-failed",
		"CreateAndImportProject failed: %v")
	resp := decodeLaunchResp(t, []byte(extractText(result)))
	if resp.Status != "failed" {
		t.Fatalf("status: got %q want failed", resp.Status)
	}
	if !strings.Contains(resp.Blockers[0].Message, "unexpected wire-level failure") {
		t.Errorf("untyped err.Error() must appear in message: %q", resp.Blockers[0].Message)
	}
	if resp.Blockers[0].APICode != "" || len(resp.Blockers[0].APIMeta) != 0 {
		t.Errorf("non-platform error must not synthesize APICode/APIMeta")
	}
}

// TestFormatPlatformErrorForAudit_CapturesStructuredDetail pins the
// audit/state-file forensic string. Karel's pre-F5 state.LastError
// was the bare "See metadata" — useless for post-hoc recovery. Now
// the audit captures Message + APICode + per-field rejections in one
// joinable line.
func TestFormatPlatformErrorForAudit_CapturesStructuredDetail(t *testing.T) {
	t.Parallel()

	pe := &platform.PlatformError{
		Code:       platform.ErrAPIError,
		Message:    "See metadata",
		Suggestion: "Rejected fields: 'project.name' (already exists).",
		APICode:    "projectImportInvalidParameter",
		APIMeta: []platform.APIMetaItem{{
			Code:  "projectImportInvalidParameter",
			Error: "Invalid parameter provided.",
			Metadata: map[string][]string{
				"project.name": {"already exists"},
			},
		}},
	}
	out := formatPlatformErrorForAudit(pe)
	for _, want := range []string{"See metadata", "projectImportInvalidParameter", "project.name", "already exists"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit string missing %q: %q", want, out)
		}
	}
}

// TestFormatPlatformErrorForAudit_NilErrorReturnsEmpty pins the
// nil-guard so callers can write straight into state.LastError without
// nil-checking themselves.
func TestFormatPlatformErrorForAudit_NilErrorReturnsEmpty(t *testing.T) {
	t.Parallel()
	if got := formatPlatformErrorForAudit(nil); got != "" {
		t.Errorf("nil err: got %q want empty", got)
	}
}

// TestFormatPlatformErrorForAudit_GenericErrorVerbatim pins the
// fallback path: non-platform errors flow through as err.Error().
func TestFormatPlatformErrorForAudit_GenericErrorVerbatim(t *testing.T) {
	t.Parallel()
	err := errors.New("network timeout after 30s")
	if got := formatPlatformErrorForAudit(err); got != "network timeout after 30s" {
		t.Errorf("generic err: got %q want verbatim", got)
	}
}

// TestLaunchOrphanProjectResponse_SurfacesPerServiceImportErrors pins
// the surface that pre-F5 forced agents to read state files (which
// they can't): when CreateAndImportProject succeeds at the project
// level but one or more services reject, the response carries every
// failing service's ImportError inline so the agent sees what
// rejected and why.
func TestLaunchOrphanProjectResponse_SurfacesPerServiceImportErrors(t *testing.T) {
	t.Parallel()

	state := &launchState{
		LaunchID:          "abc",
		SourceProjectID:   "src",
		TargetProjectName: "myapp-prod",
		ImportedServices: []importedServiceEntry{
			{ID: "svc-app", Name: "app"},
			{ID: "svc-db", Name: "db", ImportError: "projectImportMissingParameter: Mandatory parameter is missing"},
			{ID: "svc-redis", Name: "redis", ImportError: "yamlInvalidReference: unknown ${db_hostname} reference"},
		},
	}
	result := launchOrphanProjectResponse(state, "target-prj-id")
	body := extractText(result)
	resp := decodeLaunchResp(t, []byte(body))

	if resp.Status != "failed" {
		t.Fatalf("status: got %q want failed", resp.Status)
	}
	// Failing service names must appear in the human-readable message.
	if !strings.Contains(resp.Guidance, "db") || !strings.Contains(resp.Guidance, "redis") {
		t.Errorf("guidance must name failing services: %q", resp.Guidance)
	}
	// Inline ImportedServices must include the per-service ImportError
	// strings so the agent has actionable detail without state-file IO.
	if len(resp.ImportedServices) != 3 {
		t.Fatalf("expected 3 imported services in response, got %d", len(resp.ImportedServices))
	}
	foundDBError := false
	for _, svc := range resp.ImportedServices {
		if svc.Name == "db" && strings.Contains(svc.ImportError, "Mandatory parameter is missing") {
			foundDBError = true
			break
		}
	}
	if !foundDBError {
		t.Errorf("db service ImportError not preserved in response: %+v", resp.ImportedServices)
	}
}

// TestLaunchFirstDeployFailedResponse_EmbedsRetryGuidance pins the
// surface promoted from plans/backlog/launch-first-deploy-failed-recovery-hint.md:
// when the target project is created and services import successfully
// but the first-deploy poll hits a FAILED terminal process, the response
// embeds the explicit retry-via-push command, the dashboard URL, and
// the imported-services list inline so the agent has actionable next
// steps without needing a structured Recovery pointing at a cross-
// project tool that doesn't exist.
func TestLaunchFirstDeployFailedResponse_EmbedsRetryGuidance(t *testing.T) {
	t.Parallel()

	state := &launchState{
		LaunchID:          "lnchABC",
		SourceProjectID:   "src-proj",
		TargetProjectName: "myapp-prod",
		LastError:         "service appdev: process pid-123 terminal status FAILED (FAILED)",
		ImportedServices: []importedServiceEntry{
			{ID: "svc-appdev", Name: "appdev"},
			{ID: "svc-db", Name: "db"},
		},
	}
	result := launchFirstDeployFailedResponse(state, "tgt-prj-id")
	body := extractText(result)
	resp := decodeLaunchResp(t, []byte(body))

	if resp.Status != "failed" {
		t.Fatalf("status: got %q want failed", resp.Status)
	}
	for _, want := range []string{
		"first deploy did not complete cleanly",
		"git -C <source-repo> commit --allow-empty",
		"git push origin <main-branch>",
		"https://app.zerops.io/project/tgt-prj-id",
		"delete the target project",
		"importedServices",
		"service appdev: process pid-123 terminal status FAILED",
	} {
		if !strings.Contains(resp.Guidance, want) {
			t.Errorf("guidance missing %q\nfull:\n%s", want, resp.Guidance)
		}
	}
	// Imported services must land inline so agent maps LastError →
	// per-service record without state-file IO.
	if len(resp.ImportedServices) != 2 {
		t.Fatalf("expected 2 imported services in response, got %d", len(resp.ImportedServices))
	}
	// Blocker id pins for downstream consumers (audit log readers,
	// recovery atom matchers).
	if len(resp.Blockers) != 1 || resp.Blockers[0].ID != "first-deploy-failed" {
		t.Fatalf("expected single first-deploy-failed blocker, got %+v", resp.Blockers)
	}
	// ProductionProjectID must be top-level — scenario #9 retro flagged
	// the deep-link-only carrier as a cleanup blocker.
	if resp.ProductionProjectID != "tgt-prj-id" {
		t.Errorf("ProductionProjectID: got %q want %q", resp.ProductionProjectID, "tgt-prj-id")
	}
}

// TestLaunchLaunchedResponse_SurfacesProductionProjectID pins the
// top-level ProductionProjectID on the happy-path launched response.
// Scenario #9 retro: agent had to regex the project ID out of a
// blockers[].message deep-link URL because there was no top-level
// field — cleanup flow burned an extra call resolving the ID.
func TestLaunchLaunchedResponse_SurfacesProductionProjectID(t *testing.T) {
	t.Parallel()

	state := &launchState{
		LaunchID:          "lnchXYZ",
		SourceProjectID:   "src-proj",
		TargetProjectID:   "prod-prj-id-42",
		TargetProjectName: "myapp-prod",
		Status:            topology.LaunchStatusLaunched,
	}
	result := launchLaunchedResponse(nil, state)
	body := extractText(result)
	resp := decodeLaunchResp(t, []byte(body))

	if resp.Status != "launched" {
		t.Fatalf("status: got %q want launched", resp.Status)
	}
	if resp.ProductionProjectID != "prod-prj-id-42" {
		t.Errorf("ProductionProjectID: got %q want %q", resp.ProductionProjectID, "prod-prj-id-42")
	}
}
