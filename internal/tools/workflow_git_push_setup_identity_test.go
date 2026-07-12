// Tests for: workflow_git_push_setup.go — F3 human attribution
// (gitPushSetupDeriveAndSeedIdentity, gitPushIdentityMigrationNote).
package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// stubGitHubUserHTTP is a minimal ops.HTTPDoer returning a fixed
// status/body for every request — the same seam production wires to a
// real *http.Client, here standing in for GitHub's /user endpoint.
// callCount lets non-github-host tests assert derivation was never
// attempted at all.
type stubGitHubUserHTTP struct {
	status    int
	body      string
	callCount int
}

func (s *stubGitHubUserHTTP) Do(req *http.Request) (*http.Response, error) {
	s.callCount++
	// A real *http.Client populates resp.Request with the final (post-
	// redirect) request; DeriveGitHubIdentity's redirect-host hardening
	// checks that field, so the stub must mirror it — same-host here,
	// since these integration tests aren't exercising the redirect case
	// itself (that's covered at the ops-level unit tests).
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader(s.body)), Request: req}, nil
}

const octocatUserJSON = `{"login":"octocat","id":583231,"email":"octocat@github.com"}`

// writeFirstTimeConfigMeta seeds a bootstrapped, NOT-yet-git-push-configured
// pair — the shape a first-time confirm call needs.
func writeFirstTimeConfigMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestGitPushSetupContainer_DerivesAndSeedsIdentity_Absent pins F3 item 2's
// happy path: a github.com remote + valid PAT derives the human identity
// and seeds it (dispatch says both keys SEEDED — absent or exact-robot,
// the ops-level behavioral tests already pin which), surfacing it in the
// response as identityAttributed. No preserved note, no warning.
func TestGitPushSetupContainer_DerivesAndSeedsIdentity_Absent(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	// jsonResult's encoder HTML-escapes angle brackets, so match the
	// identity pieces rather than the literal "name <email>" form.
	if !strings.Contains(body, `"identityAttributed"`) || !strings.Contains(body, "octocat") || !strings.Contains(body, "octocat@github.com") {
		t.Errorf("response should report the derived+seeded identity; got: %s", body)
	}
	if strings.Contains(body, "identityPreservedNote") {
		t.Errorf("no preserved note expected when identity was seeded; got: %s", body)
	}
	if strings.Contains(body, "identityWarning") {
		t.Errorf("no warning expected on successful derivation; got: %s", body)
	}
	if httpDoer.callCount != 1 {
		t.Errorf("expected exactly 1 GitHub API call, got %d", httpDoer.callCount)
	}
	// The seed command actually carries the DERIVED identity, not the robot default.
	var seedCmd string
	for _, c := range ssh.commands {
		if strings.Contains(c, "cur_email=$(git config user.email)") {
			seedCmd = c
			break
		}
	}
	if seedCmd == "" {
		t.Fatalf("seed command never issued; commands: %v", ssh.commands)
	}
	if !strings.Contains(seedCmd, "git config user.email 'octocat@github.com'") || !strings.Contains(seedCmd, "git config user.name 'octocat'") {
		t.Errorf("seed command must carry the derived identity: %s", seedCmd)
	}
}

// TestGitPushSetupContainer_DerivesIdentity_CustomPreserved pins F3 item 2's
// preserve path: when the seed command's dispatch reports both keys
// PRESERVED (a genuinely custom identity already there), the response
// surfaces a visible identityPreservedNote — never silent — and does NOT
// claim identityAttributed.
func TestGitPushSetupContainer_DerivesIdentity_CustomPreserved(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_PRESERVED\nZCP_NAME_PRESERVED\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "identityPreservedNote") {
		t.Errorf("custom identity must surface a visible preserved note, never silently; got: %s", body)
	}
	if !strings.Contains(body, "octocat") {
		t.Errorf("preserved note should name the derived identity that was NOT applied; got: %s", body)
	}
	if strings.Contains(body, `"identityAttributed"`) {
		t.Errorf("must not claim identityAttributed when the custom identity was preserved; got: %s", body)
	}
}

// TestGitPushSetupContainer_DerivesIdentity_MixedOutcome is the Codex
// diff-review finding 2 pin at the integration level: email seeded, name
// preserved (both legitimate, non-error outcomes) must be reported
// per-key — the response carries BOTH identityAttributed (naming that
// only email was attributed) AND identityPreservedNote (naming that only
// name was preserved), never collapsed into a single misleading claim.
func TestGitPushSetupContainer_DerivesIdentity_MixedOutcome(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_PRESERVED\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, `"identityAttributed"`) {
		t.Errorf("mixed outcome must still report the email attribution; got: %s", body)
	}
	if !strings.Contains(body, "email only") {
		t.Errorf("attribution must name that only email was seeded (mixed, not collapsed); got: %s", body)
	}
	if !strings.Contains(body, "identityPreservedNote") {
		t.Errorf("mixed outcome must still report the name preservation; got: %s", body)
	}
	if strings.Contains(body, "identityWarning") {
		t.Errorf("a clean mixed outcome (seeded + preserved) is not an anomaly and must not carry a warning; got: %s", body)
	}
}

// TestGitPushSetupContainer_SeedWriteFailure_SurfacesWarning_NeverPreserved
// is the Codex diff-review finding 2 anomaly pin: a WRITE_FAILED token
// must produce a warning, never be silently misread as "preserved".
func TestGitPushSetupContainer_SeedWriteFailure_SurfacesWarning_NeverPreserved(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_WRITE_FAILED\nZCP_NAME_SEEDED\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("a seed write failure must be non-blocking, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "identityWarning") {
		t.Errorf("a write-failure token must surface a warning; got: %s", body)
	}
	if strings.Contains(body, "identityPreservedNote") {
		t.Errorf("a write failure must NEVER be misreported as preserved; got: %s", body)
	}
	if strings.Contains(body, `"identityAttributed"`) {
		t.Errorf("a write failure on one key must NOT claim attribution for the other; got: %s", body)
	}
}

// TestGitPushSetupContainer_SeedMalformedOutput_SurfacesWarning is the
// Codex diff-review finding 2 anomaly pin for garbled/wrong-shaped
// dispatch output: neither the wrong line count nor an unrecognized token
// may be silently read as "preserved".
func TestGitPushSetupContainer_SeedMalformedOutput_SurfacesWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{"empty output", ""},
		{"single line only", "ZCP_EMAIL_SEEDED\n"},
		{"unrecognized token", "GARBAGE\nZCP_NAME_SEEDED\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			writeFirstTimeConfigMeta(t, stateDir)

			ssh := &containerSSHStub{
				dispatch: func(cmd string) ([]byte, error) {
					if strings.Contains(cmd, "cur_email=$(git config user.email)") {
						return []byte(tt.output), nil
					}
					return []byte("ok"), nil
				},
			}
			httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

			result, _, _ := handleGitPushSetup(
				context.Background(), client, httpDoer, ssh, "test-project",
				WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
				stateDir, runtime.Info{InContainer: true},
			)
			if result.IsError {
				t.Fatalf("malformed seed output must be non-blocking, got error: %s", extractText(result))
			}
			body := extractText(result)
			if !strings.Contains(body, "identityWarning") {
				t.Errorf("malformed dispatch output must surface a warning; got: %s", body)
			}
			if strings.Contains(body, "identityPreservedNote") {
				t.Errorf("malformed output must NEVER be misread as preserved; got: %s", body)
			}
			if strings.Contains(body, `"identityAttributed"`) {
				t.Errorf("malformed output must NEVER be misread as attributed; got: %s", body)
			}
		})
	}
}

// TestGitPushSetupContainer_NonGitHubHost_SkipsDerivation pins F3 item 1's
// host gate: a gitlab.com remote must never attempt GitHub derivation at
// all (the API call is GitHub-specific and would 404/misbehave against
// another host) — robot fallback stands, no identity fields in the
// response, and setup still succeeds normally.
func TestGitPushSetupContainer_NonGitHubHost_SkipsDerivation(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://gitlab.com/example/app.git", GitToken: "glpat_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	if httpDoer.callCount != 0 {
		t.Errorf("GitHub API must never be called for a non-github remote; got %d calls", httpDoer.callCount)
	}
	body := extractText(result)
	for _, forbidden := range []string{"identityAttributed", "identityPreservedNote", "identityWarning"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("non-github host must carry no identity fields at all; found %q in: %s", forbidden, body)
		}
	}
	// No seed command should have been issued either.
	for _, c := range ssh.commands {
		if strings.Contains(c, "cur_email=$(git config user.email)") {
			t.Errorf("seed command must not run for a non-github host: %s", c)
		}
	}
}

// TestGitPushSetupContainer_DerivationFails_NonBlockingWarning pins F3's
// core non-blocking contract: a GitHub API failure (or a nil httpClient)
// must NOT fail git-push-setup — it falls back to the robot identity and
// surfaces a warning, but the pair still ends up configured.
func TestGitPushSetupContainer_DerivationFails_NonBlockingWarning(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{}
	httpDoer := &stubGitHubUserHTTP{status: 401, body: `{"message":"Bad credentials"}`}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("derivation failure must be non-blocking — setup should still succeed, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, `"status":"configured"`) {
		t.Errorf("pair must still end up configured despite the derivation failure; got: %s", body)
	}
	if !strings.Contains(body, "identityWarning") {
		t.Errorf("derivation failure must surface a non-blocking warning, not fail silently; got: %s", body)
	}

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if meta == nil || meta.GitPushState != topology.GitPushConfigured {
		t.Errorf("meta must still be stamped configured despite the derivation failure; got %+v", meta)
	}
}

// TestGitPushSetupContainer_DerivationFailureBody_NeverLeaksIntoWarning is
// the end-to-end Codex diff-review finding 1b pin: a sentinel embedded in
// GitHub's non-200 response body must never surface in the agent-facing
// identityWarning field — DeriveGitHubIdentity sanitizes it at the source,
// but this test proves the full pipeline (API response → ops error →
// tools warning text → JSON response) never re-introduces it.
func TestGitPushSetupContainer_DerivationFailureBody_NeverLeaksIntoWarning(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)
	const sentinel = "SENSITIVE_SENTINEL_MUST_NOT_LEAK"

	ssh := &containerSSHStub{}
	httpDoer := &stubGitHubUserHTTP{status: 403, body: `{"message":"` + sentinel + `"}`}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("derivation failure must be non-blocking, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "identityWarning") {
		t.Fatalf("expected an identityWarning field; got: %s", body)
	}
	if strings.Contains(body, sentinel) {
		t.Errorf("GitHub response body sentinel leaked into the agent-facing response: %s", body)
	}
}

// TestGitPushSetupContainer_NilHTTPClient_NonBlocking pins the same
// non-blocking contract for a nil httpClient specifically (the shape most
// existing call sites — and RegisterWorkflow test fixtures — actually use).
func TestGitPushSetupContainer_NilHTTPClient_NonBlocking(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	writeFirstTimeConfigMeta(t, stateDir)

	ssh := &containerSSHStub{}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_good"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("nil httpClient must be non-blocking — setup should still succeed, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, `"status":"configured"`) {
		t.Errorf("pair must still end up configured with a nil httpClient; got: %s", body)
	}
	if !strings.Contains(body, "identityWarning") {
		t.Errorf("nil httpClient must surface a warning, not fail silently; got: %s", body)
	}
}

// TestGitPushSetupContainer_ReconstructUsesDerivedIdentity is the F3 item 3
// pin: a rotation-with-token confirm on a configured pair whose /var/www/.git
// vanished must reconstruct using the DERIVED identity (human-attributed
// from the first init), not the robot fallback — when derivation succeeds.
func TestGitPushSetupContainer_ReconstructUsesDerivedIdentity(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("absent"), nil
			}
			if strings.Contains(cmd, "git status --porcelain") {
				return []byte(""), nil
			}
			return []byte("ok"), nil
		},
	}
	httpDoer := &stubGitHubUserHTTP{status: 200, body: octocatUserJSON}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	result, _, _ := handleGitPushSetup(
		context.Background(), client, httpDoer, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app.git", GitToken: "ghp_rotated_token"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	var recon string
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "if test ! -d .git") {
			recon = cmd
			break
		}
	}
	if recon == "" {
		t.Fatalf("reconstruction command never issued; commands: %v", ssh.commands)
	}
	if !strings.Contains(recon, "git config user.email 'octocat@github.com'") || !strings.Contains(recon, "git config user.name 'octocat'") {
		t.Errorf("reconstruction must fill the DERIVED identity, not the robot default: %s", recon)
	}
	if strings.Contains(recon, "agent@zerops.io") || strings.Contains(recon, "Zerops Agent") {
		t.Errorf("reconstruction with a successful derivation must not reference the robot identity: %s", recon)
	}
	body := extractText(result)
	if !strings.Contains(body, `"identityAttributed"`) || !strings.Contains(body, "octocat") || !strings.Contains(body, "octocat@github.com") {
		t.Errorf("response should report the derived identity used for reconstruction; got: %s", body)
	}
}

// TestGitPushSetupContainer_RecallNote_StillRobot pins F3 item 4's positive
// case: a tokenless recall on a configured pair whose identity EXACTLY
// equals the robot default (and whose remote is github.com) must surface
// a migration note prompting a one-time gitToken re-run.
func TestGitPushSetupContainer_RecallNote_StillRobot(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("present"), nil
			}
			if strings.Contains(cmd, `printf '%s\n' "$(git config user.email)"`) {
				return []byte("agent@zerops.io\nZerops Agent\n"), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	if !strings.Contains(body, "identityMigrationNote") {
		t.Errorf("still-robot identity on a github.com remote must surface the migration note; got: %s", body)
	}
	if !strings.Contains(body, "gitToken=") || !strings.Contains(body, "re-run git-push-setup once") {
		t.Errorf("migration note must instruct a one-time gitToken re-run; got: %s", body)
	}
}

// TestGitPushSetupContainer_RecallNote_AlreadyHuman pins the negative case:
// an identity that is NOT exactly-robot (already migrated, or a
// recipe-baked custom identity) must NOT surface the migration note.
func TestGitPushSetupContainer_RecallNote_AlreadyHuman(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("present"), nil
			}
			if strings.Contains(cmd, `printf '%s\n' "$(git config user.email)"`) {
				return []byte("octocat@github.com\noctocat\n"), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://github.com/example/app"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	if strings.Contains(body, "identityMigrationNote") {
		t.Errorf("an already-human identity must not surface the migration note; got: %s", body)
	}
}

// TestGitPushSetupContainer_RecallNote_NonGitHubHost pins that the
// migration note is gated to github.com — re-running with gitToken would
// derive nothing for any other host, so prompting the same fix there
// would be a false promise.
func TestGitPushSetupContainer_RecallNote_NonGitHubHost(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://gitlab.com/example/app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-05-23",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "test -d /var/www/.git") {
				return []byte("present"), nil
			}
			return []byte("ok"), nil
		},
	}

	result, _, _ := handleGitPushSetup(
		context.Background(), nil, nil, ssh, "test-project",
		WorkflowInput{Service: "appdev", RemoteURL: "https://gitlab.com/example/app"},
		stateDir, runtime.Info{InContainer: true},
	)
	if result.IsError {
		t.Fatalf("expected success, got error: %s", extractText(result))
	}
	body := extractText(result)
	if strings.Contains(body, "identityMigrationNote") {
		t.Errorf("non-github host must never surface the migration note; got: %s", body)
	}
	for _, c := range ssh.commands {
		if strings.Contains(c, `printf '%s\n' "$(git config user.email)"`) {
			t.Errorf("identity read must not run for a non-github host: %s", c)
		}
	}
}

// TestGitPushIdentityPreservedNote_ShellQuoted is the Codex diff-review
// finding 3 pin: a derived name/email containing shell metacharacters (an
// apostrophe, a command-substitution sequence) must never break the
// emitted remediation command's quoting or splice extra shell syntax —
// every value is individually shell-quoted, and the whole remote script
// is then quoted as ONE argument for the emitted `ssh host '<script>'`
// line, rather than hand-spliced with a bare `'%s'` (the earlier bug).
func TestGitPushIdentityPreservedNote_ShellQuoted(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs a real bash binary for the round-trip proof")
	}
	t.Parallel()
	identity := ops.GitIdentity{
		Name:  `O'Brien`,
		Email: `$(rm -rf /)@example.com`,
	}
	note := gitPushIdentityPreservedNote(identity, "appdev", true, true)

	// Reconstruct the expected quoting using the SAME single-owner
	// function the production code calls, rather than hand-predicting the
	// nested-escape byte sequence (quoting a string that itself contains
	// quote-escapes produces a longer, correct-but-non-obvious result —
	// let the code compute it, don't re-derive it by eye). This proves
	// gitPushIdentityPreservedNote actually ROUTES both the individual
	// values and the whole remote script through ops.ShellQuote, instead
	// of hand-splicing a bare `'%s'` (Codex diff-review finding 3's bug).
	wantScript := fmt.Sprintf("cd /var/www && git config user.name %s && git config user.email %s",
		ops.ShellQuote(identity.Name), ops.ShellQuote(identity.Email))
	wantQuotedScript := ops.ShellQuote(wantScript)
	if !strings.Contains(note, wantQuotedScript) {
		t.Errorf("note must embed the doubly-shell-quoted remote script exactly as ops.ShellQuote produces it:\nwant substring: %s\ngot note: %s", wantQuotedScript, note)
	}

	// The OLD vulnerable shape — the raw apostrophe spliced directly next
	// to the surrounding quote with no escape at all — must never appear.
	if strings.Contains(note, `user.name 'O'Brien'`) {
		t.Errorf("note contains the unescaped-apostrophe splice the fix was meant to close: %s", note)
	}

	// Functional proof: round-trip the emitted remediation command
	// through a REAL shell (git replaced by a stub that records its argv;
	// ssh never actually invoked) and confirm both values arrive intact,
	// byte-for-byte — the apostrophe doesn't truncate the name, and the
	// $(...) sequence is NEVER executed as a command substitution.
	idx := strings.Index(note, "ssh appdev '")
	if idx < 0 {
		t.Fatalf("note missing the expected `ssh appdev '<script>'` shape: %s", note)
	}
	shellArg := strings.TrimSuffix(note[idx+len("ssh appdev "):], ".")
	recordFile := filepath.Join(t.TempDir(), "argv.txt")
	// cd is stubbed to a no-op since /var/www doesn't exist on the test
	// runner — only the two `git config` invocations matter here.
	stubScript := fmt.Sprintf(`cd() { :; }; git() { printf '%%s\n' "$*" >> %s; }; eval %s`, ops.ShellQuote(recordFile), shellArg)
	cmd := exec.CommandContext(context.Background(), "bash", "-c", stubScript)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shell round-trip of the emitted remediation command failed: %v\noutput: %s\ncommand: %s", err, out, stubScript)
	}
	recorded, err := os.ReadFile(recordFile)
	if err != nil {
		t.Fatalf("read recorded git invocations: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(recorded), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 git invocations, got %d: %q", len(lines), recorded)
	}
	if lines[0] != "config user.name O'Brien" {
		t.Errorf("git config user.name received %q, want the apostrophe-containing name intact: %q", lines[0], "config user.name O'Brien")
	}
	if lines[1] != "config user.email $(rm -rf /)@example.com" {
		t.Errorf("git config user.email received %q, want the literal $(...) sequence NEVER executed: %q", lines[1], "config user.email $(rm -rf /)@example.com")
	}
}
